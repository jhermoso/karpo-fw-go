package fiscal_test

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

	"github.com/jhermoso/karpo-fw-go/contexts/facilities"
	fcapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	fcdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	fcinfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/contracts"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/hr"
	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	hdomain "github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	hinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/payroll"
	yapp "github.com/jhermoso/karpo-fw-go/contexts/payroll/application"
	ydomain "github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	yinfra "github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
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
)

// host composes Parties, Facilities, HR, Payroll and Fiscal on one hot-swappable backend.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	fiscal   *fiscal.Module
	payroll  *payroll.Module
	hr       *hr.Module
	fac      *facilities.Module
	parties  *parties.Module
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	fac := facilities.Compose(sw)
	pm := parties.Compose(sw, nil)
	hm := hr.Compose(sw, hr.WithOrganizations(hinfra.PartiesOrganizations{Hierarchy: pm.Organizations, Membership: pm.Organizations}),
		hr.WithFacilities(hinfra.FacilitiesDirectory{Directory: fac.Directory}))
	ym := payroll.Compose(sw, yinfra.HRStaff{Staff: hm.Staff})
	fm := fiscal.Compose(sw, finfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("fiscal-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, fiscal: fm, payroll: ym, hr: hm, fac: fac, parties: pm}
	users := map[string][]authz.Permission{
		"clerk":     {fapp.PermCatalogRead, fapp.PermTaxpayerRead, fapp.PermTaxpayerWrite, fapp.PermFilingRead, fapp.PermFilingCreate},
		"submitter": {fapp.PermFilingRead, fapp.PermFilingSubmit},
		"legal":     {fapp.PermCatalogRead, fapp.PermCatalogUpdate},
		"outsider":  {fapp.PermCatalogRead, fapp.PermTaxpayerRead, fapp.PermTaxpayerWrite, fapp.PermFilingRead, fapp.PermFilingCreate},
	}
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
	fm.RegisterRoutes(mux)
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

func relay(t *testing.T, r interface {
	RelayOnce(context.Context) (int, error)
}) {
	t.Helper()
	for {
		n, err := r.RelayOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

// payslip drafts, fills and approves the payslip of a month: salary and IRPF at a rate.
func (h *host) payslip(person, employer string, month time.Month, salary, rate string) yapp.PayslipDTO {
	ctx, svc := h.adminCtx, h.payroll.Service
	start := vocab.MustDate(2026, month, 1)
	p, err := svc.DraftPayslip.Handle(ctx, yapp.DraftPayslip{Person: person, Employer: employer, Start: start, End: start.AddMonths(1).AddDays(-1)})
	h.ok(err)
	id, _ := ydomain.ParsePayslipID(p.ID)
	_, err = svc.AddLine.Handle(ctx, yapp.AddLine{ID: id, Concept: "SALARIO_BASE", Amount: salary})
	h.ok(err)
	_, err = svc.AddLine.Handle(ctx, yapp.AddLine{ID: id, Concept: "IRPF_GEN", Base: salary, Percent: rate})
	h.ok(err)
	p, err = svc.Approve.Handle(ctx, yapp.ApprovePayslip{ID: id})
	h.ok(err)
	return p
}

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	ps := h.parties.Service

	// Tax catalog: legal data, its own permission; one rate per code at a time.
	var g21 fapp.RateDTO
	rate := map[string]any{"type": "vat", "territory": "common", "code": "G" + tag, "description": "General", "rate": "21", "surcharge": "5.2", "from": "2012-09-01"}
	h.must(h.do("POST", "/api/fiscal/tax-rates", "clerk", rate, nil), 403, "the catalog is not the clerk's")
	h.must(h.do("POST", "/api/fiscal/tax-rates", "legal", rate, &g21), 201, "rate")
	h.must(h.do("POST", "/api/fiscal/tax-rates", "legal", rate, nil), 422, "overlapping rate")
	h.must(h.do("POST", "/api/fiscal/tax-rates", "legal", map[string]any{"type": "igic", "territory": "common", "code": "G", "description": "IGIC",
		"rate": "7", "from": "2020-01-01"}, nil), 400, "IGIC is not levied in the common territory")
	h.must(h.do("POST", "/api/fiscal/tax-rates/"+g21.ID+"/end", "legal", map[string]any{"on": "2026-12-31"}, nil), 200, "end")
	rate["from"], rate["rate"] = "2027-01-01", "22"
	h.must(h.do("POST", "/api/fiscal/tax-rates", "legal", rate, nil), 201, "next rate")
	if r, found, err := h.fiscal.Rates.RateOn(context.Background(), "vat", "common", "g"+tag, "2026-06-30"); err != nil || !found || r.Rate != "21.00" || r.Surcharge != "5.20" {
		t.Fatalf("rate on 2026: %+v %v %v", r, found, err)
	}
	if r, _, _ := h.fiscal.Rates.RateOn(context.Background(), "vat", "common", "G"+tag, "2027-01-01"); r.Rate != "22.00" {
		t.Fatalf("rate on 2027: %+v", r)
	}
	h.must(h.do("POST", "/api/fiscal/tax-treatments", "legal", map[string]any{"territory": "common", "code": "E" + tag, "description": "Exenta art. 20",
		"kind": "exempt"}, nil), 201, "treatment")
	h.must(h.do("POST", "/api/fiscal/tax-treatments", "legal", map[string]any{"territory": "common", "code": "E" + tag, "description": "Otra",
		"kind": "exempt"}, nil), 422, "duplicate treatment")

	// Parties, HR and Payroll: Acme employs Ana and Bea.
	org := func(name string) papp.PartyDTO {
		o, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag, Roles: []string{pdomain.RoleInternalOrganization.String()}})
		h.ok(err)
		return o
	}
	acme, globex := org("Acme"), org("Globex")
	h.grant("clerk", acme.ID)
	h.grant("submitter", acme.ID)
	h.grant("outsider", globex.ID)
	acmeID, _ := pdomain.ParsePartyID(acme.ID)
	_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: acmeID, DocumentType: "c0000000-0004-0000-0000-000000000003",
		Country: "ES", Number: "A58818501", Primary: true})
	h.ok(err)
	person := func(name string) papp.PartyDTO {
		p, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: name, FirstSurname: tag,
			Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: pdomain.RelEmployment.String()}})
		h.ok(err)
		return p
	}
	ana, bea := person("Ana"), person("Bea")
	anaID, _ := pdomain.ParsePartyID(ana.ID)
	_, err = ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: anaID, DocumentType: "c0000000-0004-0000-0000-000000000002",
		Country: "ES", Number: "12345678-Z"})
	h.ok(err)
	_, err = ps.AddContact.Handle(ctx, papp.AddContact{PartyID: anaID, Kind: "postal", Purposes: []string{"default"},
		Address: &papp.AddressDTO{Line1: "Sol 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
	h.ok(err)
	tower, err := h.fac.Service.Register.Handle(ctx, fcapp.RegisterFacility{Organization: acme.ID, Type: fcdomain.TypeBuilding.String(), Name: "Torre " + tag})
	h.ok(err)
	wc, err := h.hr.Service.OpenWorkCenter.Handle(ctx, happ.OpenWorkCenter{Employer: acme.ID, Facility: tower.ID, Code: "28/1/01", Opened: vocab.MustDate(2020, 1, 1)})
	h.ok(err)
	for _, p := range []papp.PartyDTO{ana, bea} {
		job, err := h.hr.Service.Hire.Handle(ctx, happ.Hire{Person: p.ID, Employer: acme.ID, Hired: vocab.MustDate(2025, 1, 15)})
		h.ok(err)
		jobID, _ := hdomain.ParseEmploymentID(job.ID)
		_, err = h.hr.Service.AddContract.Handle(ctx, happ.AddContract{ID: jobID, TypeCode: "100", Start: vocab.MustDate(2025, 1, 15),
			Agreement: "b3800000-0003-0000-0000-000000000005", WorkCenter: wc.ID, Primary: true})
		h.ok(err)
	}
	h.payslip(ana.ID, acme.ID, time.January, "2000", "15")
	wrong := h.payslip(ana.ID, acme.ID, time.February, "2000", "15")
	wrongID, _ := ydomain.ParsePayslipID(wrong.ID)
	_, err = h.payroll.Service.Cancel.Handle(ctx, yapp.CancelPayslip{ID: wrongID, Reason: "falta el plus"})
	h.ok(err)
	h.payslip(ana.ID, acme.ID, time.February, "2100", "15")
	h.payslip(bea.ID, acme.ID, time.February, "1500", "10")
	h.payslip(ana.ID, acme.ID, time.April, "2100", "15") // second quarter

	// Payroll → Fiscal through the inbox.
	broker := inprocess.NewBroker()
	broker.Subscribe("fiscal", h.fiscal.Consumer)
	relay(t, h.payroll.Relay(broker))

	// Taxpayer profile and obligations.
	var tp fapp.TaxpayerDTO
	h.must(h.do("POST", "/api/fiscal/taxpayers", "clerk", map[string]any{"organization": acme.ID, "territory": "common", "fiscalYearStartMonth": 1}, &tp), 201, "taxpayer")
	h.must(h.do("POST", "/api/fiscal/taxpayers", "clerk", map[string]any{"organization": acme.ID, "territory": "common", "fiscalYearStartMonth": 1}, nil), 422, "one profile")
	h.must(h.do("POST", "/api/fiscal/taxpayers", "outsider", map[string]any{"organization": acme.ID, "territory": "common", "fiscalYearStartMonth": 1}, nil), 404, "outsider")
	h.must(h.do("POST", "/api/fiscal/taxpayers/"+tp.ID+"/obligations", "clerk", map[string]any{"form": "190", "periodicity": "quarterly", "fromYear": 2020}, nil), 400, "190 is annual")
	h.must(h.do("POST", "/api/fiscal/taxpayers/"+tp.ID+"/obligations", "clerk", map[string]any{"form": "111", "periodicity": "quarterly", "fromYear": 2020}, &tp), 200, "111")
	h.must(h.do("POST", "/api/fiscal/taxpayers/"+tp.ID+"/obligations", "clerk", map[string]any{"form": "190", "periodicity": "annual", "fromYear": 2020}, &tp), 200, "190")
	h.must(h.do("POST", "/api/fiscal/taxpayers/"+tp.ID+"/activities", "clerk", map[string]any{"code": "A1", "iae": "843", "category": "services",
		"description": "Consultoría", "from": "2020-01-01", "primary": true}, &tp), 200, "activity")
	if len(tp.Obligations) != 2 || len(tp.Activities) != 1 {
		t.Fatalf("taxpayer: %+v", tp)
	}

	// Modelo 111 of the first quarter: by payment date, the cancelled payslip out.
	var q1 fapp.FilingDTO
	h.must(h.do("POST", "/api/fiscal/filings", "clerk", map[string]any{"organization": acme.ID, "form": "111", "year": 2026, "period": "02"}, nil), 422, "the 111 is quarterly")
	h.must(h.do("POST", "/api/fiscal/filings", "clerk", map[string]any{"organization": acme.ID, "form": "303", "year": 2026, "period": "1T"}, nil), 422, "no 303 obligation")
	h.must(h.do("POST", "/api/fiscal/filings", "clerk", map[string]any{"organization": acme.ID, "form": "111", "year": 2026, "period": "1T"}, &q1), 201, "111 1T")
	if q1.Recipients != 2 || q1.Perceptions != "5600.00" || q1.Withheld != "765.00" || q1.DeclarantNIF != "A58818501" || len(q1.Problems) != 0 {
		t.Fatalf("111: %+v", q1)
	}
	h.must(h.do("POST", "/api/fiscal/filings/"+q1.ID+"/submit", "clerk", map[string]any{"reference": "CSV1"}, nil), 403, "the clerk does not submit")
	h.must(h.do("POST", "/api/fiscal/filings/"+q1.ID+"/submit", "submitter", map[string]any{"reference": "CSV1"}, &q1), 200, "submit 111")
	h.must(h.do("POST", "/api/fiscal/filings", "clerk", map[string]any{"organization": acme.ID, "form": "111", "year": 2026, "period": "1T"}, nil), 422, "already submitted")
	h.must(h.do("GET", "/api/fiscal/filings/"+q1.ID, "outsider", nil, nil), 404, "outsider")

	// Modelo 190: Bea has no tax number nor address yet.
	var y fapp.FilingDTO
	h.must(h.do("POST", "/api/fiscal/filings", "clerk", map[string]any{"organization": acme.ID, "form": "190", "year": 2026, "period": "0A"}, &y), 201, "190")
	if len(y.Problems) != 2 || y.Perceptions != "7700.00" || y.Withheld != "1080.00" || len(y.Lines) != 2 {
		t.Fatalf("190 draft: %+v", y)
	}
	h.must(h.do("POST", "/api/fiscal/filings/"+y.ID+"/submit", "submitter", nil, nil), 422, "incomplete 190")
	beaID, _ := pdomain.ParsePartyID(bea.ID)
	_, err = ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: beaID, DocumentType: "c0000000-0004-0000-0000-000000000002",
		Country: "ES", Number: "00000000T"})
	h.ok(err)
	_, err = ps.AddContact.Handle(ctx, papp.AddContact{PartyID: beaID, Kind: "postal", Purposes: []string{"billing"},
		Address: &papp.AddressDTO{Line1: "Rambla 1", PostalCode: "08001", Locality: "Barcelona", Country: "ES"}})
	h.ok(err)
	h.must(h.do("POST", "/api/fiscal/filings", "clerk", map[string]any{"organization": acme.ID, "form": "190", "year": 2026, "period": "0A"}, &y), 201, "regenerate")
	if len(y.Problems) != 0 {
		t.Fatalf("190 regenerated: %+v", y)
	}
	h.must(h.do("POST", "/api/fiscal/filings/"+y.ID+"/submit", "submitter", nil, &y), 200, "submit 190")
	if y.Number != 1 || q1.Number != 1 {
		t.Fatalf("numbers per form: 111 %d, 190 %d", q1.Number, y.Number)
	}

	// Revert frees the slot (the C# kept it blocked); the new submission takes the next number.
	h.must(h.do("POST", "/api/fiscal/filings/"+q1.ID+"/revert", "submitter", map[string]any{"reason": "complementaria"}, nil), 200, "revert")
	var q1b fapp.FilingDTO
	h.must(h.do("POST", "/api/fiscal/filings", "clerk", map[string]any{"organization": acme.ID, "form": "111", "year": 2026, "period": "1T"}, &q1b), 201, "again")
	h.must(h.do("POST", "/api/fiscal/filings/"+q1b.ID+"/submit", "submitter", nil, &q1b), 200, "submit again")
	if q1b.Number != 2 {
		t.Fatalf("next number: %d", q1b.Number)
	}
	var drafts fw.Page[fapp.FilingDTO]
	h.must(h.do("GET", "/api/fiscal/filings?organization="+acme.ID+"&status=submitted", "clerk", nil, &drafts), 200, "search")
	if drafts.Total != 2 {
		t.Fatalf("submitted: %+v", drafts)
	}

	// Published Language.
	store := memory.NewStore("accounting")
	acc := messaging.NewConsumer("accounting", memory.NewInbox(store), store)
	var submitted []contracts.FilingSubmittedV1
	reverted := 0
	messaging.Handle(acc, func(_ context.Context, e contracts.FilingSubmittedV1, _ application.Envelope) error {
		submitted = append(submitted, e)
		return nil
	})
	messaging.Handle(acc, func(_ context.Context, e contracts.FilingRevertedV1, _ application.Envelope) error {
		reverted++
		return nil
	})
	out := inprocess.NewBroker()
	out.Subscribe("accounting", acc)
	relay(t, h.fiscal.Relay(out))
	if len(submitted) != 3 || reverted != 1 || submitted[1].Form != "190" || submitted[1].Withheld != "1080.00" {
		t.Fatalf("published: %+v reverted %d", submitted, reverted)
	}
}

func TestFiscal_FedByPayroll_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), fcinfra.Migrations(), hinfra.Migrations(), yinfra.Migrations(), finfra.Migrations()})
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

func TestFiscal_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/fiscal/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
