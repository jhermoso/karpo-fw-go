package payroll_test

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
	fapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
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
	"github.com/jhermoso/karpo-fw-go/contexts/payroll/contracts"
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

// host composes Parties, Facilities, HR and Payroll on one hot-swappable backend.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
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

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("payroll-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, payroll: ym, hr: hm, fac: fac, parties: pm}
	prepare := []authz.Permission{yapp.PermPayslipRead, yapp.PermPayslipCreate, yapp.PermPayslipUpdate, yapp.PermProfileRead,
		yapp.PermProfileUpdate, yapp.PermAccountRead, yapp.PermAccountCreate, yapp.PermAccountUpdate, yapp.PermCatalogRead}
	users := map[string][]authz.Permission{
		"clerk":    prepare,
		"approver": {yapp.PermPayslipRead, yapp.PermPayslipApprove},
		"outsider": prepare,
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
	ym.RegisterRoutes(mux)
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

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	org := func(name string) papp.PartyDTO {
		o, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag,
			Roles: []string{pdomain.RoleInternalOrganization.String()}})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	acme, globex := org("Acme"), org("Globex")
	h.grant("clerk", acme.ID)
	h.grant("approver", acme.ID)
	h.grant("outsider", globex.ID)
	person := func(name, employer string) papp.PartyDTO {
		p, err := h.parties.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: name, FirstSurname: tag,
			Affiliation: &papp.NewAffiliation{Organization: employer, RelationshipType: pdomain.RelEmployment.String()}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	ana, carl := person("Ana", acme.ID), person("Carl", globex.ID)
	tower, err := h.fac.Service.Register.Handle(ctx, fapp.RegisterFacility{Organization: acme.ID, Type: fdomain.TypeBuilding.String(), Name: "Torre " + tag})
	if err != nil {
		t.Fatal(err)
	}
	wc, err := h.hr.Service.OpenWorkCenter.Handle(ctx, happ.OpenWorkCenter{Employer: acme.ID, Facility: tower.ID, Code: "28/1/01", Opened: vocab.MustDate(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	today := vocab.DateOf(fw.Now())
	job, err := h.hr.Service.Hire.Handle(ctx, happ.Hire{Person: ana.ID, Employer: acme.ID, Number: "E-7", Hired: today.AddDays(-200)})
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := hdomain.ParseEmploymentID(job.ID)
	if _, err := h.hr.Service.AddContract.Handle(ctx, happ.AddContract{ID: jobID, TypeCode: "100", Start: today.AddDays(-200),
		Agreement: "b3800000-0003-0000-0000-000000000005", WorkCenter: wc.ID, Primary: true}); err != nil {
		t.Fatal(err)
	}

	// Employer account (CCC): control digits, unique while active.
	var ccc yapp.AccountDTO
	h.must(h.do("POST", "/api/payroll/employer-accounts", "clerk", map[string]any{"employer": acme.ID, "code": "28/1234567/43",
		"regime": "0111", "method": "manual"}, nil), 400, "wrong control digits")
	h.must(h.do("POST", "/api/payroll/employer-accounts", "clerk", map[string]any{"employer": acme.ID, "code": "28/1234567/42",
		"regime": "0111", "method": "direct-debit", "iban": "ES91 2100 0418 4502 0005 1332"}, &ccc), 201, "ccc")
	h.must(h.do("POST", "/api/payroll/employer-accounts", "clerk", map[string]any{"employer": acme.ID, "code": "28123456742",
		"regime": "0111", "method": "manual"}, nil), 422, "duplicate ccc")
	h.must(h.do("POST", "/api/payroll/employer-accounts", "outsider", map[string]any{"employer": acme.ID, "code": "08123456700",
		"regime": "0111", "method": "manual"}, nil), 404, "acme is out of the outsider's scope")

	// Profile of the employment, with the net split: garnishment, 30 % and the residual.
	var prof yapp.ProfileDTO
	terms := map[string]any{"person": ana.ID, "employer": acme.ID, "contributionGroup": 5, "incomeTaxRate": "15",
		"salary": map[string]any{"amount": "2000", "periodicity": "monthly", "paymentsPerYear": 14}, "employerAccount": ccc.ID}
	h.must(h.do("POST", "/api/payroll/profiles", "clerk", terms, &prof), 201, "profile")
	h.must(h.do("POST", "/api/payroll/profiles", "clerk", terms, nil), 422, "one profile per employment")
	h.must(h.do("POST", "/api/payroll/profiles", "clerk", map[string]any{"person": carl.ID, "employer": acme.ID, "contributionGroup": 5}, nil),
		422, "carl is not an employee of acme")
	if prof.AnnualSalary != "28000.00" || prof.EmployerAccount != ccc.ID {
		t.Fatalf("profile: %+v", prof)
	}
	from := today.AddDays(-200).String()
	for _, split := range []map[string]any{
		{"iban": "ES6621000418401234567891", "amount": "200", "garnishment": true, "from": from},
		{"iban": "ES7921000813610123456789", "percent": "30", "priority": 1, "from": from},
		{"iban": "ES9121000418450200051332", "residual": true, "from": from},
	} {
		h.must(h.do("POST", "/api/payroll/profiles/"+prof.ID+"/splits", "clerk", split, &prof), 200, "split")
	}
	h.must(h.do("POST", "/api/payroll/profiles/"+prof.ID+"/splits", "clerk", map[string]any{"iban": "ES9121000418450200051332",
		"residual": true, "from": from}, nil), 422, "one residual")

	// Payslip of last month: lines, derived totals, approval by another role.
	end := vocab.MustDate(today.Year(), today.Month(), 1).AddDays(-1)
	start := vocab.MustDate(end.Year(), end.Month(), 1)
	var slip yapp.PayslipDTO
	draft := map[string]any{"person": ana.ID, "employer": acme.ID, "start": start.String(), "end": end.String()}
	h.must(h.do("POST", "/api/payroll/payslips", "clerk", draft, &slip), 201, "draft")
	h.must(h.do("POST", "/api/payroll/payslips", "clerk", draft, nil), 422, "one ordinary payslip per period")
	h.must(h.do("POST", "/api/payroll/payslips", "clerk", map[string]any{"person": carl.ID, "employer": acme.ID, "start": start.String(),
		"end": end.String()}, nil), 422, "carl is not employed by acme")
	if slip.EmployeeNumber != "E-7" || slip.WorkCenter != wc.ID || slip.ContributionGroup != 5 || slip.IncomeTaxRate != "15.00" ||
		slip.EmployerAccount != ccc.ID || slip.PaymentDate != end.String() {
		t.Fatalf("snapshot: %+v", slip)
	}
	for _, l := range []map[string]any{
		{"concept": "SALARIO_BASE", "amount": "2000"},
		{"concept": "plus_convenio", "amount": "105.25"},
		{"concept": "SS_EMP_CG", "base": "2105.25", "percent": "4.70"},
		{"concept": "SS_EMP_DES", "base": "2105.25", "percent": "1.55"},
		{"concept": "IRPF_GEN", "base": "2105.25", "percent": "15"},
		{"concept": "SS_ER_CC", "base": "2105.25", "percent": "23.60"},
	} {
		h.must(h.do("POST", "/api/payroll/payslips/"+slip.ID+"/lines", "clerk", l, &slip), 200, "line")
	}
	h.must(h.do("POST", "/api/payroll/payslips/"+slip.ID+"/lines", "clerk", map[string]any{"concept": "NOPE", "amount": "1"}, nil), 400, "unknown concept")
	if slip.Totals.Gross != "2105.25" || slip.Totals.Net != "1657.88" || slip.Totals.IncomeTax != "315.79" || slip.Totals.CompanyCost != "2602.09" ||
		len(slip.Lines) != 6 || slip.Lines[1].Code != "PLUS_CONVENIO" {
		t.Fatalf("payslip: %+v", slip)
	}
	h.must(h.do("POST", "/api/payroll/payslips/"+slip.ID+"/approve", "clerk", nil, nil), 403, "preparing is not approving")
	h.must(h.do("POST", "/api/payroll/payslips/"+slip.ID+"/approve", "approver", nil, &slip), 200, "approve")
	h.must(h.do("POST", "/api/payroll/payslips/"+slip.ID+"/lines", "clerk", map[string]any{"concept": "SALARIO_BASE", "amount": "1"}, nil),
		422, "an approved payslip is immutable")
	h.must(h.do("DELETE", "/api/payroll/payslips/"+slip.ID, "clerk", nil, nil), 422, "an approved payslip is not deleted")
	h.must(h.do("GET", "/api/payroll/payslips/"+slip.ID, "outsider", nil, nil), 404, "outsider")

	// Treasury: the net split across the accounts.
	pays, err := h.payroll.Remittance.NetPayments(context.Background(), []string{slip.ID})
	if err != nil || len(pays[slip.ID]) != 3 || pays[slip.ID][0].Amount != "200.00" || !pays[slip.ID][0].Garnishment ||
		pays[slip.ID][1].Amount != "497.36" || pays[slip.ID][2].Amount != "960.52" {
		t.Fatalf("net payments: %+v %v", pays, err)
	}

	// Correction: cancel, draft again, discard the draft.
	h.must(h.do("POST", "/api/payroll/payslips/"+slip.ID+"/cancel", "approver", map[string]any{"reason": "plus mal calculado"}, &slip), 200, "cancel")
	var again yapp.PayslipDTO
	h.must(h.do("POST", "/api/payroll/payslips", "clerk", draft, &again), 201, "a cancelled payslip frees the period")
	h.must(h.do("DELETE", "/api/payroll/payslips/"+again.ID, "clerk", nil, nil), 204, "discard the draft")
	var list fw.Page[yapp.PayslipDTO]
	h.must(h.do("GET", "/api/payroll/payslips?person="+ana.ID+"&from="+start.String(), "clerk", nil, &list), 200, "search")
	if list.Total != 1 || list.Items[0].Status != "cancelled" {
		t.Fatalf("search: %+v", list)
	}

	// Published Language: Fiscal and Accounting would consume this, not payroll tables.
	broker := inprocess.NewBroker()
	store := memory.NewStore("fiscal")
	fiscal := messaging.NewConsumer("fiscal", memory.NewInbox(store), store)
	var approved []contracts.PayslipApprovedV1
	cancelled := 0
	messaging.Handle(fiscal, func(_ context.Context, e contracts.PayslipApprovedV1, _ application.Envelope) error {
		approved = append(approved, e)
		return nil
	})
	messaging.Handle(fiscal, func(_ context.Context, e contracts.PayslipCancelledV1, _ application.Envelope) error {
		cancelled++
		return nil
	})
	broker.Subscribe("fiscal", fiscal)
	for {
		n, err := h.payroll.Relay(broker).RelayOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	if len(approved) != 1 || approved[0].IncomeTax != "315.79" || approved[0].TaxableBase != "2105.25" || approved[0].PerceptionKey != "A" ||
		approved[0].EmployerAccount != ccc.ID || approved[0].Person != ana.ID || cancelled != 1 {
		t.Fatalf("published: %+v cancelled %d", approved, cancelled)
	}
	trail, err := h.payroll.Audit.Trail(context.Background(), ydomain.PayslipKind, slip.ID)
	if err != nil || len(trail) != 9 || trail[len(trail)-1].Actor.Name != "approver" {
		t.Fatalf("audit: %d %v", len(trail), err)
	}
}

func TestPayroll_WithPartiesFacilitiesHR_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.scenario("mem")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), hinfra.Migrations(), yinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.scenario("sql")
}

func TestPayroll_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/payroll/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
