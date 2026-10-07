package exports_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/exports"
	eapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	"github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	einfra "github.com/jhermoso/karpo-fw-go/contexts/exports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

var permCustomers = authz.MustPermission("Parties.Customer.Read")

type customer struct {
	org    fw.UUID
	name   string
	taxID  string
	credit string
	since  time.Time
	active bool
}

// customers plays the list of a context: its rows belong to companies and it shows each caller
// those of the companies they can read.
type customers struct {
	key    string
	rows   []customer
	repeat int                  // the rows, this many times over
	onPage func(page int) error // what happens while a page is read
	mu     sync.Mutex
	pages  int
}

func (c *customers) Key() string                  { return c.key }
func (c *customers) Title() string                { return "Clientes" }
func (c *customers) Permission() authz.Permission { return permCustomers }
func (c *customers) Filters() []string            { return []string{"name", "active"} }
func (c *customers) Columns() []domain.Column {
	return []domain.Column{{Field: "name", Header: "Nombre"}, {Field: "taxId", Header: "CIF/NIF"}, {Field: "credit", Header: "Crédito", Type: domain.Number},
		{Field: "since", Header: "Alta", Type: domain.Date}, {Field: "active", Header: "Activo", Type: domain.Boolean}}
}

func (c *customers) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]domain.Row, string, error) {
	c.mu.Lock()
	c.pages++
	page := c.pages
	c.mu.Unlock()
	if c.onPage != nil {
		if err := c.onPage(page); err != nil {
			return nil, "", err
		}
	}
	ac, ok := authz.FromContext(ctx)
	if !ok || !ac.HasPermission(permCustomers) {
		return nil, "", fw.ErrForbidden
	}
	visible := []customer{}
	for _, r := range c.rows {
		if ac.CanRead(r.org) && strings.Contains(strings.ToLower(r.name), strings.ToLower(filters["name"])) &&
			(filters["active"] == "" || filters["active"] == strconv.FormatBool(r.active)) {
			visible = append(visible, r)
		}
	}
	total := len(visible) * max(c.repeat, 1)
	from, _ := strconv.Atoi(cursor)
	out := []domain.Row{}
	for i := from; i < total && i < from+limit; i++ {
		r := visible[i%len(visible)]
		out = append(out, domain.Row{"name": r.name, "taxId": r.taxID, "credit": r.credit, "since": r.since, "active": r.active})
	}
	next := ""
	if from+limit < total {
		next = strconv.Itoa(from + limit)
	}
	return out, next, nil
}

type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	exp      *exports.Module
	files    *einfra.MemoryFiles
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	return mount(t, sw)
}

// mount builds a host on a backend: its own lists and files, the storage of sw.
func mount(t *testing.T, sw *hotswap.Switch) *host {
	files := einfra.NewMemoryFiles()
	em := exports.Compose(sw, files)
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("exports-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, exp: em, files: files}
	users := map[string][]authz.Permission{
		"clerk":    {eapp.PermJobRead, eapp.PermJobCreate, permCustomers},
		"other":    {eapp.PermJobRead, eapp.PermJobCreate, permCustomers},
		"limited":  {eapp.PermJobRead, eapp.PermJobCreate},
		"outsider": {eapp.PermJobRead, eapp.PermJobCreate, permCustomers},
		"worker":   {eapp.PermJobRun},
	}
	for u, perms := range users {
		h.ids[u] = fw.NewUUID()
		dir.Put(h.ids[u], authz.Subject{Active: true, Permissions: perms})
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	ac.GlobalAdmin = true
	h.adminCtx = authz.WithContext(context.Background(), ac)
	mux := http.NewServeMux()
	em.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *host) grant(user string, org fw.UUID) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	s.Grants = append(s.Grants, authz.Grant{OrganizationID: org, Level: authz.ReadOnly})
	h.dir.Put(h.ids[user], s)
}

// do sends a request; out is a DTO to decode into (emptied first) or a *http.Response holder.
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
		switch o := out.(type) {
		case *download:
			o.header = res.Header
			o.body, _ = io.ReadAll(res.Body)
			return res.StatusCode
		case *eapp.JobDTO:
			*o = eapp.JobDTO{}
		case *eapp.RanDTO:
			*o = eapp.RanDTO{}
		}
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			h.t.Fatal(err)
		}
	}
	return res.StatusCode
}

