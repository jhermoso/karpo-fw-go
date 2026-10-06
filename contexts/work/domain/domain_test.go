package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/work/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func dec(s string) vocab.Decimal { return vocab.MustDecimal(s) }

func date(s string) vocab.Date {
	d, err := vocab.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

var (
	company = domain.OrganizationID{UUID: fw.NewUUID()}
	ana     = domain.PartyID{UUID: fw.NewUUID()}
	luis    = domain.PartyID{UUID: fw.NewUUID()}
)

func plan() domain.Plan {
	return domain.Plan{Name: "Reforma de la nave", Purpose: "improvement", EstimatedHours: dec("120"), Budget: dec("6000")}
}

func open(t *testing.T) *domain.Work {
	t.Helper()
	w, err := domain.OpenWork(domain.NewWorkID(), company, " prj-01 ", domain.Project, domain.WorkID{}, plan(), date("2026-03-02"))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestWork_Rules(t *testing.T) {
	bad := []func(*domain.Plan){
		func(p *domain.Plan) { p.Name = " " },
		func(p *domain.Plan) { p.Purpose = "fun" },
		func(p *domain.Plan) { p.PlannedEnd = date("2026-03-10") },
		func(p *domain.Plan) { p.PlannedStart, p.PlannedEnd = date("2026-03-10"), date("2026-03-09") },
		func(p *domain.Plan) { p.EstimatedHours = dec("-1") },
		func(p *domain.Plan) { p.Budget = dec("1.001") },
	}
	for i, f := range bad {
		p := plan()
		f(&p)
		if _, err := domain.OpenWork(domain.NewWorkID(), company, "PRJ-01", domain.Project, domain.WorkID{}, p, date("2026-03-02")); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if _, err := domain.OpenWork(domain.NewWorkID(), company, "PRJ 01", domain.Project, domain.WorkID{}, plan(), date("2026-03-02")); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("code: %v", err)
	}
	if _, err := domain.OpenWork(domain.NewWorkID(), company, "PRJ-01", "party", domain.WorkID{}, plan(), date("2026-03-02")); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("kind: %v", err)
	}
	id := domain.NewWorkID()
	if _, err := domain.OpenWork(id, company, "PRJ-01", domain.Task, id, plan(), date("2026-03-02")); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("part of itself: %v", err)
	}
}

