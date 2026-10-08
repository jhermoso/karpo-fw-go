package imports_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/imports"
	iapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	"github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	iinfra "github.com/jhermoso/karpo-fw-go/contexts/imports/infrastructure"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
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
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// world plays the contexts that own what is imported (Parties, HR): what exists, by identity and
// by natural key.
type world struct {
	mu      sync.Mutex
	things  map[string]map[string]string
	natural map[string]string
	runs    map[string]bool // the runs the loaders were called within
	started chan struct{}
	release chan struct{}
}

func newWorld() *world {
	return &world{things: map[string]map[string]string{}, natural: map[string]string{}, runs: map[string]bool{}, started: make(chan struct{}),
		release: make(chan struct{})}
}

type loader struct {
	w    *world
	kind string
}

func (l loader) Kind() string       { return l.kind }
func (l loader) EntityType() string { return "world." + l.kind }

// naturalKey is how the owner would recognise the record without the source's key.
func naturalKey(r domain.Record) string {
	if r.Kind == domain.KindPerson {
		return r.Kind + "|" + r.Fields["email"]
	}
	return r.Kind + "|" + domain.Fold(r.Scope) + "|" + domain.Fold(r.Key)
}

func (l loader) Find(_ context.Context, r domain.Record, _ domain.Refs) (string, error) {
	l.w.mu.Lock()
	defer l.w.mu.Unlock()
	return l.w.natural[naturalKey(r)], nil
}

func (l loader) Apply(ctx context.Context, r domain.Record, existing string, refs domain.Refs) (string, domain.Outcome, error) {
	switch r.Key {
	case "PANIC":
		panic("boom")
	case "BLOCK":
		close(l.w.started)
		<-l.w.release
	}
	if p, ok := app.ImportProvenanceFrom(ctx); ok {
		l.w.mu.Lock()
		l.w.runs[p.RunID.String()+"|"+p.SourceKey+"|"+p.SourceFile] = true
		l.w.mu.Unlock()
	}
	fields := maps.Clone(r.Fields)
	switch r.Kind {
	case domain.KindPerson:
		if !strings.Contains(r.Fields["email"], "@") {
			return "", "", fw.Violation("parties.email", "not an email: "+r.Fields["email"])
		}
	case domain.KindEmployment:
		person, ok := refs.Lookup(domain.KindPerson, domain.GlobalScope, r.Key)
		company, ok2 := refs.Lookup(domain.KindLegalEntity, domain.GlobalScope, r.Scope)
		if !ok || !ok2 {
			return "", "", fw.Violation("hr.unknown_person", "the person or the company of the employment does not exist")
		}
		fields["person"], fields["company"] = person, company
	}
	l.w.mu.Lock()
	defer l.w.mu.Unlock()
	if existing == "" {
		id := fw.NewUUID().String()
		l.w.things[id], l.w.natural[naturalKey(r)] = fields, id
		return id, domain.Created, nil
	}
	if maps.Equal(l.w.things[existing], fields) {
		return existing, domain.Unchanged, nil
	}
	l.w.things[existing] = fields
	return existing, domain.Updated, nil
}

type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	ids      map[string]fw.UUID
	tokens   map[string]string
	imp      *imports.Module
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	return mount(t, sw)
}

