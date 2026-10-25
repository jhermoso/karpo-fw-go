package receivables_test

import (
	"context"
	"testing"

	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// Los listados de cobros pendientes, cobros y perfiles de crédito dejan fuera por defecto los de
// un cliente o pagador fusionado en otra party, y los traen con includeMergedParties; el registro
// pedido por su identidad se sigue encontrando (docs/DECISIONES-COMUNES.md, D-100).
func TestListingsLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, receivables.WithMergedParties(merged))
	ctx := h.adminCtx
	ps := h.parties.Service
	acme, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme fusiones",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	h.grant("clerk", acme.ID)
	doc := func(party, docType, number string) {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: docType, Country: "ES", Number: number, Primary: true})
		h.ok(err)
	}
	doc(acme.ID, "c0000000-0004-0000-0000-000000000003", "A58818501")
	person := func(name, dni string) string {
		p, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: name, FirstSurname: "Fusiones"})
		h.ok(err)
		doc(p.ID, "c0000000-0004-0000-0000-000000000002", dni)
		return p.ID
	}
	ana, bea := person("Ana", "12345678Z"), person("Bea", "00000000T")
	merged.ids = []string{bea}

	_, err = h.fiscal.Service.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	h.ok(err)
	_, err = h.fiscal.Service.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)
	fa, err := h.billing.Service.OpenSeries.Handle(ctx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
	h.ok(err)

	// Una factura, un cobro y un perfil de crédito por cliente.
	invoices, collections := map[string]string{}, map[string]string{}
	limit := "500"
	for _, c := range []string{ana, bea} {
		invoices[c] = h.invoice(acme.ID, c, fa.ID, "100", "1", vocab.MustDate(2026, 10, 28), "").ID
		col, err := h.rec.Service.RegisterCollection.Handle(ctx, rapp.RegisterCollection{Seller: acme.ID, Payer: c, Date: vocab.MustDate(2026, 10, 16),
			Amount: "10", Method: "cash"})
		h.ok(err)
		collections[c] = col.ID
		_, err = h.rec.Service.SetCredit.Handle(ctx, rapp.SetCredit{Seller: acme.ID, Customer: c, Limit: &limit})
		h.ok(err)
	}
	broker := inprocess.NewBroker()
	broker.Subscribe("receivables", h.rec.Consumer)
	relay(t, h.billing.Relay(broker))

	// Cobros pendientes, por HTTP.
	customers := func(path string) []string {
		t.Helper()
		var page fw.Page[rapp.ReceivableDTO]
		h.must(h.do("GET", path, "clerk", nil, &page), 200, path)
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.Customer)
		}
		return out
	}
	path := "/api/receivables/invoices?seller=" + acme.ID
	if got := customers(path); len(got) != 1 || got[0] != ana {
		t.Fatalf("receivables: %v, want only %s", got, ana)
	}
	if got := customers(path + "&includeMergedParties=true"); len(got) != 2 {
		t.Fatalf("receivables with includeMergedParties: %v", got)
	}
	h.must(h.do("GET", "/api/receivables/invoices/"+invoices[bea], "clerk", nil, nil), 200, "the receivable of a merged party, by id")

	// Cobros, por HTTP.
	payers := func(path string) []string {
		t.Helper()
		var page fw.Page[rapp.CollectionDTO]
		h.must(h.do("GET", path, "clerk", nil, &page), 200, path)
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.Payer)
		}
		return out
	}
	path = "/api/receivables/collections?seller=" + acme.ID
	if got := payers(path); len(got) != 1 || got[0] != ana {
		t.Fatalf("collections: %v, want only %s", got, ana)
	}
	if got := payers(path + "&includeMergedParties=true"); len(got) != 2 {
		t.Fatalf("collections with includeMergedParties: %v", got)
	}
	h.must(h.do("GET", "/api/receivables/collections/"+collections[bea], "clerk", nil, nil), 200, "the collection of a merged party, by id")

	// Perfiles de crédito, por el servicio (no tienen listado HTTP).
	for include, want := range map[bool]int{false: 1, true: 2} {
		cs, err := h.rec.Service.SearchCredit.Handle(ctx, rapp.SearchCredit{Seller: acme.ID, IncludeMergedParties: include})
		h.ok(err)
		if len(cs) != want || !include && cs[0].Customer != ana {
			t.Fatalf("credit profiles (includeMergedParties %v): %+v", include, cs)
		}
	}
}
