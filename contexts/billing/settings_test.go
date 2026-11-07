package billing_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	"github.com/jhermoso/karpo-fw-go/contexts/billing/contracts"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// settings son los ajustes de cada empresa, que la prueba cambia cuando quiere (en el anfitrión,
// los del perfil de Parties).
type settings struct {
	mu sync.Mutex
	by map[string]bapp.CompanySettings
}

func (s *settings) Settings(_ context.Context, company string) (bapp.CompanySettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.by[company], nil
}

func (s *settings) set(company string, c bapp.CompanySettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.by[company] = c
}

// Los ajustes de la empresa en la facturación (D-144), en memoria y en SQLite: la forma de redondear
// y los decimales del precio se toman al preparar el borrador y se quedan en la factura, emitida o
// no, aunque la empresa los cambie después; el código de una serie nueva cumple el patrón de la
// empresa.
func TestBilling_CompanySettings(t *testing.T) {
	for _, useSQL := range []bool{false, true} {
		name := map[bool]string{false: "memory", true: "sqlite"}[useSQL]
		t.Run(name, func(t *testing.T) {
			conf := &settings{by: map[string]bapp.CompanySettings{}}
			h := compose(t, billing.WithSettings(conf))
			ctx := context.Background()
			if useSQL {
				raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "settings.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
				if err != nil {
					t.Fatal(err)
				}
				raw.SetMaxOpenConns(1)
				t.Cleanup(func() { _ = raw.Close() })
				db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
				m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations()})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := m.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				if err := h.sw.Swap(ctx, db); err != nil {
					t.Fatal(err)
				}
			}
			actx, ps, bs := h.adminCtx, h.parties.Service, h.billing.Service
			acme, err := ps.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			h.ok(err)
			ana, err := ps.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Ajustes"})
			h.ok(err)
			for party, doc := range map[string][2]string{acme.ID: {"c0000000-0004-0000-0000-000000000003", "A58818501"}, ana.ID: {"c0000000-0004-0000-0000-000000000002", "12345678Z"}} {
				id, _ := pdomain.ParsePartyID(party)
				_, err := ps.AddIdentification.Handle(actx, papp.AddIdentification{PartyID: id, DocumentType: doc[0], Country: "ES", Number: doc[1], Primary: true})
				h.ok(err)
				// El domicilio fiscal: sin él no se emite una factura completa (D-147).
				_, err = ps.AddContact.Handle(actx, papp.AddContact{PartyID: id, Kind: "postal", Purposes: []string{"default"},
					Address: &papp.AddressDTO{Line1: "Calle Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
				h.ok(err)
			}
			_, err = h.fiscal.Service.CreateRate.Handle(actx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
				From: vocab.MustDate(2012, 9, 1)})
			h.ok(err)
			_, err = h.fiscal.Service.RegisterTaxpayer.Handle(actx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
			h.ok(err)

			// El patrón de las series de Acme admite la barra, que la regla fija no admite.
			conf.set(acme.ID, bapp.CompanySettings{SeriesPattern: `F[A-Z]/[0-9]{2}`})
			var rv *fw.RuleViolationError
			if _, err := bs.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026}); !errors.As(err, &rv) || rv.Code != "billing.series_code_pattern" {
				t.Fatalf("a series that does not match the pattern: %v", err)
			}
			series, err := bs.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme.ID, Code: "FA/26", Year: 2026})
			h.ok(err)

			// 1 × 0.125 con un 50 %: 0.06 redondeando una vez, 0.07 redondeando el bruto antes.
			draft := func() (bdomain.InvoiceID, bapp.InvoiceDTO) {
				t.Helper()
				inv, err := bs.DraftInvoice.Handle(actx, bapp.DraftInvoice{Seller: acme.ID, Customer: ana.ID, DetailsInput: bapp.DetailsInput{Description: "Tornillos"}})
				h.ok(err)
				id, _ := bdomain.ParseInvoiceID(inv.ID)
				return id, inv
			}
			line := func(id bdomain.InvoiceID, price string) (bapp.InvoiceDTO, error) {
				return bs.AddLine.Handle(actx, bapp.AddLine{ID: id, Description: "Tornillo", Quantity: "1", UnitPrice: price, Discount: "50", TaxCode: "G21"})
			}
			usual, dto := draft()
			if dto.LineRounding != "once" || dto.PriceDecimals != 4 {
				t.Fatalf("without settings, the usual calculation: %+v", dto)
			}
			conf.set(acme.ID, bapp.CompanySettings{Calculation: bdomain.Calculation{Rounding: bdomain.RoundGrossThenDiscount, PriceDecimals: 4},
				SeriesPattern: `F[A-Z]/[0-9]{2}`})
			gross, _ := draft()
			got, err := line(gross, "0.125")
			h.ok(err)
			if got.Lines[0].Net != "0.07" || got.LineRounding != "gross_then_discount" {
				t.Fatalf("rounding the gross amount first: %+v", got)
			}
			got, err = line(usual, "0.125")
			h.ok(err)
			if got.Lines[0].Net != "0.06" || got.LineRounding != "once" {
				t.Fatalf("a draft keeps the calculation it was prepared with: %+v", got)
			}

			// Los decimales del precio: con 2, un precio de 3 decimales es un error del campo; con 6,
			// cabe uno de 6, también en la columna.
			conf.set(acme.ID, bapp.CompanySettings{Calculation: bdomain.Calculation{Rounding: bdomain.RoundOnce, PriceDecimals: 2}})
			cents, _ := draft()
			if _, err := line(cents, "0.125"); !errors.Is(err, fw.ErrValidation) {
				t.Fatalf("3 decimals in a price of 2: %v", err)
			}
			conf.set(acme.ID, bapp.CompanySettings{Calculation: bdomain.Calculation{Rounding: bdomain.RoundOnce, PriceDecimals: 6}})
			fine, _ := draft()
			_, err = bs.AddLine.Handle(actx, bapp.AddLine{ID: fine, Description: "Kilovatio", Quantity: "1000", UnitPrice: "0.123456", TaxCode: "G21"})
			h.ok(err)
			if got, err := bs.GetInvoice.Handle(actx, bapp.GetInvoice{ID: fine}); err != nil || got.Lines[0].UnitPrice != "0.123456" ||
				got.Lines[0].Net != "123.46" || got.PriceDecimals != 6 {
				t.Fatalf("a price of 6 decimals: %+v %v", got, err)
			}

			// Emitida, la factura no cambia aunque la empresa cambie sus ajustes.
			issued, err := bs.Issue.Handle(actx, bapp.IssueInvoice{ID: gross, Series: series.ID, Date: vocab.MustDate(2026, 9, 28)})
			h.ok(err)
			if issued.Number != "FA/26-2026-000001" || issued.Net != "0.07" || issued.LineRounding != "gross_then_discount" {
				t.Fatalf("issued: %+v", issued)
			}
			conf.set(acme.ID, bapp.CompanySettings{Calculation: bdomain.Calculation{Rounding: bdomain.RoundOnce, PriceDecimals: 2}})
			if got, err := bs.GetInvoice.Handle(actx, bapp.GetInvoice{ID: gross}); err != nil || got.Net != "0.07" || got.Lines[0].Net != "0.07" ||
				got.LineRounding != "gross_then_discount" || got.PriceDecimals != 4 || got.Taxes.Net != "0.07" {
				t.Fatalf("an issued invoice is frozen: %+v %v", got, err)
			}

			// La rectificativa se calcula como la original, no con los ajustes de hoy (una vez, 2
			// decimales): admite su precio de 3 decimales y anula exactamente sus 0.07.
			fix, err := bs.DraftInvoice.Handle(actx, bapp.DraftInvoice{Seller: acme.ID, Corrects: gross.String(), Reason: "R1"})
			h.ok(err)
			if fix.LineRounding != "gross_then_discount" || fix.PriceDecimals != 4 {
				t.Fatalf("the corrective invoice takes the calculation of the original: %+v", fix)
			}
			fixID, _ := bdomain.ParseInvoiceID(fix.ID)
			fix, err = bs.AddLine.Handle(actx, bapp.AddLine{ID: fixID, Description: "Tornillo", Quantity: "-1", UnitPrice: "0.125", Discount: "50", TaxCode: "G21"})
			h.ok(err)
			if fix.Net != "-0.07" {
				t.Fatalf("the corrective invoice cancels the original: %+v", fix)
			}
		})
	}
}

