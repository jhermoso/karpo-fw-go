package payments_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/payments"
	yapp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	ydomain "github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
	yinfra "github.com/jhermoso/karpo-fw-go/contexts/payments/infrastructure"
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
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

func mustIBAN(s string) vocab.IBAN {
	i, err := vocab.NewIBAN(s)
	if err != nil {
		panic(err)
	}
	return i
}

// netPay plays the Payroll Remittance port: the splits of the net pay of each payslip.
type netPay map[string][]ydomain.PayTo

func (n netPay) NetPayments(_ context.Context, ids []string) (map[string][]ydomain.PayTo, error) {
	out := map[string][]ydomain.PayTo{}
	for _, id := range ids {
		if s, ok := n[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

// host composes Parties, Payments, Treasury and Accounting on one hot-swappable backend, with one
// in-process broker carrying the Published Language. Payroll and Fiscal are played by hand-made
// envelopes.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	pay      *payments.Module
	treasury *treasury.Module
	acc      *accounting.Module
	parties  *parties.Module
	broker   *inprocess.Broker
	splits   netPay
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	// Treasury stamps a transfer order with the moment its file is generated and settles it on that
	// day or later: the scenario runs on a fixed clock, so that its dates hold whatever the day the
	// test runs. The tokens are issued and checked on the same clock.
	t.Cleanup(fw.SetClock(fake.New(time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC))))
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	splits := netPay{}
	pm := parties.Compose(sw, nil)
	ym := payments.Compose(sw, splits)
	tm := treasury.Compose(sw, nil, tinfra.PaymentsPayables{Payable: ym.Payable}, tinfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	am := accounting.Compose(sw)
	broker := inprocess.NewBroker()
	broker.Subscribe("payments", ym.Consumer)
	broker.Subscribe("accounting", am.Consumer, "payments.payment-allocated.v1", "payments.allocation-reversed.v1") // this ledger has no payroll accounts

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("payments-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, pay: ym, treasury: tm, acc: am, parties: pm,
		broker: broker, splits: splits}
	all := []authz.Permission{yapp.PermPayableRead, yapp.PermPayableWrite, yapp.PermPaymentRead, yapp.PermPaymentWrite, tapp.PermTransferRead}
	users := map[string][]authz.Permission{"clerk": all, "payer": {yapp.PermPayableRead, yapp.PermPaymentRead, yapp.PermPaymentWrite},
		"registrar": {yapp.PermPayableRead, yapp.PermPayableWrite, yapp.PermPaymentRead}, "outsider": all}
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
	ym.RegisterRoutes(mux)
	tm.RegisterRoutes(mux)
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

func (h *host) request(method, path, user string, body any) *http.Response {
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
	return res
}

func (h *host) do(method, path, user string, body, out any) int {
	h.t.Helper()
	res := h.request(method, path, user, body)
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

// deliver relays the Published Language of Treasury and Payments until quiet.
func (h *host) deliver() {
	for moved := true; moved; {
		moved = false
		for _, r := range []interface {
			RelayOnce(context.Context) (int, error)
		}{h.treasury.Relay(h.broker), h.pay.Relay(h.broker)} {
			n, err := r.RelayOnce(context.Background())
			h.ok(err)
			moved = moved || n > 0
		}
	}
}

// send plays Payroll or Fiscal: one of their facts on the broker.
func (h *host) send(id, typ string, data any) {
	h.t.Helper()
	raw, _ := json.Marshal(data)
	h.ok(h.broker.Send(context.Background(), application.Envelope{ID: id, Type: typ, OccurredAt: time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC), Data: raw}))
}

func (h *host) payable(id string) yapp.PayableDTO {
	h.t.Helper()
	var p yapp.PayableDTO
	h.must(h.do("GET", "/api/payments/payables/"+id, "clerk", nil, &p), 200, "payable")
	return p
}

func (h *host) balances(company string) map[string]string {
	h.t.Helper()
	rows, err := h.acc.Service.TrialBalance.Handle(h.adminCtx, aapp.TrialBalance{Company: company})
	h.ok(err)
	out := map[string]string{}
	for _, r := range rows {
		out[r.Account] = r.Balance
	}
	return out
}

func (h *host) expect(company string, want map[string]string) {
	h.t.Helper()
	got := h.balances(company)
	for a, b := range want {
		if got[a] != b {
			h.t.Fatalf("balance of %s = %q, want %s (all: %v)", a, got[a], b, got)
		}
	}
}

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	ps := h.parties.Service
	org := func(name string, roles ...string) papp.PartyDTO {
		o, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag, Roles: roles})
		h.ok(err)
		return o
	}
	acme, globex := org("Acme", pdomain.RoleInternalOrganization.String()), org("Globex", pdomain.RoleInternalOrganization.String())
	supplier := org("Suministros Núñez")
	ana, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Muñoz " + tag})
	h.ok(err)
	for _, u := range []string{"clerk", "payer", "registrar"} {
		h.grant(u, acme.ID)
	}
	h.grant("outsider", globex.ID)
	var list0 fw.Page[yapp.PayableDTO]

	// The chart and ledger of Acme.
	for code, name := range map[string]string{"4000": "Proveedores", "5700": "Caja", "5720": "Bancos", "4650": "Remuneraciones pendientes",
		"4751": "HP acreedora por retenciones"} {
		_, err := h.acc.Service.CreateAccount.Handle(ctx, aapp.CreateAccount{Company: acme.ID, Code: code, Name: name, Postable: true})
		h.ok(err)
	}
	_, err = h.acc.Service.OpenLedger.Handle(ctx, aapp.OpenLedger{Company: acme.ID, StartMonth: 1, Accounts: map[string]string{"suppliers": "4000",
		"cash": "5700", "bank": "5720", "net-pay-payable": "4650", "withholding-payable": "4751"}})
	h.ok(err)

	// Purchases: a received invoice owes what is paid to the supplier, once however many times it
	// arrives; a credit of the supplier owes nothing here.
	f1 := fw.NewUUID().String()
	invoice := func(id, number, issued, due, payable string, payTo []map[string]any) map[string]any {
		return map[string]any{"invoiceId": id, "company": acme.ID, "supplier": supplier.ID, "supplierNumber": number, "issued": issued, "due": due,
			"payable": payable, "payTo": payTo}
	}
	registered := invoice(f1, "F-1/2026", "2026-09-15", "2026-10-05", "605.00", []map[string]any{{"iban": "ES9121000418450200051332", "amount": "605.00"}})
	msgF1 := fw.NewUUID().String()
	h.send(msgF1, "purchases.invoice-registered.v1", registered)
	h.send(msgF1, "purchases.invoice-registered.v1", registered)
	h.send(fw.NewUUID().String(), "purchases.invoice-registered.v1", registered)
	h.send(fw.NewUUID().String(), "purchases.invoice-registered.v1", invoice(fw.NewUUID().String(), "F-2/2026", "2026-09-20", "2026-10-20", "121.00", nil))
	h.send(fw.NewUUID().String(), "purchases.invoice-registered.v1", invoice(fw.NewUUID().String(), "A-1/2026", "2026-09-21", "2026-09-21", "-50.00", nil))
	var supplierPayables fw.Page[yapp.PayableDTO]
	h.must(h.do("GET", "/api/payments/payables?company="+acme.ID+"&kind=supplier-invoice", "registrar", nil, &supplierPayables), 200, "supplier payables")
	h.must(h.do("GET", "/api/payments/payables?company="+acme.ID, "outsider", nil, &list0), 200, "outsider sees nothing")
	if len(supplierPayables.Items) != 2 || len(list0.Items) != 0 || supplierPayables.Items[0].Document != "F-1/2026" ||
		supplierPayables.Items[0].Source != "purchases.invoice-registered.v1" || len(supplierPayables.Items[0].PayTo) != 1 {
		t.Fatalf("supplier payables: %+v / outsider %+v", supplierPayables.Items, list0.Items)
	}
	inv1, inv2 := supplierPayables.Items[0], supplierPayables.Items[1]
	h.must(h.do("PUT", "/api/payments/payables/"+inv2.ID+"/pay-to", "payer", map[string]any{"payTo": []map[string]any{{"iban": "ES9121000418450200051332",
		"amount": "121.00"}}}, nil), 403, "paying is not maintaining what is owed")
	h.must(h.do("PUT", "/api/payments/payables/"+inv2.ID+"/pay-to", "registrar", map[string]any{"payTo": []map[string]any{{"iban": "ES9121000418450200051333",
		"amount": "121.00"}}}, nil), 400, "IBAN check digits")

	// Payroll: an approved payslip owes its net pay on its payment date to the split accounts,
	// once however many times it arrives.
	slip := fw.NewUUID().String()
	h.splits[slip] = []ydomain.PayTo{{IBAN: mustIBAN("ES7921000813610123456789"), Amount: vocab.MustDecimal("1000.00")},
		{IBAN: mustIBAN("ES6621000418401234567891"), Amount: vocab.MustDecimal("469.70")}}
	approved := map[string]any{"payslipId": slip, "person": ana.ID, "employer": acme.ID, "periodEnd": "2026-09-30", "paymentDate": "2026-10-03",
		"net": "1469.70"}
	msg := fw.NewUUID().String()
	h.send(msg, "payroll.payslip-approved.v1", approved)
	h.send(msg, "payroll.payslip-approved.v1", approved)
	h.send(fw.NewUUID().String(), "payroll.payslip-approved.v1", approved)
	// A second payslip, cancelled before being paid.
	slip2 := fw.NewUUID().String()
	h.send(fw.NewUUID().String(), "payroll.payslip-approved.v1", map[string]any{"payslipId": slip2, "person": ana.ID, "employer": acme.ID,
		"periodEnd": "2026-09-30", "net": "50.00"})
	h.send(fw.NewUUID().String(), "payroll.payslip-cancelled.v1", map[string]any{"payslipId": slip2, "reason": "duplicada"})

	// Fiscal: the Modelo 111 of the third quarter is due on 20 October; the 190 is informative.
	h.send(fw.NewUUID().String(), "fiscal.filing-submitted.v1", map[string]any{"filingId": fw.NewUUID().String(), "declarant": acme.ID, "form": "111",
		"year": 2026, "period": "3T", "withheld": "216.00"})
	h.send(fw.NewUUID().String(), "fiscal.filing-submitted.v1", map[string]any{"filingId": fw.NewUUID().String(), "declarant": acme.ID, "form": "190",
		"year": 2026, "period": "0A", "withheld": "900.00"})

	var list fw.Page[yapp.PayableDTO]
	h.must(h.do("GET", "/api/payments/payables?company="+acme.ID+"&open=true", "clerk", nil, &list), 200, "open payables")
	if len(list.Items) != 4 {
		t.Fatalf("open payables: %+v", list.Items)
	}
	byKind := map[string]yapp.PayableDTO{}
	for _, p := range list.Items {
		byKind[p.Kind+"|"+p.Document] = p
	}
	nomina, tax := byKind["payroll|Nómina 2026-09"], byKind["tax|Modelo 111 2026-3T"]
	if nomina.Due != "2026-10-03" || nomina.Payee != ana.ID || len(nomina.PayTo) != 2 || tax.Due != "2026-10-20" || tax.Payee != "AEAT" ||
		tax.Amount != "216.00" || list.Items[0].Document != "Nómina 2026-09" {
		t.Fatalf("payroll and tax payables: %+v", list.Items)
	}
	var cancelled fw.Page[yapp.PayableDTO]
	h.must(h.do("GET", "/api/payments/payables?company="+acme.ID+"&kind=payroll", "clerk", nil, &cancelled), 200, "payroll payables")
	if len(cancelled.Items) != 2 || cancelled.Items[0].Cancelled == cancelled.Items[1].Cancelled {
		t.Fatalf("one payslip cancelled: %+v", cancelled.Items)
	}

	// Treasury proposes the transfers of what carries accounts: the supplier and the payslip.
	bank, err := h.treasury.Service.OpenAccount.Handle(ctx, tapp.OpenAccount{Owner: acme.ID, IBAN: "ES1000492352082414205416", Alias: "Pagos " + tag,
		Payments: true, Opened: vocab.MustDate(2020, 1, 1)})
	h.ok(err)
	order, err := h.treasury.Service.ProposeTransfers.Handle(ctx, tapp.ProposeTransfers{Debtor: acme.ID, Account: bank.ID,
		ExecutionDate: vocab.MustDate(2026, 10, 5), DueTo: vocab.MustDate(2026, 10, 31)})
	if err != nil || len(order.Transfers) != 3 || order.Total != "2074.70" || order.WithoutAccount != 2 {
		t.Fatalf("proposal: %+v %v", order, err)
	}
	again, err := h.treasury.Service.ProposeTransfers.Handle(ctx, tapp.ProposeTransfers{Debtor: acme.ID, Account: bank.ID,
		ExecutionDate: vocab.MustDate(2026, 10, 5), DueTo: vocab.MustDate(2026, 10, 31)})
	if err != nil || len(again.Transfers) != 0 {
		t.Fatalf("nothing twice: %+v %v", again, err)
	}
	oid, _ := tdomain.ParseTransferOrderID(order.ID)
	order, err = h.treasury.Service.GenerateTransfers.Handle(ctx, tapp.GenerateTransfers{ID: oid})
	h.ok(err)
	res := h.request("GET", "/api/treasury/transfers/"+order.ID+"/pain001", "clerk", nil)
	file, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(file), "<CtrlSum>2074.70</CtrlSum>") || !strings.Contains(string(file), "<Nm>Ana Munoz "+tag) ||
		!strings.Contains(string(file), "<Nm>Suministros Nunez "+tag) || !strings.Contains(string(file), "<IBAN>ES1000492352082414205416</IBAN>") {
		t.Fatalf("pain.001 (%d):\n%s", res.StatusCode, file)
	}
	h.must(h.do("GET", "/api/treasury/transfers/"+order.ID, "outsider", nil, nil), 404, "outsider")

	// The bank executes on the day the file was generated (that of the fixed clock), never before:
	// Payments registers a payment per transfer and Accounting posts them.
	_, err = h.treasury.Service.SettleTransfers.Handle(ctx, tapp.SettleTransfers{ID: oid, On: vocab.MustDate(2026, 10, 5)})
	h.ok(err)
	h.deliver()
	if p := h.payable(inv1.ID); !p.Settled {
		t.Fatalf("supplier paid: %+v", p)
	}
	if p := h.payable(nomina.ID); !p.Settled {
		t.Fatalf("payslip paid: %+v", p)
	}
	h.expect(acme.ID, map[string]string{"4000": "605.00", "4650": "1469.70", "5720": "-2074.70"})

	// The second account of the payslip rejects the transfer: the payment is cancelled and
	// reversed, and 469.70 are owed again.
	var second string
	for _, tr := range order.Transfers {
		if tr.Payable == nomina.ID && tr.Amount == "469.70" {
			second = tr.EndToEnd
		}
	}
	_, err = h.treasury.Service.RejectTransfer.Handle(ctx, tapp.RejectTransfer{ID: oid, EndToEnd: second, On: vocab.MustDate(2026, 10, 7), Reason: "AC04"})
	h.ok(err)
	h.deliver()
	if p := h.payable(nomina.ID); p.Settled || p.Open != "469.70" {
		t.Fatalf("rejected: %+v", p)
	}
	h.expect(acme.ID, map[string]string{"4650": "1000.00", "5720": "-1605.00"})

	// Paying by hand: the tax at the bank, part of the second invoice in cash.
	var taxPay, cash yapp.PaymentDTO
	h.must(h.do("POST", "/api/payments/payments", "payer", map[string]any{"company": acme.ID, "authority": "AEAT", "date": "2026-10-20", "amount": "216.00",
		"method": "transfer", "reference": "NRC 1234", "allocations": []map[string]any{{"payable": tax.ID, "amount": "216.00"}}}, &taxPay), 201, "tax paid")
	h.must(h.do("POST", "/api/payments/payments", "registrar", map[string]any{"company": acme.ID, "payee": supplier.ID, "date": "2026-10-10",
		"amount": "100.00", "method": "cash"}, nil), 403, "registering is not paying")
	h.must(h.do("POST", "/api/payments/payments", "payer", map[string]any{"company": acme.ID, "payee": supplier.ID, "date": "2026-10-10", "amount": "200.00",
		"method": "cash", "allocations": []map[string]any{{"payable": inv2.ID, "amount": "121.01"}}}, nil), 422, "over paid")
	h.must(h.do("POST", "/api/payments/payments", "payer", map[string]any{"company": acme.ID, "payee": ana.ID, "date": "2026-10-10", "amount": "10.00",
		"method": "cash", "allocations": []map[string]any{{"payable": inv2.ID, "amount": "10.00"}}}, nil), 422, "another payee")
	h.must(h.do("POST", "/api/payments/payments", "payer", map[string]any{"company": acme.ID, "payee": supplier.ID, "date": "2026-10-10", "amount": "100.00",
		"method": "cash", "allocations": []map[string]any{{"payable": inv2.ID, "amount": "100.00"}}}, &cash), 201, "cash")
	h.must(h.do("POST", "/api/payments/payables/"+inv2.ID+"/cancel", "registrar", map[string]any{"reason": "error"}, nil), 422, "cancel a paid payable")
	h.deliver()
	h.expect(acme.ID, map[string]string{"4000": "705.00", "4751": "216.00", "5700": "-100.00", "5720": "-1821.00"})

	// A payment is cancelled, not deleted: the payable is open again and the entry reversed.
	h.must(h.do("POST", "/api/payments/payments/"+cash.ID+"/cancel", "payer", nil, &cash), 200, "cancel payment")
	h.must(h.do("POST", "/api/payments/payments/"+cash.ID+"/cancel", "payer", nil, nil), 422, "cancelled once")
	h.deliver()
	if p := h.payable(inv2.ID); p.Open != "121.00" || !cash.Cancelled {
		t.Fatalf("cancelled payment: %+v", p)
	}
	h.expect(acme.ID, map[string]string{"4000": "605.00", "5700": "0.00"})
	h.must(h.do("POST", "/api/payments/payables/"+inv2.ID+"/cancel", "registrar", map[string]any{"reason": "error"}, nil), 200, "cancel unpaid payable")
	h.send(fw.NewUUID().String(), "purchases.invoice-cancelled.v1", map[string]any{"invoiceId": f1, "reason": "error"})
	if p := h.payable(inv1.ID); p.Cancelled || !p.Settled {
		t.Fatalf("a paid payable is not withdrawn by a cancelled invoice: %+v", p)
	}
	h.must(h.do("GET", "/api/payments/payables/"+inv2.ID, "outsider", nil, nil), 404, "outsider")

	var pays fw.Page[yapp.PaymentDTO]
	h.must(h.do("GET", "/api/payments/payments?company="+acme.ID, "clerk", nil, &pays), 200, "payments")
	if len(pays.Items) != 5 {
		t.Fatalf("payments: %d", len(pays.Items))
	}
}

func TestPayments_OwesPaysAndPosts_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), yinfra.Migrations(), tinfra.Migrations(), ainfra.Migrations()})
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

func TestPayments_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/payments/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
