package billing_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	"github.com/jhermoso/karpo-fw-go/contexts/billing/contracts"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pcontracts "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
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

// host compone Parties, Fiscal y Billing sobre un único backend intercambiable en caliente.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	billing  *billing.Module
	fiscal   *fiscal.Module
	parties  *parties.Module
	adminCtx context.Context
}

func compose(t *testing.T, opts ...billing.Option) *host {
	return composeOn(t, func(*hotswap.Switch) []billing.Option { return opts })
}

// composeOn es compose con opciones que necesitan el backend de Billing.
func composeOn(t *testing.T, options func(sw *hotswap.Switch) []billing.Option) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	pm := parties.Compose(sw)
	fm := fiscal.Compose(sw, finfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	bm := billing.Compose(sw, binfra.FiscalTaxes{Engine: fm.TaxEngine}, binfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities}, options(sw)...)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("billing-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, billing: bm, fiscal: fm, parties: pm}
	prepare := []authz.Permission{bapp.PermInvoiceRead, bapp.PermInvoiceCreate, bapp.PermInvoiceUpdate, bapp.PermSeriesRead, bapp.PermSeriesUpdate}
	users := map[string][]authz.Permission{"clerk": prepare, "issuer": {bapp.PermInvoiceRead, bapp.PermInvoiceIssue}, "outsider": prepare}
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
	bm.RegisterRoutes(mux)
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

// refused comprueba que la petición se rechaza con 422 y el código dado.
func (h *host) refused(method, path, user string, body any, code, what string) {
	h.t.Helper()
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req, _ := http.NewRequest(method, h.srv.URL+path, &buf)
	req.Header.Set("Authorization", h.tokens[user])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	var p struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(res.Body).Decode(&p)
	if res.StatusCode != 422 || p.Code != code {
		h.t.Fatalf("%s: status %d, code %q; want 422 %s", what, res.StatusCode, p.Code, code)
	}
}

func (h *host) ok(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatal(err)
	}
}

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	ps := h.parties.Service

	// Parties: el vendedor con su CIF, dos clientes con su DNI.
	acme, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme " + tag, Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	globex, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Globex " + tag, Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	h.grant("clerk", acme.ID)
	h.grant("issuer", acme.ID)
	h.grant("outsider", globex.ID)
	doc := func(party, docType, number string) {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: docType, Country: "ES", Number: number, Primary: true})
		h.ok(err)
	}
	doc(acme.ID, "c0000000-0004-0000-0000-000000000003", "A58818501")
	// El domicilio fiscal: sin él no se emite una factura completa (D-147).
	address := func(party, street string) {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddContact.Handle(ctx, papp.AddContact{PartyID: id, Kind: "postal", Purposes: []string{"default"},
			Address: &papp.AddressDTO{Line1: street, PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
		h.ok(err)
	}
	bare := func(name, dni string) papp.PartyDTO {
		p, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: name, FirstSurname: tag})
		h.ok(err)
		doc(p.ID, "c0000000-0004-0000-0000-000000000002", dni)
		return p
	}
	person := func(name, dni string) papp.PartyDTO {
		p := bare(name, dni)
		address(p.ID, "Calle de "+name+" 1")
		return p
	}
	ana, bea := person("Ana", "12345678Z"), person("Bea", "00000000T")

	// Fiscal: tipos del territorio común y el perfil fiscal del vendedor.
	fs := h.fiscal.Service
	for _, r := range []fapp.CreateRate{
		{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21", Surcharge: "5.2", From: vocab.MustDate(2012, 9, 1)},
		{Type: "vat", Territory: "common", Code: "R10", Description: "Reducido", Rate: "10", Surcharge: "1.4", From: vocab.MustDate(2012, 9, 1)},
	} {
		_, err := fs.CreateRate.Handle(ctx, r)
		h.ok(err)
	}
	_, err = fs.CreateTreatment.Handle(ctx, fapp.CreateTreatment{Territory: "common", Code: "E1", Description: "Exenta art. 20", Kind: "exempt"})
	h.ok(err)
	_, err = fs.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)

	// Series: ordinaria y rectificativa, una por código y año.
	var fa, rs bapp.SeriesDTO
	h.must(h.do("POST", "/api/billing/series", "clerk", map[string]any{"seller": acme.ID, "code": "fa", "year": 2026}, &fa), 201, "series")
	h.must(h.do("POST", "/api/billing/series", "clerk", map[string]any{"seller": acme.ID, "code": "FA", "year": 2026}, nil), 422, "duplicate series")
	h.must(h.do("POST", "/api/billing/series", "clerk", map[string]any{"seller": acme.ID, "code": "R", "year": 2026, "corrective": true}, &rs), 201, "corrective series")
	h.must(h.do("POST", "/api/billing/series", "outsider", map[string]any{"seller": acme.ID, "code": "X", "year": 2026}, nil), 404, "outsider")

	// Al duplicado de otra party no se le factura (D-70): ni se prepara borrador ni se emite.
	carla := person("Carla", "11111111H")
	var pending bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": carla.ID, "dueDate": "2026-10-28"}, &pending), 201, "draft for carla")
	h.must(h.do("POST", "/api/billing/invoices/"+pending.ID+"/lines", "clerk", map[string]any{"description": "Hora", "quantity": "1", "unitPrice": "10", "taxCode": "G21"}, nil), 200, "a line")
	_, err = ps.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: pdomain.RelDuplicateOf.String(), From: carla.ID, To: bea.ID})
	h.ok(err)
	h.must(h.do("POST", "/api/billing/invoices/"+pending.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"}, nil), 422, "carla turned out a duplicate")
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": carla.ID, "dueDate": "2026-10-28"}, nil), 422, "nor a new draft for her")

	// Sin el domicilio fiscal de las dos partes no se emite (D-147); el rechazo no consume número.
	var homeless bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": ana.ID, "description": "Consultoría"}, &homeless), 201, "draft")
	h.must(h.do("POST", "/api/billing/invoices/"+homeless.ID+"/lines", "clerk", map[string]any{"description": "X", "quantity": "1", "unitPrice": "1", "taxCode": "G21"}, nil), 200, "line")
	h.refused("POST", "/api/billing/invoices/"+homeless.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"},
		"billing.seller_address_required", "a seller without an address")
	address(acme.ID, "Calle Mayor 1")
	dora := bare("Dora", "87654321X")
	var nowhere bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": dora.ID, "description": "Consultoría"}, &nowhere), 201, "draft")
	h.must(h.do("POST", "/api/billing/invoices/"+nowhere.ID+"/lines", "clerk", map[string]any{"description": "X", "quantity": "1", "unitPrice": "1", "taxCode": "G21"}, nil), 200, "line")
	h.refused("POST", "/api/billing/invoices/"+nowhere.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"},
		"billing.customer_address_required", "a customer without an address")
	for _, d := range []string{homeless.ID, nowhere.ID} {
		h.must(h.do("DELETE", "/api/billing/invoices/"+d, "clerk", nil, nil), 204, "discard the draft")
	}

	// Una factura con tres tipos: impuestos sobre la base agregada por tipo.
	var inv bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": ana.ID, "dueDate": "2026-10-28", "description": "Consultoría"}, &inv), 201, "draft")
	for _, l := range []map[string]any{
		{"description": "Hora de consultoría", "quantity": "1", "unitPrice": "10.03", "taxCode": "G21"},
		{"description": "Hora de consultoría", "quantity": "1", "unitPrice": "10.03", "taxCode": "G21"},
		{"description": "Hora de consultoría", "quantity": "1", "unitPrice": "10.03", "taxCode": "G21"},
		{"description": "Libros", "quantity": "2", "unitPrice": "50", "discount": "10", "taxCode": "R10"},
		{"description": "Formación reglada", "quantity": "1", "unitPrice": "50", "treatment": "E1"},
	} {
		h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/lines", "clerk", l, &inv), 200, "line")
	}
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/lines", "clerk", map[string]any{"description": "X", "quantity": "1", "unitPrice": "1"}, nil), 400, "no tax code")
	var preview bapp.BreakdownDTO
	h.must(h.do("GET", "/api/billing/invoices/"+inv.ID+"/taxes?on=2026-09-28", "clerk", nil, &preview), 200, "preview")
	// 30.09 × 21 % = 6.32 (línea a línea, 6.33); 90 × 10 % = 9.00; 50 exentos.
	if preview.Net != "170.09" || preview.Tax != "15.32" || preview.Total != "185.41" || len(preview.Lines) != 3 {
		t.Fatalf("preview: %+v", preview)
	}
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/issue", "clerk", map[string]any{"series": fa.ID, "date": "2026-09-28"}, nil), 403, "preparing is not issuing")
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/issue", "issuer", map[string]any{"series": rs.ID, "date": "2026-09-28"}, nil), 422, "not in a corrective series")
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"}, &inv), 200, "issue")
	if inv.Number != "FA-2026-000001" || inv.Status != "issued" || inv.Taxes == nil || inv.Taxes.Total != "185.41" || inv.CustomerNIF != "12345678Z" ||
		inv.SellerNIF != "A58818501" || inv.SellerAddress == nil || inv.SellerAddress.Street != "Calle Mayor 1" ||
		inv.CustomerAddress == nil || inv.CustomerAddress.Street != "Calle de Ana 1" {
		t.Fatalf("issued: %+v", inv)
	}
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"}, nil), 422, "issued once")
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/lines", "clerk", map[string]any{"description": "X", "quantity": "1", "unitPrice": "1", "taxCode": "G21"}, nil),
		422, "an issued invoice is immutable")
	h.must(h.do("DELETE", "/api/billing/invoices/"+inv.ID, "clerk", nil, nil), 422, "an issued invoice is never deleted")
	h.must(h.do("GET", "/api/billing/invoices/"+inv.ID, "outsider", nil, nil), 404, "outsider")

	// Un minorista en el régimen de recargo de equivalencia.
	var retail bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": bea.ID, "equivalenceSurcharge": true, "description": "Mercancía"}, &retail), 201, "retail")
	h.must(h.do("POST", "/api/billing/invoices/"+retail.ID+"/lines", "clerk", map[string]any{"description": "Mercancía", "quantity": "4", "unitPrice": "25", "taxCode": "G21"}, nil), 200, "line")
	h.must(h.do("POST", "/api/billing/invoices/"+retail.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"}, &retail), 200, "issue retail")
	if retail.Number != "FA-2026-000002" || retail.Taxes.Surcharge != "5.20" || retail.Taxes.Total != "126.20" {
		t.Fatalf("retail: %+v", retail.Taxes)
	}

	// Una emisión rechazada no consume número (sin huecos).
	var late bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": ana.ID, "description": "Consultoría"}, &late), 201, "draft")
	h.must(h.do("POST", "/api/billing/invoices/"+late.ID+"/lines", "clerk", map[string]any{"description": "X", "quantity": "1", "unitPrice": "1", "taxCode": "G21"}, nil), 200, "line")
	h.must(h.do("POST", "/api/billing/invoices/"+late.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2027-01-02"}, nil), 422, "series of another year")
	h.must(h.do("DELETE", "/api/billing/invoices/"+late.ID, "clerk", nil, nil), 204, "discard the draft")
	var series []bapp.SeriesDTO
	h.must(h.do("GET", "/api/billing/series?seller="+acme.ID, "clerk", nil, &series), 200, "series")
	if series[0].Code != "FA" || series[0].Last != 2 {
		t.Fatalf("no gaps: %+v", series)
	}

	// Ana se muda después de la primera factura: su domicilio de facturación es otro.
	anaID, _ := pdomain.ParsePartyID(ana.ID)
	_, err = ps.AddContact.Handle(ctx, papp.AddContact{PartyID: anaID, Kind: "postal", Purposes: []string{"billing"},
		Address: &papp.AddressDTO{Line1: "Avenida Nueva 9", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
	h.ok(err)

	// Factura rectificativa (por diferencias) de la primera: lleva los domicilios de la original,
	// no el nuevo de Ana (D-147).
	var fix bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "corrects": inv.ID, "reason": "r1"}, &fix), 201, "corrective draft")
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "corrects": fix.ID, "reason": "R1"}, nil), 422, "a draft is not corrected")
	h.must(h.do("POST", "/api/billing/invoices/"+fix.ID+"/lines", "clerk", map[string]any{"description": "Horas no prestadas", "quantity": "-3",
		"unitPrice": "10.03", "taxCode": "G21"}, nil), 200, "negative line")
	h.must(h.do("POST", "/api/billing/invoices/"+fix.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"}, nil), 422, "corrective series only")
	h.must(h.do("POST", "/api/billing/invoices/"+fix.ID+"/issue", "issuer", map[string]any{"series": rs.ID, "date": "2026-09-28"}, &fix), 200, "issue corrective")
	if fix.Number != "R-2026-000001" || fix.Customer != ana.ID || fix.Corrects != inv.ID || fix.Reason != "R1" || fix.Taxes.Tax != "-6.32" || fix.Taxes.Total != "-36.41" {
		t.Fatalf("corrective: %+v %+v", fix, fix.Taxes)
	}
	if fix.SellerAddress == nil || *fix.SellerAddress != *inv.SellerAddress || fix.CustomerAddress == nil || *fix.CustomerAddress != *inv.CustomerAddress {
		t.Fatalf("the corrective carries the addresses of the invoice it corrects: %+v %+v", fix.SellerAddress, fix.CustomerAddress)
	}

	// Una factura antigua, emitida antes de D-147 sin domicilios: su rectificativa toma los de hoy.
	// Se escribe directamente en el repositorio, y en memoria cada repositorio guarda lo suyo: sólo
	// sobre SQLite.
	corrections := 1
	if tag == "s" {
		corrections = 2
		h.correctOld(acme.ID, ana.ID, fa.ID, rs.ID, tag)
	}
	var issued fw.Page[bapp.InvoiceDTO]
	h.must(h.do("GET", "/api/billing/invoices?seller="+acme.ID+"&status=issued&from=2026-09-01", "clerk", nil, &issued), 200, "search")
	if issued.Total != int64(2+corrections) {
		t.Fatalf("issued: %d", issued.Total)
	}

	// Published Language.
	store := memory.NewStore("fiscal-books")
	books := messaging.NewConsumer("books", memory.NewInbox(store), store)
	var got []contracts.InvoiceIssuedV1
	messaging.Handle(books, func(_ context.Context, e contracts.InvoiceIssuedV1, _ application.Envelope) error {
		got = append(got, e)
		return nil
	})
	broker := inprocess.NewBroker()
	broker.Subscribe("books", books)
	for {
		n, err := h.billing.Relay(broker).RelayOnce(context.Background())
		h.ok(err)
		if n == 0 {
			break
		}
	}
	if len(got) != 2+corrections || got[0].Number != "FA-2026-000001" || len(got[0].Taxes) != 3 || got[0].Taxes[0].Amount != "6.32" || got[2].Kind != "corrective" ||
		got[2].Corrects != inv.ID || got[1].Surcharge != "5.20" || got[0].Country != "ES" {
		t.Fatalf("published: %+v", got)
	}
}

// correctOld guarda una factura de Ana emitida antes de D-147, sin domicilios, y la rectifica: la
// rectificativa toma los domicilios de hoy (el de Ana, el nuevo de facturación).
func (h *host) correctOld(acmeID, anaID, faID, rsID, tag string) {
	t, ctx := h.t, h.adminCtx
	d, err := bdomain.DraftInvoice(bdomain.NewInvoiceID(), bdomain.InvoiceState{Seller: bdomain.OrganizationID{UUID: fw.MustParseUUID(acmeID)},
		Customer: bdomain.PartyID{UUID: fw.MustParseUUID(anaID)}, Kind: bdomain.Ordinary, Description: "Factura antigua"})
	h.ok(err)
	_, err = d.AddLine(bdomain.LineInput{Description: "Hora", Quantity: vocab.MustDecimal("1"), UnitPrice: vocab.MustDecimal("10"), TaxCode: "G21"})
	h.ok(err)
	st := d.State()
	series, _ := bdomain.ParseSeriesID(faID)
	st.Status, st.Series, st.Number, st.IssueDate = bdomain.Issued, series, "FA-2025-000099", vocab.MustDate(2025, 6, 30)
	st.SellerIdentity = bdomain.Identity{NIF: "A58818501", Name: "Acme " + tag, Country: "ES"}
	st.CustomerIdentity = bdomain.Identity{NIF: "12345678Z", Name: "Ana " + tag, Country: "ES"}
	st.Taxes = bdomain.Breakdown{Country: "ES", Net: vocab.MustDecimal("10"), Tax: vocab.MustDecimal("2.10"), Surcharge: vocab.MustDecimal("0")}
	old, err := bdomain.ReconstituteInvoice(d.ID(), st)
	h.ok(err)
	h.ok(hotswap.Repository(h.sw, binfra.InvoiceRepositoryFactory).Save(ctx, old))
	var got bapp.InvoiceDTO
	h.must(h.do("GET", "/api/billing/invoices/"+old.ID().String(), "clerk", nil, &got), 200, "the old invoice")
	if got.SellerAddress != nil || got.CustomerAddress != nil {
		t.Fatalf("the old invoice has no addresses: %+v %+v", got.SellerAddress, got.CustomerAddress)
	}
	var fix bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acmeID, "corrects": got.ID, "reason": "R1"}, &fix), 201, "corrective of the old one")
	h.must(h.do("POST", "/api/billing/invoices/"+fix.ID+"/lines", "clerk", map[string]any{"description": "Abono", "quantity": "-1",
		"unitPrice": "10", "taxCode": "G21"}, nil), 200, "negative line")
	h.must(h.do("POST", "/api/billing/invoices/"+fix.ID+"/issue", "issuer", map[string]any{"series": rsID, "date": "2026-09-28"}, &fix), 200, "issue it")
	if fix.SellerAddress == nil || fix.SellerAddress.Street != "Calle Mayor 1" || fix.CustomerAddress == nil || fix.CustomerAddress.Street != "Avenida Nueva 9" {
		t.Fatalf("the addresses of today: %+v %+v", fix.SellerAddress, fix.CustomerAddress)
	}
}

