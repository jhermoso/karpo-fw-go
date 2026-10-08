package integration

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/exports"
	eapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	edomain "github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	einfra "github.com/jhermoso/karpo-fw-go/contexts/exports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

var permExported = authz.MustPermission("Parties.Customer.Read")

// exportedList plays the list of a context: rows of companies, shown to who can read each.
type exportedList struct {
	rows   map[fw.UUID][]string
	repeat int
	onPage func(page int)
	pages  int
}

func (l *exportedList) Key() string                  { return "customers" }
func (l *exportedList) Title() string                { return "Clientes" }
func (l *exportedList) Permission() authz.Permission { return permExported }
func (l *exportedList) Filters() []string            { return []string{"name"} }
func (l *exportedList) Columns() []edomain.Column {
	return []edomain.Column{{Field: "name", Header: "Nombre"}, {Field: "n", Header: "Nº", Type: edomain.Number}}
}

func (l *exportedList) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]edomain.Row, string, error) {
	l.pages++
	if l.onPage != nil {
		l.onPage(l.pages)
	}
	ac, _ := authz.FromContext(ctx)
	names := []string{}
	for org, rows := range l.rows {
		for _, n := range rows {
			if ac != nil && ac.HasPermission(permExported) && ac.CanRead(org) && strings.Contains(n, filters["name"]) {
				names = append(names, n)
			}
		}
	}
	total := len(names) * max(l.repeat, 1)
	from, _ := strconv.Atoi(cursor)
	out := []edomain.Row{}
	for i := from; i < total && i < from+limit; i++ {
		out = append(out, edomain.Row{"name": names[i%len(names)], "n": i + 1})
	}
	if from+limit < total {
		return out, strconv.Itoa(from + limit), nil
	}
	return out, "", nil
}

