package host_test

import (
	"errors"
	"testing"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	dcontracts "github.com/jhermoso/karpo-fw-go/contexts/products/contracts"
	ddomain "github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// De punta a punta, con el perfil de Parties (D-148), en memoria y en SQLite: la tarifa general de
// la empresa es su ajuste default_price_list, y el anfitrión se lo lleva a Products. Sin ajuste, o
// con la tarifa retirada, la cadena cae al precio base.
func TestProductsSettings_GeneralListFromThePartiesProfile(t *testing.T) {
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
			acme, err := h.Parties.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme Tarifas SL",
				Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			acmeID, _ := pdomain.ParsePartyID(acme.ID)
			tornillo, err := h.Products.Service.RegisterProduct.Handle(actx, dapp.RegisterProduct{Company: acme.ID, SKU: "TOR-M8",
				DetailsInput: dapp.DetailsInput{Name: "Tornillo M8", Kind: "good", UoM: "ea", BasePrice: "0.125", ForSale: true}})
			must(err)
			general, err := h.Products.Service.CreatePriceList.Handle(actx, dapp.CreatePriceList{Company: acme.ID, Code: "GENERAL", Name: "General"})
			must(err)
			gid, _ := ddomain.ParsePriceListID(general.ID)
			_, err = h.Products.Service.SetPrice.Handle(actx, dapp.SetPrice{ID: gid, Product: tornillo.ID, UnitPrice: "0.12", From: vocab.MustDate(2026, 1, 1)})
			must(err)
			quote := func() dcontracts.Quote {
				t.Helper()
				q, err := h.Products.Pricing.Quote(actx, dcontracts.QuoteRequest{Company: acme.ID, Product: tornillo.ID, Quantity: "10", On: "2026-10-04"})
				must(err)
				return q
			}
			if q := quote(); q.Source != dcontracts.SourceBase {
				t.Fatalf("no general list yet: %+v", q)
			}
			// Al guardarla, Parties la comprueba en Products: una que no existe o de otra empresa, 422.
			globex, err := h.Parties.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Globex Tarifas SL",
				Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			foreign, err := h.Products.Service.CreatePriceList.Handle(actx, dapp.CreatePriceList{Company: globex.ID, Code: "GENERAL", Name: "Ajena"})
			must(err)
			for what, id := range map[string]string{"unknown": fw.NewUUID().String(), "foreign": foreign.ID} {
				_, err := h.Parties.Service.SetInternalOrganizationProfile.Handle(actx, papp.SetInternalOrganizationProfile{ID: acmeID, DefaultPriceList: id})
				var rv *fw.RuleViolationError
				if !errors.As(err, &rv) || rv.Code != pdomain.ErrUnknownDefaultPriceList {
					t.Fatalf("a %s general list: %v", what, err)
				}
			}
			_, err = h.Parties.Service.SetInternalOrganizationProfile.Handle(actx, papp.SetInternalOrganizationProfile{ID: acmeID, DefaultPriceList: general.ID})
			must(err)
			if q := quote(); q.Source != dcontracts.SourceDefaultList || q.Net != "0.1200" {
				t.Fatalf("the general list of the profile: %+v", q)
			}
			_, err = h.Products.Service.SetPriceListActive.Handle(actx, dapp.SetPriceListActive{ID: gid, Active: false})
			must(err)
			if q := quote(); q.Source != dcontracts.SourceBase {
				t.Fatalf("a retired general list is skipped: %+v", q)
			}
			// Una retirada tampoco se puede poner de nuevo.
			_, err = h.Parties.Service.SetInternalOrganizationProfile.Handle(actx, papp.SetInternalOrganizationProfile{ID: acmeID})
			must(err)
			_, err = h.Parties.Service.SetInternalOrganizationProfile.Handle(actx, papp.SetInternalOrganizationProfile{ID: acmeID, DefaultPriceList: general.ID})
			var rv *fw.RuleViolationError
			if !errors.As(err, &rv) || rv.Code != pdomain.ErrUnknownDefaultPriceList {
				t.Fatalf("a retired general list: %v", err)
			}
		})
	}
}
