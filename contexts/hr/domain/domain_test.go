package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func TestWellKnownCatalogs(t *testing.T) {
	if n := len(domain.WellKnownPositionStatuses()); n != 6 {
		t.Fatalf("statuses: %d", n)
	}
	classes := map[domain.PositionClassID]bool{}
	for _, c := range domain.WellKnownPositionClasses() {
		classes[c.ID] = true
	}
	types := domain.WellKnownPositionTypes()
	if len(classes) != 9 || len(types) != 57 {
		t.Fatalf("classes %d types %d", len(classes), len(types))
	}
	links := 0
	for _, pt := range types {
		for _, c := range pt.Classes {
			links++
			if !classes[c.Class] || c.StandardWeeklyHours.Sign() < 0 {
				t.Fatalf("type %s: bad class %+v", pt.Title, c)
			}
		}
	}
	if links != 23 {
		t.Fatalf("type classes: %d", links)
	}
	for _, a := range domain.WellKnownAgreements() {
		if err := a.Validate(); err != nil {
			t.Fatalf("agreement %s: %v", a.Code, err)
		}
	}
	company := domain.Agreement{Code: "X", Name: "Empresa", Scope: domain.ScopeCompany, Start: vocab.MustDate(2024, 1, 1)}
	if company.Validate() == nil {
		t.Fatal("a company agreement needs its organization")
	}
}

func TestPosition(t *testing.T) {
	var pt domain.PositionType
	for _, x := range domain.WellKnownPositionTypes() {
		if x.Active {
			pt = x
			break
		}
	}
	acme := domain.OrganizationID{UUID: fw.NewUUID()}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	planned, _ := vocab.OpenPeriodFrom(t0)
	st := domain.PositionState{Unit: acme, Organization: acme, Planned: planned}
	if _, err := domain.OpenPosition(domain.NewPositionID(), pt, domain.PositionState{Unit: acme, Organization: acme, Planned: planned,
		Status: domain.StatusVacant}); !isViolation(err, "hr.position_status") {
		t.Fatalf("vacant is derived: %v", err)
	}
	ceo, err := domain.OpenPosition(domain.NewPositionID(), pt, st)
	if err != nil || ceo.Status() != domain.StatusActive || !ceo.IsVacantAt(t0) {
		t.Fatal(err)
	}
	ana, bea := domain.PersonID{UUID: fw.NewUUID()}, domain.PersonID{UUID: fw.NewUUID()}
	if err := ceo.Fill(ana, t0); err != nil {
		t.Fatal(err)
	}
	if err := ceo.Fill(bea, t0.Add(time.Hour)); !isViolation(err, "hr.position_filled") {
		t.Fatalf("one holder at a time: %v", err)
	}
	if h, ok := ceo.HolderAt(t0.Add(time.Hour)); !ok || h != ana || ceo.IsVacantAt(t0.Add(time.Hour)) {
		t.Fatal("ana holds the position")
	}
	t1 := t0.Add(24 * time.Hour)
	if err := ceo.Vacate(t1); err != nil {
		t.Fatal(err)
	}
	if err := ceo.Fill(bea, t1); err != nil {
		t.Fatalf("bea takes over: %v", err)
	}
	if h, _ := ceo.HolderAt(t1); h != bea {
		t.Fatal("bea holds it now")
	}
	if err := ceo.SetStatus(domain.StatusVacant); !isViolation(err, "hr.position_status") {
		t.Fatal("vacant cannot be set")
	}

	cfo, _ := domain.OpenPosition(domain.NewPositionID(), pt, st)
	cto, _ := domain.OpenPosition(domain.NewPositionID(), pt, st)
	if err := cfo.ReportTo(cfo.ID(), true, t0); !isViolation(err, "hr.reporting_self") {
		t.Fatal("not to itself")
	}
	if err := cfo.ReportTo(ceo.ID(), true, t0); err != nil {
		t.Fatal(err)
	}
	if err := cfo.ReportTo(ceo.ID(), false, t0); !isViolation(err, "hr.reporting_duplicate") {
		t.Fatal("not twice")
	}
	if err := cfo.ReportTo(cto.ID(), true, t1); err != nil {
		t.Fatal(err)
	}
	if sup, _ := cfo.PrimarySupervisorAt(t1); sup != cto.ID() {
		t.Fatal("the new primary replaces the previous one")
	}
	if err := cfo.EndReporting(ceo.ID(), t1); err != nil {
		t.Fatal(err)
	}
	if !domain.DirectReportsOf(cto.ID()).IsSatisfiedBy(cfo) || domain.DirectReportsOf(ceo.ID()).IsSatisfiedBy(cfo) {
		t.Fatal("direct reports")
	}
	if !domain.HeldBy(bea, t1.Add(time.Hour)).IsSatisfiedBy(ceo) || domain.HeldBy(ana, t1.Add(time.Hour)).IsSatisfiedBy(ceo) {
		t.Fatal("held by")
	}
	if err := ceo.Close(t1.Add(time.Hour)); err != nil || ceo.Status() != domain.StatusInactive || ceo.IsVacantAt(t1.Add(2*time.Hour)) {
		t.Fatal(err)
	}
	if _, held := ceo.HolderAt(t1.Add(2 * time.Hour)); held {
		t.Fatal("closing vacates")
	}
	if err := ceo.Fill(ana, t1.Add(2*time.Hour)); !isViolation(err, "hr.position_not_active") {
		t.Fatal("a closed position cannot be filled")
	}
}