type download struct {
	header http.Header
	body   []byte
}

func (h *host) must(got, want int, what string) {
	h.t.Helper()
	if got != want {
		h.t.Fatalf("%s: status %d, want %d", what, got, want)
	}
}

// start asks for an export as clerk and returns the job.
func (h *host) start(user string, body map[string]any) eapp.JobDTO {
	h.t.Helper()
	var j eapp.JobDTO
	h.must(h.do("POST", "/api/exports", user, body, &j), 202, "start")
	return j
}

// work makes the worker write the next job.
func (h *host) work() eapp.JobDTO {
	h.t.Helper()
	var ran eapp.RanDTO
	h.must(h.do("POST", "/api/exports/run-next", "worker", nil, &ran), 200, "run next")
	if ran.Job == nil {
		h.t.Fatal("no job waited")
	}
	return *ran.Job
}

func (h *host) scenario() {
	t := h.t
	acme, globex := fw.NewUUID(), fw.NewUUID()
	for _, u := range []string{"clerk", "other", "limited"} {
		h.grant(u, acme)
	}
	h.grant("outsider", fw.NewUUID())
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	list := &customers{key: "customers", rows: []customer{
		{org: acme, name: "Acme, S.A.", taxID: "A12345678", credit: "1500.50", since: day("2020-03-01"), active: true},
		{org: globex, name: "Globex", taxID: "B87654321", credit: "0", since: day("2021-01-01"), active: true},
		{org: acme, name: "=cmd|' /C calc'!A0", taxID: "", credit: "-20", since: day("2024-12-31"), active: false},
		{org: acme, name: "Ñu \"Hermanos\"", taxID: "X1234567L", credit: "sin límite", active: true},
	}}
	h.exp.Offer(list)
	svc := h.exp.Service

	// What can be exported is what one may read.
	var sets []eapp.DatasetDTO
	h.must(h.do("GET", "/api/exports/datasets", "worker", nil, nil), 403, "worker")
	h.must(h.do("GET", "/api/exports/datasets", "limited", nil, &sets), 200, "limited")
	if len(sets) != 0 {
		t.Fatalf("limited sees: %+v", sets)
	}
	h.must(h.do("GET", "/api/exports/datasets", "clerk", nil, &sets), 200, "clerk")
	if len(sets) != 1 || sets[0].Key != "customers" || len(sets[0].Columns) != 5 || sets[0].Columns[2].Type != "number" || len(sets[0].Filters) != 2 {
		t.Fatalf("datasets: %+v", sets)
	}

	// Asking.
	ask := map[string]any{"dataset": "customers"}
	h.must(h.do("POST", "/api/exports", "worker", ask, nil), 403, "the worker asks for nothing")
	h.must(h.do("POST", "/api/exports", "limited", ask, nil), 403, "may not read the list")
	h.must(h.do("POST", "/api/exports", "clerk", map[string]any{"dataset": "invoices"}, nil), 400, "unknown list")
	h.must(h.do("POST", "/api/exports", "clerk", map[string]any{"dataset": "customers", "format": "pdf"}, nil), 400, "format")
	h.must(h.do("POST", "/api/exports", "clerk", map[string]any{"dataset": "customers", "filter": map[string]string{"city": "x"}}, nil), 400, "unknown filter")
	h.must(h.do("POST", "/api/exports", "clerk", map[string]any{"dataset": "customers", "columns": []string{"name"}}, nil), 400, "unknown field")
	first := h.start("clerk", ask)
	if first.Status != "queued" || first.Format != "csv" || first.RequestedBy != "clerk" || first.Rows != 0 || first.FileName != "" {
		t.Fatalf("queued: %+v", first)
	}
	h.must(h.do("GET", "/api/exports/"+first.ID+"/download", "clerk", nil, nil), 422, "nothing to download yet")

	// The file is written with the sight of who asked when they asked: being given another
	// company afterwards changes nothing.
	h.grant("clerk", globex)
	h.must(h.do("POST", "/api/exports/run-next", "clerk", nil, nil), 403, "asking is not writing")
	done := h.work()
	if done.ID != first.ID || done.Status != "completed" || done.Rows != 3 || !strings.HasPrefix(done.FileName, "export-customers-") ||
		!strings.HasSuffix(done.FileName, ".csv") || done.Size == 0 || done.ExpiresAt == "" || done.StartedAt == "" {
		t.Fatalf("written: %+v", done)
	}
	var ran eapp.RanDTO
	h.must(h.do("POST", "/api/exports/run-next", "worker", nil, &ran), 200, "nothing waits")
	if ran.Job != nil {
		t.Fatalf("nothing waited: %+v", ran)
	}

	// Handing it over: to who asked, and to nobody else.
	var file download
	h.must(h.do("GET", "/api/exports/"+first.ID+"/download", "other", nil, nil), 404, "somebody else")
	h.must(h.do("GET", "/api/exports/"+first.ID, "other", nil, nil), 404, "somebody else")
	h.must(h.do("POST", "/api/exports/"+first.ID+"/cancel", "other", nil, nil), 404, "somebody else")
	h.must(h.do("GET", "/api/exports/"+first.ID+"/download", "worker", nil, nil), 403, "the worker reads nothing")
	h.must(h.do("GET", "/api/exports/"+fw.NewUUID().String()+"/download", "clerk", nil, nil), 404, "no such job")
	h.must(h.do("GET", "/api/exports/nope", "clerk", nil, nil), 400, "bad id")
	h.must(h.do("GET", "/api/exports/"+first.ID+"/download", "clerk", nil, &file), 200, "download")
	want := string(rune(0xFEFF)) + "Nombre,CIF/NIF,Crédito,Alta,Activo\r\n" +
		"\"Acme, S.A.\",A12345678,1500.50,2020-03-01,Sí\r\n" +
		"'=cmd|' /C calc'!A0,,-20,2024-12-31,No\r\n" +
		"\"Ñu \"\"Hermanos\"\"\",X1234567L,sin límite,,Sí\r\n"
	if string(file.body) != want {
		t.Fatalf("file:\n%q\nwant\n%q", file.body, want)
	}
	if file.header.Get("Content-Type") != "text/csv; charset=utf-8" || !strings.Contains(file.header.Get("Content-Disposition"), "attachment; filename="+done.FileName) ||
		file.header.Get("Cache-Control") != "no-store" || file.header.Get("Content-Length") != strconv.Itoa(len(want)) || int(done.Size) != len(want) {
		t.Fatalf("headers: %+v", file.header)
	}
	id, _ := domain.ParseJobID(first.ID)
	if admin, err := svc.Download.Handle(h.adminCtx, eapp.DownloadJob{ID: id}); err != nil || admin.Name != done.FileName {
		t.Fatalf("a global administrator: %v", err)
	} else {
		admin.Content.Close()
	}
	// Who exported what stays written.
	if trail, err := h.exp.Audit.Trail(context.Background(), domain.JobKind, first.ID); err != nil || len(trail) != 3 || trail[0].Actor.Name != "clerk" {
		t.Fatalf("audit: %+v %v", trail, err)
	}

	// A sheet, filtered and with its title; now clerk sees both companies.
	sheet := h.start("clerk", map[string]any{"dataset": "customers", "format": "XLSX", "title": "Clientes activos", "filter": map[string]string{"active": "true", "name": " "}})
	done = h.work()
	if done.ID != sheet.ID || done.Rows != 3 || !strings.HasSuffix(done.FileName, ".xlsx") {
		t.Fatalf("sheet: %+v", done)
	}
	h.must(h.do("GET", "/api/exports/"+sheet.ID+"/download", "clerk", nil, &file), 200, "download the sheet")
	z, err := zip.NewReader(bytes.NewReader(file.body), int64(len(file.body)))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, f := range z.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(b)
	}
	xml := parts["xl/worksheets/sheet1.xml"]
	if !strings.Contains(parts["xl/workbook.xml"], `name="Clientes activos"`) || !strings.Contains(xml, `<c r="C2"><v>1500.50</v></c>`) ||
		!strings.Contains(xml, ">Globex<") || !strings.Contains(xml, `<row r="4">`) || strings.Contains(xml, `<row r="5">`) || strings.Contains(xml, "calc") ||
		!strings.HasPrefix(file.header.Get("Content-Type"), "application/vnd.openxmlformats") {
		t.Fatalf("sheet: %s\n%s", parts["xl/workbook.xml"], xml)
	}

	// Who sees no company gets the header and nothing else.
	empty := h.start("outsider", ask)
	if done = h.work(); done.ID != empty.ID || done.Rows != 0 || done.Status != "completed" {
		t.Fatalf("empty: %+v", done)
	}
	h.must(h.do("GET", "/api/exports/"+empty.ID+"/download", "outsider", nil, &file), 200, "download")
	if string(file.body) != string(rune(0xFEFF))+"Nombre,CIF/NIF,Crédito,Alta,Activo\r\n" {
		t.Fatalf("empty file: %q", file.body)
	}

	// Giving up a job that waits: the worker passes it by.
	var gone eapp.JobDTO
	waiting := h.start("other", ask)
	h.must(h.do("POST", "/api/exports/"+waiting.ID+"/cancel", "other", nil, &gone), 200, "cancel")
	if gone.Status != "cancelled" || gone.FinishedAt == "" {
		t.Fatalf("cancelled: %+v", gone)
	}
	version := gone.Version
	h.must(h.do("POST", "/api/exports/"+waiting.ID+"/cancel", "other", nil, &gone), 200, "cancel again")
	if gone.Version != version {
		t.Fatalf("cancelling twice changes nothing: %+v", gone)
	}
	h.must(h.do("POST", "/api/exports/run-next", "worker", nil, &ran), 200, "nothing waits")
	if ran.Job != nil {
		t.Fatalf("a cancelled job is not written: %+v", ran)
	}
	h.must(h.do("GET", "/api/exports/"+waiting.ID+"/download", "other", nil, nil), 422, "cancelled")

	// Five at a time each.
	mine := []eapp.JobDTO{}
	for range domain.MaxActiveJobs {
		mine = append(mine, h.start("other", ask))
	}
	h.must(h.do("POST", "/api/exports", "other", ask, nil), 422, "too many")
	h.must(h.do("POST", "/api/exports", "clerk", ask, &gone), 202, "somebody else may")
	for _, j := range append(mine, gone) {
		jid, _ := domain.ParseJobID(j.ID)
		if _, err := svc.Cancel.Handle(h.adminCtx, eapp.CancelJob{ID: jid}); err != nil {
			t.Fatal(err)
		}
	}

	// Given up while it is being written: it stops at the next page and leaves no file.
	long := &customers{key: "long", rows: list.rows, repeat: 2000}
	h.exp.Offer(long)
	var cancelled eapp.JobDTO
	long.onPage = func(page int) error {
		if page == 2 {
			h.must(h.do("POST", "/api/exports/"+cancelled.ID+"/cancel", "clerk", nil, nil), 200, "cancel while writing")
		}
		return nil
	}
	cancelled = h.start("clerk", map[string]any{"dataset": "long"})
	if done = h.work(); done.ID != cancelled.ID || done.Status != "cancelled" || done.Rows != 0 || done.FileName != "" {
		t.Fatalf("cancelled while writing: %+v", done)
	}
	if long.pages != 2 {
		t.Fatalf("it stopped after the page it was reading: %d", long.pages)
	}

	// A list that breaks: the person is told it failed, the worker is told why.
	long.pages, long.onPage = 0, func(int) error { return errors.New("dial tcp 10.0.0.7:5432: password authentication failed") }
	broken := h.start("clerk", map[string]any{"dataset": "long"})
	h.must(h.do("POST", "/api/exports/run-next", "worker", nil, nil), 500, "the worker learns it failed")
	h.must(h.do("GET", "/api/exports/"+broken.ID, "clerk", nil, &gone), 200, "failed")
	if gone.Status != "failed" || gone.Error != "the file could not be written" || strings.Contains(gone.Error, "password") {
		t.Fatalf("failed: %+v", gone)
	}

	// More rows than a sheet holds: it fails saying so, to the person too.
	long.pages, long.onPage, long.repeat = 0, nil, domain.MaxRows/4+1
	huge := h.start("clerk", map[string]any{"dataset": "long"})
	if done = h.work(); done.ID != huge.ID || done.Status != "failed" || !strings.Contains(done.Error, "narrow it") {
		t.Fatalf("too many rows: %+v", done)
	}

	// A job nobody finishes writing is given up; when its worker comes back, it stays so.
	long.pages, long.repeat = 0, 2000
	hold, held := make(chan struct{}), make(chan struct{})
	long.onPage = func(page int) error {
		if page == 1 {
			close(held)
			<-hold
		}
		return nil
	}
	stuck := h.start("clerk", map[string]any{"dataset": "long"})
	result := make(chan eapp.RanDTO, 1)
	go func() {
		out, _ := svc.RunNext.Handle(h.adminCtx, eapp.RunNext{})
		result <- out
	}()
	<-held
	if p, err := svc.Purge.Handle(h.adminCtx, eapp.Purge{}); err != nil || len(p.GivenUp) != 0 || len(p.Expired) != 0 {
		t.Fatalf("nothing to purge yet: %+v %v", p, err)
	}
	restore := fw.SetClock(fake.New(fw.Now().Add(31 * time.Minute)))
	p, err := svc.Purge.Handle(h.adminCtx, eapp.Purge{})
	restore()
	if err != nil || len(p.GivenUp) != 1 || p.GivenUp[0] != stuck.ID || len(p.Expired) != 0 {
		t.Fatalf("given up: %+v %v", p, err)
	}
	close(hold)
	if out := <-result; out.Job == nil || out.Job.ID != stuck.ID || out.Job.Status != "failed" || !strings.Contains(out.Job.Error, "too long") {
		t.Fatalf("its worker comes back: %+v", out.Job)
	}

	// Only what was completed keeps a file; a day later, none.
	if keys := h.files.Keys(); len(keys) != 3 {
		t.Fatalf("files kept: %v", keys)
	}
	h.must(h.do("POST", "/api/exports/purge", "clerk", nil, nil), 403, "asking is not purging")
	restore = fw.SetClock(fake.New(fw.Now().Add(25 * time.Hour)))
	p, err = svc.Purge.Handle(h.adminCtx, eapp.Purge{})
	_, late := svc.Download.Handle(h.adminCtx, eapp.DownloadJob{ID: id})
	restore()
	var rv *fw.RuleViolationError
	if err != nil || len(p.Expired) != 3 || len(h.files.Keys()) != 0 || !errors.As(late, &rv) || rv.Code != "exports.no_file" {
		t.Fatalf("expired: %+v %v %v", p, err, late)
	}
	h.must(h.do("GET", "/api/exports/"+first.ID, "clerk", nil, &gone), 200, "expired")
	if gone.Status != "expired" || gone.Rows != 3 || gone.FileName == "" {
		t.Fatalf("expired: %+v", gone)
	}

	// Each one's jobs, the latest first.
	var page fw.Page[eapp.JobDTO]
	h.must(h.do("GET", "/api/exports?size=50", "clerk", nil, &page), 200, "mine")
	if page.Total != 7 || page.Items[0].ID != stuck.ID || page.Items[6].ID != first.ID {
		t.Fatalf("mine: %d %+v", page.Total, page.Items)
	}
	h.must(h.do("GET", "/api/exports", "outsider", nil, &page), 200, "outsider's")
	if page.Total != 1 || page.Items[0].ID != empty.ID {
		t.Fatalf("outsider's: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/exports?all=true", "clerk", nil, nil), 403, "everybody's")
	if all, err := svc.List.Handle(h.adminCtx, eapp.ListJobs{All: true, Size: 50}); err != nil || all.Total != 14 {
		t.Fatalf("everybody's: %d %v", all.Total, err)
	}
}

func TestExports_AskWriteDownloadPurge_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{einfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	mount(t, h.sw).scenario()
}

func TestDiskFiles(t *testing.T) {
	ctx := context.Background()
	files, err := einfra.NewDiskFiles(filepath.Join(t.TempDir(), "exports"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := files.Create(ctx, "a.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("hola"))
	_ = w.Close()
	r, err := files.Open(ctx, "a.csv")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != "hola" {
		t.Fatalf("read: %q", b)
	}
	if err := files.Remove(ctx, "a.csv"); err != nil {
		t.Fatal(err)
	}
	if err := files.Remove(ctx, "a.csv"); err != nil {
		t.Fatalf("removing what is not there is fine: %v", err)
	}
	if _, err := files.Open(ctx, "a.csv"); err == nil {
		t.Fatal("removed")
	}
	// A key is a plain file name: nothing leaves the folder.
	for _, key := range []string{"", "..", "../a.csv", "sub/a.csv", `..\a.csv`} {
		if _, err := files.Create(ctx, key); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("key %q: %v", key, err)
		}
	}
}

func TestExports_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/exports/domain")
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
