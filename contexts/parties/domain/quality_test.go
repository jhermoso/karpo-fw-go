package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Un e-mail o un teléfono con formato erróneo se guarda tal cual y deja una incidencia, que se va
// cuando el contacto termina (D-92).
func TestParty_BadlyFormedContactsAreKeptWithAnIssue(t *testing.T) {
	ana := person(t, "Ana", "García", "")
	mail, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "  ana.garcia@  ", Purposes: []domain.Purpose{domain.PurposeDefault}}, t0)
	if err != nil {
		t.Fatalf("a badly formed e-mail is kept: %v", err)
	}
	if c, ok := ana.ContactFor(domain.ContactEmail, domain.PurposeDefault); !ok || c.ID != mail || c.Value != "ana.garcia@" {
		t.Fatalf("kept as written, trimmed: %+v", c)
	}
	issues := ana.DataQualityIssuesOf(mail.UUID)
	if len(issues) != 1 || issues[0].Code != domain.IssueInvalidEmail || issues[0].Severity != domain.SeverityWarning ||
		issues[0].Value != "ana.garcia@" || issues[0].Field != "contact.value" || issues[0].Message == "" || issues[0].Since.IsZero() {
		t.Fatalf("the e-mail issue: %+v", issues)
	}
	phone, err := ana.AddContact(domain.ContactData{Kind: domain.ContactPhone, Value: "600 11 22 33"}, t0)
	if err != nil {
		t.Fatalf("a phone without its international prefix is kept: %v", err)
	}
	if got := ana.DataQualityIssuesOf(phone.UUID); len(got) != 1 || got[0].Code != domain.IssueInvalidPhone || got[0].Value != "600 11 22 33" {
		t.Fatalf("the phone issue: %+v", got)
	}
	fax, err := ana.AddContact(domain.ContactData{Kind: domain.ContactFax, Value: "fax?"}, t0)
	if err != nil || len(ana.DataQualityIssuesOf(fax.UUID)) != 1 {
		t.Fatalf("a fax is a phone number: %v", err)
	}
	if !ana.HasDataQualityIssues() || len(ana.DataQualityIssues()) != 3 {
		t.Fatalf("three issues: %+v", ana.DataQualityIssues())
	}
	if !domain.WithDataQualityIssues().IsSatisfiedBy(ana) {
		t.Fatal("the filter finds the party")
	}

	// Lo bien formado no deja nada; lo vacío o lo que no cabe se sigue rechazando.
	good, err := ana.AddContact(domain.ContactData{Kind: domain.ContactPhone, Value: "+34 600 000 001"}, t0)
	if err != nil || len(ana.DataQualityIssuesOf(good.UUID)) != 0 {
		t.Fatalf("a good phone: %v", err)
	}
	for _, d := range []domain.ContactData{{Kind: domain.ContactEmail, Value: " "}, {Kind: domain.ContactPhone},
		{Kind: domain.ContactEmail, Value: strings.Repeat("a", domain.MaxContactValue+1)}, {Kind: domain.ContactWeb, Value: "not a url"}} {
		if _, err := ana.AddContact(d, t0); !errors.Is(err, fw.ErrValidation) {
			t.Errorf("%s %q is refused: %v", d.Kind, d.Value, err)
		}
	}
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ANA.GARCIA@"}, t0); !isViolation(err, "parties.duplicate_contact") {
		t.Fatalf("the same badly formed e-mail twice: %v", err)
	}

	// Quitar el contacto quita su incidencia; corregirlo es terminarlo y añadir otro.
	if err := ana.EndContact(mail, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(ana.DataQualityIssuesOf(mail.UUID)) != 0 || len(ana.DataQualityIssues()) != 2 {
		t.Fatalf("ending the contact clears its issue: %+v", ana.DataQualityIssues())
	}
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@example.com"}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []domain.ContactID{phone, fax} {
		if err := ana.EndContact(id, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if ana.HasDataQualityIssues() || domain.WithDataQualityIssues().IsSatisfiedBy(ana) {
		t.Fatalf("no issues left: %+v", ana.DataQualityIssues())
	}
	if _, ok := ana.AuditSnapshot()["dataQualityIssues"]; ok {
		t.Fatal("the audit record only mentions issues when there are some")
	}
}

// Las incidencias vuelven con la party guardada.
func TestReconstitute_KeepsDataQualityIssues(t *testing.T) {
	ana := person(t, "Ana", "García", "")
	id, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ana.AuditSnapshot()["dataQualityIssues"]; !ok {
		t.Fatal("the audit record mentions the issue")
	}
	back, err := domain.Reconstitute(ana.ID(), domain.PartyState{Kind: domain.KindPerson, Person: ana.Person(), Contacts: ana.Contacts(),
		Quality: ana.DataQualityIssues(), Active: true})
	if err != nil || len(back.DataQualityIssuesOf(id.UUID)) != 1 {
		t.Fatalf("reconstituted: %v", err)
	}
	if err := back.EndContact(id, t0.Add(time.Hour)); err != nil || back.HasDataQualityIssues() || !ana.HasDataQualityIssues() {
		t.Fatal("ending the contact on the copy leaves the original alone")
	}
}

// Terminar un contacto con fecha futura quita su incidencia en el acto, aunque el contacto siga en
// vigor hasta entonces; si vuelve el mismo valor erróneo después, vuelve con una incidencia nueva.
func TestParty_IssueOfAContactEndingLater(t *testing.T) {
	ana := person(t, "Ana", "García", "")
	id, err := ana.AddContact(domain.ContactData{Kind: domain.ContactPhone, Value: "600 11 22 33", Purposes: []domain.Purpose{domain.PurposeDefault}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	first := ana.DataQualityIssuesOf(id.UUID)[0]
	later := fw.Now().Add(24 * time.Hour)
	if err := ana.EndContact(id, later); err != nil {
		t.Fatal(err)
	}
	if c, ok := ana.ContactFor(domain.ContactPhone, domain.PurposeDefault); ok || !ana.Contacts()[0].IsActiveAt(fw.Now()) || c.ID == id {
		t.Fatal("the contact is still in force until it ends, without its purposes")
	}
	if ana.HasDataQualityIssues() {
		t.Fatalf("the issue goes when the end is set, not when it comes: %+v", ana.DataQualityIssues())
	}
	again, err := ana.AddContact(domain.ContactData{Kind: domain.ContactPhone, Value: "600 11 22 33"}, later.Add(time.Hour))
	if err != nil {
		t.Fatalf("the same badly formed phone after the first one ends: %v", err)
	}
	if got := ana.DataQualityIssuesOf(again.UUID); len(got) != 1 || got[0].ID == first.ID || got[0].Code != domain.IssueInvalidPhone {
		t.Fatalf("a new issue: %+v", got)
	}
}

// Un valor con caracteres de control no es un dato que corregir: se rechaza, sin incidencia.
func TestParty_ContactValuesWithControlCharactersAreRefused(t *testing.T) {
	ana := person(t, "Ana", "García", "")
	for _, v := range []string{"ana\x00@example.com", "ana@\nexample", "600\t11 22 33", "+34 600\x00000001"} {
		kind := domain.ContactEmail
		if !strings.Contains(v, "@") {
			kind = domain.ContactPhone
		}
		if _, err := ana.AddContact(domain.ContactData{Kind: kind, Value: v}, t0); !errors.Is(err, fw.ErrValidation) {
			t.Errorf("%q is refused: %v", v, err)
		}
	}
	if len(ana.Contacts()) != 0 || ana.HasDataQualityIssues() {
		t.Fatalf("nothing kept: %+v %+v", ana.Contacts(), ana.DataQualityIssues())
	}
	// Los espacios y saltos de los extremos se recortan, como siempre.
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "\tana@\n"}, t0); err != nil || ana.Contacts()[0].Value != "ana@" {
		t.Fatalf("trimmed: %v", err)
	}
}

// El orden de las incidencias no depende del orden en que llegan ni del reloj.
func TestReconstitute_OrdersIssuesTheSameWay(t *testing.T) {
	ana := person(t, "Ana", "García", "")
	for _, v := range []string{"b@", "a@", "c@"} {
		if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: v}, t0); err != nil {
			t.Fatal(err)
		}
	}
	issues := ana.DataQualityIssues()
	reversed := slices.Clone(issues)
	slices.Reverse(reversed)
	back, err := domain.Reconstitute(ana.ID(), domain.PartyState{Kind: domain.KindPerson, Person: ana.Person(), Contacts: ana.Contacts(),
		Quality: reversed, Active: true})
	if err != nil || !slices.Equal(back.DataQualityIssues(), issues) {
		t.Fatalf("same order: %+v / %+v %v", back.DataQualityIssues(), issues, err)
	}
}