// TestExportsContext runs Exports on every engine: a job with its filters, the scope of who
// asked and its instants as each engine gives them back, the file written as that person by a
// worker who is somebody else, the limit of jobs at a time, cancellation while writing, the
// progress noted on a long list, expiry on a clock moved forward and the listings.
func TestExportsContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			einfra.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{einfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			files := einfra.NewMemoryFiles()
			acme, globex := fw.NewUUID(), fw.NewUUID()
			list := &exportedList{rows: map[fw.UUID][]string{acme: {"Añil, S.L."}, globex: {"Globex"}}}
			em := exports.Compose(hotswap.New(db), files).Offer(list)
			svc := em.Service
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			as := func(name string, global bool, perms []authz.Permission, orgs ...fw.UUID) context.Context {
				c := authz.Context{Subject: fw.NewUUID(), SubjectName: name, Kind: authz.Service, Permissions: perms, GlobalAdmin: global}
				for _, o := range orgs {
					c.Grants = append(c.Grants, authz.Grant{OrganizationID: o, Level: authz.ReadOnly})
				}
				ac, err := authz.NewContext(c)
				must(err)
				return authz.WithContext(ctx, ac)
			}
			clerk := as("Íñigo", false, []authz.Permission{eapp.PermJobRead, eapp.PermJobCreate, permExported}, acme)
			worker := as("worker", false, []authz.Permission{eapp.PermJobRun})
			admin := as("admin", true, []authz.Permission{authz.Wildcard})
			id := func(j eapp.JobDTO) edomain.JobID { x, _ := edomain.ParseJobID(j.ID); return x }
			read := func(c context.Context, j eapp.JobDTO) string {
				t.Helper()
				f, err := svc.Download.Handle(c, eapp.DownloadJob{ID: id(j)})
				must(err)
				defer f.Content.Close()
				b, _ := io.ReadAll(f.Content)
				return string(b)
			}

			first, err := svc.Start.Handle(clerk, eapp.StartExport{Dataset: "customers", Title: "Clientes de Añil", Filter: map[string]string{"name": "ñ"}})
			must(err)
			if _, err := svc.RunNext.Handle(clerk, eapp.RunNext{}); !errors.Is(err, fw.ErrForbidden) {
				t.Fatalf("asking is not writing: %v", err)
			}
			ran, err := svc.RunNext.Handle(worker, eapp.RunNext{})
			must(err)
			// As stored: who asked, the title, the instants; and the file, with the sight of who asked.
			got, err := svc.Get.Handle(clerk, eapp.GetJob{ID: id(first)})
			must(err)
			requested, _ := time.Parse(time.RFC3339, got.RequestedAt)
			finished, _ := time.Parse(time.RFC3339, got.FinishedAt)
			expires, _ := time.Parse(time.RFC3339, got.ExpiresAt)
			if ran.Job == nil || ran.Job.ID != first.ID || got.Status != "completed" || got.Rows != 1 || got.RequestedBy != "Íñigo" || got.Title != "Clientes de Añil" ||
				got.Version != 3 || finished.Before(requested) || expires.Sub(finished) != 24*time.Hour || fw.Now().Sub(requested) > time.Hour {
				t.Fatalf("stored job: %+v", got)
			}
			if body := read(clerk, first); body != string(rune(0xFEFF))+"Nombre,Nº\r\n\"Añil, S.L.\",1\r\n" {
				t.Fatalf("file: %q", body)
			}
			if _, err := svc.Get.Handle(worker, eapp.GetJob{ID: id(first)}); !errors.Is(err, fw.ErrForbidden) {
				t.Fatalf("the worker reads no job: %v", err)
			}
			if _, err := svc.Get.Handle(as("other", false, []authz.Permission{eapp.PermJobRead}), eapp.GetJob{ID: id(first)}); !errors.Is(err, fw.ErrNotFound) {
				t.Fatalf("somebody else: %v", err)
			}

			// A global administrator sees every company; the scope stored with the job is none.
			all, err := svc.Start.Handle(admin, eapp.StartExport{Dataset: "customers", Format: "xlsx"})
			must(err)
			ran, err = svc.RunNext.Handle(worker, eapp.RunNext{})
			if err != nil || ran.Job.Rows != 2 || !strings.HasSuffix(ran.Job.FileName, ".xlsx") || !strings.HasPrefix(read(admin, all), "PK") {
				t.Fatalf("sheet: %+v %v", ran.Job, err)
			}

			// Five at a time; the worker takes the one that has waited longest.
			queue := []eapp.JobDTO{}
			for range edomain.MaxActiveJobs {
				j, err := svc.Start.Handle(clerk, eapp.StartExport{Dataset: "customers"})
				must(err)
				queue = append(queue, j)
				time.Sleep(5 * time.Millisecond)
			}
			var rv *fw.RuleViolationError
			if _, err := svc.Start.Handle(clerk, eapp.StartExport{Dataset: "customers"}); !errors.As(err, &rv) || rv.Code != "exports.too_many_jobs" {
				t.Fatalf("too many: %v", err)
			}
			for _, j := range queue[1:] {
				c, err := svc.Cancel.Handle(clerk, eapp.CancelJob{ID: id(j)})
				if err != nil || c.Status != "cancelled" {
					t.Fatalf("cancel: %+v %v", c, err)
				}
			}
			// A long list: progress is noted on the way, and giving it up stops it without a file.
			list.repeat, list.pages = 30*eapp.BatchSize, 0
			var midway eapp.JobDTO
			list.onPage = func(page int) {
				if page == 12 {
					midway, _ = svc.Get.Handle(clerk, eapp.GetJob{ID: id(queue[0])})
					_, err := svc.Cancel.Handle(clerk, eapp.CancelJob{ID: id(queue[0])})
					must(err)
				}
			}
			ran, err = svc.RunNext.Handle(worker, eapp.RunNext{})
			if err != nil || ran.Job.ID != queue[0].ID || ran.Job.Status != "cancelled" || midway.Status != "processing" || midway.Rows != 10*eapp.BatchSize ||
				list.pages != 12 || len(files.Keys()) != 2 {
				t.Fatalf("cancelled while writing: %+v (midway %+v, %d pages, files %v) %v", ran.Job, midway, list.pages, files.Keys(), err)
			}
			if again, err := svc.RunNext.Handle(worker, eapp.RunNext{}); err != nil || again.Job != nil {
				t.Fatalf("the cancelled ones are passed by: %+v %v", again.Job, err)
			}

			// A day later the files are gone and the jobs say so.
			func() {
				defer fw.SetClock(fake.New(fw.Now().Add(25 * time.Hour)))()
				p, err := svc.Purge.Handle(worker, eapp.Purge{})
				if err != nil || len(p.Expired) != 2 || len(p.GivenUp) != 0 || len(files.Keys()) != 0 {
					t.Fatalf("purge: %+v %v", p, err)
				}
				if _, err := svc.Download.Handle(clerk, eapp.DownloadJob{ID: id(first)}); !errors.As(err, &rv) || rv.Code != "exports.no_file" {
					t.Fatalf("expired: %v", err)
				}
			}()
			mine, err := svc.List.Handle(clerk, eapp.ListJobs{Size: 50})
			if err != nil || mine.Total != 6 || mine.Items[5].ID != first.ID || mine.Items[5].Status != "expired" || mine.Items[0].ID != queue[4].ID {
				t.Fatalf("mine, the latest first: %+v %v", mine, err)
			}
			if everybody, err := svc.List.Handle(admin, eapp.ListJobs{All: true}); err != nil || everybody.Total != 7 {
				t.Fatalf("everybody's: %+v %v", everybody, err)
			}
			if trail, err := em.Audit.Trail(ctx, edomain.JobKind, first.ID); err != nil || len(trail) != 4 || trail[0].AggregateVersion != 1 || trail[3].AggregateVersion != 4 {
				t.Fatalf("audit: %+v %v", trail, err)
			}
		})
	}
}
