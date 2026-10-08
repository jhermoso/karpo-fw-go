package integration

import (
	"context"
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/imports"
	iapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	idomain "github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	iinfra "github.com/jhermoso/karpo-fw-go/contexts/imports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// importedWorld plays the contexts that own what is imported.
type importedWorld struct {
	mu     sync.Mutex
	things map[string]map[string]string
	hold   chan struct{}
	held   chan struct{}
}

type worldLoader struct {
	w    *importedWorld
	kind string
}

func (l worldLoader) Kind() string       { return l.kind }
func (l worldLoader) EntityType() string { return "world." + l.kind }
func (l worldLoader) Find(context.Context, idomain.Record, idomain.Refs) (string, error) {
	return "", nil
}

func (l worldLoader) Apply(_ context.Context, r idomain.Record, existing string, refs idomain.Refs) (string, idomain.Outcome, error) {
	if r.Key == "HOLD" {
		close(l.w.held)
		<-l.w.hold
	}
	if r.Kind == idomain.KindPerson && !strings.Contains(r.Fields["email"], "@") {
		return "", "", fw.Violation("parties.email", "not an email: "+r.Fields["email"]+" "+strings.Repeat("ñ", 1500))
	}
	if r.Kind == idomain.KindEmployment {
		if _, ok := refs.Lookup(idomain.KindPerson, idomain.GlobalScope, r.Key); !ok {
			return "", "", fw.Violation("hr.unknown_person", "the person of the employment does not exist")
		}
	}
	l.w.mu.Lock()
	defer l.w.mu.Unlock()
	if existing == "" {
		id := fw.NewUUID().String()
		l.w.things[id] = maps.Clone(r.Fields)
		return id, idomain.Created, nil
	}
	if maps.Equal(l.w.things[existing], r.Fields) {
		return existing, idomain.Unchanged, nil
	}
	l.w.things[existing] = maps.Clone(r.Fields)
	return existing, idomain.Updated, nil
}

// TestImportsContext runs Imports on every engine: a run with its counts and its messages (long
// texts cut, accents, lines) as each engine gives them back, the references that make the second
// run find what the first created (two keys that differ only in their scope), the run that changes
// one entity, the one in progress that stops another, and the one given up for dead.
func TestImportsContext(t *testing.T) {
	const org = "unitType,name,parentOrg,notes\nInternalOrganization,Maccorp Exact Change,,\nInternalOrganization,Karpo Servicios,,\n" +
		"Department,Operaciones,Maccorp Exact Change,\nDepartment,Operaciones,Karpo Servicios,homónimo\nDepartment,Fantasma,No Existe SL,\n"
	const header = "employeeNumber,firstName,lastName,preferredName,email,gender,legalEntity,department,workCenter,jobTitle,isSupervisor,notes,hireDate,terminationDate\n"
	const people = header + "P-1,Ana,García,,ana@maccorp.test,F,Maccorp Exact Change,Operaciones,,\"Auxiliar, Caja\",Sí,,01/03/2020,\n" +
		"P-2,Luis,Pérez,,luis@maccorp.test,M,Maccorp / Karpo (PLURIEMPLEO),Operaciones,,Cajero,no,,2021-05-10,\n" +
		"P-3,Eva,Ruiz,,eva-at-maccorp,F,Karpo Servicios,,,,,,,\n"
	const later = header + "P-1,Ana,García,,ana@maccorp.test,F,Maccorp Exact Change,Operaciones,,\"Auxiliar, Caja\",Sí,,01/03/2020,\n" +
		"P-2,Luis,Pérez,Luis P.,luis@maccorp.test,M,Maccorp / Karpo (PLURIEMPLEO),Operaciones,,Cajero,no,,2021-05-10,\n"
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			iinfra.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{iinfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			w := &importedWorld{things: map[string]map[string]string{}, hold: make(chan struct{}), held: make(chan struct{})}
			im := imports.Compose(hotswap.New(db)).Load(worldLoader{w, idomain.KindLegalEntity}, worldLoader{w, idomain.KindDepartment},
				worldLoader{w, idomain.KindPerson}, worldLoader{w, idomain.KindEmployment}, worldLoader{w, idomain.KindPosition})
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := im.Service
			run := func(orgCSV, peopleCSV string) iapp.RunImport {
				c := iapp.RunImport{Source: "personio", Files: []iapp.FileDTO{{Role: "org-units", Name: "org.csv", Content: orgCSV}}}
				if peopleCSV != "" {
					c.Files = append(c.Files, iapp.FileDTO{Role: "people", Name: "people.csv", Content: peopleCSV})
				}
				return c
			}
			count := func(cs []iapp.CountDTO, kind string) iapp.CountDTO {
				for _, c := range cs {
					if c.Kind == kind {
						return c
					}
				}
				return iapp.CountDTO{}
			}

			first, err := svc.Execute.Handle(actx, run(org, people))
			if err != nil {
				t.Fatal(err)
			}
			if first.Status != "completed-with-errors" || first.Errors != 3 || first.Version != 2 || count(first.Counts, idomain.KindPerson).Created != 2 ||
				count(first.Counts, idomain.KindPerson).Failed != 1 || count(first.Counts, idomain.KindEmployment).Created != 3 ||
				count(first.Counts, idomain.KindDepartment).Created != 2 {
				t.Fatalf("first run: %+v", first)
			}
			// As stored: the order of the kinds and of the messages, the cut text, the instants.
			got, err := svc.GetRun.Handle(actx, iapp.GetRun{ID: idomain.RunID{UUID: fw.MustParseUUID(first.ID)}})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Counts) != 7 || got.Counts[0].Kind != idomain.KindLegalEntity || got.Counts[2].Kind != idomain.KindWorkCenter || got.Counts[2].Read != 0 ||
				got.Counts[4] != (iapp.CountDTO{Kind: idomain.KindEmployment, Read: 4, Created: 3, Failed: 1}) {
				t.Fatalf("counts: %+v", got.Counts)
			}
			if len(got.Messages) != 3 || got.Messages[0].Code != "personio.unknown_legal_entity" || got.Messages[0].Line != 6 || got.Messages[0].File != "org.csv" ||
				got.Messages[1].Code != "parties.email" || got.Messages[1].Key != "P-3" || got.Messages[1].Line != 4 || len([]rune(got.Messages[1].Text)) != 1000 ||
				!strings.HasSuffix(got.Messages[1].Text, "ñ") || got.Messages[2].Code != "hr.unknown_person" || got.Messages[2].Entity != "" {
				t.Fatalf("messages: %+v", got.Messages)
			}
			started, _ := time.Parse(time.RFC3339, got.StartedAt)
			finished, _ := time.Parse(time.RFC3339, got.FinishedAt)
			if got.StartedAt != first.StartedAt || got.FinishedAt != first.FinishedAt || finished.Before(started) || fw.Now().Sub(started) > time.Hour ||
				got.StartedBy != "admin" || len(got.Files) != 2 || got.Files[1] != "people.csv" {
				t.Fatalf("stored run: %+v", got)
			}

			refs, err := svc.References.Handle(actx, iapp.SearchReferences{Source: "personio", Size: 50})
			if err != nil || refs.Total != 11 { // and the positions of Ana and Luis
				t.Fatalf("references: %+v %v", refs, err)
			}
			deps, err := svc.References.Handle(actx, iapp.SearchReferences{Kind: idomain.KindDepartment, Key: "Operaciones"})
			if err != nil || deps.Total != 2 || deps.Items[0].Scope != "Karpo Servicios" || deps.Items[1].Scope != "Maccorp Exact Change" ||
				deps.Items[0].EntityID == deps.Items[1].EntityID || deps.Items[0].FirstRun != first.ID {
				t.Fatalf("two departments of one name: %+v %v", deps, err)
			}

			// The loaders here find nothing by themselves: only the references make the second run
			// recognise what the first created.
			second, err := svc.Execute.Handle(actx, run(strings.Join(strings.Split(org, "\n")[:5], "\n")+"\n", later))
			if err != nil {
				t.Fatal(err)
			}
			if second.Status != "succeeded" || count(second.Counts, idomain.KindLegalEntity) != (iapp.CountDTO{Kind: idomain.KindLegalEntity, Read: 2, Unchanged: 2}) ||
				count(second.Counts, idomain.KindPerson) != (iapp.CountDTO{Kind: idomain.KindPerson, Read: 2, Updated: 1, Unchanged: 1}) ||
				count(second.Counts, idomain.KindEmployment).Unchanged != 3 || len(w.things) != 11 {
				t.Fatalf("second run: %+v (%d things)", second, len(w.things))
			}
			luis, err := svc.References.Handle(actx, iapp.SearchReferences{Kind: idomain.KindPerson, Key: "P-2"})
			if err != nil || luis.Total != 1 || luis.Items[0].FirstRun != first.ID || luis.Items[0].LastRun != second.ID {
				t.Fatalf("luis: %+v %v", luis, err)
			}
			byEntity, err := svc.References.Handle(actx, iapp.SearchReferences{Entity: luis.Items[0].EntityID})
			if err != nil || byEntity.Total != 1 {
				t.Fatalf("by entity: %+v %v", byEntity, err)
			}
			pre, err := svc.Preview.Handle(actx, run(org, people))
			if err != nil || count(pre.Counts, idomain.KindPerson).Unchanged != 2 || count(pre.Counts, idomain.KindPerson).Created != 1 || pre.Errors != 1 {
				t.Fatalf("preview: %+v %v", pre, err)
			}

			// A run in progress stops another of its source; given up for dead two hours later, it
			// stays failed when it ends, with what it did.
			result := make(chan error, 1)
			var slow iapp.RunDTO
			go func() {
				out, err := svc.Execute.Handle(actx, run("unitType,name\nInternalOrganization,HOLD\n", ""))
				slow = out
				result <- err
			}()
			<-w.held
			var rv *fw.RuleViolationError
			if _, err := svc.Execute.Handle(actx, run(org, "")); !errors.As(err, &rv) || rv.Code != "imports.run_in_progress" {
				t.Fatalf("in progress: %v", err)
			}
			running, err := svc.SearchRuns.Handle(actx, iapp.SearchRuns{Status: "running"})
			if err != nil || running.Total != 1 || running.Items[0].FinishedAt != "" {
				t.Fatalf("running: %+v %v", running, err)
			}
			if closed, err := svc.CloseStale.Handle(actx, iapp.CloseStale{}); err != nil || len(closed.Runs) != 0 {
				t.Fatalf("not stale yet: %+v %v", closed, err)
			}
			restore := fw.SetClock(fake.New(fw.Now().Add(2 * time.Hour)))
			closed, err := svc.CloseStale.Handle(actx, iapp.CloseStale{})
			restore()
			if err != nil || len(closed.Runs) != 1 {
				t.Fatalf("stale: %+v %v", closed, err)
			}
			close(w.hold)
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			if slow.ID != closed.Runs[0] || slow.Status != "failed" || slow.Reason == "" || count(slow.Counts, idomain.KindLegalEntity).Created != 1 {
				t.Fatalf("given up for dead: %+v", slow)
			}

			all, err := svc.SearchRuns.Handle(actx, iapp.SearchRuns{Source: "personio"})
			if err != nil || all.Total != 3 || all.Items[0].StartedAt < all.Items[1].StartedAt || all.Items[1].StartedAt < all.Items[2].StartedAt {
				t.Fatalf("runs, the latest first: %+v %v", all, err)
			}
			if n, err := im.Relay(inprocess.NewBroker()).RelayOnce(ctx); err != nil || n != 3 {
				t.Fatalf("published: %d %v", n, err)
			}
		})
	}
}
