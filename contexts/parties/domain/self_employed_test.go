package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Una organización sólo lleva identificaciones fiscales y otros documentos; una persona, todos
// salvo la identificación fiscal española, que es el CIF (D-85).
func TestParty_DocumentsByKind(t *testing.T) {
	expiry, authority := vocab.MustDate(2031, 2, 3), "Policía Nacional"
	personal := map[string]domain.IdentificationData{
		"passport":        {Type: domain.DocPassport, Country: es, Number: "PAA123456", ExpiresOn: expiry, IssuingAuthority: authority},
		"national id":     {Type: domain.DocNationalID, Country: es, Number: "12345678Z"},
		"driver licence":  {Type: domain.DocDriverLicense, Country: es, Number: "12345678Z", ExpiresOn: expiry, IssuingAuthority: authority},
		"social security": {Type: domain.DocSocialSecurity, Country: es, Number: "281234567840"},
		"alien number":    {Type: domain.DocAlienRegistation, Country: es, Number: "X1234567L", ExpiresOn: expiry},
	}
	for name, d := range personal {
		acme := org(t, "Acme", "")
		if _, err := acme.AddIdentification(policy, d, false); !isViolation(err, "parties.document_not_for_kind") {
			t.Errorf("an organization cannot carry a %s: %v", name, err)
		}
		ana := person(t, "Ana", "García", "")
		if _, err := ana.AddIdentification(policy, d, false); err != nil {
			t.Errorf("a person carries a %s: %v", name, err)
		}
	}

	acme := org(t, "Acme", "")
	if _, err := acme.AddIdentification(policy, domain.IdentificationData{Type: domain.DocTaxID, Country: es, Number: "B12345674"}, false); err != nil {
		t.Fatalf("an organization carries its CIF: %v", err)
	}
	if _, err := acme.AddIdentification(policy, domain.IdentificationData{Type: domain.DocOther, Country: es, Number: "REG-1"}, false); err != nil {
		t.Fatalf("an organization carries another document: %v", err)
	}
	if _, err := acme.AddIdentification(policy, domain.IdentificationData{Type: domain.DocTaxID, Country: es, Number: "B12345678"}, false); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("an invalid CIF is still refused as invalid (D-5): %v", err)
	}

	ana := person(t, "Ana", "García", "")
	if _, err := ana.AddIdentification(policy, domain.IdentificationData{Type: domain.DocTaxID, Country: es, Number: "B12345674"}, false); !isViolation(err, "parties.document_not_for_kind") {
		t.Fatalf("a person carries no Spanish tax id (it is a CIF): %v", err)
	}
	if _, err := ana.AddIdentification(policy, domain.IdentificationData{Type: domain.DocTaxID, Country: pt, Number: "501964843"}, false); err != nil {
		t.Fatalf("a person carries the tax id of another country: %v", err)
	}
	if _, err := ana.AddIdentification(policy, domain.IdentificationData{Type: domain.DocOther, Country: es, Number: "X-1"}, false); err != nil {
		t.Fatalf("a person carries another document: %v", err)
	}
	if _, err := ana.AddIdentification(policy, domain.IdentificationData{Type: domain.DocNationalID, Country: es, Number: "12345678A"}, false); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("an invalid DNI is still refused as invalid (D-5): %v", err)
	}

	// Lo guardado antes de la regla se rehidrata tal cual.
	n, _ := domain.NewOrganizationName("Old", "")
	old, err := domain.Reconstitute(domain.NewPartyID(), domain.PartyState{Kind: domain.KindOrganization, Active: true,
		Organization: domain.OrganizationDetails{Name: n}, Identities: []domain.Identification{{ID: domain.NewIdentificationID(),
			Type: domain.DocNationalID, Country: es, Number: "12345678Z", Primary: true}}})
	if err != nil || len(old.Identifications()) != 1 {
		t.Fatalf("an organization stored with a DNI is read back: %v", err)
	}
}