func TestWork_StatusMovesByTheRule(t *testing.T) {
	w := open(t)
	if s := w.State(); s.Code != "PRJ-01" || s.Status != domain.Created || len(s.History) != 1 {
		t.Fatalf("opened: %+v", s)
	}
	if err := w.Schedule(date("2026-03-03")); !isViolation(err, "work.plan_required") {
		t.Fatalf("schedule without dates: %v", err)
	}
	if err := w.Hold(date("2026-03-03")); !isViolation(err, "work.transition") {
		t.Fatalf("hold before starting: %v", err)
	}
	if err := w.Complete(date("2026-03-03"), dec("0"), dec("0")); !isViolation(err, "work.transition") {
		t.Fatalf("complete before starting: %v", err)
	}
	p := plan()
	p.PlannedStart, p.PlannedEnd = date("2026-03-09"), date("2026-03-20")
	if err := w.Change(p); err != nil {
		t.Fatal(err)
	}
	if err := w.Schedule(date("2026-03-01")); !isViolation(err, "work.date") {
		t.Fatalf("before it was opened: %v", err)
	}
	if err := w.Schedule(date("2026-03-03")); err != nil {
		t.Fatal(err)
	}
	if err := w.Change(plan()); !isViolation(err, "work.plan_required") {
		t.Fatalf("scheduled work keeps its dates: %v", err)
	}
	if err := w.Start(date("2026-03-09")); err != nil {
		t.Fatal(err)
	}
	if err := w.Hold(date("2026-03-11")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.RateFor(ana, date("2026-03-11")); !isViolation(err, "work.not_in_progress") {
		t.Fatalf("no time on hold: %v", err)
	}
	if err := w.Start(date("2026-03-12")); err != nil || w.State().Started != date("2026-03-09") {
		t.Fatalf("resume keeps the start: %v", err)
	}
	if err := w.Complete(date("2026-03-20"), dec("16.5"), dec("412.50")); err != nil {
		t.Fatal(err)
	}
	s := w.State()
	if s.Status != domain.Completed || s.Finished != date("2026-03-20") || !s.Hours.Equal(dec("16.5")) || len(s.History) != 6 || w.Open() {
		t.Fatalf("completed: %+v", s)
	}
	for _, err := range []error{w.Change(plan()), w.Assign(ana, "", dec("10"), date("2026-03-09")), w.Release(ana, date("2026-03-09"))} {
		if !isViolation(err, "work.closed") {
			t.Fatalf("closed work: %v", err)
		}
	}
	if err := w.Cancel(date("2026-03-21"), "no"); !isViolation(err, "work.transition") {
		t.Fatalf("cancel completed work: %v", err)
	}

	c := open(t)
	if err := c.Cancel(date("2026-03-05"), " "); !isViolation(err, "work.cancel_reason") {
		t.Fatalf("reason: %v", err)
	}
	if err := c.Cancel(date("2026-03-05"), "El cliente desiste"); err != nil || c.State().CancelReason != "El cliente desiste" || c.State().Status != domain.Cancelled {
		t.Fatalf("cancel: %v", err)
	}
}

func TestWork_AssignmentsAndTime(t *testing.T) {
	w := open(t)
	if err := w.Assign(ana, "Oficial", dec("25.005"), date("2026-03-09")); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("rate: %v", err)
	}
	if err := w.Assign(ana, "Oficial", dec("25"), date("2026-03-09")); err != nil {
		t.Fatal(err)
	}
	if err := w.Assign(luis, "Ayudante", dec("15"), date("2026-03-09")); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordTime(domain.NewTimeEntryID(), w, ana, date("2026-03-09"), dec("8"), true, ""); !isViolation(err, "work.not_in_progress") {
		t.Fatalf("not started: %v", err)
	}
	_ = w.Start(date("2026-03-09"))
	if err := w.Release(luis, date("2026-03-08")); !isViolation(err, "work.date") {
		t.Fatalf("release before the first day: %v", err)
	}
	if err := w.Release(luis, date("2026-03-10")); err != nil {
		t.Fatal(err)
	}
	if err := w.Release(domain.PartyID{UUID: fw.NewUUID()}, date("2026-03-10")); !isViolation(err, "work.not_assigned") {
		t.Fatalf("release a stranger: %v", err)
	}
	if _, err := domain.RecordTime(domain.NewTimeEntryID(), w, luis, date("2026-03-11"), dec("8"), true, ""); !isViolation(err, "work.not_assigned") {
		t.Fatalf("released: %v", err)
	}
	if _, err := domain.RecordTime(domain.NewTimeEntryID(), w, ana, date("2026-03-08"), dec("8"), true, ""); !isViolation(err, "work.time_before_start") {
		t.Fatalf("before the start: %v", err)
	}
	for _, h := range []string{"0", "24.5", "1.234"} {
		if _, err := domain.RecordTime(domain.NewTimeEntryID(), w, ana, date("2026-03-09"), dec(h), true, ""); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("hours %s: %v", h, err)
		}
	}
	e, err := domain.RecordTime(domain.NewTimeEntryID(), w, ana, date("2026-03-09"), dec("7.5"), true, " Demolición ")
	if err != nil || !e.Cost().Equal(dec("187.50")) || e.State().Comment != "Demolición" || !e.State().Rate.Equal(dec("25")) {
		t.Fatalf("recorded: %+v %v", e, err)
	}
	// A new rate does not change what is already recorded.
	_ = w.Assign(ana, "Encargada", dec("30"), date("2026-03-09"))
	if err := e.Correct(dec("8"), false, "Demolición y desescombro"); err != nil || !e.Cost().Equal(dec("200.00")) || e.State().Billable {
		t.Fatalf("corrected: %v %s", err, e.Cost())
	}
	if err := e.Approve(); err != nil || !e.State().Approved || len(e.PendingEvents()) != 1 {
		t.Fatalf("approved: %v", err)
	}
	for _, err := range []error{e.Approve(), e.Correct(dec("1"), true, ""), e.Withdraw()} {
		if !isViolation(err, "work.time_approved") {
			t.Fatalf("approved time is final: %v", err)
		}
	}
}
