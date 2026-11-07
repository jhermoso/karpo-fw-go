package host_test

import (
	"errors"
	"testing"

	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bildomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	fisapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	recapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	recdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// De punta a punta, con el perfil de Parties (D-144), en memoria y en SQLite: la empresa guarda
// redondear el bruto antes, el patrón de sus series y, después, los vencimientos con la emisión.
// Con after_issue (lo de siempre) el cobro de Receivables nace al entregar el outbox; con
// with_issue, en la misma transacción que la emisión, y la entrega posterior no duplica nada.
func TestBillingSettings_FromThePartiesProfile(t *testing.T) {
	for name, sw := range map[string]func(*testing.T) *hotswap.Switch{"memory": memorySwitch, "sqlite": sqliteSwitch} {
		t.Run(name, func(t *testing.T) {
			th := newTradeHost(t, sw(t))
			h, actx := th.h, th.actx
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			organization := func(name, nif string, roles []string, affiliation *papp.NewAffiliation) string {
				t.Helper()
				p, err := h.Parties.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: name, Roles: roles, Affiliation: affiliation})
				must(err)
				pid, _ := pdomain.ParsePartyID(p.ID)
				_, err = h.Parties.Service.AddIdentification.Handle(actx, papp.AddIdentification{PartyID: pid,
					DocumentType: "c0000000-0004-0000-0000-000000000003", Country: "ES", Number: nif, Primary: true})
				must(err)
				// El domicilio fiscal: sin él no se emite una factura completa (D-147).
				_, err = h.Parties.Service.AddContact.Handle(actx, papp.AddContact{PartyID: pid, Kind: "postal", Purposes: []string{"default"},
					Address: &papp.AddressDTO{Line1: "Calle Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
				must(err)
				return p.ID
			}
			acme := organization("Acme Ajustes SL", "B12345674", []string{pdomain.RoleInternalOrganization.String()}, nil)
			customer := organization("Cliente Ajustes SL", "A08015497", []string{pdomain.RoleCustomer.String()},
				&papp.NewAffiliation{Organization: acme, RelationshipType: pdomain.RelCustomer.String()})
			_, err := h.Fiscal.Service.CreateRate.Handle(actx, fisapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General",
				Rate: "21", From: vocab.MustDate(2012, 9, 1)})
			must(err)
			_, err = h.Fiscal.Service.RegisterTaxpayer.Handle(actx, fisapp.RegisterTaxpayer{Organization: acme,
				TermsInput: fisapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
			must(err)
			acmeID, _ := pdomain.ParsePartyID(acme)
			profile := func(timing string) {
				t.Helper()
				_, err := h.Parties.Service.SetInternalOrganizationProfile.Handle(actx, papp.SetInternalOrganizationProfile{ID: acmeID,
					LineRounding: pdomain.LineRoundingGrossThenDiscount, InstallmentsTiming: timing})
				must(err)
			}
			profile(pdomain.InstallmentsAfterIssue)
			_, err = h.Parties.Service.SetInternalOrganizationCodePattern.Handle(actx, papp.SetInternalOrganizationCodePattern{ID: acmeID,
				Kind: pdomain.CodeKindInvoiceSeries, Pattern: `A[0-9]{2}`})
			must(err)

			var rv *fw.RuleViolationError
			if _, err := h.Billing.Service.OpenSeries.Handle(actx, bilapp.OpenSeries{Seller: acme, Code: "FA", Year: 2026}); !errors.As(err, &rv) ||
				rv.Code != "billing.series_code_pattern" {
				t.Fatalf("the series pattern of the profile: %v", err)
			}
			series, err := h.Billing.Service.OpenSeries.Handle(actx, bilapp.OpenSeries{Seller: acme, Code: "A26", Year: 2026})
			must(err)
			issue := func() string {
				t.Helper()
				inv, err := h.Billing.Service.DraftInvoice.Handle(actx, bilapp.DraftInvoice{Seller: acme, Customer: customer,
					DetailsInput: bilapp.DetailsInput{Description: "Tornillos"}})
				must(err)
				id, _ := bildomain.ParseInvoiceID(inv.ID)
				// 1 × 0.125 con un 50 %: 0.07 redondeando el bruto antes (0.06 una sola vez).
				_, err = h.Billing.Service.AddLine.Handle(actx, bilapp.AddLine{ID: id, Description: "Tornillo", Quantity: "100", UnitPrice: "0.125",
					Discount: "50", TaxCode: "G21"})
				must(err)
				_, err = h.Billing.Service.AddLine.Handle(actx, bilapp.AddLine{ID: id, Description: "Tornillo", Quantity: "1", UnitPrice: "0.125",
					Discount: "50", TaxCode: "G21"})
				must(err)
				out, err := h.Billing.Service.Issue.Handle(actx, bilapp.IssueInvoice{ID: id, Series: series.ID, Date: vocab.MustDate(2026, 9, 28)})
				must(err)
				if out.Lines[1].Net != "0.07" || out.LineRounding != "gross_then_discount" {
					t.Fatalf("the rounding of the profile: %+v", out)
				}
				return out.ID
			}
			receivable := func(id string) error {
				_, err := h.Receivables.Service.GetReceivable.Handle(actx, recapp.GetReceivable{ID: recdomain.ReceivableID{UUID: fw.MustParseUUID(id)}})
				return err
			}

			after := issue()
			if err := receivable(after); !errors.Is(err, fw.ErrNotFound) {
				t.Fatalf("after_issue: no receivable before the delivery: %v", err)
			}
			_, err = h.Deliver(actx)
			must(err)
			must(receivable(after))

			profile(pdomain.InstallmentsWithIssue)
			with := issue()
			must(receivable(with)) // sin entregar nada
			_, err = h.Deliver(actx)
			must(err)
			page, err := h.Receivables.Service.SearchReceivables.Handle(actx, recapp.SearchReceivables{Seller: acme})
			must(err)
			if page.Total != 2 {
				t.Fatalf("the delivery afterwards does not repeat it: %d receivables", page.Total)
			}
		})
	}
}