// Con with_issue (D-130), la emisión entrega billing.invoice-issued.v1 a quien haga nacer los
// vencimientos (en el anfitrión, Receivables) dentro de su propia transacción: si éste falla, la
// factura sigue en borrador y la serie no consume el número. Con after_issue, no se le entrega nada:
// los vencimientos nacen después, del outbox.
func TestBilling_InstallmentsWithIssue(t *testing.T) {
	conf := &settings{by: map[string]bapp.CompanySettings{}}
	var received []contracts.InvoiceIssuedV1
	fail := false
	receivables := application.MessageHandlerFunc(func(_ context.Context, env application.Envelope) error {
		if fail {
			return fw.Violation("receivables.refused", "no")
		}
		var e contracts.InvoiceIssuedV1
		if err := json.Unmarshal(env.Data, &e); err != nil || env.Type != "billing.invoice-issued.v1" || env.ID == "" {
			t.Fatalf("envelope: %+v %v", env, err)
		}
		received = append(received, e)
		return nil
	})
	h := composeOn(t, func(sw *hotswap.Switch) []billing.Option {
		return []billing.Option{billing.WithSettings(conf), billing.WithInstallmentsInTransaction(receivables, sw)}
	})
	actx, ps, bs := h.adminCtx, h.parties.Service, h.billing.Service
	acme, err := ps.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	ana, err := ps.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Vencimientos"})
	h.ok(err)
	for party, doc := range map[string][2]string{acme.ID: {"c0000000-0004-0000-0000-000000000003", "A58818501"}, ana.ID: {"c0000000-0004-0000-0000-000000000002", "12345678Z"}} {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddIdentification.Handle(actx, papp.AddIdentification{PartyID: id, DocumentType: doc[0], Country: "ES", Number: doc[1], Primary: true})
		h.ok(err)
		// El domicilio fiscal: sin él no se emite una factura completa (D-147).
		_, err = ps.AddContact.Handle(actx, papp.AddContact{PartyID: id, Kind: "postal", Purposes: []string{"default"},
			Address: &papp.AddressDTO{Line1: "Calle Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
		h.ok(err)
	}
	_, err = h.fiscal.Service.CreateRate.Handle(actx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	h.ok(err)
	_, err = h.fiscal.Service.RegisterTaxpayer.Handle(actx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)
	series, err := bs.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme.ID, Code: "FV", Year: 2026})
	h.ok(err)
	issue := func() (bdomain.InvoiceID, error) {
		t.Helper()
		inv, err := bs.DraftInvoice.Handle(actx, bapp.DraftInvoice{Seller: acme.ID, Customer: ana.ID, DetailsInput: bapp.DetailsInput{Description: "Cuota"}})
		h.ok(err)
		id, _ := bdomain.ParseInvoiceID(inv.ID)
		_, err = bs.AddLine.Handle(actx, bapp.AddLine{ID: id, Description: "Cuota", Quantity: "1", UnitPrice: "10", TaxCode: "G21"})
		h.ok(err)
		_, err = bs.Issue.Handle(actx, bapp.IssueInvoice{ID: id, Series: series.ID, Date: vocab.MustDate(2026, 9, 28)})
		return id, err
	}

	_, err = issue()
	h.ok(err)
	if len(received) != 0 {
		t.Fatalf("after_issue: nothing in the transaction: %+v", received)
	}
	conf.set(acme.ID, bapp.CompanySettings{InstallmentsWithIssue: true})
	id, err := issue()
	h.ok(err)
	if len(received) != 1 || received[0].InvoiceID != id.String() || received[0].Number != "FV-2026-000002" || received[0].Total != "12.10" {
		t.Fatalf("with_issue: %+v", received)
	}
	fail = true
	id, err = issue()
	var rv *fw.RuleViolationError
	if !errors.As(err, &rv) || rv.Code != "receivables.refused" {
		t.Fatalf("the installments fail, the issue fails: %v", err)
	}
	if got, err := bs.GetInvoice.Handle(actx, bapp.GetInvoice{ID: id}); err != nil || got.Status != "draft" || got.Number != "" {
		t.Fatalf("still a draft: %+v %v", got, err)
	}
	if ss, err := bs.SearchSeries.Handle(actx, bapp.SearchSeries{Seller: acme.ID}); err != nil || ss[0].Last != 2 {
		t.Fatalf("no number taken: %+v %v", ss, err)
	}
}

// Si quien abre los cobros está sobre otro backend, la emisión no puede ser una sola transacción con
// él: Compose lo descarta y los vencimientos nacen después, aunque la empresa pida with_issue.
func TestBilling_InstallmentsWithIssueNeedTheSameBackend(t *testing.T) {
	conf := &settings{by: map[string]bapp.CompanySettings{}}
	called := false
	other := hotswap.New(memory.NewStore("other"))
	t.Cleanup(func() { _ = other.Close(context.Background()) })
	receivables := application.MessageHandlerFunc(func(context.Context, application.Envelope) error { called = true; return nil })
	h := compose(t, billing.WithSettings(conf), billing.WithInstallmentsInTransaction(receivables, other))
	acme, ana := h.seller(t)
	conf.set(acme, bapp.CompanySettings{InstallmentsWithIssue: true})
	series, err := h.billing.Service.OpenSeries.Handle(h.adminCtx, bapp.OpenSeries{Seller: acme, Code: "FV", Year: 2026})
	h.ok(err)
	inv, err := h.billing.Service.DraftInvoice.Handle(h.adminCtx, bapp.DraftInvoice{Seller: acme, Customer: ana, DetailsInput: bapp.DetailsInput{Description: "Cuota"}})
	h.ok(err)
	id, _ := bdomain.ParseInvoiceID(inv.ID)
	_, err = h.billing.Service.AddLine.Handle(h.adminCtx, bapp.AddLine{ID: id, Description: "Cuota", Quantity: "1", UnitPrice: "10", TaxCode: "G21"})
	h.ok(err)
	_, err = h.billing.Service.Issue.Handle(h.adminCtx, bapp.IssueInvoice{ID: id, Series: series.ID, Date: vocab.MustDate(2026, 9, 28)})
	h.ok(err)
	if called {
		t.Fatal("a handler on another backend is not called in the transaction")
	}
}

// El borrador de un albarán lleva al menos los 4 decimales del precio del pedido, aunque la empresa
// pida 2: 1,2345 × 1000 son 1234,50, no 1230,00.
func TestBilling_DeliveryKeepsThePriceOfTheOrder(t *testing.T) {
	conf := &settings{by: map[string]bapp.CompanySettings{}}
	h := compose(t, billing.WithSettings(conf))
	acme, ana := h.seller(t)
	conf.set(acme, bapp.CompanySettings{Calculation: bdomain.Calculation{Rounding: bdomain.RoundOnce, PriceDecimals: 2}})
	data, _ := json.Marshal(map[string]any{"deliveryId": fw.NewUUID().String(), "orderNumber": "P-1", "company": acme, "customer": ana,
		"number": "A-1", "date": "2026-09-28", "lines": []map[string]any{{"sku": "T", "description": "Tornillo", "taxCode": "G21",
			"quantity": "1000", "netPrice": "1.2345"}}})
	h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
		Source: "orders", Data: data}))
	page, err := h.billing.Service.SearchInvoices.Handle(h.adminCtx, bapp.SearchInvoices{Seller: acme, Status: "draft"})
	h.ok(err)
	if page.Total != 1 || page.Items[0].Net != "1234.50" || page.Items[0].Lines[0].UnitPrice != "1.2345" || page.Items[0].PriceDecimals != 4 {
		t.Fatalf("the draft of a delivery: %+v", page.Items)
	}
}

// seller registra una empresa con su CIF y su perfil fiscal, y una clienta con su DNI.
func (h *host) seller(t *testing.T) (string, string) {
	t.Helper()
	actx, ps := h.adminCtx, h.parties.Service
	acme, err := ps.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	ana, err := ps.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Albarán"})
	h.ok(err)
	for party, doc := range map[string][2]string{acme.ID: {"c0000000-0004-0000-0000-000000000003", "A58818501"}, ana.ID: {"c0000000-0004-0000-0000-000000000002", "12345678Z"}} {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddIdentification.Handle(actx, papp.AddIdentification{PartyID: id, DocumentType: doc[0], Country: "ES", Number: doc[1], Primary: true})
		h.ok(err)
		// El domicilio fiscal: sin él no se emite una factura completa (D-147).
		_, err = ps.AddContact.Handle(actx, papp.AddContact{PartyID: id, Kind: "postal", Purposes: []string{"default"},
			Address: &papp.AddressDTO{Line1: "Calle Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
		h.ok(err)
	}
	_, err = h.fiscal.Service.CreateRate.Handle(actx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	h.ok(err)
	_, err = h.fiscal.Service.RegisterTaxpayer.Handle(actx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)
	return acme.ID, ana.ID
}
