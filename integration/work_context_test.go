package integration

import (
	"context"
	"errors"
	"testing"

	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	asinfra "github.com/jhermoso/karpo-fw-go/contexts/assets/infrastructure"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	fcinfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	hinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	yinfra "github.com/jhermoso/karpo-fw-go/contexts/payments/infrastructure"
	prinfra "github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
	uinfra "github.com/jhermoso/karpo-fw-go/contexts/purchases/infrastructure"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	tinfra "github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/work"
	wapp "github.com/jhermoso/karpo-fw-go/contexts/work/application"
	wdomain "github.com/jhermoso/karpo-fw-go/contexts/work/domain"
	winfra "github.com/jhermoso/karpo-fw-go/contexts/work/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestWorkContext runs Work with Parties on every engine: a project with a task, the round trip
// of the plan with its optional dates, the assignments and the history in order, time recorded,
// corrected, withdrawn and approved, the daily limit over the SQL rows, completion with the hours
// and cost of the approved time, the searches and the Published Language in the outbox.
func TestWorkContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			winfra.DropAll(ctx, db)
			asinfra.DropAll(ctx, db)
			uinfra.DropAll(ctx, db)
			yinfra.DropAll(ctx, db)
			ainfra.DropAll(ctx, db)
			tinfra.DropAll(ctx, db)
			rinfra.DropAll(ctx, db)
			binfra.DropAll(ctx, db)
			finfra.DropAll(ctx, db)
			prinfra.DropAll(ctx, db)
			hinfra.DropAll(ctx, db)
			fcinfra.DropAll(ctx, db)
			ginfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), winfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			sw := hotswap.New(db)
			pm := parties.Compose(sw, nil)
			wm := work.Compose(sw)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			violates := func(err error, what string) {
				t.Helper()
				if !errors.Is(err, fw.ErrRuleViolation) {
					t.Fatalf("%s: %v", what, err)
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := wm.Service
			day := func(d int) vocab.Date { return vocab.MustDate(2026, 3, d) }

			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme, S.A.", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			client, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Talleres Vega, S.L."})
			must(err)
			ana, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Ruiz"})
			must(err)
			luis, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Luis", FirstSurname: "Gil"})
			must(err)

			prj, err := svc.Open.Handle(actx, wapp.OpenWork{Company: acme.ID, Code: "prj-01", Kind: "project", On: day(2),
				PlanInput: wapp.PlanInput{Name: "Reforma de la nave", Purpose: "improvement", Customer: client.ID, EstimatedHours: "120", Budget: "6000"}})
			must(err)
			_, err = svc.Open.Handle(actx, wapp.OpenWork{Company: acme.ID, Code: "PRJ-01", Kind: "project", On: day(2), PlanInput: wapp.PlanInput{Name: "Otra"}})
			violates(err, "code used once")
			task, err := svc.Open.Handle(actx, wapp.OpenWork{Company: acme.ID, Code: "T-01", Kind: "task", Parent: prj.ID, On: day(2),
				PlanInput: wapp.PlanInput{Name: "Demolición", Customer: client.ID, PlannedStart: day(9), PlannedEnd: day(10), EstimatedHours: "16"}})
			must(err)
			tid, _ := wdomain.ParseWorkID(task.ID)
			pid, _ := wdomain.ParseWorkID(prj.ID)
			_, err = svc.Assign.Handle(actx, wapp.AssignPerson{ID: tid, Person: ana.ID, Role: "Oficial", Rate: "25", From: day(9)})
			must(err)
			_, err = svc.Assign.Handle(actx, wapp.AssignPerson{ID: tid, Person: luis.ID, Rate: "15", From: day(9)})
			must(err)
			_, err = svc.Progress.Handle(actx, wapp.ProgressWork{ID: tid, To: "scheduled", On: day(3)})
			must(err)
			_, err = svc.Progress.Handle(actx, wapp.ProgressWork{ID: tid, To: "in-progress", On: day(9)})
			must(err)
			_, err = svc.Progress.Handle(actx, wapp.ProgressWork{ID: pid, To: "in-progress", On: day(9)})
			must(err)

			ta, err := svc.Record.Handle(actx, wapp.RecordTime{Work: task.ID, Person: ana.ID, Date: day(9), Hours: "7.5", Comment: "Demolición"})
			must(err)
			_, err = svc.Record.Handle(actx, wapp.RecordTime{Work: task.ID, Person: ana.ID, Date: day(9), Hours: "17"})
			violates(err, "more than 24 hours in a day")
			tl, err := svc.Record.Handle(actx, wapp.RecordTime{Work: task.ID, Person: luis.ID, Date: day(9), Hours: "8"})
			must(err)
			aid, _ := wdomain.ParseTimeEntryID(ta.ID)
			lid, _ := wdomain.ParseTimeEntryID(tl.ID)
			ta, err = svc.Correct.Handle(actx, wapp.CorrectTime{ID: aid, Hours: "8", Billable: true, Comment: "Demolición y desescombro"})
			if err != nil || ta.Cost != "200.00" || ta.Version != 2 || ta.Comment != "Demolición y desescombro" {
				t.Fatalf("correct: %+v %v", ta, err)
			}
			_, err = svc.Approve.Handle(actx, wapp.ApproveTime{ID: aid})
			must(err)
			_, err = svc.Correct.Handle(actx, wapp.CorrectTime{ID: aid, Hours: "1"})
			violates(err, "approved time is final")
			_, err = svc.Progress.Handle(actx, wapp.ProgressWork{ID: tid, To: "completed", On: day(10)})
			violates(err, "time waiting for approval")
			_, err = svc.Withdraw.Handle(actx, wapp.WithdrawTime{ID: lid})
			must(err)
			tl, err = svc.Record.Handle(actx, wapp.RecordTime{Work: task.ID, Person: luis.ID, Date: day(10), Hours: "4"})
			must(err)
			lid, _ = wdomain.ParseTimeEntryID(tl.ID)
			_, err = svc.Approve.Handle(actx, wapp.ApproveTime{ID: lid})
			must(err)
			_, err = svc.Release.Handle(actx, wapp.ReleasePerson{ID: tid, Person: luis.ID, Until: day(10)})
			must(err)
			_, err = svc.Progress.Handle(actx, wapp.ProgressWork{ID: pid, To: "completed", On: day(10)})
			violates(err, "open parts")
			done, err := svc.Progress.Handle(actx, wapp.ProgressWork{ID: tid, To: "completed", On: day(10)})
			if err != nil || done.Hours != "12.00" || done.Cost != "260.00" || done.Status != "completed" {
				t.Fatalf("completed: %+v %v", done, err)
			}

			got, err := svc.GetWork.Handle(actx, wapp.GetWork{ID: tid})
			if err != nil || got.Code != "T-01" || got.Parent != prj.ID || got.Customer != client.ID || got.PlannedStart != "2026-03-09" ||
				got.PlannedEnd != "2026-03-10" || got.EstimatedHours != "16.00" || got.Started != "2026-03-09" || got.Finished != "2026-03-10" ||
				len(got.Assignments) != 2 || got.Assignments[0].Person != ana.ID || got.Assignments[0].Role != "Oficial" || got.Assignments[0].Until != "" ||
				got.Assignments[1].Rate != "15.00" || got.Assignments[1].Until != "2026-03-10" || len(got.History) != 4 || got.History[0].Status != "created" ||
				got.History[1].On != "2026-03-03" || got.History[3].Status != "completed" || got.Hours != "12.00" || got.PendingHours != "0.00" {
				t.Fatalf("task round trip: %+v %v", got, err)
			}
			p, err := svc.GetWork.Handle(actx, wapp.GetWork{ID: pid})
			if err != nil || p.PlannedStart != "" || p.Purpose != "improvement" || p.Budget != "6000.00" || p.Parent != "" || p.Finished != "" || p.Status != "in-progress" {
				t.Fatalf("project round trip: %+v %v", p, err)
			}
			_, err = svc.Progress.Handle(actx, wapp.ProgressWork{ID: pid, To: "cancelled", On: day(12), Reason: "El cliente desiste"})
			must(err)

			parts, err := svc.SearchWorks.Handle(actx, wapp.SearchWorks{Company: acme.ID, Parent: prj.ID, Status: "completed"})
			if err != nil || len(parts.Items) != 1 || parts.Items[0].Code != "T-01" {
				t.Fatalf("parts: %+v %v", parts.Items, err)
			}
			all, err := svc.SearchWorks.Handle(actx, wapp.SearchWorks{Company: acme.ID, Customer: client.ID})
			if err != nil || len(all.Items) != 2 || all.Items[0].Code != "PRJ-01" || all.Items[0].CancelReason != "El cliente desiste" {
				t.Fatalf("work of the customer: %+v %v", all.Items, err)
			}
			times, err := svc.SearchTime.Handle(actx, wapp.SearchTime{Work: task.ID, From: "2026-03-10", To: "2026-03-31"})
			if err != nil || len(times.Items) != 1 || times.Items[0].Person != luis.ID || times.Items[0].Hours != "4.00" || !times.Items[0].Approved ||
				!times.Items[0].Billable {
				t.Fatalf("time from the 10th: %+v %v", times.Items, err)
			}
			sheet, err := svc.Timesheet.Handle(actx, wapp.GetTimesheet{Company: acme.ID, Person: ana.ID, From: "2026-03-01", To: "2026-03-31"})
			if err != nil || len(sheet.Entries) != 1 || sheet.Hours != "8.00" || sheet.ApprovedHours != "8.00" || sheet.Cost != "200.00" {
				t.Fatalf("timesheet: %+v %v", sheet, err)
			}
			// Two approvals and one completion.
			if n, err := wm.Relay(inprocess.NewBroker()).RelayOnce(ctx); err != nil || n != 3 {
				t.Fatalf("published: %d %v", n, err)
			}
		})
	}
}
