package work_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/work"
	wapp "github.com/jhermoso/karpo-fw-go/contexts/work/application"
	winfra "github.com/jhermoso/karpo-fw-go/contexts/work/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

// host composes Parties and Work on one hot-swappable backend.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	work     *work.Module
	parties  *parties.Module
	broker   *inprocess.Broker
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	pm := parties.Compose(sw, nil)
	wm := work.Compose(sw)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("work-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, work: wm, parties: pm, broker: inprocess.NewBroker()}
	users := map[string][]authz.Permission{
		"planner":  {wapp.PermWorkRead, wapp.PermWorkUpdate},
		"lead":     {wapp.PermWorkRead, wapp.PermWorkProgress, wapp.PermTimeRead, wapp.PermTimeApprove},
		"worker":   {wapp.PermWorkRead, wapp.PermTimeRead, wapp.PermTimeRecord},
		"viewer":   {wapp.PermWorkRead, wapp.PermTimeRead},
		"outsider": wapp.Permissions(),
	}
	for u, perms := range users {
		h.ids[u] = fw.NewUUID()
		dir.Put(h.ids[u], authz.Subject{Active: true, Permissions: perms})
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	ac.GlobalAdmin = true
	h.adminCtx = authz.WithContext(ctx, ac)

	mux := http.NewServeMux()
	wm.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	return h
}

func (h *host) grant(user string, orgs ...string) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	for _, o := range orgs {
		s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(o), Level: authz.Full})
	}
	h.dir.Put(h.ids[user], s)
}

func (h *host) do(method, path, user string, body, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, &buf)
	req.Header.Set("Authorization", h.tokens[user])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			h.t.Fatal(err)
		}
	}
	return res.StatusCode
}

func (h *host) must(got, want int, what string) {
	h.t.Helper()
	if got != want {
		h.t.Fatalf("%s: status %d, want %d", what, got, want)
	}
}

