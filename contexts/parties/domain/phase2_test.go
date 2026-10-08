package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

var (
	policy = domain.WellKnownDocumentPolicy()
	es     = vocab.MustCountryCode("ES")
	pt     = vocab.MustCountryCode("PT")
	us     = vocab.MustCountryCode("US")
)

func TestDocumentPolicy(t *testing.T) {
	ok := []domain.IdentificationData{
		{Type: domain.DocNationalID, Country: es, Number: " 12345678z "},
		{Type: domain.DocTaxID, Country: es, Number: "B12345674"},
		{Type: domain.DocTaxID, Country: pt, Number: "501964843"},
		{Type: domain.DocAlienRegistation, Country: es, Number: "X1234567L", ExpiresOn: vocab.MustDate(2030, 1, 1)},
		{Type: domain.DocPassport, Country: us, Number: "X12345678", ExpiresOn: vocab.MustDate(2030, 1, 1), IssuingAuthority: "US Department of State"},
		{Type: domain.DocOther, Country: us, Number: "anything-123"},
	}
	for _, d := range ok {
		if _, err := policy.Validate(d); err != nil {
			t.Errorf("%s %s: %v", d.Country, d.Number, err)
		}
	}
	if n, _ := policy.Validate(ok[0]); n != "12345678Z" {
		t.Fatalf("canonical number: %q", n)
	}
	for in, want := range map[string]string{"12345678-z": "12345678Z", "131052-308T": "131052-308T", "811218-9876": "8112189876"} {
		country := es
		switch in {
		case "131052-308T":
			country = vocab.MustCountryCode("FI")
		case "811218-9876":
			country = vocab.MustCountryCode("SE")
		}
		typ := domain.DocNationalID
		if country != es {
			typ = domain.DocTaxID
		}
		if n, err := policy.Validate(domain.IdentificationData{Type: typ, Country: country, Number: in}); err != nil || n != want {
			t.Errorf("%s: %q %v, want %q (separators dropped unless the format needs them)", in, n, err, want)
		}
	}
	bad := []domain.IdentificationData{
		{Type: domain.DocNationalID, Country: es, Number: "12345678A"},                                      // check letter
		{Type: domain.DocNationalID, Country: es, Number: "1234567Z"},                                       // length and format
		{Type: domain.DocTaxID, Country: es, Number: "12345678Z"},                                           // TXID in ES is the CIF form
		{Type: domain.DocTaxID, Country: pt, Number: "501964844"},                                           // PT check digit
		{Type: domain.DocAlienRegistation, Country: es, Number: "X1234567L"},                                // NIE requires expiry
		{Type: domain.DocPassport, Country: us, Number: "X12345678", ExpiresOn: vocab.MustDate(2030, 1, 1)}, // authority
		{Type: domain.DocumentTypeID{UUID: fw.NewUUID()}, Country: es, Number: "1"},
		{Type: domain.DocNationalID, Number: "12345678Z"},
	}
	for i, d := range bad {
		if _, err := policy.Validate(d); !errors.Is(err, fw.ErrValidation) {
			t.Errorf("bad %d must be a validation error: %v", i, err)
		}
	}
	if opts := policy.Options(es); len(opts) != 5 || opts[0].DocumentType != domain.DocNationalID {
		t.Fatalf("Spanish options in display order: %+v", opts)
	}
	if _, err := domain.NewDocumentPolicy(domain.WellKnownDocumentTypes(), []domain.CountryDocumentRule{{ID: "x", Country: es,
		DocumentType: domain.DocTaxID, CheckDigit: "NOPE", Active: true}}); err == nil {
		t.Fatal("unknown check digit keys fail at load time")
	}
}

