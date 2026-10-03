package domain_test

import (
	"encoding/xml"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func iban(s string) vocab.IBAN {
	i, err := vocab.NewIBAN(s)
	if err != nil {
		panic(err)
	}
	return i
}

func date(s string) vocab.Date {
	d, err := vocab.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestIdentifiers(t *testing.T) {
	if id, ok := domain.CreditorID("ES", "ZZZ", "A58818501"); !ok || id != "ES30ZZZA58818501" {
		t.Fatalf("creditor id: %s", id)
	}
	if id, _ := domain.CreditorID("es", "000", "a-58818501"); id != "ES30000A58818501" {
		t.Fatalf("the business code does not count in the check digits: %s", id)
	}
	for bic, ok := range map[string]bool{"CAIXESBB": true, "CAIXESBBXXX": true, "CAIXESB": false, "CAI1ESBB": false, "caixesbb": false} {
		if domain.ValidBIC(bic) != ok {
			t.Fatalf("BIC %s", bic)
		}
	}
	if _, err := domain.ReconstituteAccount(domain.NewAccountID(), domain.AccountState{Owner: domain.OrganizationID{UUID: fw.NewUUID()},
		IBAN: iban("ES9121000418450200051332"), BIC: "XX", Alias: "Principal", Currency: vocab.MustCurrencyCode("EUR"), Opened: date("2020-01-01")}); err == nil {
		t.Fatal("invalid BIC")
	}
	a, err := domain.ReconstituteAccount(domain.NewAccountID(), domain.AccountState{Owner: domain.OrganizationID{UUID: fw.NewUUID()},
		IBAN: iban("ES9121000418450200051332"), Alias: "Principal", Currency: vocab.MustCurrencyCode("EUR"), Opened: date("2020-01-01")})
	if err != nil || a.State().CreditorSuffix != "000" {
		t.Fatal(err)
	}
	_ = a.Close(date("2026-12-31"))
	if a.OpenOn(date("2027-01-01")) || !a.OpenOn(date("2026-12-31")) {
		t.Fatal("closed at the end of the day")
	}
}

func TestMandate(t *testing.T) {
	st := domain.MandateState{Creditor: domain.OrganizationID{UUID: fw.NewUUID()}, Debtor: domain.PartyID{UUID: fw.NewUUID()},
		IBAN: iban("ES7921000813610123456789"), Reference: "MANDATO-ÑU", Scheme: domain.Core, Signed: date("2023-01-10")}
	if _, err := domain.ReconstituteMandate(domain.NewMandateID(), st); err == nil {
		t.Fatal("SEPA characters only")
	}
	st.Reference = "MAND-0001"
	m, err := domain.ReconstituteMandate(domain.NewMandateID(), st)
	if err != nil || m.Sequence() != "FRST" {
		t.Fatal(err)
	}
	if m.UsableOn(date("2026-01-10")) {
		t.Fatal("36 months without use: expired")
	}
	if err := m.Use(date("2025-06-01")); err != nil || m.Sequence() != "RCUR" || !m.UsableOn(date("2028-05-31")) || m.UsableOn(date("2028-06-01")) {
		t.Fatalf("use: %v", err)
	}
	_ = m.Revoke(date("2026-01-01"))
	if err := m.Use(date("2026-02-01")); !isViolation(err, "treasury.mandate_unusable") {
		t.Fatal("revoked")
	}
}

func TestRemittanceAndPain008(t *testing.T) {
	creditor := domain.OrganizationID{UUID: fw.NewUUID()}
	r, _ := domain.DraftRemittance(domain.NewRemittanceID(), creditor, domain.NewAccountID(), domain.Core, date("2026-11-10"))
	m1, m2 := domain.NewMandateID(), domain.NewMandateID()
	inv := domain.InvoiceID{UUID: fw.NewUUID()}
	item := func(inst int, amount string, m domain.MandateID, name string) domain.Item {
		return domain.Item{Invoice: inv, Number: "FA-2026-000001", Installment: inst, Debtor: domain.PartyID{UUID: fw.NewUUID()}, DebtorName: name,
			Mandate: m, MandateRef: "MAND-" + name[:3], Signed: date("2025-01-10"), IBAN: iban("ES7921000813610123456789"), Amount: vocab.MustDecimal(amount)}
	}
	if err := r.Add(item(1, "0", m1, "Ana")); !isViolation(err, "treasury.item_amount") {
		t.Fatal("a positive amount")
	}
	_ = r.Add(item(1, "60.50", m1, "Ana García Núñez"))
	if err := r.Add(item(1, "60.50", m1, "Ana")); !isViolation(err, "treasury.item_duplicate") {
		t.Fatal("one item per installment")
	}
	_ = r.Add(item(2, "39.50", m2, "Bea López"))
	if err := r.Settle(date("2026-11-10")); !isViolation(err, "treasury.remittance_not_generated") {
		t.Fatal("generate first")
	}
	if _, err := r.Pain008(); err == nil {
		t.Fatal("no file for a draft")
	}
	c := domain.Creditor{Name: "Acme, S.A.", ID: "ES30000A58818501", IBAN: iban("ES9121000418450200051332"), BIC: "CAIXESBBXXX"}
	if err := r.Generate(c, map[domain.MandateID]string{m1: "FRST"}, time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)); !isViolation(err, "treasury.sequence") {
		t.Fatal("each item needs its sequence")
	}
	if err := r.Generate(c, map[domain.MandateID]string{m1: "FRST", m2: "RCUR"}, time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(item(3, "1", m1, "Ana")); !isViolation(err, "treasury.remittance_not_draft") {
		t.Fatal("frozen")
	}
	file, err := r.Pain008()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Header struct {
			Count int    `xml:"NbOfTxs"`
			Sum   string `xml:"CtrlSum"`
		} `xml:"CstmrDrctDbtInitn>GrpHdr"`
		Payments []struct {
			Sequence string `xml:"PmtTpInf>SeqTp"`
			Scheme   string `xml:"PmtTpInf>LclInstrm>Cd"`
			Date     string `xml:"ReqdColltnDt"`
			Creditor string `xml:"CdtrSchmeId>Id>PrvtId>Othr>Id"`
			Debits   []struct {
				EndToEnd string `xml:"PmtId>EndToEndId"`
				Amount   string `xml:"InstdAmt"`
				Debtor   string `xml:"Dbtr>Nm"`
				Agent    string `xml:"DbtrAgt>FinInstnId>Othr>Id"`
			} `xml:"DrctDbtTxInf"`
		} `xml:"CstmrDrctDbtInitn>PmtInf"`
	}
	if err := xml.Unmarshal(file, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(file), `xmlns="urn:iso:std:iso:20022:tech:xsd:pain.008.001.02"`) || doc.Header.Count != 2 || doc.Header.Sum != "100.00" ||
		len(doc.Payments) != 2 || doc.Payments[0].Sequence != "FRST" || doc.Payments[1].Sequence != "RCUR" || doc.Payments[0].Scheme != "CORE" ||
		doc.Payments[0].Date != "2026-11-10" || doc.Payments[0].Creditor != "ES30000A58818501" ||
		doc.Payments[0].Debits[0].EndToEnd != "FA-2026-000001-1" || doc.Payments[0].Debits[0].Debtor != "Ana Garcia Nunez" ||
		doc.Payments[0].Debits[0].Agent != "NOTPROVIDED" || doc.Payments[1].Debits[0].Amount != "39.50" {
		t.Fatalf("pain.008:\n%s", file)
	}
	if err := r.Settle(date("2026-11-10")); err != nil {
		t.Fatal(err)
	}
	if err := r.Return("FA-2026-000001-2", date("2026-11-09"), "AM04"); !isViolation(err, "treasury.return_invalid") {
		t.Fatal("not before the settlement")
	}
	if err := r.Return("FA-2026-000001-2", date("2026-11-20"), "am04"); err != nil {
		t.Fatal(err)
	}
	if err := r.Return("FA-2026-000001-2", date("2026-11-21"), "AM04"); !isViolation(err, "treasury.already_returned") {
		t.Fatal("once")
	}
	if r.Cancel() == nil {
		t.Fatal("a settled remittance is not cancelled")
	}
}
