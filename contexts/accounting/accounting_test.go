package accounting_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury"
	tapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	tdomain "github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	tinfra "github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/testkit"
)

// host composes Parties, Fiscal, Billing, Receivables, Treasury and Accounting on one hot-swappable
// backend, with one in-process broker carrying the Published Language between them. Payroll is
// played by hand-made envelopes (its composition needs HR and Facilities).
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	acc      *accounting.Module
	treasury *treasury.Module
	rec      *receivables.Module
	billing  *billing.Module
	fiscal   *fiscal.Module
	parties  *parties.Module
	broker   *inprocess.Broker
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	pm := parties.Compose(sw, nil)
	fm := fiscal.Compose(sw, finfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	bm := billing.Compose(sw, binfra.FiscalTaxes{Engine: fm.TaxEngine}, binfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	rm := receivables.Compose(sw, nil)
	tm := treasury.Compose(sw, tinfra.ReceivablesDueItems{Collectable: rm.Collectable}, nil, tinfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	am := accounting.Compose(sw)
	broker := inprocess.NewBroker()
	broker.Subscribe("receivables", rm.Consumer)
	broker.Subscribe("accounting", am.Consumer)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("accounting-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, acc: am, treasury: tm, rec: rm, billing: bm, fiscal: fm,
		parties: pm, broker: broker}
	all := []authz.Permission{aapp.PermAccountRead, aapp.PermAccountWrite, aapp.PermLedgerRead, aapp.PermLedgerWrite, aapp.PermEntryRead,
		aapp.PermEntryCreate, aapp.PermEntryReverse}
	users := map[string][]authz.Permission{"accountant": all, "viewer": {aapp.PermAccountRead, aapp.PermLedgerRead, aapp.PermEntryRead},
		"outsider": all}
	for u, perms := range users {
		h.ids[u] = fw.NewUUID()
		dir.Put(h.ids[u], authz.Subject{Active: true, Permissions: perms})
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	ac.GlobalAdmin = true
	h.adminCtx = authz.WithContext(ctx, ac)

	mux := http.NewServeMux()
	am.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	return h
}

func (h *host) grant(user string, orgs ...string) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	for _, o := range orgs {
		s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(o), Level: authz.Full})
	}
	h.dir.Put(h.ids[user], s)
}

func (h *host) do(method, path, user string, body, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, &buf)
	req.Header.Set("Authorization", h.tokens[user])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			h.t.Fatal(err)
		}
	}
	return res.StatusCode
}

func (h *host) must(got, want int, what string) {
	h.t.Helper()
	if got != want {
		h.t.Fatalf("%s: status %d, want %d", what, got, want)
	}
}

func (h *host) ok(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatal(err)
	}
}

// deliver relays the Published Language of Billing, Treasury and Receivables until quiet.
func (h *host) deliver() {
	for moved := true; moved; {
		moved = false
		for _, r := range []interface {
			RelayOnce(context.Context) (int, error)
		}{h.billing.Relay(h.broker), h.treasury.Relay(h.broker), h.rec.Relay(h.broker)} {
			n, err := r.RelayOnce(context.Background())
			h.ok(err)
			moved = moved || n > 0
		}
	}
}

// payroll plays Payroll: it sends one of its facts to the broker.
func (h *host) payroll(id, typ string, data any) {
	h.t.Helper()
	raw, _ := json.Marshal(data)
	h.ok(h.broker.Send(context.Background(), application.Envelope{ID: id, Type: typ, Source: "payroll", OccurredAt: fw.Now(), Data: raw}))
}

func (h *host) balances(company string) map[string]string {
	h.t.Helper()
	var rows []aapp.BalanceRow
	h.must(h.do("GET", "/api/accounting/trial-balance?company="+company, "viewer", nil, &rows), 200, "trial balance")
	out := map[string]string{}
	for _, r := range rows {
		out[r.Account] = r.Balance
	}
	return out
}

