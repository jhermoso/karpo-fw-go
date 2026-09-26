package parties_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	papp "github.com/jhermoso/karpo-fw-go/examples/parties/application"
	pdist "github.com/jhermoso/karpo-fw-go/examples/parties/distribution"
	"github.com/jhermoso/karpo-fw-go/examples/parties/domain"
	"github.com/jhermoso/karpo-fw-go/examples/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/events/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

type env struct {
	t      *testing.T
	srv    *httptest.Server
	sw     *hotswap.Switch
	outbox application.OutboxStore
}

// compose is the composition root: the only place that knows concrete technologies.
func compose(t *testing.T) *env {
	sw := hotswap.New(memory.NewStore("memory"))
	repo := hotswap.Repository(sw, infrastructure.RepositoryFactory)
	outbox := hotswap.Outbox(sw, infrastructure.OutboxFactory)
	svc := papp.NewService(repo, sw, application.NewOutbox(outbox), memory.NewIdempotencyStore())

	mux := http.NewServeMux()
	pdist.NewModule(svc).RegisterRoutes(mux)
	srv := httptest.NewServer(distribution.Chain(mux, distribution.Correlation()))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	return &env{t: t, srv: srv, sw: sw, outbox: outbox}
}

func (e *env) do(method, path string, body any, headers map[string]string) (int, []byte) {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, &buf)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.Bytes()
}

func (e *env) register(name, taxID string) papp.PartyDTO {
	e.t.Helper()
	status, body := e.do("POST", "/parties", map[string]string{"type": "organization", "legalName": name, "taxId": taxID}, nil)
	if status != http.StatusCreated {
		e.t.Fatalf("register %s: %d %s", name, status, body)
	}
	var dto papp.PartyDTO
	_ = json.Unmarshal(body, &dto)
	return dto
}

func (e *env) search(query string) []string {
	e.t.Helper()
	status, body := e.do("GET", "/parties?"+query, nil, nil)
	if status != http.StatusOK {
		e.t.Fatalf("search %s: %d %s", query, status, body)
	}
	var page fw.Page[papp.PartyDTO]
	_ = json.Unmarshal(body, &page)
	names := make([]string, 0, len(page.Items))
	for _, p := range page.Items {
		names = append(names, p.LegalName)
	}
	return names
}

func (e *env) scenario(prefix string) {
	t := e.t
	acme := e.register(prefix+" Acme Corporation", prefix+"-B12345678")
	globex := e.register(prefix+" Globex", prefix+"-B87654321")
	e.register(prefix+" Initech", prefix+"-B11112222")

	// Validation errors come back as RFC 9457 problems with field details.
	status, body := e.do("POST", "/parties", map[string]string{"type": "alien", "legalName": "", "taxId": "1"}, nil)
	var problem distribution.ProblemDetails
	_ = json.Unmarshal(body, &problem)
	if status != http.StatusBadRequest || len(problem.Errors) != 3 {
		t.Fatalf("expected 400 with 3 field errors, got %d %s", status, body)
	}
	// Business rule: tax id is unique.
	if status, body = e.do("POST", "/parties", map[string]string{"type": "organization", "legalName": "Copy", "taxId": prefix + "-B12345678"}, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for duplicate tax id, got %d %s", status, body)
	}
	// Idempotent replay returns the same party.
	hdr := map[string]string{"Idempotency-Key": prefix + "-req-1"}
	_, first := e.do("POST", "/parties", map[string]string{"type": "person", "legalName": prefix + " Jane Roe", "taxId": prefix + "-X0000001"}, hdr)
	_, second := e.do("POST", "/parties", map[string]string{"type": "person", "legalName": prefix + " Jane Roe", "taxId": prefix + "-X0000001"}, hdr)
	if !bytes.Equal(first, second) {
		t.Fatalf("idempotent replay differs:\n%s\n%s", first, second)
	}

	for _, c := range []map[string]any{
		{"kind": "email", "value": "info@acme.test", "primary": true},
		{"kind": "phone", "value": "+34 900 000 000", "primary": false},
	} {
		if status, body := e.do("POST", "/parties/"+acme.ID.String()+"/contacts", c, nil); status != http.StatusOK {
			t.Fatalf("add contact: %d %s", status, body)
		}
	}
	if status, body := e.do("POST", "/parties/"+globex.ID.String()+"/contacts", map[string]any{"kind": "phone", "value": "555", "primary": true}, nil); status != http.StatusOK {
		t.Fatalf("add contact: %d %s", status, body)
	}
	if status, body := e.do("PUT", "/parties/"+globex.ID.String()+"/legal-name", map[string]string{"legalName": prefix + " Globex Corporation"}, nil); status != http.StatusOK {
		t.Fatalf("rename: %d %s", status, body)
	}

	status, body = e.do("GET", "/parties/"+acme.ID.String(), nil, nil)
	var got papp.PartyDTO
	_ = json.Unmarshal(body, &got)
	if status != http.StatusOK || len(got.Contacts) != 2 || got.Version != 3 {
		t.Fatalf("get: %d %s", status, body)
	}

	// Every criterion is part of one specification executed by the store.
	// Each backend only holds its own scenario, so queries need no prefix. Tax ids are
	// normalized (upper case, no dashes) by the TaxID value object.
	taxPrefix := strings.ToUpper(prefix) + "B1"
	checks := map[string][]string{
		"q=corp":                    {prefix + " Acme Corporation", prefix + " Globex Corporation"},
		"q=" + taxPrefix:            {prefix + " Acme Corporation", prefix + " Initech"},
		"reachableBy=email":         {prefix + " Acme Corporation"},
		"minContacts=2":             {prefix + " Acme Corporation"},
		"minContacts=1&active=true": {prefix + " Acme Corporation", prefix + " Globex Corporation"},
	}
	for q, want := range checks {
		if got := e.search(q); !slices.Equal(got, want) {
			t.Fatalf("search %q: want %v got %v", q, want, got)
		}
	}
	if got := e.search("size=3&page=2"); len(got) != 1 { // 4 parties, pages of 3

		t.Fatalf("paging: %v", got)
	}
}