// mount builds a host on a backend: its own sources and loaders, the storage of sw.
func mount(t *testing.T, sw *hotswap.Switch) *host {
	ctx := context.Background()
	im := imports.Compose(sw)
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("imports-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, ids: map[string]fw.UUID{}, tokens: map[string]string{}, imp: im}
	users := map[string][]authz.Permission{
		"importer": {iapp.PermRunRead, iapp.PermRunExecute},
		"other":    {iapp.PermRunRead, iapp.PermRunExecute},
		"reader":   {iapp.PermRunRead},
		"auditor":  {iapp.PermReferenceRead},
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
	im.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	return h
}

// do sends a request and decodes the answer into out.
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

const orgUnits = `unitType,name,parentOrg,notes
InternalOrganization,Maccorp Exact Change,,
InternalOrganization,Karpo Servicios,,
Department,Operaciones,Maccorp Exact Change,
Department,Operaciones,Karpo Servicios,homónimo
Office,Oficina Sol,Maccorp Exact Change,
Office,Aeropuerto T4,,
Department,Sin dueño,,
Department,Fantasma,No Existe SL,
`

const peopleHeader = "employeeNumber,firstName,lastName,preferredName,email,gender,legalEntity,department,workCenter,jobTitle,isSupervisor,notes,hireDate,terminationDate\n"

const people = peopleHeader + `P-MC-001,Ana,García,,ana@maccorp.test,F,Maccorp Exact Change,Operaciones,Oficina Sol,"Auxiliar, Caja",Sí,,01/03/2020,
P-MC-002,Luis,Pérez,Lucho Pérez,luis@maccorp.test,M,Maccorp / Karpo (PLURIEMPLEO),Operaciones,Oficina Sol,Cajero,no,,2021-05-10,2024-12-31
P-MC-006,Eva,Ruiz,,eva-at-maccorp,F,Karpo Servicios,Operaciones,,Gerente,1,,,
P-MC-007,Sin,Empresa,,x@y.test,,Otra SA,,,,,,,
P-MC-001,Dup,Licado,,d@y.test,,Karpo Servicios,,,,,,,
`

// The same people a month later: Luis changed his name and his address, Eva's address was fixed.
const peopleLater = peopleHeader + `P-MC-001,Ana,García,,ana@maccorp.test,F,Maccorp Exact Change,Operaciones,Oficina Sol,"Auxiliar, Caja",Sí,,01/03/2020,
P-MC-002,Luis,Pérez,Luis P.,lperez@maccorp.test,M,Maccorp / Karpo (PLURIEMPLEO),Operaciones,Oficina Sol,Cajero,no,,2021-05-10,2024-12-31
P-MC-006,Eva,Ruiz,,eva@maccorp.test,F,Karpo Servicios,Operaciones,,Gerente,1,,,
`

func files(org, ppl string) map[string]any {
	fs := []map[string]any{{"role": "org-units", "name": "org.csv", "content": org}}
	if ppl != "" {
		fs = append(fs, map[string]any{"role": "people", "name": "people.csv", "content": ppl})
	}
	return map[string]any{"source": "personio", "files": fs}
}

func count(t *testing.T, cs []iapp.CountDTO, kind string) iapp.CountDTO {
	t.Helper()
	for _, c := range cs {
		if c.Kind == kind {
			return c
		}
	}
	t.Fatalf("no count of %s in %+v", kind, cs)
	return iapp.CountDTO{}
}

func (h *host) scenario() {
	t := h.t
	w := newWorld()

	// Before the host says who writes each kind, the source is offered and nothing is loaded.
	var sources []iapp.SourceDTO
	h.must(h.do("GET", "/api/imports/sources", "auditor", nil, nil), 403, "auditor")
	h.must(h.do("GET", "/api/imports/sources", "reader", nil, &sources), 200, "sources")
	if len(sources) != 1 || sources[0].Key != "personio" || len(sources[0].Files) != 2 || !sources[0].Files[0].Required || len(sources[0].Kinds) != 5 {
		t.Fatalf("sources: %+v", sources)
	}
	h.imp.Load(loader{w, domain.KindLegalEntity}, loader{w, domain.KindDepartment}, loader{w, domain.KindWorkCenter}, loader{w, domain.KindPerson})
	var partial iapp.ReportDTO
	h.must(h.do("POST", "/api/imports/preview", "importer", files(orgUnits, people), &partial), 200, "preview without a loader")
	if c := count(t, partial.Counts, domain.KindEmployment); c.Read != 4 || c.Skipped != 4 || partial.Warnings != 1 {
		t.Fatalf("a kind nobody takes: %+v %+v", c, partial)
	}
	h.imp.Load(loader{w, domain.KindEmployment})
	h.must(h.do("GET", "/api/imports/sources", "reader", nil, &sources), 200, "sources")
	if len(sources[0].Unloaded) != 0 {
		t.Fatalf("unloaded: %+v", sources[0])
	}

	// What is refused before anything happens.
	h.must(h.do("POST", "/api/imports/preview", "reader", files(orgUnits, people), nil), 403, "reader")
	h.must(h.do("POST", "/api/imports/runs", "reader", files(orgUnits, people), nil), 403, "reader")
	h.must(h.do("POST", "/api/imports/runs", "importer", map[string]any{"source": "sap", "files": []any{}}, nil), 400, "unknown source")
	h.must(h.do("POST", "/api/imports/runs", "importer", map[string]any{"source": "personio", "files": []any{}}, nil), 400, "no org units")
	h.must(h.do("POST", "/api/imports/runs", "importer", map[string]any{"source": "personio", "files": []map[string]any{
		{"role": "org-units", "content": orgUnits}, {"role": "payroll", "content": "a\n1\n"}}}, nil), 400, "a role the source does not take")
	h.must(h.do("POST", "/api/imports/runs", "importer", files("unitType,name\nOffice,\"broken\n", ""), nil), 422, "unreadable file")
	h.must(h.do("POST", "/api/imports/runs", "importer", files(orgUnits+strings.Repeat("x", domain.MaxFileBytes), ""), nil), 400, "too big")
	var page fw.Page[iapp.RunDTO]
	h.must(h.do("GET", "/api/imports/runs", "importer", nil, &page), 200, "runs")
	if page.Total != 0 {
		t.Fatalf("nothing ran yet: %+v", page)
	}

	// The preview says what executing would do, and writes nothing.
	var pre iapp.ReportDTO
	h.must(h.do("POST", "/api/imports/preview", "importer", files(orgUnits, people), &pre), 200, "preview")
	if c := count(t, pre.Counts, domain.KindPerson); c.Read != 3 || c.Created != 3 || pre.Errors != 4 || len(pre.Messages) != 4 ||
		count(t, pre.Counts, domain.KindEmployment).Created != 4 || count(t, pre.Counts, domain.KindLegalEntity).Created != 2 {
		t.Fatalf("preview: %+v", pre)
	}
	if m := pre.Messages[0]; m.File != "org.csv" || m.Line != 8 || m.Code != "personio.unknown_legal_entity" || m.Severity != "error" {
		t.Fatalf("message: %+v", m)
	}
	if len(w.things) != 0 {
		t.Fatalf("a preview writes nothing: %+v", w.things)
	}

	// The first run: what the files say exists, except what is wrong, line by line.
	var first iapp.RunDTO
	h.must(h.do("POST", "/api/imports/runs", "importer", files(orgUnits, people), &first), 201, "first run")
	if first.Status != "completed-with-errors" || first.Errors != 6 || first.FinishedAt == "" || first.StartedBy != "importer" || len(first.Files) != 2 ||
		first.Files[1] != "people.csv" || first.Version != 2 {
		t.Fatalf("first run: %+v", first)
	}
	for kind, want := range map[string]iapp.CountDTO{
		domain.KindLegalEntity: {Read: 2, Created: 2}, domain.KindDepartment: {Read: 2, Created: 2}, domain.KindWorkCenter: {Read: 2, Created: 2},
		domain.KindPerson: {Read: 3, Created: 2, Failed: 1}, domain.KindEmployment: {Read: 4, Created: 3, Failed: 1}} {
		want.Kind = kind
		if got := count(t, first.Counts, kind); got != want {
			t.Fatalf("first run, %s: %+v", kind, got)
		}
	}
	// The owner's own refusal comes back with its code, on the line of the file it is about.
	var refused, orphan iapp.MessageDTO
	for _, m := range first.Messages {
		switch m.Code {
		case "parties.email":
			refused = m
		case "hr.unknown_person":
			orphan = m
		}
	}
	if refused.Key != "P-MC-006" || refused.Line != 4 || refused.File != "people.csv" || refused.Kind != domain.KindPerson || orphan.Key != "P-MC-006" {
		t.Fatalf("refused: %+v %+v", refused, orphan)
	}
	if len(w.things) != 11 || !w.runs[first.ID+"|personio|people.csv"] || !w.runs[first.ID+"|personio|org.csv"] {
		t.Fatalf("the world after the first run: %d %+v", len(w.things), w.runs)
	}

	// Each key of the source now points at what it became.
	var refs fw.Page[iapp.ReferenceDTO]
	h.must(h.do("GET", "/api/imports/references?source=personio", "reader", nil, nil), 403, "reader")
	h.must(h.do("GET", "/api/imports/references?source=personio&size=50", "auditor", nil, &refs), 200, "references")
	if refs.Total != 11 {
		t.Fatalf("references: %d", refs.Total)
	}
	h.must(h.do("GET", "/api/imports/references?source=personio&kind=employment&key=P-MC-002", "auditor", nil, &refs), 200, "two employments")
	if refs.Total != 2 || refs.Items[0].Scope != "Karpo Servicios" || refs.Items[1].Scope != "Maccorp Exact Change" || refs.Items[0].EntityType != "world.employment" {
		t.Fatalf("two employments: %+v", refs.Items)
	}
	h.must(h.do("GET", "/api/imports/references?kind=person&key=P-MC-002", "auditor", nil, &refs), 200, "luis")
	luis := refs.Items[0]
	if luis.FirstRun != first.ID || luis.LastRun != first.ID || w.things[luis.EntityID]["fullName"] != "Lucho Pérez" {
		t.Fatalf("luis: %+v", luis)
	}

	// A month later: who has not changed is left alone, Luis is found by his key although his name
	// and his address changed, and Eva arrives now that her address is right.
	var second iapp.RunDTO
	h.must(h.do("POST", "/api/imports/runs", "importer", files(strings.Join(strings.Split(orgUnits, "\n")[:7], "\n")+"\n", peopleLater), &second), 201, "second run")
	if second.Status != "succeeded" || second.Errors != 0 || len(second.Messages) != 0 {
		t.Fatalf("second run: %+v", second)
	}
	for kind, want := range map[string]iapp.CountDTO{
		domain.KindLegalEntity: {Read: 2, Unchanged: 2}, domain.KindDepartment: {Read: 2, Unchanged: 2}, domain.KindWorkCenter: {Read: 2, Unchanged: 2},
		domain.KindPerson: {Read: 3, Created: 1, Updated: 1, Unchanged: 1}, domain.KindEmployment: {Read: 4, Created: 1, Unchanged: 3}} {
		want.Kind = kind
		if got := count(t, second.Counts, kind); got != want {
			t.Fatalf("second run, %s: %+v", kind, got)
		}
	}
	if len(w.things) != 13 || w.things[luis.EntityID]["fullName"] != "Luis P." {
		t.Fatalf("the world after the second run: %d %+v", len(w.things), w.things[luis.EntityID])
	}
	h.must(h.do("GET", "/api/imports/references?kind=person&size=50", "auditor", nil, &refs), 200, "people")
	if refs.Total != 3 {
		t.Fatalf("people: %+v", refs.Items)
	}
	for _, r := range refs.Items {
		want := map[string][2]string{"P-MC-001": {first.ID, first.ID}, "P-MC-002": {first.ID, second.ID}, "P-MC-006": {second.ID, second.ID}}[r.Key]
		if r.FirstRun != want[0] || r.LastRun != want[1] {
			t.Fatalf("reference of %s: %+v", r.Key, r)
		}
	}
	h.must(h.do("GET", "/api/imports/references?entity="+luis.EntityID, "auditor", nil, &refs), 200, "by entity")
	if refs.Total != 1 || refs.Items[0].Key != "P-MC-002" {
		t.Fatalf("by entity: %+v", refs.Items)
	}

	// A preview now finds everything.
	h.must(h.do("POST", "/api/imports/preview", "importer", files(orgUnits, peopleLater), &pre), 200, "preview")
	if c := count(t, pre.Counts, domain.KindPerson); c.Unchanged != 3 || c.Created != 0 || count(t, pre.Counts, domain.KindEmployment).Unchanged != 4 {
		t.Fatalf("preview after: %+v", pre.Counts)
	}

	// Runs: the latest first; each sees its own, a global administrator sees all.
	h.must(h.do("GET", "/api/imports/runs", "importer", nil, &page), 200, "runs")
	if page.Total != 2 || page.Items[0].StartedAt < page.Items[1].StartedAt || len(page.Items[0].Messages) != 0 || page.Items[0].Errors+page.Items[1].Errors != 6 {
		t.Fatalf("runs: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/imports/runs?status=succeeded&source=personio", "importer", nil, &page), 200, "succeeded")
	if page.Total != 1 {
		t.Fatalf("succeeded: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/imports/runs?status=odd", "importer", nil, nil), 400, "status")
	h.must(h.do("GET", "/api/imports/runs", "other", nil, &page), 200, "other")
	if page.Total != 0 {
		t.Fatalf("other sees: %+v", page.Items)
	}
	var got iapp.RunDTO
	h.must(h.do("GET", "/api/imports/runs/"+first.ID, "other", nil, nil), 404, "other")
	h.must(h.do("GET", "/api/imports/runs/"+first.ID, "auditor", nil, nil), 403, "auditor")
	h.must(h.do("GET", "/api/imports/runs/nope", "importer", nil, nil), 400, "bad id")
	h.must(h.do("GET", "/api/imports/runs/"+first.ID, "importer", nil, &got), 200, "the first run")
	if len(got.Messages) != 6 || got.Messages[0].Line != 8 || len(got.Counts) != 5 || got.Counts[0].Kind != domain.KindLegalEntity {
		t.Fatalf("the first run: %+v", got)
	}
	all, err := h.imp.Service.SearchRuns.Handle(h.adminCtx, iapp.SearchRuns{})
	if err != nil || all.Total != 2 {
		t.Fatalf("admin: %+v %v", all, err)
	}

	// A loader that breaks leaves the run failed, not running for ever.
	var broken iapp.RunDTO
	h.must(h.do("POST", "/api/imports/runs", "other", files("unitType,name\nInternalOrganization,Acme\nInternalOrganization,PANIC\nOffice,Central\n", ""), &broken), 201, "broken")
	if broken.Status != "failed" || !strings.Contains(broken.Reason, "boom") || count(t, broken.Counts, domain.KindLegalEntity).Created != 1 || broken.FinishedAt == "" {
		t.Fatalf("broken: %+v", broken)
	}

	// While a run of a source goes on, another does not start; previews do.
	result := make(chan error, 1)
	var blocked iapp.RunDTO
	go func() {
		out, err := h.imp.Service.Execute.Handle(h.adminCtx, iapp.RunImport{Source: "personio",
			Files: []iapp.FileDTO{{Role: "org-units", Name: "slow.csv", Content: "unitType,name\nInternalOrganization,BLOCK\n"}}})
		blocked = out
		result <- err
	}()
	<-w.started
	h.must(h.do("POST", "/api/imports/runs", "importer", files(orgUnits, ""), nil), 422, "in progress")
	h.must(h.do("POST", "/api/imports/preview", "importer", files(orgUnits, ""), nil), 200, "preview meanwhile")
	if closed, err := h.imp.Service.CloseStale.Handle(h.adminCtx, iapp.CloseStale{}); err != nil || len(closed.Runs) != 0 {
		t.Fatalf("a run that just started is not stale: %+v %v", closed, err)
	}
	if _, err := h.imp.Service.CloseStale.Handle(h.adminCtx, iapp.CloseStale{OlderThanMinutes: 1}); err == nil {
		t.Fatal("five minutes at least")
	}
	// Two hours later someone gives it up for dead; when it does end, it stays failed with what it did.
	restore := fw.SetClock(fake.New(fw.Now().Add(2 * time.Hour)))
	closed, err := h.imp.Service.CloseStale.Handle(h.adminCtx, iapp.CloseStale{})
	restore()
	if err != nil || len(closed.Runs) != 1 {
		t.Fatalf("stale: %+v %v", closed, err)
	}
	close(w.release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if blocked.ID != closed.Runs[0] || blocked.Status != "failed" || count(t, blocked.Counts, domain.KindLegalEntity).Created != 1 || blocked.Reason == "" {
		t.Fatalf("given up for dead: %+v", blocked)
	}
	h.must(h.do("POST", "/api/imports/runs", "importer", files(orgUnits, ""), &got), 201, "free again")

	// Each run that ended told the others, once.
	if n, err := h.imp.Relay(inprocess.NewBroker()).RelayOnce(context.Background()); err != nil || n != 5 {
		t.Fatalf("published: %d %v", n, err)
	}
}

func TestImports_RunsPreviewsAndReferences_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.scenario()

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{iinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	mount(t, h.sw).scenario() // a host that starts again: nothing is loaded until it says so
}

func TestImports_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/imports/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
