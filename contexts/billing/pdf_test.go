package billing_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/render/pdf"
)

// inPDF es como sale s en el flujo de contenido: en WinAnsi y con los paréntesis protegidos.
func inPDF(s string) []byte {
	return pdf.Encode(strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`).Replace(s))
}

// get pide path y devuelve el estado, las cabeceras y el cuerpo.
func (h *host) get(path, user string) (int, http.Header, []byte) {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.srv.URL+path, nil)
	req.Header.Set("Authorization", h.tokens[user])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, body
}

// El PDF de una factura emitida lleva lo del artículo 6 del Reglamento de facturación que guarda
// la factura: número, fechas, nombre, NIF y domicilio de las dos partes, descripción, líneas,
// desglose, total y la causa de la exención; el de una rectificativa, la factura que rectifica. Un
// borrador no tiene PDF (422) y sin permiso de lectura, 403.
func TestBilling_InvoicePDF(t *testing.T) {
	h := compose(t)
	ctx, ps := h.adminCtx, h.parties.Service

	// Quien no puede leer facturas.
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("billing-test")})
	h.ids["blind"] = fw.NewUUID()
	h.dir.Put(h.ids["blind"], authz.Subject{Active: true, Permissions: []authz.Permission{bapp.PermSeriesRead}})
	tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids["blind"].String(), Username: "blind", PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
	h.tokens["blind"] = "Bearer " + tok

	acme, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme Ibérica SL", Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	ana, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Muñoz"})
	h.ok(err)
	h.grant("clerk", acme.ID)
	h.grant("issuer", acme.ID)
	h.grant("blind", acme.ID)
	for party, d := range map[string][3]string{
		acme.ID: {"c0000000-0004-0000-0000-000000000003", "A58818501", "Calle Mayor 1"},
		ana.ID:  {"c0000000-0004-0000-0000-000000000002", "12345678Z", "Plaza de España 7"},
	} {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: d[0], Country: "ES", Number: d[1], Primary: true})
		h.ok(err)
		_, err = ps.AddContact.Handle(ctx, papp.AddContact{PartyID: id, Kind: "postal", Purposes: []string{"default"},
			Address: &papp.AddressDTO{Line1: d[2], PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
		h.ok(err)
	}
	fs := h.fiscal.Service
	_, err = fs.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21", Surcharge: "5.2", From: vocab.MustDate(2012, 9, 1)})
	h.ok(err)
	_, err = fs.CreateTreatment.Handle(ctx, fapp.CreateTreatment{Territory: "common", Code: "E1", Description: "Exenta art. 20", Kind: "exempt"})
	h.ok(err)
	_, err = fs.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)
	var fa, rs bapp.SeriesDTO
	h.must(h.do("POST", "/api/billing/series", "clerk", map[string]any{"seller": acme.ID, "code": "FA", "year": 2026}, &fa), 201, "series")
	h.must(h.do("POST", "/api/billing/series", "clerk", map[string]any{"seller": acme.ID, "code": "R", "year": 2026, "corrective": true}, &rs), 201, "corrective series")

	var inv bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": ana.ID, "description": "Licencia y formación",
		"operationDate": "2026-09-25"}, &inv), 201, "draft")
	for _, l := range []map[string]any{
		{"description": "Licencia anual", "quantity": "1", "unitPrice": "1200", "taxCode": "G21"},
		{"description": "Formación reglada", "quantity": "2", "unitPrice": "25", "treatment": "E1"},
	} {
		h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/lines", "clerk", l, nil), 200, "line")
	}
	path := "/api/billing/invoices/" + inv.ID + "/pdf"
	if status, _, _ := h.get(path, "clerk"); status != 422 {
		t.Fatalf("a draft has no PDF: %d", status)
	}
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"}, &inv), 200, "issue")

	status, header, body := h.get(path, "clerk")
	if status != 200 || header.Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatalf("pdf: %d %v %q", status, header, body[:min(len(body), 40)])
	}
	if header.Get("Content-Disposition") != "inline; filename=FA-2026-000001.pdf" || header.Get("Cache-Control") != "no-store" ||
		header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", header)
	}
	_ = os.WriteFile(filepath.Join(t.TempDir(), "factura.pdf"), body, 0o600) // para mirarlo a mano
	for _, want := range []string{
		"FA-2026-000001", "28/09/2026", "25/09/2026", // número y fechas de expedición y de operación
		"Acme Ibérica SL", "NIF: A58818501", "Calle Mayor 1", "28013 Madrid", // emisor
		"NIF: 12345678Z", "Plaza de España 7", // destinatario
		"Licencia y formación", "Licencia anual", "1.200,00 €", "Exenta (E1)", // descripción, líneas y desglose
		"252,00 €", "1.502,00 €", // cuota y total
		"Operación exenta del impuesto, causa E1 (artículo 20 de la Ley 37/1992 del IVA).",
	} {
		if !bytes.Contains(body, inPDF(want)) {
			t.Errorf("the PDF does not contain %q", want)
		}
	}
	if status, _, _ := h.get(path, "blind"); status != 403 {
		t.Fatalf("without the read permission: %d", status)
	}
	if status, _, _ := h.get(path, "outsider"); status != 404 {
		t.Fatalf("outside the scope: %d", status)
	}

	// Un número con letras que no son ASCII va en filename* (RFC 2231).
	var an bapp.SeriesDTO
	h.must(h.do("POST", "/api/billing/series", "clerk", map[string]any{"seller": acme.ID, "code": "AÑ", "year": 2026}, &an), 201, "series AÑ")
	var other bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": ana.ID, "description": "Licencia"}, &other), 201, "draft")
	h.must(h.do("POST", "/api/billing/invoices/"+other.ID+"/lines", "clerk", map[string]any{"description": "Licencia", "quantity": "1", "unitPrice": "10",
		"taxCode": "G21"}, nil), 200, "line")
	h.must(h.do("POST", "/api/billing/invoices/"+other.ID+"/issue", "issuer", map[string]any{"series": an.ID, "date": "2026-09-28"}, nil), 200, "issue in AÑ")
	_, header, _ = h.get("/api/billing/invoices/"+other.ID+"/pdf", "clerk")
	if got := header.Get("Content-Disposition"); got != "inline; filename*=utf-8''A%C3%91-2026-000001.pdf" {
		t.Fatalf("non-ASCII file name: %q", got)
	}

	// La rectificativa cita la factura que rectifica y la causa.
	var fix bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "corrects": inv.ID, "reason": "R1"}, &fix), 201, "corrective draft")
	h.must(h.do("POST", "/api/billing/invoices/"+fix.ID+"/lines", "clerk", map[string]any{"description": "Descuento no aplicado", "quantity": "-1",
		"unitPrice": "100", "taxCode": "G21"}, nil), 200, "negative line")
	h.must(h.do("POST", "/api/billing/invoices/"+fix.ID+"/issue", "issuer", map[string]any{"series": rs.ID, "date": "2026-09-30"}, nil), 200, "issue corrective")
	_, _, body = h.get("/api/billing/invoices/"+fix.ID+"/pdf", "clerk")
	for _, want := range []string{"FACTURA RECTIFICATIVA", "R-2026-000001", "Rectifica la factura FA-2026-000001, de 28/09/2026.", "Causa R1:", "-121,00 €"} {
		if !bytes.Contains(body, inPDF(want)) {
			t.Errorf("the corrective PDF does not contain %q", want)
		}
	}
}