func TestEmployment(t *testing.T) {
	today := vocab.MustDate(2026, 9, 1)
	st := domain.EmploymentState{Person: domain.PersonID{UUID: fw.NewUUID()}, Employer: domain.OrganizationID{UUID: fw.NewUUID()},
		Number: "E-001", Hired: vocab.MustDate(2026, 1, 15)}
	if _, err := domain.Hire(domain.NewEmploymentID(), domain.EmploymentState{Person: st.Person, Employer: st.Employer,
		Hired: today.AddDays(400)}, today); err == nil {
		t.Fatal("hire date too far ahead")
	}
	e, err := domain.Hire(domain.NewEmploymentID(), st, today)
	if err != nil || !e.ActiveOn(today) {
		t.Fatal(err)
	}
	agreement := domain.WellKnownAgreements()[0].ID
	wc := domain.NewWorkCenterID()
	if _, err := e.AddContract(domain.Contract{TypeCode: "100", Start: vocab.MustDate(2026, 1, 1), Agreement: agreement, WorkCenter: wc}); !isViolation(err, "hr.contract_outside_employment") {
		t.Fatalf("before hiring: %v", err)
	}
	first, err := e.AddContract(domain.Contract{TypeCode: "401", Start: st.Hired, Agreement: agreement, WorkCenter: wc, Primary: true,
		WeeklyHours: vocab.DecimalFromInt(40)})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := e.AddContract(domain.Contract{TypeCode: "100", Start: vocab.MustDate(2026, 6, 1), Agreement: agreement, WorkCenter: wc, Primary: true})
	if c, ok := e.PrimaryContractOn(today); !ok || c.ID != second {
		t.Fatal("the new primary contract replaces the old one")
	}
	if err := e.EndContract(first, vocab.MustDate(2026, 5, 31), "fin obra"); err != nil {
		t.Fatal(err)
	}
	if err := e.Terminate(vocab.MustDate(2025, 1, 1), "baja"); !isViolation(err, "hr.termination_before_hire") {
		t.Fatal("before hiring")
	}
	if err := e.Terminate(vocab.MustDate(2026, 12, 31), "baja voluntaria"); err != nil {
		t.Fatal(err)
	}
	for _, c := range e.Contracts() {
		if c.End.IsZero() || c.End.After(vocab.MustDate(2026, 12, 31)) {
			t.Fatalf("termination ends every contract: %+v", c)
		}
	}
	if e.ActiveOn(vocab.MustDate(2027, 1, 1)) || e.Terminate(vocab.MustDate(2027, 1, 1), "") == nil {
		t.Fatal("terminated once")
	}
	if domain.CurrentEmploymentsOf(st.Person).IsSatisfiedBy(e) {
		t.Fatal("no longer current")
	}
}

func TestWorkCenter(t *testing.T) {
	st := domain.WorkCenterState{Employer: domain.OrganizationID{UUID: fw.NewUUID()}, Facility: domain.FacilityID{UUID: fw.NewUUID()},
		Code: " 28/1234567/01 ", Headquarters: true, Opened: vocab.MustDate(2020, 1, 1)}
	w, err := domain.OpenWorkCenter(domain.NewWorkCenterID(), st)
	if err != nil || w.Code() != "28/1234567/01" || !w.IsHeadquarters() {
		t.Fatal(err)
	}
	if err := w.Close(vocab.MustDate(2019, 1, 1)); err == nil {
		t.Fatal("closing before opening")
	}
	if err := w.Close(vocab.MustDate(2026, 1, 1)); err != nil || w.IsOpen() || w.IsHeadquarters() {
		t.Fatal(err)
	}
	if err := w.MarkHeadquarters(true); !isViolation(err, "hr.work_center_closed") {
		t.Fatal("a closed work center is not the headquarters")
	}
}