func TestParty_SelfEmployedDetails(t *testing.T) {
	ana := person(t, "Ana", "García", "")
	self, err := ana.AssignRole(cat, domain.RoleSelfEmployed, t0)
	if err != nil {
		t.Fatal(err)
	}
	customer, err := ana.AssignRole(cat, domain.RoleCustomer, t0)
	if err != nil {
		t.Fatal(err)
	}

	d, err := domain.NewSelfEmployedDetails("  Fontanería   Ana ", " 504.1 ", vocab.MustDate(2020, 3, 1))
	if err != nil || d.TradeName != "Fontanería Ana" || d.ActivityCode != "504.1" {
		t.Fatalf("details are trimmed: %+v %v", d, err)
	}
	if lower, _ := domain.NewSelfEmployedDetails("", "abc", vocab.Date{}); lower.ActivityCode != "ABC" {
		t.Fatalf("the activity code is kept in capitals: %+v", lower)
	}
	if _, err := domain.NewSelfEmployedDetails("", "", vocab.DateOf(fw.Now()).AddDays(1)); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a registration in the future: %v", err)
	}
	if _, err := domain.NewSelfEmployedDetails("", "12345678901", vocab.Date{}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("an activity code of eleven characters: %v", err)
	}
	if empty, err := domain.NewSelfEmployedDetails(" ", "", vocab.Date{}); err != nil || !empty.IsZero() {
		t.Fatalf("all optional: %+v %v", empty, err)
	}

	if err := ana.SetSelfEmployedDetails(cat, customer, d); !isViolation(err, "parties.role_details_not_for_type") {
		t.Fatalf("a customer role carries no self-employed profile: %v", err)
	}
	if err := ana.SetSelfEmployedDetails(cat, domain.NewPartyRoleID(), d); !errors.Is(err, fw.ErrNotFound) {
		t.Fatalf("unknown role: %v", err)
	}
	ana.ClearEvents()
	if err := ana.SetSelfEmployedDetails(cat, self, d); err != nil {
		t.Fatal(err)
	}
	if got := roleByID(ana, self).Details.SelfEmployed; got != d {
		t.Fatalf("profile: %+v", got)
	}
	if evs := ana.PendingEvents(); len(evs) != 1 || evs[0].(domain.SelfEmployedDetailsSet).RegisteredOn != "2020-03-01" {
		t.Fatalf("events: %+v", evs)
	}
	if err := ana.SetSelfEmployedDetails(cat, self, d); err != nil || len(ana.PendingEvents()) != 1 {
		t.Fatalf("the same profile again changes nothing: %v", err)
	}
	if err := ana.SetSelfEmployedDetails(cat, self, domain.SelfEmployedDetails{}); err != nil || !roleByID(ana, self).Details.SelfEmployed.IsZero() {
		t.Fatalf("an empty profile clears it: %v", err)
	}

	// El agregado hace cumplir las reglas aunque el perfil se construya a mano.
	for name, bad := range map[string]domain.SelfEmployedDetails{
		"trade name of 121 characters": {TradeName: strings.Repeat("x", 121)},
		"activity code of 11":          {ActivityCode: "12345678901"},
		"registration in the future":   {RegisteredOn: vocab.DateOf(fw.Now()).AddDays(1)},
	} {
		if err := ana.SetSelfEmployedDetails(cat, self, bad); !errors.Is(err, fw.ErrValidation) {
			t.Errorf("hand-built %s: %v", name, err)
		}
	}
	if err := ana.SetSelfEmployedDetails(cat, self, domain.SelfEmployedDetails{TradeName: "  a  b ", ActivityCode: "x1"}); err != nil ||
		roleByID(ana, self).Details.SelfEmployed != (domain.SelfEmployedDetails{TradeName: "a b", ActivityCode: "X1"}) {
		t.Fatalf("a hand-built profile is normalized: %+v %v", roleByID(ana, self).Details.SelfEmployed, err)
	}

	// El registro de auditoría lleva el perfil de los roles vigentes, con el id del rol y sus datos
	// entre comillas: una coma o una barra en el nombre no lo confunden.
	if err := ana.SetSelfEmployedDetails(cat, self, domain.SelfEmployedDetails{TradeName: `Pérez, "P|S"`, ActivityCode: "504.1",
		RegisteredOn: vocab.MustDate(2020, 3, 1)}); err != nil {
		t.Fatal(err)
	}
	want := self.String() + `:"Pérez, \"P|S\"" "504.1" "2020-03-01"`
	if got := ana.AuditSnapshot()["selfEmployed"]; got != want {
		t.Fatalf("audit snapshot: %v, want %v", got, want)
	}
	// El perfil de un rol terminado ya no cambia (y, como en roles, no aparece en el registro de lo
	// vigente): así todo cambio de perfil queda en la auditoría.
	if err := ana.EndRole(self, fw.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := ana.SetSelfEmployedDetails(cat, self, d); !isViolation(err, "parties.role_ended") {
		t.Fatalf("the profile of an ended role: %v", err)
	}
	if _, in := ana.AuditSnapshot()["selfEmployed"]; in {
		t.Fatalf("an ended role in the audit snapshot: %v", ana.AuditSnapshot()["selfEmployed"])
	}

	// Un rol por debajo de Self Employed lleva el mismo perfil.
	child := domain.RoleTypeID{UUID: fw.NewUUID()}
	types := append(domain.WellKnownRoleTypes(), domain.RoleType{ID: child, Name: vocab.MustName("Freelance Translator"), Parent: ptr(domain.RoleSelfEmployed)})
	ext, err := domain.NewRoleCatalog(types, domain.RolePersonCategory, domain.RoleOrganizationCategory)
	if err != nil {
		t.Fatal(err)
	}
	luis := person(t, "Luis", "Pérez", "")
	translator, err := luis.AssignRole(ext, child, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := luis.SetSelfEmployedDetails(ext, translator, d); err != nil {
		t.Fatalf("a role below Self Employed carries its profile: %v", err)
	}

	// Un rol anulado no admite cambios.
	if _, err := luis.AnnulRole(translator, t0.AddDate(0, 1, 0), true); err != nil {
		t.Fatal(err)
	}
	if err := luis.SetSelfEmployedDetails(ext, translator, domain.SelfEmployedDetails{}); !isViolation(err, "parties.role_annulled") {
		t.Fatalf("an annulled role: %v", err)
	}

	// Un duplicado no admite cambios (D-64).
	if err := ana.MarkDuplicate(); err != nil {
		t.Fatal(err)
	}
	if err := ana.SetSelfEmployedDetails(cat, self, d); !isViolation(err, "parties.duplicate") {
		t.Fatalf("a duplicate: %v", err)
	}
}

func roleByID(p *domain.Party, id domain.PartyRoleID) domain.PartyRole {
	for _, r := range p.Roles() {
		if r.ID == id {
			return r
		}
	}
	return domain.PartyRole{}
}
