package financial_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/financial"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	"github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/financial/infrastructure"
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
)

type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	fin      *financial.Module
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	fm := financial.Compose(sw)
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("financial-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, fin: fm}
	users := map[string][]authz.Permission{
		"opener":   {fapp.PermAccountRead, fapp.PermAccountCreate, fapp.PermAccountUpdate},
		"officer":  {fapp.PermAccountRead, fapp.PermAccountBlock},
		"closer":   {fapp.PermAccountRead, fapp.PermAccountClose},
		"viewer":   {fapp.PermAccountRead},
		"outsider": fapp.Permissions(),
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

func (h *host) grant(user, org string) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(org), Level: authz.Full})
	h.dir.Put(h.ids[user], s)
}

// do sends a request and decodes the answer into out, emptied first.
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
		if o, ok := out.(*fapp.AccountDTO); ok { // omitted fields must not keep what a previous answer left
			*o = fapp.AccountDTO{}
		}
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

func (h *host) scenario() {
	t := h.t
	acme, globex := fw.NewUUID().String(), fw.NewUUID().String()
	ana, luis, eva := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
	for _, u := range []string{"opener", "officer", "closer", "viewer"} {
		h.grant(u, acme)
	}
	h.grant("outsider", globex)
	today := vocab.DateOf(fw.Now())
	path := func(a fapp.AccountDTO, action string) string { return "/api/financial/accounts/" + a.ID + "/" + action }

	// Opening: an IBAN whose digits are right, or an identifier of the institution's own.
	open := map[string]any{"company": acme, "number": "es91 2100 0418 4502 0005 1332", "holder": ana, "name": "Cuenta de pago de Ana",
		"uses": []string{"customer-payment", "Customer-Payment"}}
	var acc fapp.AccountDTO
	h.must(h.do("POST", "/api/financial/accounts", "viewer", open, nil), 403, "viewer")
	h.must(h.do("POST", "/api/financial/accounts", "outsider", open, nil), 404, "outsider")
	for what, bad := range map[string]map[string]any{
		"wrong check digits": {"company": acme, "number": "ES0021000418450200051332", "holder": ana},
		"no holder":          {"company": acme, "number": "ES9121000418450200051332"},
		"short identifier":   {"company": acme, "number": "V-1", "virtual": true, "holder": ana},
		"unknown use":        {"company": acme, "number": "ES9121000418450200051332", "holder": ana, "uses": []string{"savings"}},
		"own operating":      {"company": acme, "number": "ES9121000418450200051332", "holder": ana, "uses": []string{"own-operating"}},
		"currency":           {"company": acme, "number": "ES9121000418450200051332", "holder": ana, "currency": "EURO"},
		"bic":                {"company": acme, "number": "ES9121000418450200051332", "holder": ana, "bic": "CAIXES"},
	} {
		h.must(h.do("POST", "/api/financial/accounts", "opener", bad, nil), 400, what)
	}
	h.must(h.do("POST", "/api/financial/accounts", "opener", open, &acc), 201, "open")
	if acc.Number != "ES9121000418450200051332" || acc.Virtual || acc.Status != "active" || acc.Currency != "EUR" || acc.Holder != ana || acc.Opened != today.String() ||
		len(acc.Holders) != 1 || !acc.Holders[0].Primary || acc.Holders[0].Role != "holder" || len(acc.Uses) != 1 || acc.Uses[0].Use != "customer-payment" || acc.Version != 1 {
		t.Fatalf("opened: %+v", acc)
	}
	h.must(h.do("POST", "/api/financial/accounts", "opener", map[string]any{"company": acme, "number": "ES91 2100 0418 4502 0005 1332", "holder": luis}, nil), 422, "the same number")
	var wallet, demo fapp.AccountDTO
	h.must(h.do("POST", "/api/financial/accounts", "opener", map[string]any{"company": acme, "number": "virt-000001", "virtual": true, "holder": luis,
		"currency": "usd", "uses": []string{"virtual-multicurrency"}, "opened": today.AddDays(-30).String()}, &wallet), 201, "a virtual account")
	h.must(h.do("POST", "/api/financial/accounts", "opener", map[string]any{"company": acme, "number": "DE89370400440532013000", "holder": eva, "demo": true,
		"name": "Pruebas"}, &demo), 201, "a demo account")
	if wallet.Number != "VIRT-000001" || !wallet.Virtual || wallet.Currency != "USD" || wallet.Opened != today.AddDays(-30).String() || !demo.Demo {
		t.Fatalf("virtual and demo: %+v %+v", wallet, demo)
	}

	// Its details.
	h.must(h.do("PUT", "/api/financial/accounts/"+acc.ID, "officer", map[string]any{"name": "x"}, nil), 403, "stopping is not keeping")
	h.must(h.do("PUT", "/api/financial/accounts/"+acc.ID, "outsider", map[string]any{"name": "x"}, nil), 404, "outsider")
	h.must(h.do("PUT", "/api/financial/accounts/"+acc.ID, "opener", map[string]any{"name": "x", "bic": "CAIX"}, nil), 400, "bic")
	product := fw.NewUUID().String()
	h.must(h.do("PUT", "/api/financial/accounts/"+acc.ID, "opener", map[string]any{"name": "Cuenta de Ana y Luis", "bic": "caixesbbxxx", "product": product}, &acc), 200, "describe")
	if acc.Name != "Cuenta de Ana y Luis" || acc.BIC != "CAIXESBBXXX" || acc.Product != product {
		t.Fatalf("described: %+v", acc)
	}

	// Who is related to it: the account is always filed under one of its holders.
	rel := func(party, role string, primary bool) map[string]any {
		return map[string]any{"party": party, "role": role, "primary": primary}
	}
	h.must(h.do("POST", path(acc, "holders"), "viewer", rel(luis, "holder", false), nil), 403, "viewer")
	h.must(h.do("POST", path(acc, "holders"), "opener", rel(luis, "owner", false), nil), 400, "role")
	h.must(h.do("POST", path(acc, "holders"), "opener", rel(eva, "authorized", true), nil), 400, "filed under a holder only")
	h.must(h.do("POST", path(acc, "holders"), "opener", rel(eva, "authorized", false), &acc), 200, "authorized")
	h.must(h.do("POST", path(acc, "holders"), "opener", rel(eva, "authorized", false), nil), 422, "twice")
	h.must(h.do("POST", path(acc, "holders/end"), "opener", map[string]any{"party": ana, "role": "holder"}, nil), 422, "filed under her")
	h.must(h.do("POST", path(acc, "holders/primary"), "opener", map[string]any{"party": eva}, nil), 422, "eva does not hold it")
	h.must(h.do("POST", path(acc, "holders"), "opener", rel(luis, "holder", false), &acc), 200, "a second holder")
	h.must(h.do("POST", path(acc, "holders/primary"), "opener", map[string]any{"party": luis}, &acc), 200, "filed under luis")
	h.must(h.do("POST", path(acc, "holders/end"), "opener", map[string]any{"party": ana, "role": "holder"}, &acc), 200, "ana leaves")
	h.must(h.do("POST", path(acc, "holders/end"), "opener", map[string]any{"party": ana, "role": "holder"}, nil), 422, "she already left")
	h.must(h.do("POST", path(acc, "holders/end"), "opener", map[string]any{"party": luis, "role": "holder"}, nil), 422, "the last holder stays")
	if acc.Holder != luis || len(acc.Holders) != 3 || acc.Holders[0].Thru != today.String() || acc.Holders[0].Primary || !acc.Holders[2].Primary {
		t.Fatalf("holders: %+v", acc)
	}

	// What it is used for.
	h.must(h.do("POST", path(acc, "uses"), "opener", map[string]any{"use": "savings"}, nil), 400, "unknown use")
	h.must(h.do("POST", path(acc, "uses"), "opener", map[string]any{"use": "customer-payment"}, nil), 422, "it already has it")
	h.must(h.do("POST", path(acc, "uses"), "opener", map[string]any{"use": "player"}, &acc), 200, "another use")
	h.must(h.do("POST", path(acc, "uses/end"), "opener", map[string]any{"use": "customer-payment"}, &acc), 200, "end a use")
	h.must(h.do("POST", path(acc, "uses/end"), "opener", map[string]any{"use": "customer-payment"}, nil), 422, "it no longer has it")
	h.must(h.do("POST", path(acc, "uses"), "opener", map[string]any{"use": "customer-payment"}, &acc), 200, "and has it again")
	if len(acc.Uses) != 3 || acc.Uses[0].Thru != today.String() || acc.Uses[2].Thru != "" {
		t.Fatalf("uses: %+v", acc.Uses)
	}

	// Stopping it and letting it go on.
	why := map[string]any{"reason": "Orden judicial 123/2026"}
	h.must(h.do("POST", path(acc, "block"), "opener", why, nil), 403, "keeping is not stopping")
	h.must(h.do("POST", path(acc, "block"), "outsider", why, nil), 404, "outsider")
	h.must(h.do("POST", path(acc, "block"), "officer", nil, nil), 422, "say why")
	h.must(h.do("POST", path(acc, "release"), "officer", nil, nil), 422, "nothing to release")
	h.must(h.do("POST", path(acc, "block"), "officer", why, &acc), 200, "block")
	if acc.Status != "blocked" || acc.Reason != "Orden judicial 123/2026" {
		t.Fatalf("blocked: %+v", acc)
	}
	h.must(h.do("POST", path(acc, "block"), "officer", why, nil), 422, "blocked twice")
	h.must(h.do("POST", path(acc, "abandon"), "officer", map[string]any{"reason": "Sin movimientos en 6 años"}, &acc), 200, "abandoned")
	h.must(h.do("POST", path(acc, "release"), "officer", nil, &acc), 200, "release")
	if acc.Status != "active" || acc.Reason != "" {
		t.Fatalf("released: %+v", acc)
	}
	// A blocked account still has its holders and uses kept.
	h.must(h.do("POST", path(wallet, "block"), "officer", why, &wallet), 200, "block the wallet")
	h.must(h.do("PUT", "/api/financial/accounts/"+wallet.ID, "opener", map[string]any{"name": "Monedero de Luis"}, &wallet), 200, "describe a blocked account")

	// Reading and searching (before anything is closed).
	var got fapp.AccountDTO
	h.must(h.do("GET", "/api/financial/accounts/"+acc.ID, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/financial/accounts/nope", "viewer", nil, nil), 400, "bad id")
	h.must(h.do("GET", "/api/financial/accounts/by-number/es9121000418450200051332?company="+acme, "viewer", nil, &got), 200, "by number")
	h.must(h.do("GET", "/api/financial/accounts/by-number/ES9121000418450200051332?company="+globex, "outsider", nil, nil), 404, "not in that company")
	if got.ID != acc.ID || len(got.Holders) != 3 || got.Holders[1].Role != "authorized" || got.Product != product {
		t.Fatalf("by number: %+v", got)
	}
	var page fw.Page[fapp.AccountDTO]
	search := func(user, query string, want ...string) {
		t.Helper()
		h.must(h.do("GET", "/api/financial/accounts?"+query, user, nil, &page), 200, query)
		if int(page.Total) != len(want) {
			t.Fatalf("%s: %d accounts, want %d: %+v", query, page.Total, len(want), page.Items)
		}
		for i, id := range want {
			if page.Items[i].ID != id {
				t.Fatalf("%s: item %d is %s", query, i, page.Items[i].Number)
			}
		}
	}
	search("viewer", "company="+acme, demo.ID, acc.ID, wallet.ID) // by number: DE…, ES…, VIRT…
	search("viewer", "party="+luis, acc.ID, wallet.ID)
	search("viewer", "party="+ana)                  // she left: nothing now
	search("viewer", "party="+eva, demo.ID, acc.ID) // authorized on one, holder of the other
	search("viewer", "status=blocked", wallet.ID)
	search("viewer", "use=player", acc.ID)
	search("viewer", "currency=usd", wallet.ID)
	search("viewer", "name=LUIS", acc.ID, wallet.ID)
	search("viewer", "demo=true", demo.ID)
	search("outsider", "")
	h.must(h.do("GET", "/api/financial/accounts?status=frozen", "viewer", nil, nil), 400, "status")
	h.must(h.do("GET", "/api/financial/accounts?demo=maybe", "viewer", nil, nil), 400, "demo")

	// Counting, within the company one may see; demo accounts apart.
	var stats fapp.StatsDTO
	h.must(h.do("GET", "/api/financial/accounts/stats?company="+acme, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/financial/accounts/stats", "viewer", nil, nil), 400, "which company")
	h.must(h.do("GET", "/api/financial/accounts/stats?company="+acme, "viewer", nil, &stats), 200, "stats")
	if stats.Total != 2 || stats.Demo != 1 || stats.ByStatus["active"] != 1 || stats.ByStatus["blocked"] != 1 || stats.ByCurrency["USD"] != 1 ||
		stats.ByUse["player"] != 1 || stats.ByUse["customer-payment"] != 1 || stats.ByUse["virtual-multicurrency"] != 1 {
		t.Fatalf("stats: %+v", stats)
	}

	// What other contexts ask: the accounts of a party, and whether each can operate.
	refs, err := h.fin.Accounts.OfParty(context.Background(), acme, luis)
	if err != nil || len(refs) != 2 || refs[0].Number != acc.Number || !refs[0].Operable || refs[0].Role != "holder" || refs[1].Operable || refs[1].Status != "blocked" {
		t.Fatalf("accounts of luis: %+v %v", refs, err)
	}
	if none, err := h.fin.Accounts.OfParty(context.Background(), globex, luis); err != nil || len(none) != 0 {
		t.Fatalf("in another institution: %+v %v", none, err)
	}
	if _, err := h.fin.Accounts.OfParty(context.Background(), "x", luis); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("ids: %v", err)
	}

	// Closing: its holders and uses end with it, and it changes no more.
	h.must(h.do("POST", path(wallet, "close"), "officer", nil, nil), 403, "stopping is not closing")
	h.must(h.do("POST", path(wallet, "close"), "closer", map[string]any{"on": today.AddDays(-31).String()}, nil), 422, "before it was opened")
	h.must(h.do("POST", path(wallet, "close"), "closer", map[string]any{"reason": "A petición del titular"}, &wallet), 200, "close")
	if wallet.Status != "closed" || wallet.Closed != today.String() || wallet.Holder != "" || wallet.Holders[0].Thru != today.String() || wallet.Holders[0].Primary ||
		wallet.Uses[0].Thru != today.String() || wallet.Reason != "A petición del titular" {
		t.Fatalf("closed: %+v", wallet)
	}
	h.must(h.do("POST", path(wallet, "close"), "closer", nil, nil), 422, "closed twice")
	h.must(h.do("POST", path(wallet, "release"), "officer", nil, nil), 422, "a closed account is not released")
	h.must(h.do("PUT", "/api/financial/accounts/"+wallet.ID, "opener", map[string]any{"name": "x"}, nil), 422, "nor described")
	h.must(h.do("POST", path(wallet, "holders"), "opener", rel(ana, "holder", true), nil), 422, "nor held")
	h.must(h.do("POST", path(wallet, "uses"), "opener", map[string]any{"use": "player"}, nil), 422, "nor used")
	search("viewer", "party="+luis, acc.ID)
	h.must(h.do("GET", "/api/financial/accounts/"+wallet.ID, "viewer", nil, &got), 200, "it is still read")
	if got.Status != "closed" || len(got.Holders) != 1 {
		t.Fatalf("closed, read back: %+v", got)
	}
	// Its number stays taken.
	h.must(h.do("POST", "/api/financial/accounts", "opener", map[string]any{"company": acme, "number": "VIRT-000001", "virtual": true, "holder": ana}, nil), 422, "taken")

	// Three opened; blocked, abandoned and released; the wallet blocked and closed.
	if n, err := h.fin.Relay(inprocess.NewBroker()).RelayOnce(context.Background()); err != nil || n != 8 {
		t.Fatalf("published: %d %v", n, err)
	}
}

func TestFinancial_OpenHoldBlockClose_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.scenario()

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{finfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.scenario()
}

func TestAccount_Rules(t *testing.T) {
	day := func(s string) vocab.Date { d, _ := vocab.ParseDate(s); return d }
	ana, luis := domain.PartyID{UUID: fw.NewUUID()}, domain.PartyID{UUID: fw.NewUUID()}
	open := func() *domain.Account {
		t.Helper()
		a, err := domain.Open(domain.NewAccountID(), domain.Opening{Company: domain.OrganizationID{UUID: fw.NewUUID()}, Number: "gb82 west 1234 5698 7654 32",
			Currency: vocab.MustCurrencyCode("GBP"), Opened: day("2026-01-10"), Holder: ana, Uses: []string{"agent"}})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	violates := func(err error, code string) {
		t.Helper()
		var rv *fw.RuleViolationError
		if !errors.As(err, &rv) || rv.Code != code {
			t.Fatalf("want %s: %v", code, err)
		}
	}
	a := open()
	if s := a.State(); s.Number != "GB82WEST12345698765432" || s.IBAN.Country() != "GB" || a.PrimaryHolder() != ana || !a.Used("agent") || len(a.PendingEvents()) != 1 {
		t.Fatalf("opened: %+v", s)
	}
	// A relation or a use does not end before it began, and a bad change leaves all as it was.
	if err := a.Relate(luis, domain.RoleBeneficiary, day("2026-02-01"), false); err != nil {
		t.Fatal(err)
	}
	if err := a.Unrelate(luis, domain.RoleBeneficiary, day("2026-01-31")); !errors.Is(err, fw.ErrValidation) || !a.State().Holders[1].Current() {
		t.Fatalf("ending before beginning: %v", err)
	}
	if err := a.Withdraw("agent", day("2026-01-09")); !errors.Is(err, fw.ErrValidation) || !a.Used("agent") {
		t.Fatalf("ending a use before it began: %v", err)
	}
	violates(a.Withdraw("player", day("2026-03-01")), "financial.not_used")
	violates(a.FileUnder(luis), "financial.not_related") // a beneficiary does not hold it
	violates(a.Abandon(" "), "financial.reason")
	if err := a.Abandon("sin uso"); err != nil || a.State().Status != domain.Abandoned {
		t.Fatalf("abandon: %v", err)
	}
	violates(a.Block("x"), "financial.transition")
	violates(a.Abandon("x"), "financial.transition")
	violates(a.Close(day("2026-01-09"), ""), "financial.closing_date")
	// Closed the day it was opened: what began later ends the day it began.
	if err := a.Close(day("2026-01-10"), ""); err != nil {
		t.Fatal(err)
	}
	s := a.State()
	if s.Status != domain.Closed || s.Holders[0].Thru != day("2026-01-10") || s.Holders[1].Thru != day("2026-02-01") || !a.PrimaryHolder().IsZero() || a.Used("agent") {
		t.Fatalf("closed: %+v", s)
	}
	violates(a.Release(), "financial.transition")
	violates(a.Assign("player", day("2026-03-01")), "financial.closed")
	if _, err := domain.ReconstituteAccount(a.ID(), s); err != nil {
		t.Fatalf("a closed account is rebuilt: %v", err)
	}
	// An account that is not closed has a holder it is filed under.
	live := open().State()
	live.Holders[0].Primary = false
	if _, err := domain.ReconstituteAccount(domain.NewAccountID(), live); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("filed under nobody: %v", err)
	}
	if _, err := domain.Open(domain.NewAccountID(), domain.Opening{Company: domain.OrganizationID{UUID: fw.NewUUID()}, Number: "GB82WEST12345698765432", Virtual: true,
		Currency: vocab.MustCurrencyCode("GBP"), Opened: day("2026-01-10"), Holder: domain.PartyID{}}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("no holder: %v", err)
	}
}

func TestFinancial_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/financial/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