func TestParty_Identifications(t *testing.T) {
	ana := person(t, "Ana", "García", "")
	first, err := ana.AddIdentification(policy, domain.IdentificationData{Type: domain.DocNationalID, Country: es, Number: "12345678Z"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := ana.PrimaryIdentification(); p.ID != first {
		t.Fatal("the first document is primary")
	}
	if _, err := ana.AddIdentification(policy, domain.IdentificationData{Type: domain.DocNationalID, Country: es, Number: "12345678-z"}, false); !isViolation(err, "parties.duplicate_identification") {
		t.Fatalf("duplicate: %v", err)
	}
	second, err := ana.AddIdentification(policy, domain.IdentificationData{Type: domain.DocPassport, Country: es, Number: "PAA123456",
		ExpiresOn: vocab.MustDate(2031, 2, 3), IssuingAuthority: "Policía Nacional"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := ana.PrimaryIdentification(); p.ID != second || len(ana.Identifications()) != 2 {
		t.Fatal("a new primary demotes the previous one")
	}
	if !domain.HoldsDocument(domain.DocNationalID, "ES", "12345678Z").IsSatisfiedBy(ana) || !domain.WithDocumentNumber("PAA123456").IsSatisfiedBy(ana) {
		t.Fatal("specs")
	}
	if err := ana.RemoveIdentification(first); err != nil || len(ana.Identifications()) != 1 {
		t.Fatal(err)
	}
	if err := ana.RemoveIdentification(first); !errors.Is(err, fw.ErrNotFound) {
		t.Fatal("remove twice")
	}
}

func TestParty_Contacts(t *testing.T) {
	ana := person(t, "Ana", "García", "")
	e1, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "Ana@Example.COM", Purposes: []domain.Purpose{domain.PurposeDefault}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@example.com"}, t0); !isViolation(err, "parties.duplicate_contact") {
		t.Fatalf("duplicate e-mail: %v", err)
	}
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "not-an-email"}, t0); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("invalid e-mail")
	}
	e2, _ := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "work@acme.test", Purposes: []domain.Purpose{domain.PurposeDefault, domain.PurposeWork}}, t0)
	if c, ok := ana.ContactFor(domain.ContactEmail, domain.PurposeDefault); !ok || c.ID != e2 {
		t.Fatal("the default purpose moves to the new holder")
	}
	if err := ana.SetContactPurposes(e1, []domain.Purpose{domain.PurposeDefault}); err != nil {
		t.Fatal(err)
	}
	if c, _ := ana.ContactFor(domain.ContactEmail, domain.PurposeDefault); c.ID != e1 {
		t.Fatal("purposes can move back")
	}
	if c, _ := ana.ContactFor(domain.ContactEmail, domain.PurposeWork); c.ID != e2 {
		t.Fatal("other purposes stay")
	}

	addr := domain.PostalAddress{StreetType: "cl", Line1: " Mayor  1 ", PostalCode: "28013", Locality: "Madrid", Country: es,
		Geo: domain.GeoRef{Boundary: fw.NewUUID()}}
	home, err := ana.AddContact(domain.ContactData{Kind: domain.ContactPostal, Address: addr, Purposes: []domain.Purpose{domain.PurposeHome, domain.PurposeBilling}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := ana.ContactFor(domain.ContactPostal, domain.PurposeBilling); c.ID != home || c.Address.String() != "CL Mayor 1, 28013 Madrid, ES" {
		t.Fatalf("address: %q", c.Address.String())
	}
	// An e-mail and an address are different kinds: both can be "default".
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactPhone, Value: "+34 600 000 001", Purposes: []domain.Purpose{domain.PurposeDefault}}, t0); err != nil {
		t.Fatal(err)
	}
	if err := ana.EndContact(e1, t0.Add(-time.Hour)); !isViolation(err, "parties.contact_end_before_start") {
		t.Fatal("end before start")
	}
	if err := ana.EndContact(e1, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok := ana.ContactFor(domain.ContactEmail, domain.PurposeDefault); ok {
		t.Fatal("an ended contact loses its purposes")
	}
	if err := ana.SetContactPurposes(e1, []domain.Purpose{domain.PurposeDefault}); !isViolation(err, "parties.contact_ended") {
		t.Fatal("an ended contact cannot get purposes")
	}
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@example.com"}, t0.Add(2*time.Hour)); err != nil {
		t.Fatalf("the same e-mail can come back after it ended: %v", err)
	}
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactPostal, Address: domain.PostalAddress{Country: es}}, t0); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("an address needs a first line")
	}
}

func TestParty_Classifications(t *testing.T) {
	cc := domain.WellKnownClassificationCatalog()
	acme := org(t, "Acme", "")
	ana := person(t, "Ana", "García", "")

	if _, err := acme.Classify(cc, domain.ClassSegmentFamily, t0); !isViolation(err, "parties.classification_is_family") {
		t.Fatalf("families: %v", err)
	}
	if _, err := acme.Classify(cc, domain.ClassAmlLow, t0); !isViolation(err, "parties.classification_inactive") {
		t.Fatalf("AML is inactive until compliance enables it: %v", err)
	}
	if _, err := ana.Classify(cc, domain.ClassSizeMicro, t0); !isViolation(err, "parties.classification_incompatible") {
		t.Fatalf("a person has no organization size: %v", err)
	}
	retail, err := acme.Classify(cc, domain.ClassRetail, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acme.Classify(cc, domain.ClassCorporate, t0.AddDate(0, 1, 0)); !isViolation(err, "parties.classification_exclusive") {
		t.Fatalf("one customer segment at a time: %v", err)
	}
	if err := acme.EndClassification(retail, t0.AddDate(0, 1, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := acme.Classify(cc, domain.ClassCorporate, t0.AddDate(0, 1, 0)); err != nil {
		t.Fatalf("after ending, the segment changes: %v", err)
	}
	// Sectors are not exclusive.
	if _, err := acme.Classify(cc, domain.ClassFinancial, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := acme.Classify(cc, domain.ClassCommerce, t0); err != nil {
		t.Fatal(err)
	}
	if !domain.ClassifiedAt(t0.AddDate(0, 2, 0), domain.ClassCorporate).IsSatisfiedBy(acme) ||
		domain.ClassifiedAt(t0.AddDate(0, 2, 0), domain.ClassRetail).IsSatisfiedBy(acme) {
		t.Fatal("ClassifiedAt honours validity")
	}
	if len(cc.All()) != 27 {
		t.Fatalf("catalog: %d", len(cc.All()))
	}
}
