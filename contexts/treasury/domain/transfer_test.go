package domain_test

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func TestTransferOrderAndPain001(t *testing.T) {
	o, err := domain.DraftTransferOrder(domain.NewTransferOrderID(), domain.OrganizationID{UUID: fw.NewUUID()}, domain.NewAccountID(), date("2026-10-05"))
	if err != nil {
		t.Fatal(err)
	}
	supplier, payslip := domain.PayableID{UUID: fw.NewUUID()}, domain.PayableID{UUID: fw.NewUUID()}
	add := func(p domain.PayableID, line int, name, account, amount string) error {
		return o.Add(domain.Transfer{Payable: p, Line: line, Document: "Factura F-1/2026", Payee: fw.NewUUID().String(), PayeeName: name, IBAN: iban(account),
			Amount: vocab.MustDecimal(amount)})
	}
	if err := add(supplier, 1, "Suministros Núñez, S.L.", "ES9121000418450200051332", "0"); !isViolation(err, "treasury.transfer_invalid") {
		t.Fatalf("zero amount: %v", err)
	}
	if err := add(supplier, 1, "Suministros Núñez, S.L.", "ES9121000418450200051332", "605.00"); err != nil {
		t.Fatal(err)
	}
	if err := add(supplier, 1, "Otro", "ES9121000418450200051332", "1"); !isViolation(err, "treasury.transfer_duplicate") {
		t.Fatalf("duplicate: %v", err)
	}
	if err := add(payslip, 1, "Ana Muñoz", "ES7921000813610123456789", "1000.00"); err != nil {
		t.Fatal(err)
	}
	if err := add(payslip, 2, "Ana Muñoz", "ES6621000418401234567891", "469.70"); err != nil {
		t.Fatal(err)
	}
	if got := o.State().Transfers[1].EndToEnd; len(got) != 34 || !strings.HasSuffix(got, "-1") {
		t.Fatalf("end to end: %s", got)
	}
	if _, err := o.Pain001(); !isViolation(err, "treasury.order_not_generated") {
		t.Fatalf("file of a draft: %v", err)
	}
	if err := o.Settle(date("2026-10-05")); !isViolation(err, "treasury.order_not_generated") {
		t.Fatalf("settle a draft: %v", err)
	}
	if err := o.Generate(domain.Debtor{Name: "Acme"}, time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)); !isViolation(err, "treasury.debtor_incomplete") {
		t.Fatalf("debtor without account: %v", err)
	}
	if err := o.Generate(domain.Debtor{Name: "Acme, S.A.", IBAN: iban("ES9121000418450200051332"), BIC: "CAIXESBBXXX"},
		time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := o.Remove(supplier); !isViolation(err, "treasury.order_not_draft") {
		t.Fatalf("frozen: %v", err)
	}
	file, err := o.Pain001()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		XMLName xml.Name
		Header  struct {
			Count int    `xml:"NbOfTxs"`
			Sum   string `xml:"CtrlSum"`
		} `xml:"CstmrCdtTrfInitn>GrpHdr"`
		Payment struct {
			Method    string `xml:"PmtMtd"`
			Execution string `xml:"ReqdExctnDt"`
			IBAN      string `xml:"DbtrAcct>Id>IBAN"`
			BIC       string `xml:"DbtrAgt>FinInstnId>BIC"`
			Credits   []struct {
				EndToEnd string `xml:"PmtId>EndToEndId"`
				Amount   string `xml:"Amt>InstdAmt"`
				Name     string `xml:"Cdtr>Nm"`
				IBAN     string `xml:"CdtrAcct>Id>IBAN"`
			} `xml:"CdtTrfTxInf"`
		} `xml:"CstmrCdtTrfInitn>PmtInf"`
	}
	if err := xml.Unmarshal(file, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.XMLName.Space != "urn:iso:std:iso:20022:tech:xsd:pain.001.001.03" || doc.Header.Count != 3 || doc.Header.Sum != "2074.70" ||
		doc.Payment.Method != "TRF" || doc.Payment.Execution != "2026-10-05" || doc.Payment.BIC != "CAIXESBBXXX" || len(doc.Payment.Credits) != 3 ||
		doc.Payment.Credits[0].Name != "Suministros Nunez, S.L." || doc.Payment.Credits[2].Amount != "469.70" {
		t.Fatalf("pain.001:\n%s", file)
	}

	if err := o.Settle(date("2026-09-30")); !isViolation(err, "treasury.settle_date") {
		t.Fatalf("settled before generated: %v", err)
	}
	if err := o.Settle(date("2026-10-05")); err != nil {
		t.Fatal(err)
	}
	e2e := o.State().Transfers[0].EndToEnd
	if err := o.Reject(e2e, date("2026-10-04"), "AC04"); !isViolation(err, "treasury.reject_invalid") {
		t.Fatalf("rejected before execution: %v", err)
	}
	if err := o.Reject(e2e, date("2026-10-07"), "ac04"); err != nil {
		t.Fatal(err)
	}
	if err := o.Reject(e2e, date("2026-10-07"), "AC04"); !isViolation(err, "treasury.already_rejected") {
		t.Fatalf("rejected twice: %v", err)
	}
	if err := o.Cancel(); !isViolation(err, "treasury.order_closed") {
		t.Fatalf("cancel executed: %v", err)
	}
}
