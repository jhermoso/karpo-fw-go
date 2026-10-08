package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func relType(t *testing.T, id domain.RelationshipTypeID) domain.RelationshipType {
	t.Helper()
	for _, rt := range domain.WellKnownRelationshipTypes() {
		if rt.ID == id {
			return rt
		}
	}
	t.Fatalf("unknown relationship type %s", id)
	return domain.RelationshipType{}
}

func TestRelationshipTypeCodes(t *testing.T) {
	types := domain.WellKnownRelationshipTypes()
	byID, err := domain.IndexRelationshipTypes(types)
	if err != nil || len(byID) != len(types) {
		t.Fatalf("well-known types: %d %v", len(byID), err)
	}
	for _, rt := range types {
		if rt.Code == "" {
			t.Errorf("%s has no code", rt.Name)
		}
	}
	p := byID[domain.RelProspect]
	if p.Code != domain.CodeProspect || p.FromRole != domain.RoleProspect || p.ToRole != domain.RoleInternalOrganization {
		t.Fatalf("prospect relationship: %+v", p)
	}

	// A type without code is a plain catalog row; two types cannot share a code.
	plain := domain.RelationshipType{ID: domain.MustRelationshipTypeID("10000000-0000-0000-0002-0000000000f1"), Name: vocab.MustName("Guarantor")}
	other := domain.RelationshipType{ID: domain.MustRelationshipTypeID("10000000-0000-0000-0002-0000000000f2"), Name: vocab.MustName("Sponsor")}
	if _, err := domain.IndexRelationshipTypes(append(types, plain, other)); err != nil {
		t.Fatalf("types without code: %v", err)
	}
	other.Code = domain.CodeProspect
	if _, err := domain.IndexRelationshipTypes(append(types, other)); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a repeated code must be refused: %v", err)
	}
}