func TestParties_EndToEnd_WithLiveDatabaseSwap(t *testing.T) {
	e := compose(t)
	ctx := context.Background()

	// 1. The service starts on the in-memory backend.
	e.scenario("mem")
	memoryParty := e.search("q=mem%20acme")
	if len(memoryParty) != 1 {
		t.Fatalf("memory data: %v", memoryParty)
	}

	// 2. A SQL database is prepared and swapped in while the service keeps running.
	path := filepath.Join(t.TempDir(), "parties.db")
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	if err := infrastructure.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := e.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}

	// 3. Same HTTP API, same use cases, same specifications: now translated to SQL.
	e.scenario("sql")
	if got := e.search("q=mem%20acme"); len(got) != 0 {
		t.Fatalf("new backend must not see the memory data: %v", got)
	}
	var rows int
	if err := raw.QueryRow("SELECT COUNT(*) FROM parties").Scan(&rows); err != nil || rows != 4 {
		t.Fatalf("expected 4 parties stored in SQLite, got %d (%v)", rows, err)
	}

	// 4. The outbox committed with the state is relayed to subscribers, decoded and typed.
	bus := inprocess.New()
	reg := events.NewRegistry()
	events.Register[domain.PartyRegistered](reg)
	events.Register[domain.PartyRenamed](reg)
	events.Register[domain.ContactAdded](reg)
	var registered []string
	var renamed int
	events.Subscribe(bus, func(_ context.Context, e domain.PartyRegistered) error {
		registered = append(registered, e.LegalName)
		return nil
	})
	events.Subscribe(bus, func(context.Context, domain.PartyRenamed) error { renamed++; return nil })
	relay := application.NewOutboxRelay(e.outbox, reg, bus)
	n, err := relay.RelayOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 8 || len(registered) != 4 || renamed != 1 { // 4 registered + 3 contacts + 1 rename
		t.Fatalf("relayed %d, registered %v, renamed %d", n, registered, renamed)
	}
	if left, _ := e.outbox.Pending(ctx, 100, 10); len(left) != 0 {
		t.Fatalf("outbox not drained: %d", len(left))
	}
}

func TestParty_Invariants(t *testing.T) {
	tax, _ := domain.NewTaxID("b-1234-5678")
	if tax.String() != "B12345678" {
		t.Fatalf("tax id normalization: %s", tax)
	}
	p, err := domain.Register(domain.NewPartyID(), domain.Organization, "Acme", tax)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.AddContact(domain.Contact{Kind: domain.Email, Value: "a@acme.test", Primary: true}); err != nil {
		t.Fatal(err)
	}
	if err := p.AddContact(domain.Contact{Kind: domain.Email, Value: "b@acme.test", Primary: true}); err != nil {
		t.Fatal(err)
	}
	primaries := 0
	for _, c := range p.Contacts() {
		if c.Primary {
			primaries++
		}
	}
	if primaries != 1 {
		t.Fatal("only one primary contact per kind")
	}
	if err := p.AddContact(domain.Contact{Kind: domain.Email, Value: "A@acme.test"}); !errors.Is(err, fw.ErrRuleViolation) {
		t.Fatalf("duplicate contact must violate a rule, got %v", err)
	}
	p.Deactivate()
	if err := p.Rename("Other"); !errors.Is(err, fw.ErrRuleViolation) {
		t.Fatalf("inactive party cannot be renamed, got %v", err)
	}
	if !domain.MinContacts(2).IsSatisfiedBy(p) || domain.Active().IsSatisfiedBy(p) || !domain.ReachableBy(domain.Email).IsSatisfiedBy(p) {
		t.Fatal("in-memory specification semantics")
	}
}

// The bounded context's domain depends only on the standard library and the framework domain.
func TestParties_DomainIsPure(t *testing.T) {
	archtest.AssertTreeOnlyImports(t, "domain", true, []string{
		"github.com/jhermoso/karpo-fw-go/pkg/domain",
		"github.com/jhermoso/karpo-fw-go/examples/parties/domain",
	})
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql"})
}
