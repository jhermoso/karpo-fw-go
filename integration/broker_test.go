package integration

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jhermoso/karpo-fw-go/contexts/hr"
	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	hdomain "github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	hinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	knats "github.com/jhermoso/karpo-fw-go/nats"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/transporttest"
)

// The broker of the integration module (see docker-compose.yml):
//
//	KARPO_NATS_URL   nats://localhost:54222
const envNATS = "KARPO_NATS_URL"

func natsURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv(envNATS)
	if u == "" {
		t.Skipf("%s not set", envNATS)
	}
	return u
}

// runID names what a test run creates on servers that outlive it (streams, databases).
func runID() string { return strings.ReplaceAll(fw.NewUUID().String()[19:], "-", "") }

// dropStreams removes the streams of a transport when the test ends.
func dropStreams(t *testing.T, natsURL string, streams ...string) {
	t.Cleanup(func() {
		conn, err := natsgo.Connect(natsURL)
		if err != nil {
			return
		}
		defer conn.Close()
		js, _ := jetstream.New(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, s := range streams {
			_ = js.DeleteStream(ctx, s)
			_ = js.DeleteStream(ctx, s+"_DEAD")
		}
	})
}

// TestNATSConformance runs the transport conformance suite against the NATS server: delivery,
// type filter, redelivery, duplicates, replicas, order per subject, durability and dead letters.
func TestNATSConformance(t *testing.T) {
	server, run := natsURL(t), runID()
	var mu sync.Mutex
	opened := map[*testing.T]*knats.Transport{}
	transporttest.Run(t, transporttest.Harness{
		New: func(t *testing.T) transporttest.Transport {
			mu.Lock()
			defer mu.Unlock()
			n := len(opened) + 1
			stream := fmt.Sprintf("CONFORMANCE_%s_%d", run, n)
			dropStreams(t, server, stream)
			tr, err := knats.Open(context.Background(), knats.Config{URL: server, Stream: stream, Prefix: fmt.Sprintf("conformance%s%d", run, n),
				MaxDeliveries: transporttest.MaxDeliveries, Backoff: []time.Duration{20 * time.Millisecond}, AckWait: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tr.Close() })
			opened[t] = tr
			return transporttest.Transport{Sender: tr, Subscriber: tr}
		},
		Replicas: true,
		Durable:  true,
		DeadLetters: func(t *testing.T, consumer string) []application.Envelope {
			mu.Lock()
			tr := opened[t]
			mu.Unlock()
			dead, err := tr.DeadLetters(context.Background(), consumer)
			if err != nil {
				t.Fatal(err)
			}
			out := make([]application.Envelope, len(dead))
			for i, d := range dead {
				out[i] = d.Envelope
			}
			return out
		},
	})
}

// ---------------------------------------------------------------------------------------------
// Two services in separate processes
// ---------------------------------------------------------------------------------------------

// Environment of a service replica started by TestTwoProcesses_HRToParties.
const (
	envRole    = "KARPO_E2E_ROLE" // "hr" or "parties"
	envReplica = "KARPO_E2E_REPLICA"
	envDSN     = "KARPO_E2E_DSN" // the database of the service
	envStream  = "KARPO_E2E_STREAM"
	envPrefix  = "KARPO_E2E_PREFIX"
)

// TestTwoProcesses_HRToParties deploys HR and Parties as microservices: two replicas of each, in
// four processes, each service with a database of its own, and NATS between them. HR
// terminates employments; Parties, which shares nothing with HR but the broker, ends the
// employment relationships. Then one replica of each service is killed and the survivors carry
// on: the other relay drains the outbox and the other consumer takes the messages.
func TestTwoProcesses_HRToParties(t *testing.T) {
	server := natsURL(t)
	pg := os.Getenv("KARPO_PG_DSN")
	if pg == "" {
		t.Skip("KARPO_PG_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	run := runID()
	stream, prefix := "E2E_"+run, "e2e"+run
	dropStreams(t, server, stream)
	hrDSN, partiesDSN := createDatabase(t, pg, "karpo_e2e_hr_"+run), createDatabase(t, pg, "karpo_e2e_parties_"+run)

	// The deployment step: each service gets its schema in its own database.
	hrDB, partiesDB := openPostgres(t, hrDSN), openPostgres(t, partiesDSN)
	if m, err := hinfra.Migrator(hrDB); err != nil {
		t.Fatal(err)
	} else if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if m, err := pinfra.Migrator(partiesDB); err != nil {
		t.Fatal(err)
	} else if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// What a user of Parties did before: Acme and six employees affiliated to it.
	pm := parties.Compose(hotswap.New(partiesDB), nil)
	actx := authz.WithContext(ctx, e2eAdmin())
	acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
	if err != nil {
		t.Fatal(err)
	}
	var people []string
	for i := range 6 {
		p, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: fmt.Sprintf("Empleado %d", i+1), FirstSurname: "García",
			Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: pdomain.RelEmployment.String()}})
		if err != nil {
			t.Fatal(err)
		}
		people = append(people, p.ID)
	}
	affiliated := func(ids ...string) int {
		orgs, err := pm.Organizations.InternalOrganizations(ctx, ids)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, id := range ids {
			n += len(orgs[id])
		}
		return n
	}
	if n := affiliated(people...); n != len(people) {
		t.Fatalf("affiliations before: %d", n)
	}

	env := []string{envNATS + "=" + server, envStream + "=" + stream, envPrefix + "=" + prefix}
	parties1 := startReplica(t, "parties", "parties-1", append(env, envDSN+"="+partiesDSN))
	parties2 := startReplica(t, "parties", "parties-2", append(env, envDSN+"="+partiesDSN))
	hr1 := startReplica(t, "hr", "hr-1", append(env, envDSN+"="+hrDSN))
	hr2 := startReplica(t, "hr", "hr-2", append(env, envDSN+"="+hrDSN))
	for _, r := range []*replica{parties1, parties2, hr1, hr2} {
		r.wait(t, "READY")
	}

	// Each HR replica hires and terminates two people; both relays share the outbox of HR.
	terminate := func(r *replica, n int) {
		r.send(t, fmt.Sprintf("%s %s E-%d", people[n], acme.ID, n+1))
	}
	terminate(hr1, 0)
	terminate(hr2, 1)
	terminate(hr1, 2)
	terminate(hr2, 3)
	for n := range 4 {
		r := []*replica{hr1, hr2}[n%2]
		r.wait(t, "DONE "+people[n])
	}
	eventuallyE2E(t, "Parties to end the four employment relationships", func() bool { return affiliated(people[:4]...) == 0 })
	if n := affiliated(people[4:]...); n != 2 {
		t.Fatalf("the people HR did not terminate keep their affiliation: %d", n)
	}

	// One replica of each service dies. Nothing is lost and nobody has to step in.
	parties1.kill()
	hr1.kill()
	terminate(hr2, 4)
	terminate(hr2, 5)
	hr2.wait(t, "DONE "+people[4])
	hr2.wait(t, "DONE "+people[5])
	eventuallyE2E(t, "the surviving replicas to carry on", func() bool { return affiliated(people...) == 0 })

	// Every termination was handled once: six messages in the inbox of Parties, none pending
	// in the outbox of HR, and no dead letters.
	eventuallyE2E(t, "the outbox of HR to be drained", func() bool {
		return count(t, hrDB, "SELECT COUNT(*) FROM "+hinfra.TableIntegrationOutbox+" WHERE processed_at IS NULL") == 0
	})
	if n := count(t, partiesDB, "SELECT COUNT(*) FROM "+pinfra.TableInbox); n != 6 {
		t.Errorf("inbox of Parties: %d messages, want 6", n)
	}
	sent := count(t, hrDB, "SELECT COUNT(*) FROM "+hinfra.TableIntegrationOutbox)
	handled := append(parties1.lines("HANDLED "), parties2.lines("HANDLED ")...)
	unique := slices.Clone(handled)
	slices.Sort(unique)
	if unique = slices.Compact(unique); len(unique) != 6 {
		t.Errorf("Parties handled %d different messages, want 6: %v", len(unique), handled)
	}
	tr, err := knats.Open(ctx, knats.Config{URL: server, Stream: stream, Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if dead, err := tr.DeadLetters(ctx, "parties"); err != nil || len(dead) != 0 {
		t.Errorf("dead letters: %+v %v", dead, err)
	}
	t.Logf("HR published %d integration events through two relays; Parties asked for one type and handled %d (parties-1: %d, parties-2: %d)",
		sent, len(handled), len(parties1.lines("HANDLED ")), len(parties2.lines("HANDLED ")))
}

// TestServiceProcess is one replica of a service when TestTwoProcesses_HRToParties starts this
// test binary again with a role; on its own it does nothing. It composes the bounded context
// exactly as a host would: database, transport, relay or consumer.
func TestServiceProcess(t *testing.T) {
	role := os.Getenv(envRole)
	if role == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	say := func(format string, args ...any) { fmt.Printf("@@ "+format+"\n", args...) }

	sw := hotswap.New(openPostgres(t, os.Getenv(envDSN)))
	transport, err := knats.Open(ctx, knats.Config{URL: os.Getenv(envNATS), Name: os.Getenv(envReplica),
		Stream: os.Getenv(envStream), Prefix: os.Getenv(envPrefix), AckWait: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()

	switch role {
	case "parties":
		mod := parties.Compose(sw, nil)
		reported := application.MessageHandlerFunc(func(ctx context.Context, env application.Envelope) error {
			err := mod.Consumer.HandleMessage(ctx, env)
			if err == nil {
				say("HANDLED %s", env.ID)
			}
			return err
		})
		if _, err := transport.Attach(ctx, mod.Consumer.Name(), reported, mod.Consumer.Types()...); err != nil {
			t.Fatal(err)
		}
		say("READY")
		_, _ = io.Copy(io.Discard, os.Stdin) // until the parent closes the pipe

	case "hr":
		mod := hr.Compose(sw) // no synchronous ports: Parties is in another process
		relay := mod.Relay(transport, outbox.WithClaims(sw), outbox.WithBatchSize(5))
		go func() { _ = relay.Run(ctx, 20*time.Millisecond) }()
		actx := authz.WithContext(ctx, e2eAdmin())
		today := vocab.DateOf(fw.Now())
		say("READY")
		// Each line is a request: "person employer number".
		lines := bufio.NewScanner(os.Stdin)
		for lines.Scan() {
			f := strings.Fields(lines.Text())
			if len(f) != 3 {
				continue
			}
			e, err := mod.Service.Hire.Handle(actx, happ.Hire{Person: f[0], Employer: f[1], Number: f[2], Hired: today.AddDays(-100), JobCategory: "GP1"})
			if err == nil {
				id, _ := hdomain.ParseEmploymentID(e.ID)
				_, err = mod.Service.Terminate.Handle(actx, happ.Terminate{ID: id, On: today.AddDays(-1), Reason: "baja"})
			}
			if err != nil {
				say("ERROR %s %v", f[0], err)
				continue
			}
			say("DONE %s", f[0])
		}
	}
}

func e2eAdmin() *authz.Context {
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	return admin
}

// createDatabase creates an empty database on the PostgreSQL server of dsn, dropped when the
// test ends, and returns its DSN.
func createDatabase(t *testing.T, dsn, name string) string {
	t.Helper()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`)
		_ = admin.Close()
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

func openPostgres(t *testing.T, dsn string) *sqlrepo.DB {
	t.Helper()
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return sqlrepo.New(raw, postgres.New())
}

func count(t *testing.T, db *sqlrepo.DB, query string) int {
	t.Helper()
	var n int
	if err := db.SQL().QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func eventuallyE2E(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// replica is a service process started by the test.
type replica struct {
	name  string
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu   sync.Mutex
	said []string // what the process reported, without the "@@ " mark
}

// startReplica runs this test binary again as one replica of a service.
func startReplica(t *testing.T, role, name string, env []string) *replica {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestServiceProcess$", "-test.timeout=5m")
	cmd.Env = append(os.Environ(), append(env, envRole+"="+role, envReplica+"="+name)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r := &replica{name: name, cmd: cmd, stdin: stdin}
	go func() {
		lines := bufio.NewScanner(stdout)
		for lines.Scan() {
			if said, ok := strings.CutPrefix(lines.Text(), "@@ "); ok {
				r.mu.Lock()
				r.said = append(r.said, said)
				r.mu.Unlock()
			}
		}
	}()
	t.Cleanup(func() {
		_ = stdin.Close() // the service stops when its input ends
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			r.kill()
		}
	})
	return r
}

func (r *replica) lines(prefix string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, s := range r.said {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			out = append(out, rest)
		}
	}
	return out
}

// wait blocks until the process reported line.
func (r *replica) wait(t *testing.T, line string) {
	t.Helper()
	eventuallyE2E(t, r.name+" to report "+line, func() bool {
		if errs := r.lines("ERROR "); len(errs) > 0 {
			t.Fatalf("%s: %v", r.name, errs)
		}
		return len(r.lines(line)) > 0
	})
}

func (r *replica) send(t *testing.T, request string) {
	t.Helper()
	if _, err := io.WriteString(r.stdin, request+"\n"); err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
}

// kill ends the process at once, as a crash would.
func (r *replica) kill() { _ = r.cmd.Process.Kill() }
