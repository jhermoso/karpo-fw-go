package host_test

import (
	"testing"

	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bildomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	fisapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	ordapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	ddomain "github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// De punta a punta, en memoria y en SQLite (D-148): la factura directa toma sola el precio de
// Products con la cadena completa y el descuento del cliente de Orders encadenado. La empresa tiene
// la tarifa general en su perfil de Parties; un cliente tiene precio especial del tornillo y un 10 %
// de descuento, y el otro no tiene nada. La tuerca no está en la tarifa general: precio base.
func TestBilling_AutomaticPricingFromProductsAndOrders(t *testing.T) {
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
				_, err = h.Parties.Service.AddContact.Handle(actx, papp.AddContact{PartyID: pid, Kind: "postal", Purposes: []string{"default"},
					Address: &papp.AddressDTO{Line1: "Calle Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
				must(err)
				return p.ID
			}
			acme := organization("Acme Precios SL", "B12345674", []string{pdomain.RoleInternalOrganization.String()}, nil)
			customer := func(name, nif string) string {
				return organization(name, nif, []string{pdomain.RoleCustomer.String()},
					&papp.NewAffiliation{Organization: acme, RelationshipType: pdomain.RelCustomer.String()})
			}
			special, plain := customer("Cliente Especial SL", "A08015497"), customer("Cliente Corriente SL", "A58818501")
			_, err := h.Fiscal.Service.CreateRate.Handle(actx, fisapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General",
				Rate: "21", From: vocab.MustDate(2012, 9, 1)})
			must(err)
			_, err = h.Fiscal.Service.RegisterTaxpayer.Handle(actx, fisapp.RegisterTaxpayer{Organization: acme,
				TermsInput: fisapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
			must(err)

			product := func(sku, name, price string) string {
				t.Helper()
				p, err := h.Products.Service.RegisterProduct.Handle(actx, dapp.RegisterProduct{Company: acme, SKU: sku,
					DetailsInput: dapp.DetailsInput{Name: name, Kind: "good", UoM: "ea", TaxCode: "G21", BasePrice: price, ForSale: true}})
				must(err)
				return p.ID
			}
			tornillo, tuerca := product("TOR-M8", "Tornillo M8", "0.125"), product("TUE-M8", "Tuerca M8", "0.5")
			general, err := h.Products.Service.CreatePriceList.Handle(actx, dapp.CreatePriceList{Company: acme, Code: "GENERAL", Name: "General"})
			must(err)
			gid, _ := ddomain.ParsePriceListID(general.ID)
			_, err = h.Products.Service.SetPrice.Handle(actx, dapp.SetPrice{ID: gid, Product: tornillo, UnitPrice: "0.12", From: vocab.MustDate(2026, 1, 1)})
			must(err)
			acmeID, _ := pdomain.ParsePartyID(acme)
			_, err = h.Parties.Service.SetInternalOrganizationProfile.Handle(actx, papp.SetInternalOrganizationProfile{ID: acmeID, DefaultPriceList: general.ID})
			must(err)
			_, err = h.Products.Service.SetSpecialPrice.Handle(actx, dapp.SetSpecialPrice{Company: acme, Customer: special, Product: tornillo, UnitPrice: "0.09",
				From: vocab.MustDate(2026, 1, 1)})
			must(err)
			_, err = h.Orders.Service.SetTerms.Handle(actx, ordapp.SetTerms{Company: acme, Customer: special, Discount: "10"})
			must(err)
			series, err := h.Billing.Service.OpenSeries.Handle(actx, bilapp.OpenSeries{Seller: acme, Code: "FA", Year: 2026})
			must(err)

			type want struct{ price, discount, source, net string }
			invoice := func(customer string, lines [2]want, net, tax, total string) {
				t.Helper()
				inv, err := h.Billing.Service.DraftInvoice.Handle(actx, bilapp.DraftInvoice{Seller: acme, Customer: customer,
					DetailsInput: bilapp.DetailsInput{Description: "Venta directa", OperationDate: vocab.MustDate(2026, 10, 15)}})
				must(err)
				id, _ := bildomain.ParseInvoiceID(inv.ID)
				for _, l := range []bilapp.AddLine{{ID: id, Product: tornillo, Quantity: "100"}, {ID: id, Product: tuerca, Quantity: "10"}} {
					_, err = h.Billing.Service.AddLine.Handle(actx, l)
					must(err)
				}
				out, err := h.Billing.Service.Issue.Handle(actx, bilapp.IssueInvoice{ID: id, Series: series.ID, Date: vocab.MustDate(2026, 10, 20)})
				must(err)
				for k, w := range lines {
					l := out.Lines[k]
					if l.UnitPrice != w.price || l.Discount != w.discount || l.PriceSource != w.source || l.Net != w.net || l.TaxCode != "G21" {
						t.Fatalf("line %d of %s: %+v, want %+v", k+1, customer, l, w)
					}
				}
				if out.Taxes.Net != net || out.Taxes.Tax != tax || out.Taxes.Total != total {
					t.Fatalf("totals of %s: %+v", customer, out.Taxes)
				}
			}
			// El especial con el descuento del cliente (100 × 0,09 × 0,90) y la tuerca a precio base
			// con el mismo descuento (10 × 0,5 × 0,90): 12,60 + 21 % = 15,25.
			invoice(special, [2]want{{"0.09", "10.00", "special", "8.10"}, {"0.5", "10.00", "base", "4.50"}}, "12.60", "2.65", "15.25")
			// Sin especial, y con una tarifa que Products no encuentra en sus condiciones, que se salta:
			// la tarifa general y el precio base. 17,00 + 21 % = 20,57.
			_, err = h.Orders.Service.SetTerms.Handle(actx, ordapp.SetTerms{Company: acme, Customer: plain, PriceList: fw.NewUUID().String()})
			must(err)
			invoice(plain, [2]want{{"0.12", "", "default_list", "12.00"}, {"0.5", "", "base", "5.00"}}, "17.00", "3.57", "20.57")
		})
	}
}
