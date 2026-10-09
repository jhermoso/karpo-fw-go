package host_test

import (
	"errors"
	"testing"
	"time"

	exgapp "github.com/jhermoso/karpo-fw-go/contexts/exchange/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// promotionsScenario gives the collaborators of two companies their promotion codes in Parties and
// reserves currency with them in Exchange.
func promotionsScenario(t *testing.T, sw *hotswap.Switch) {
	th := newTradeHost(t, sw)
	h, actx := th.h, th.actx
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	organization := func(name string, role pardomain.RoleTypeID, of string, rel pardomain.RelationshipTypeID) string {
		t.Helper()
		c := parapp.RegisterOrganization{LegalName: name, Roles: []string{role.String()}}
		if of != "" {
			c.Affiliation = &parapp.NewAffiliation{Organization: of, RelationshipType: rel.String()}
		}
		p, err := h.Parties.Service.RegisterOrganization.Handle(actx, c)
		must(err)
		return p.ID
	}
	// relationship returns the relationship of a kind a party has.
	relationship := func(party string, kind pardomain.RelationshipTypeID) pardomain.RelationshipID {
		t.Helper()
		pid, _ := pardomain.ParsePartyID(party)
		rels, err := h.Parties.Service.Relationships.Handle(actx, parapp.PartyRelationships{PartyID: pid, ActiveOnly: true})
		must(err)
		for _, r := range rels {
			if r.Type == kind.String() {
				id, _ := pardomain.ParseRelationshipID(r.ID)
				return id
			}
		}
		t.Fatalf("%s has no relationship of that kind: %+v", party, rels)
		return pardomain.RelationshipID{}
	}
	code := func(rel pardomain.RelationshipID, written string) (parapp.RelationshipDTO, error) {
		return h.Parties.Service.SetPromotionCode.Handle(actx, parapp.SetPromotionCode{ID: rel, PromotionCode: written})
	}
	internal, none := pardomain.RoleInternalOrganization, pardomain.RelationshipTypeID{}
	acme, other := organization("Acme Cambios SL", internal, "", none), organization("Otra Cambios SL", internal, "", none)
	sol := organization("Agencia Sol", pardomain.RoleCollaborator, acme, pardomain.RelCollaborator)
	mar := organization("Agencia Mar", pardomain.RoleCollaborator, acme, pardomain.RelCollaborator)
	luna := organization("Agencia Luna", pardomain.RoleCollaborator, other, pardomain.RelCollaborator)
	customer := organization("Cliente Uno SL", pardomain.RoleCustomer, acme, pardomain.RelCustomer)

	// The code is kept in capitals, on the relationship of the collaborator with the company.
	got, err := code(relationship(sol, pardomain.RelCollaborator), "  promo-1 ")
	if err != nil || got.Collaborator == nil || got.Collaborator.PromotionCode != "PROMO-1" {
		t.Fatalf("the code of Sol: %+v %v", got, err)
	}
	// Two collaborators of a company do not share a code; those of two companies may.
	var rule *fw.RuleViolationError
	if _, err := code(relationship(mar, pardomain.RelCollaborator), "Promo-1"); !errors.As(err, &rule) || rule.Code != "parties.duplicate_promotion_code" {
		t.Fatalf("the code of another collaborator: %v", err)
	}
	if _, err := code(relationship(luna, pardomain.RelCollaborator), "PROMO-1"); err != nil {
		t.Fatalf("the same code in another company: %v", err)
	}
	if _, err := code(relationship(sol, pardomain.RelCollaborator), "promo-1"); err != nil {
		t.Fatalf("its own code again: %v", err)
	}
	for _, bad := range []string{"MAR 1", "MAR/1", "ABCDEFGHIJKLMNOP"} {
		if _, err := code(relationship(mar, pardomain.RelCollaborator), bad); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("%q is not a code: %v", bad, err)
		}
	}
	if _, err := code(relationship(mar, pardomain.RelCollaborator), "MAR_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := code(relationship(customer, pardomain.RelCustomer), "X1"); !errors.As(err, &rule) || rule.Code != "parties.not_a_collaborator_relationship" {
		t.Fatalf("a customer has no promotion code: %v", err)
	}
	// What other contexts ask.
	who, found, err := h.Parties.Collaborators.ByPromotionCode(actx, acme, " promo-1")
	if err != nil || !found || who.PartyID != sol || who.PromotionCode != "PROMO-1" {
		t.Fatalf("whose PROMO-1 is in Acme: %+v %v %v", who, found, err)
	}
	if who, found, _ := h.Parties.Collaborators.ByPromotionCode(actx, other, "PROMO-1"); !found || who.PartyID != luna {
		t.Fatalf("whose PROMO-1 is in the other company: %+v", who)
	}
	if _, found, _ := h.Parties.Collaborators.ByPromotionCode(actx, other, "MAR_1"); found {
		t.Fatal("the code of a collaborator of another company is nobody's here")
	}

	// Exchange attributes a reservation to the collaborator of its code.
	_, err = h.Exchange.Service.SetCurrency.Handle(actx, exgapp.SetCurrency{Company: acme, Code: "USD", Name: "Dólar"})
	must(err)
	_, err = h.Exchange.Service.SetRate.Handle(actx, exgapp.SetRate{Company: acme, Code: "USD", Rate: "0.92"})
	must(err)
	_, err = h.Exchange.Service.SetMargin.Handle(actx, exgapp.SetMargin{Company: acme, Currency: "USD", Kind: "percent", Level1: "1.5", Level2: "2", Level3: "2.5"})
	must(err)
	reserve := func(written string) (exgapp.ReservationDTO, error) {
		return h.Exchange.Service.Reserve.Handle(actx, exgapp.Reserve{Company: acme, Customer: customer, Channel: "web", Pickup: time.Now().Add(48 * time.Hour),
			PromotionCode: written, Lines: []exgapp.ReserveLine{{Currency: "USD", Amount: "500"}}})
	}
	res, err := reserve("promo-1")
	if err != nil || res.Collaborator != sol || res.PromotionCode != "promo-1" {
		t.Fatalf("a reservation with the code of Sol: %+v %v", res, err)
	}
	if res, err := reserve("mar_1"); err != nil || res.Collaborator != mar {
		t.Fatalf("a reservation with the code of Mar: %+v %v", res, err)
	}
	if res, err := reserve(""); err != nil || res.Collaborator != "" {
		t.Fatalf("a reservation without a code: %+v %v", res, err)
	}
	if _, err := reserve("NOPE"); !errors.As(err, &rule) || rule.Code != "exchange.promotion_unknown" {
		t.Fatalf("a code of nobody: %v", err)
	}
	// A collaborator that loses its code no longer gets reservations.
	if _, err := code(relationship(sol, pardomain.RelCollaborator), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := reserve("promo-1"); !errors.As(err, &rule) || rule.Code != "exchange.promotion_unknown" {
		t.Fatalf("a code taken away: %v", err)
	}
	// And one whose relationship ended either: the code is free for another.
	if _, err := h.Parties.Service.TerminateRelationship.Handle(actx, parapp.TerminateRelationship{ID: relationship(mar, pardomain.RelCollaborator)}); err != nil {
		t.Fatal(err)
	}
	if _, err := reserve("MAR_1"); !errors.As(err, &rule) || rule.Code != "exchange.promotion_unknown" {
		t.Fatalf("the code of who is no longer a collaborator: %v", err)
	}
	if _, err := code(relationship(sol, pardomain.RelCollaborator), "MAR_1"); err != nil {
		t.Fatalf("a code that is free again: %v", err)
	}
}

func TestPromotions_OnMemory(t *testing.T) { promotionsScenario(t, memorySwitch(t)) }
func TestPromotions_OnSQLite(t *testing.T) { promotionsScenario(t, sqliteSwitch(t)) }