func TestProspectTrial(t *testing.T) {
	prospectType := relType(t, domain.RelProspect)
	since := fw.Now().Add(-24 * time.Hour)
	ana := person(t, "Ana", "García", "")
	paranoia := org(t, "Paranoia", "")
	_, _ = paranoia.AssignRole(cat, domain.RoleInternalOrganization, since)

	if _, err := domain.Establish(domain.NewRelationshipID(), prospectType, ana, paranoia, cat, since, ""); !isViolation(err, "parties.relationship_role_missing") {
		t.Fatalf("a prospect relationship needs the prospect role: %v", err)
	}
	_, _ = ana.AssignRole(cat, domain.RoleProspect, since)
	r, err := domain.Establish(domain.NewRelationshipID(), prospectType, ana, paranoia, cat, since, "")
	if err != nil {
		t.Fatal(err)
	}
	now := fw.Now()
	if r.TrialUntil() != nil || r.InTrialAt(now) || domain.InTrialAt(now).IsSatisfiedBy(r) {
		t.Fatal("a prospect has no trial until one is granted")
	}

	// Granting, extending and withdrawing the trial.
	until := now.Add(30 * 24 * time.Hour)
	if err := r.SetTrial(prospectType, &until); err != nil {
		t.Fatal(err)
	}
	events := r.PendingEvents()
	if len(events) != 2 || events[1].EventType() != "parties.prospect_trial_changed" {
		t.Fatalf("events: %v", events)
	}
	if e := events[1].(domain.ProspectTrialChanged); e.Prospect != ana.ID().String() || e.Organization != paranoia.ID().String() || !e.TrialUntil.Equal(until) {
		t.Fatalf("event: %+v", e)
	}
	if !r.InTrialAt(now) || r.InTrialAt(until) || r.InTrialAt(since.Add(-time.Hour)) {
		t.Fatal("the trial runs from the start of the relationship to its end, half-open")
	}
	if !domain.InTrialAt(now).IsSatisfiedBy(r) || domain.InTrialAt(until.Add(time.Hour)).IsSatisfiedBy(r) {
		t.Fatal("InTrialAt specification")
	}
	if err := r.SetTrial(prospectType, &until); err != nil || len(r.PendingEvents()) != 2 {
		t.Fatal("setting the same end changes nothing")
	}
	longer := until.Add(15 * 24 * time.Hour)
	if err := r.SetTrial(prospectType, &longer); err != nil || !r.InTrialAt(until) || len(r.PendingEvents()) != 3 {
		t.Fatalf("extended: %v", err)
	}
	before := since.Add(-time.Hour)
	if err := r.SetTrial(prospectType, &before); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("the trial cannot end before the relationship starts: %v", err)
	}
	if err := r.SetTrial(prospectType, nil); err != nil || r.TrialUntil() != nil || r.InTrialAt(now) {
		t.Fatalf("withdrawn: %v", err)
	}

	// The trial ends with the relationship, and an ended relationship keeps what it had.
	_ = r.SetTrial(prospectType, &longer)
	if err := r.Terminate(now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if r.InTrialAt(now) || !r.InTrialAt(now.Add(-time.Hour)) {
		t.Fatal("the trial is in force only while the relationship is")
	}
	if err := r.SetTrial(prospectType, &until); !isViolation(err, "parties.relationship_ended") {
		t.Fatalf("an ended relationship: %v", err)
	}

	// Only prospect relationships have a trial.
	employment := relType(t, domain.RelEmployment)
	_, _ = ana.AssignRole(cat, domain.RoleEmployee, since)
	job, err := domain.Establish(domain.NewRelationshipID(), employment, ana, paranoia, cat, since, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := job.SetTrial(employment, &until); !isViolation(err, "parties.not_a_prospect_relationship") {
		t.Fatalf("employment has no trial: %v", err)
	}
	if err := job.SetTrial(prospectType, &until); !isViolation(err, "parties.not_a_prospect_relationship") {
		t.Fatalf("the type must be the relationship's own: %v", err)
	}
}
func TestOwnershipShare(t *testing.T) {
	ownership := relType(t, domain.RelOwnership)
	since := fw.Now().Add(-24 * time.Hour)
	ana := person(t, "Ana", "García", "")
	talleres := org(t, "Talleres Pérez", "")
	_, _ = ana.AssignRole(cat, domain.RoleShareholder, since)
	_, _ = talleres.AssignRole(cat, domain.RoleInternalOrganization, since)
	r, err := domain.Establish(domain.NewRelationshipID(), ownership, ana, talleres, cat, since, "")
	if err != nil || r.OwnershipShare() != nil {
		t.Fatalf("an ownership starts without a known share: %v", err)
	}

	share := vocab.MustPercentage("30")
	if err := r.SetOwnershipShare(ownership, &share); err != nil || !r.OwnershipShare().Equal(share) {
		t.Fatal(err)
	}
	events := r.PendingEvents()
	if e, ok := events[len(events)-1].(domain.OwnershipShareChanged); !ok || e.Share != "30.00" || e.Shareholder != ana.ID().String() || e.Organization != talleres.ID().String() {
		t.Fatalf("event: %+v", events[len(events)-1])
	}
	same := vocab.MustPercentage("30.00")
	if err := r.SetOwnershipShare(ownership, &same); err != nil || len(r.PendingEvents()) != len(events) {
		t.Fatal("the same share changes nothing")
	}
	for _, bad := range []string{"0", "-5", "100.01", "33.333"} {
		p := vocab.MustPercentage(bad)
		if err := r.SetOwnershipShare(ownership, &p); !errors.Is(err, fw.ErrValidation) {
			t.Errorf("share %s must be refused: %v", bad, err)
		}
	}
	whole := vocab.MustPercentage("100")
	if err := r.SetOwnershipShare(ownership, &whole); err != nil {
		t.Fatalf("a sole owner: %v", err)
	}
	if err := r.SetOwnershipShare(ownership, nil); err != nil || r.OwnershipShare() != nil {
		t.Fatalf("cleared: %v", err)
	}

	// Each detail belongs to its own type.
	if err := r.SetOwnershipShare(relType(t, domain.RelProspect), &share); !isViolation(err, "parties.not_an_ownership_relationship") {
		t.Fatalf("the type must be the relationship's own: %v", err)
	}
	until := fw.Now().Add(time.Hour)
	if err := r.SetTrial(ownership, &until); !isViolation(err, "parties.not_a_prospect_relationship") {
		t.Fatalf("an ownership has no trial: %v", err)
	}
	_ = r.Terminate(fw.Now().Add(-time.Minute))
	if err := r.SetOwnershipShare(ownership, &share); !isViolation(err, "parties.relationship_ended") {
		t.Fatalf("an ended relationship: %v", err)
	}
}