func TestBilling_WithFiscalAndParties_MemoryThenSQLite(t *testing.T) {
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
	h.scenario("s")
}

func TestBilling_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/billing/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}

// storedOrMadrid es, como en el anfitrión, la zona que la empresa guarda en su perfil de Parties;
// si no guarda ninguna, la de Madrid (la de toda empresa española en la prueba).
type storedOrMadrid struct{ stored pcontracts.CompanyTimeZones }

func (z *storedOrMadrid) Location(ctx context.Context, company, _ string) (*time.Location, error) {
	if code, err := z.stored.TimeZone(ctx, company); err != nil || code != "" {
		if err != nil {
			return nil, err
		}
		return time.LoadLocation(code)
	}
	return time.LoadLocation("Europe/Madrid")
}

// Sin fecha, una factura se emite con la de hoy en la zona de la empresa, no en UTC: a las 22:30
// UTC del 14 de octubre ya es día 15 en Madrid (decisión de Javier, 2026-11-05). Una empresa de
// Canarias que guarda Atlantic/Canary emite el 31 de diciembre a las 23:30, hora canaria (las
// 00:30 del 1 de enero en Madrid), con fecha del 31 (D-133).
func TestBilling_IssueDateIsTodayInTheSellerZone(t *testing.T) {
	madridAt := time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)
	canaryAt := time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC) // en invierno, Canarias está en UTC
	for name, c := range map[string]struct {
		zones bool
		zone  string // la que guarda la empresa
		at    time.Time
		want  vocab.Date
	}{
		"in Madrid":                    {true, "", madridAt, vocab.MustDate(2026, 10, 15)},
		"without zones, UTC":           {false, "", madridAt, vocab.MustDate(2026, 10, 14)},
		"in the Canaries, stored zone": {true, "Atlantic/Canary", canaryAt, vocab.MustDate(2026, 12, 31)},
	} {
		t.Run(name, func(t *testing.T) {
			zones := &storedOrMadrid{}
			var opts []billing.Option
			if c.zones {
				opts = append(opts, billing.WithZones(zones))
			}
			h := compose(t, opts...)
			zones.stored = h.parties.CompanyTimeZones
			ctx, ps := h.adminCtx, h.parties.Service
			acme, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			h.ok(err)
			if c.zone != "" {
				acmeID, _ := pdomain.ParsePartyID(acme.ID)
				_, err = ps.SetInternalOrganizationProfile.Handle(ctx, papp.SetInternalOrganizationProfile{ID: acmeID, TimeZone: c.zone})
				h.ok(err)
			}
			ana, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Zona"})
			h.ok(err)
			for party, doc := range map[string][2]string{acme.ID: {"c0000000-0004-0000-0000-000000000003", "A58818501"}, ana.ID: {"c0000000-0004-0000-0000-000000000002", "12345678Z"}} {
				id, _ := pdomain.ParsePartyID(party)
				_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: doc[0], Country: "ES", Number: doc[1], Primary: true})
				h.ok(err)
				// El domicilio fiscal: sin él no se emite una factura completa (D-147). Vale desde antes
				// de la hora que finge la prueba.
				since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				_, err = ps.AddContact.Handle(ctx, papp.AddContact{PartyID: id, Kind: "postal", Purposes: []string{"default"}, From: &since,
					Address: &papp.AddressDTO{Line1: "Calle Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
				h.ok(err)
			}
			_, err = h.fiscal.Service.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21", Surcharge: "5.2", From: vocab.MustDate(2012, 9, 1)})
			h.ok(err)
			_, err = h.fiscal.Service.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
			h.ok(err)
			sr, err := h.billing.Service.OpenSeries.Handle(ctx, bapp.OpenSeries{Seller: acme.ID, Code: "FZ", Year: 2026})
			h.ok(err)
			draft, err := h.billing.Service.DraftInvoice.Handle(ctx, bapp.DraftInvoice{Seller: acme.ID, Customer: ana.ID, DetailsInput: bapp.DetailsInput{Description: "Cuota"}})
			h.ok(err)
			id, _ := bdomain.ParseInvoiceID(draft.ID)
			_, err = h.billing.Service.AddLine.Handle(ctx, bapp.AddLine{ID: id, Description: "Cuota", Quantity: "1", UnitPrice: "10", TaxCode: "G21"})
			h.ok(err)

			defer fw.SetClock(fake.New(c.at))()
			issued, err := h.billing.Service.Issue.Handle(ctx, bapp.IssueInvoice{ID: id, Series: sr.ID})
			h.ok(err)
			if issued.IssueDate != c.want.String() {
				t.Fatalf("issue date: %s, want %s", issued.IssueDate, c.want)
			}
		})
	}
}