func (h *host) ok(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatal(err)
	}
}

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	ps := h.parties.Service
	org := func(name string, roles ...string) papp.PartyDTO {
		o, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag, Roles: roles})
		h.ok(err)
		return o
	}
	person := func(name string) papp.PartyDTO {
		p, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: name, FirstSurname: "Obra " + tag})
		h.ok(err)
		return p
	}
	acme, globex := org("Acme", pdomain.RoleInternalOrganization.String()), org("Globex", pdomain.RoleInternalOrganization.String())
	client := org("Talleres Vega")
	ana, luis := person("Ana"), person("Luis")
	for _, u := range []string{"planner", "lead", "worker", "viewer"} {
		h.grant(u, acme.ID)
	}
	h.grant("outsider", globex.ID)

	// The project for the customer and a task in it.
	project := map[string]any{"company": acme.ID, "code": "prj-01", "kind": "project", "name": "Reforma de la nave", "purpose": "improvement",
		"customer": client.ID, "estimatedHours": "120", "budget": "6000", "on": "2026-03-02"}
	var prj, task, extra wapp.WorkDTO
	h.must(h.do("POST", "/api/work", "viewer", project, nil), 403, "viewer")
	h.must(h.do("POST", "/api/work", "outsider", project, nil), 404, "outsider")
	h.must(h.do("POST", "/api/work", "planner", map[string]any{"company": acme.ID, "code": "X", "kind": "party", "name": "Fiesta"}, nil), 400, "kind")
	h.must(h.do("POST", "/api/work", "planner", project, &prj), 201, "project")
	h.must(h.do("POST", "/api/work", "planner", project, nil), 422, "code used once")
	h.must(h.do("POST", "/api/work", "planner", map[string]any{"company": acme.ID, "code": "T-01", "kind": "task", "name": "Demolición", "parent": prj.ID,
		"customer": client.ID, "on": "2026-03-02"}, &task), 201, "task")
	h.must(h.do("POST", "/api/work", "planner", map[string]any{"company": acme.ID, "code": "T-02", "kind": "task", "name": "Pintura", "parent": prj.ID,
		"on": "2026-03-02"}, &extra), 201, "second task")
	if prj.Code != "PRJ-01" || prj.Status != "created" || len(prj.History) != 1 || task.Parent != prj.ID || prj.Budget != "6000.00" {
		t.Fatalf("opened: %+v / %+v", prj, task)
	}

	// People on the task, each at their hourly rate.
	h.must(h.do("POST", "/api/work/"+task.ID+"/assign", "worker", map[string]any{"person": ana.ID, "rate": "25", "from": "2026-03-09"}, nil), 403, "recording is not planning")
	h.must(h.do("POST", "/api/work/"+task.ID+"/assign", "planner", map[string]any{"person": ana.ID, "role": "Oficial", "rate": "25", "from": "2026-03-09"}, nil), 200, "assign Ana")
	h.must(h.do("POST", "/api/work/"+task.ID+"/assign", "planner", map[string]any{"person": luis.ID, "role": "Ayudante", "rate": "15", "from": "2026-03-09"}, &task), 200, "assign Luis")
	if len(task.Assignments) != 2 || task.Assignments[1].Rate != "15.00" {
		t.Fatalf("assignments: %+v", task.Assignments)
	}

	// The status moves by its rule.
	anaDay := map[string]any{"work": task.ID, "person": ana.ID, "date": "2026-03-09", "hours": "7.5", "comment": "Demolición"}
	h.must(h.do("POST", "/api/time", "worker", anaDay, nil), 422, "time before the work starts")
	h.must(h.do("POST", "/api/work/"+task.ID+"/progress", "planner", map[string]any{"to": "in-progress", "on": "2026-03-09"}, nil), 403, "planning is not progressing")
	h.must(h.do("POST", "/api/work/"+task.ID+"/progress", "lead", map[string]any{"to": "finished"}, nil), 400, "status")
	h.must(h.do("POST", "/api/work/"+task.ID+"/progress", "lead", map[string]any{"to": "scheduled", "on": "2026-03-03"}, nil), 422, "scheduling needs dates")
	h.must(h.do("POST", "/api/work/"+task.ID+"/progress", "lead", map[string]any{"to": "completed", "on": "2026-03-03"}, nil), 422, "not started")
	h.must(h.do("PUT", "/api/work/"+task.ID, "planner", map[string]any{"name": "Demolición", "customer": client.ID, "plannedStart": "2026-03-09",
		"plannedEnd": "2026-03-10", "estimatedHours": "16"}, nil), 200, "plan")
	h.must(h.do("POST", "/api/work/"+task.ID+"/progress", "lead", map[string]any{"to": "scheduled", "on": "2026-03-03"}, nil), 200, "scheduled")
	h.must(h.do("POST", "/api/work/"+task.ID+"/progress", "lead", map[string]any{"to": "in-progress", "on": "2026-03-09"}, &task), 200, "started")
	h.must(h.do("POST", "/api/work/"+prj.ID+"/progress", "lead", map[string]any{"to": "in-progress", "on": "2026-03-09"}, nil), 200, "project started")
	if task.Status != "in-progress" || task.Started != "2026-03-09" || len(task.History) != 3 {
		t.Fatalf("started: %+v", task)
	}

	// Time: who is assigned, on work in progress, at most 24 hours a day.
	var ta, tl, tl2 wapp.TimeDTO
	h.must(h.do("POST", "/api/time", "viewer", anaDay, nil), 403, "viewer")
	h.must(h.do("POST", "/api/time", "outsider", anaDay, nil), 404, "outsider")
	h.must(h.do("POST", "/api/time", "worker", map[string]any{"work": task.ID, "person": client.ID, "date": "2026-03-09", "hours": "1"}, nil), 422, "not assigned")
	h.must(h.do("POST", "/api/time", "worker", map[string]any{"work": task.ID, "person": ana.ID, "date": "2026-03-09", "hours": "25"}, nil), 400, "hours")
	h.must(h.do("POST", "/api/time", "worker", anaDay, &ta), 201, "Ana's day")
	h.must(h.do("POST", "/api/time", "worker", map[string]any{"work": task.ID, "person": ana.ID, "date": "2026-03-09", "hours": "17"}, nil), 422, "more than 24 hours")
	h.must(h.do("POST", "/api/time", "worker", map[string]any{"work": task.ID, "person": luis.ID, "date": "2026-03-09", "hours": "8", "billable": false}, &tl), 201, "Luis's day")
	if ta.Rate != "25.00" || ta.Cost != "187.50" || !ta.Billable || ta.Approved || tl.Cost != "120.00" || tl.Billable {
		t.Fatalf("recorded: %+v / %+v", ta, tl)
	}
	h.must(h.do("PUT", "/api/time/"+ta.ID, "lead", map[string]any{"hours": "8", "billable": true}, nil), 403, "approving is not recording")
	h.must(h.do("PUT", "/api/time/"+ta.ID, "worker", map[string]any{"hours": "8", "billable": true, "comment": "Demolición y desescombro"}, &ta), 200, "correct")
	h.must(h.do("POST", "/api/time/"+ta.ID+"/approve", "worker", nil, nil), 403, "recording is not approving")
	h.must(h.do("POST", "/api/time/"+ta.ID+"/approve", "lead", nil, &ta), 200, "approve")
	if !ta.Approved || ta.Cost != "200.00" || ta.Hours != "8.00" {
		t.Fatalf("approved: %+v", ta)
	}
	h.must(h.do("PUT", "/api/time/"+ta.ID, "worker", map[string]any{"hours": "1", "billable": true}, nil), 422, "approved time is final")
	h.must(h.do("DELETE", "/api/time/"+ta.ID, "worker", nil, nil), 422, "approved time is not withdrawn")

	// Completing: no time waiting for approval, no open parts.
	h.must(h.do("POST", "/api/work/"+task.ID+"/progress", "lead", map[string]any{"to": "completed", "on": "2026-03-10"}, nil), 422, "time waiting for approval")
	h.must(h.do("DELETE", "/api/time/"+tl.ID, "lead", nil, nil), 403, "approving is not withdrawing")
	h.must(h.do("DELETE", "/api/time/"+tl.ID, "worker", nil, nil), 204, "withdraw")
	h.must(h.do("POST", "/api/time", "worker", map[string]any{"work": task.ID, "person": luis.ID, "date": "2026-03-10", "hours": "4"}, &tl2), 201, "Luis's half day")
	h.must(h.do("POST", "/api/time/"+tl2.ID+"/approve", "lead", nil, nil), 200, "approve Luis")
	h.must(h.do("POST", "/api/work/"+task.ID+"/release", "planner", map[string]any{"person": luis.ID, "until": "2026-03-10"}, nil), 200, "release Luis")
	h.must(h.do("POST", "/api/time", "worker", map[string]any{"work": task.ID, "person": luis.ID, "date": "2026-03-11", "hours": "4"}, nil), 422, "released")
	h.must(h.do("POST", "/api/work/"+prj.ID+"/progress", "lead", map[string]any{"to": "completed", "on": "2026-03-10"}, nil), 422, "open parts")
	h.must(h.do("POST", "/api/work/"+extra.ID+"/progress", "lead", map[string]any{"to": "cancelled", "on": "2026-03-10"}, nil), 422, "reason")
	h.must(h.do("POST", "/api/work/"+extra.ID+"/progress", "lead", map[string]any{"to": "cancelled", "on": "2026-03-10", "reason": "El cliente pinta por su cuenta"}, &extra), 200, "cancel")
	h.must(h.do("POST", "/api/work/"+task.ID+"/progress", "lead", map[string]any{"to": "completed", "on": "2026-03-10"}, &task), 200, "task completed")
	if task.Status != "completed" || task.Finished != "2026-03-10" || task.Hours != "12.00" || task.Cost != "260.00" || task.PendingHours != "0.00" ||
		extra.Status != "cancelled" || extra.CancelReason == "" {
		t.Fatalf("completed: %+v / %+v", task, extra)
	}
	h.must(h.do("POST", "/api/time", "worker", map[string]any{"work": task.ID, "person": ana.ID, "date": "2026-03-10", "hours": "1"}, nil), 422, "time on completed work")
	h.must(h.do("PUT", "/api/work/"+task.ID, "planner", map[string]any{"name": "Otra cosa"}, nil), 422, "completed work is closed")
	h.must(h.do("POST", "/api/work", "planner", map[string]any{"company": acme.ID, "code": "T-03", "kind": "task", "name": "Tarde", "parent": task.ID}, nil), 422, "part of closed work")
	h.must(h.do("POST", "/api/work/"+prj.ID+"/progress", "lead", map[string]any{"to": "on-hold", "on": "2026-03-11"}, nil), 200, "hold")
	h.must(h.do("POST", "/api/work/"+prj.ID+"/progress", "lead", map[string]any{"to": "completed", "on": "2026-03-12"}, nil), 422, "on hold is not completed")
	h.must(h.do("POST", "/api/work/"+prj.ID+"/progress", "lead", map[string]any{"to": "in-progress", "on": "2026-03-12"}, nil), 200, "resume")
	h.must(h.do("POST", "/api/work/"+prj.ID+"/progress", "lead", map[string]any{"to": "completed", "on": "2026-03-20"}, &prj), 200, "project completed")
	if prj.Started != "2026-03-09" || prj.Hours != "0.00" || len(prj.History) != 5 {
		t.Fatalf("project: %+v", prj)
	}

	// Two approvals and two completions leave for the other contexts.
	if n, err := h.work.Relay(h.broker).RelayOnce(context.Background()); err != nil || n != 4 {
		t.Fatalf("published: %d %v", n, err)
	}

	var got wapp.WorkDTO
	h.must(h.do("GET", "/api/work/"+task.ID, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/work/"+task.ID, "viewer", nil, &got), 200, "get")
	if len(got.Assignments) != 2 || got.Assignments[1].Until != "2026-03-10" || got.Assignments[0].Role != "Oficial" || len(got.History) != 4 ||
		got.History[3].Status != "completed" || got.History[1].On != "2026-03-03" || got.PlannedEnd != "2026-03-10" || got.Customer != client.ID {
		t.Fatalf("task: %+v", got)
	}
	var page fw.Page[wapp.WorkDTO]
	h.must(h.do("GET", "/api/work?company="+acme.ID+"&parent="+prj.ID, "viewer", nil, &page), 200, "parts")
	if len(page.Items) != 2 || page.Items[0].Code != "T-01" {
		t.Fatalf("parts: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/work?kind=task&status=cancelled", "viewer", nil, &page), 200, "cancelled tasks")
	if len(page.Items) != 1 || page.Items[0].Code != "T-02" {
		t.Fatalf("cancelled tasks: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/work", "outsider", nil, &page), 200, "outsider's work")
	if len(page.Items) != 0 {
		t.Fatalf("outsider: %+v", page.Items)
	}
	var times fw.Page[wapp.TimeDTO]
	h.must(h.do("GET", "/api/time?work="+task.ID, "viewer", nil, &times), 200, "time of the task")
	if len(times.Items) != 2 || times.Items[0].Date != "2026-03-09" || times.Items[1].Person != luis.ID {
		t.Fatalf("time: %+v", times.Items)
	}
	h.must(h.do("GET", "/api/time?work="+task.ID+"&pending=true", "viewer", nil, &times), 200, "pending time")
	if len(times.Items) != 0 {
		t.Fatalf("pending: %+v", times.Items)
	}
	var sheet wapp.TimesheetDTO
	h.must(h.do("GET", "/api/time/sheet?company="+acme.ID+"&person="+ana.ID, "viewer", nil, nil), 400, "period")
	h.must(h.do("GET", "/api/time/sheet?company="+acme.ID+"&person="+ana.ID+"&from=2026-03-01&to=2026-03-31", "viewer", nil, &sheet), 200, "timesheet")
	if len(sheet.Entries) != 1 || sheet.Hours != "8.00" || sheet.ApprovedHours != "8.00" || sheet.Cost != "200.00" {
		t.Fatalf("timesheet: %+v", sheet)
	}
}

func TestWork_PlansRecordsAndCompletes_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.scenario("m")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), winfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.scenario("s")
}

func TestWork_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/work/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