func (h *host) expect(company string, want map[string]string) {
	h.t.Helper()
	got := h.balances(company)
	for acc, b := range want {
		if got[acc] != b {
			h.t.Fatalf("balance of %s = %q, want %s (all: %v)", acc, got[acc], b, got)
		}
	}
}

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	ps := h.parties.Service
	org := func(name string) papp.PartyDTO {
		o, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag, Roles: []string{pdomain.RoleInternalOrganization.String()}})
		h.ok(err)
		return o
	}
	acme, globex := org("Acme"), org("Globex")
	h.grant("accountant", acme.ID)
	h.grant("viewer", acme.ID)
	h.grant("outsider", globex.ID)
	doc := func(party, docType, number string) {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: docType, Country: "ES", Number: number, Primary: true})
		h.ok(err)
	}
	doc(acme.ID, "c0000000-0004-0000-0000-000000000003", "A58818501")
	person := func(name, dni string) papp.PartyDTO {
		p, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: name, FirstSurname: "Núñez " + tag})
		h.ok(err)
		doc(p.ID, "c0000000-0004-0000-0000-000000000002", dni)
		return p
	}
	ana, bea := person("Ana", "12345678Z"), person("Bea", "00000000T")

	// The chart and the ledger of Acme.
	chart := map[string]string{"43": "Clientes (cabecera)", "4300": "Clientes", "7000": "Ventas", "4770": "HP IVA repercutido", "4775": "Recargo de equivalencia",
		"5700": "Caja", "5720": "Bancos", "4312": "Efectos al cobro", "6400": "Sueldos y salarios", "6420": "Seguridad Social a cargo de la empresa",
		"4760": "Organismos de la Seguridad Social, acreedores", "4751": "HP acreedora por retenciones", "4650": "Remuneraciones pendientes de pago",
		"4600": "Otras deducciones"}
	codes := make([]string, 0, len(chart))
	for c := range chart {
		codes = append(codes, c)
	}
	slices.Sort(codes)
	for _, c := range codes {
		h.must(h.do("POST", "/api/accounting/accounts", "accountant", map[string]any{"company": acme.ID, "code": c, "name": chart[c], "postable": c != "43"}, nil),
			201, "account "+c)
	}
	h.must(h.do("POST", "/api/accounting/accounts", "accountant", map[string]any{"company": acme.ID, "code": "4300", "name": "Otra", "postable": true}, nil),
		422, "the code is unique in the chart")
	h.must(h.do("POST", "/api/accounting/accounts", "accountant", map[string]any{"company": acme.ID, "code": "0430", "name": "X", "postable": true}, nil),
		400, "codes start with 1-9")
	h.must(h.do("POST", "/api/accounting/accounts", "viewer", map[string]any{"company": acme.ID, "code": "5701", "name": "X", "postable": true}, nil),
		403, "reading is not writing")
	h.must(h.do("POST", "/api/accounting/accounts", "outsider", map[string]any{"company": acme.ID, "code": "5701", "name": "X", "postable": true}, nil),
		404, "outsider")
	var customers []aapp.AccountDTO
	h.must(h.do("GET", "/api/accounting/accounts?company="+acme.ID+"&prefix=43", "viewer", nil, &customers), 200, "chart")
	if len(customers) != 3 || customers[0].Code != "43" || customers[0].Postable || customers[1].Nature != "balance-sheet" {
		t.Fatalf("chart under 43: %+v", customers)
	}

	profile := map[string]string{"revenue": "7000", "customers": "4300", "surcharge": "4775", "cash": "5700", "bank": "5720",
		"direct-debit-clearing": "4312", "wages": "6400", "employer-social-security": "6420", "social-security-payable": "4760",
		"withholding-payable": "4751", "other-deductions": "4600", "net-pay-payable": "4650"}
	var ledger aapp.LedgerDTO
	h.must(h.do("POST", "/api/accounting/ledgers", "accountant", map[string]any{"company": acme.ID, "startMonth": 1, "accounts": profile,
		"taxCodes": map[string]string{"g21": "4770"}}, &ledger), 201, "ledger")
	h.must(h.do("POST", "/api/accounting/ledgers", "accountant", map[string]any{"company": acme.ID, "startMonth": 1}, nil), 422, "one ledger per company")
	h.must(h.do("GET", "/api/accounting/ledgers?company="+acme.ID, "viewer", nil, &ledger), 200, "get ledger")
	if ledger.TaxCodes["G21"] != "4770" || ledger.Accounts["bank"] != "5720" {
		t.Fatalf("ledger: %+v", ledger)
	}

	// Sales: two invoices of 21% VAT post customers against revenue and output tax.
	_, err := h.fiscal.Service.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	h.ok(err)
	_, err = h.fiscal.Service.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)
	fa, err := h.billing.Service.OpenSeries.Handle(ctx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
	h.ok(err)
	invoice := func(customer, price string) bapp.InvoiceDTO {
		inv, err := h.billing.Service.DraftInvoice.Handle(ctx, bapp.DraftInvoice{Seller: acme.ID, Customer: customer,
			DetailsInput: bapp.DetailsInput{DueDate: vocab.MustDate(2026, 10, 15)}})
		h.ok(err)
		id, _ := bdomain.ParseInvoiceID(inv.ID)
		_, err = h.billing.Service.AddLine.Handle(ctx, bapp.AddLine{ID: id, Description: "Cuota", Quantity: "1", UnitPrice: price, TaxCode: "G21"})
		h.ok(err)
		inv, err = h.billing.Service.Issue.Handle(ctx, bapp.IssueInvoice{ID: id, Series: fa.ID, Date: vocab.MustDate(2026, 9, 28)})
		h.ok(err)
		return inv
	}
	invAna, invBea := invoice(ana.ID, "100"), invoice(bea.ID, "50") // 121.00 and 60.50
	h.deliver()
	h.expect(acme.ID, map[string]string{"4300": "181.50", "7000": "-150.00", "4770": "-31.50"})
	var journal fw.Page[aapp.EntryDTO]
	h.must(h.do("GET", "/api/accounting/entries?company="+acme.ID, "viewer", nil, &journal), 200, "journal")
	if len(journal.Items) != 2 || journal.Items[0].Number != 1 || journal.Items[0].Date != "2026-09-28" || journal.Items[0].Total != "121.00" ||
		journal.Items[0].Source != "billing.invoice-issued.v1" || len(journal.Items[0].Lines) != 3 || journal.Items[0].Lines[0].Party != ana.ID {
		t.Fatalf("journal: %+v", journal.Items)
	}

	// Bea pays by transfer: bank against customers.
	_, err = h.rec.Service.RegisterCollection.Handle(ctx, rapp.RegisterCollection{Seller: acme.ID, Payer: bea.ID, Date: vocab.MustDate(2026, 10, 1),
		Amount: "60.50", Method: "transfer", Allocations: []rapp.AllocationInput{{Invoice: invBea.ID, Installment: 1, Amount: "60.50"}}})
	h.ok(err)
	h.deliver()
	h.expect(acme.ID, map[string]string{"4300": "121.00", "5720": "60.50"})

	// Ana pays by direct debit: the bank charges (bank against the clearing account, which
	// Receivables' allocation credits against customers), then the debit is returned.
	ts := h.treasury.Service
	bank, err := ts.OpenAccount.Handle(ctx, tapp.OpenAccount{Owner: acme.ID, IBAN: "ES91 2100 0418 4502 0005 1332", BIC: "CAIXESBBXXX", Alias: "Cobros " + tag,
		Collections: true, Opened: vocab.MustDate(2020, 1, 1)})
	h.ok(err)
	_, err = ts.RegisterMandate.Handle(ctx, tapp.RegisterMandate{Creditor: acme.ID, Debtor: ana.ID, IBAN: "ES7921000813610123456789", Reference: "MAND-ANA",
		Scheme: "CORE", Signed: vocab.MustDate(2025, 1, 10)})
	h.ok(err)
	rem, err := ts.Propose.Handle(ctx, tapp.ProposeRemittance{Creditor: acme.ID, Account: bank.ID, Scheme: "CORE", CollectionDate: vocab.MustDate(2026, 10, 20),
		DueTo: vocab.MustDate(2026, 10, 31)})
	h.ok(err)
	if len(rem.Items) != 1 || rem.Items[0].Invoice != invAna.ID {
		t.Fatalf("remittance: %+v", rem)
	}
	rid, _ := tdomain.ParseRemittanceID(rem.ID)
	_, err = ts.Generate.Handle(ctx, tapp.GenerateRemittance{ID: rid})
	h.ok(err)
	_, err = ts.Settle.Handle(ctx, tapp.SettleRemittance{ID: rid, On: vocab.MustDate(2026, 10, 20)})
	h.ok(err)
	h.deliver()
	h.expect(acme.ID, map[string]string{"4300": "0.00", "5720": "181.50", "4312": "0.00"})
	_, err = ts.Return.Handle(ctx, tapp.ReturnDebit{ID: rid, EndToEnd: rem.Items[0].EndToEnd, On: vocab.MustDate(2026, 10, 27), Reason: "AM04"})
	h.ok(err)
	h.deliver()
	h.expect(acme.ID, map[string]string{"4300": "121.00", "5720": "60.50", "4312": "0.00"})
	var mayor []aapp.Movement
	h.must(h.do("GET", "/api/accounting/ledger?company="+acme.ID+"&account=4300", "viewer", nil, &mayor), 200, "account ledger")
	if len(mayor) != 5 || mayor[0].Balance != "121.00" || mayor[1].Balance != "181.50" || mayor[4].Balance != "121.00" || mayor[4].Debit != "121.00" {
		t.Fatalf("ledger of 4300: %+v", mayor)
	}

	// Payroll: an approved payslip posts wages and social security; its redelivery and a second
	// message about the same payslip post nothing; its cancellation reverses it.
	slip := fw.NewUUID().String()
	approved := map[string]any{"payslipId": slip, "person": ana.ID, "employer": acme.ID, "periodEnd": "2026-09-30", "gross": "2000.00",
		"socialSecurity": "127.00", "incomeTax": "300.00", "otherDeductions": "0.00", "net": "1573.00", "employerCost": "640.00"}
	msg := fw.NewUUID().String()
	h.payroll(msg, "payroll.payslip-approved.v1", approved)
	h.payroll(msg, "payroll.payslip-approved.v1", approved)
	h.payroll(fw.NewUUID().String(), "payroll.payslip-approved.v1", approved)
	h.expect(acme.ID, map[string]string{"6400": "2000.00", "6420": "640.00", "4760": "-767.00", "4751": "-300.00", "4650": "-1573.00"})
	if b := h.balances(acme.ID); b["4600"] != "" {
		t.Fatalf("no other deductions line: %v", b)
	}

	// Manual entries: balanced, on detail accounts, in open periods; reversed once.
	var e1 aapp.EntryDTO
	cashIn := map[string]any{"company": acme.ID, "date": "2026-10-05", "description": "Retirada de caja", "lines": []map[string]any{
		{"account": "5700", "debit": "500"}, {"account": "5720", "credit": "500"}}}
	h.must(h.do("POST", "/api/accounting/entries", "accountant", cashIn, &e1), 201, "manual entry")
	h.must(h.do("POST", "/api/accounting/entries", "viewer", cashIn, nil), 403, "viewer")
	h.must(h.do("POST", "/api/accounting/entries", "accountant", map[string]any{"company": acme.ID, "date": "2026-10-05", "description": "Descuadre",
		"lines": []map[string]any{{"account": "5700", "debit": "500"}, {"account": "5720", "credit": "499.99"}}}, nil), 400, "unbalanced")
	h.must(h.do("POST", "/api/accounting/entries", "accountant", map[string]any{"company": acme.ID, "date": "2026-10-05", "description": "Cabecera",
		"lines": []map[string]any{{"account": "43", "debit": "5"}, {"account": "5720", "credit": "5"}}}, nil), 422, "header account")
	h.must(h.do("POST", "/api/accounting/entries", "accountant", map[string]any{"company": acme.ID, "date": "2026-10-05", "description": "Fuera",
		"lines": []map[string]any{{"account": "5701", "debit": "5"}, {"account": "5720", "credit": "5"}}}, nil), 422, "account not in the chart")
	h.expect(acme.ID, map[string]string{"5700": "500.00", "5720": "-439.50"})
	var rev aapp.EntryDTO
	h.must(h.do("POST", "/api/accounting/entries/"+e1.ID+"/reverse", "accountant", map[string]any{"date": "2026-10-06"}, &rev), 200, "reverse")
	if rev.Reverses != e1.ID || rev.Lines[0].Account != "5700" || rev.Lines[0].Credit != "500.00" {
		t.Fatalf("reversal: %+v", rev)
	}
	h.must(h.do("POST", "/api/accounting/entries/"+e1.ID+"/reverse", "accountant", nil, nil), 422, "reversed once")
	h.must(h.do("POST", "/api/accounting/entries/"+rev.ID+"/reverse", "accountant", nil, nil), 422, "a reversal is not reversed")
	h.must(h.do("GET", "/api/accounting/entries/"+e1.ID, "viewer", nil, &e1), 200, "get entry")
	if e1.ReversedBy != rev.ID {
		t.Fatalf("reversed by: %+v", e1)
	}
	h.must(h.do("GET", "/api/accounting/entries/"+e1.ID, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/accounting/trial-balance?company="+acme.ID, "outsider", nil, nil), 404, "outsider")

	// Closed periods reject entries until reopened.
	h.must(h.do("POST", "/api/accounting/ledgers/"+ledger.ID+"/periods/close", "accountant", map[string]any{"year": 2026, "month": 8}, &ledger), 200, "close")
	august := map[string]any{"company": acme.ID, "date": "2026-08-31", "description": "Ajuste", "lines": []map[string]any{
		{"account": "5700", "debit": "1"}, {"account": "5720", "credit": "1"}}}
	h.must(h.do("POST", "/api/accounting/entries", "accountant", august, nil), 422, "closed period")
	h.must(h.do("POST", "/api/accounting/ledgers/"+ledger.ID+"/periods/reopen", "accountant", map[string]any{"year": 2026, "month": 8}, &ledger), 200, "reopen")
	if len(ledger.Closed) != 0 {
		t.Fatalf("reopened: %+v", ledger)
	}
	h.must(h.do("POST", "/api/accounting/entries", "accountant", august, nil), 201, "open again")

	// The payslip is cancelled: its entry is reversed, once.
	cancel := fw.NewUUID().String()
	h.payroll(cancel, "payroll.payslip-cancelled.v1", map[string]any{"payslipId": slip})
	h.payroll(fw.NewUUID().String(), "payroll.payslip-cancelled.v1", map[string]any{"payslipId": slip})
	h.expect(acme.ID, map[string]string{"6400": "0.00", "6420": "0.00", "4760": "0.00", "4751": "0.00", "4650": "0.00"})

	// The journal balances and numbers the year without gaps.
	var all fw.Page[aapp.EntryDTO]
	h.must(h.do("GET", "/api/accounting/entries?company="+acme.ID+"&size=100", "viewer", nil, &all), 200, "journal")
	var numbers []int64
	for _, e := range all.Items {
		numbers = append(numbers, e.Number)
	}
	slices.Sort(numbers)
	for i, n := range numbers {
		if n != int64(i+1) {
			t.Fatalf("numbers: %v", numbers)
		}
	}
	if len(numbers) != 12 {
		t.Fatalf("12 entries expected, got %d", len(numbers))
	}
	sum := vocab.DecimalFromInt(0)
	for _, b := range h.balances(acme.ID) {
		sum = sum.Add(vocab.MustDecimal(b))
	}
	if !sum.IsZero() {
		t.Fatalf("the trial balance does not balance: %s", sum)
	}
}

func TestAccounting_PostsWhatTheOtherContextsPublish_MemoryThenSQLite(t *testing.T) {
	// The scenario's dates are literals in October 2026 and the journal numbers its entries per
	// year: entries posted from events without a date take "today", so the domain clock is fixed
	// inside the scenario (with the real clock the journal splits into two years from 2027).
	testkit.FixClock(t, time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC))
	h := compose(t)
	h.scenario("m")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations(), rinfra.Migrations(),
		tinfra.Migrations(), ainfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.scenario("s")
}

func TestAccounting_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/accounting/domain")
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
