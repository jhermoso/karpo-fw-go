package exchange_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/exchange"
	xapp "github.com/jhermoso/karpo-fw-go/contexts/exchange/application"
	"github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
	xinfra "github.com/jhermoso/karpo-fw-go/contexts/exchange/infrastructure"
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
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// collaborators plays Parties: the promotion codes of the collaborators.
type collaborators map[string]domain.PartyID

func (c collaborators) ByPromotionCode(_ context.Context, _ domain.OrganizationID, code string) (domain.PartyID, bool, error) {
	p, ok := c[strings.ToUpper(code)]
	return p, ok, nil
}

type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	exg      *exchange.Module
	partner  domain.PartyID
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	partner := domain.PartyID{UUID: fw.NewUUID()}
	xm := exchange.Compose(sw, collaborators{"PROMO1": partner})
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("exchange-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, exg: xm, partner: partner}
	users := map[string][]authz.Permission{
		"pricer":   {xapp.PermPricingRead, xapp.PermPricingUpdate},
		"clerk":    {xapp.PermReservationRead, xapp.PermReservationCreate},
		"desk":     {xapp.PermReservationRead, xapp.PermReservationProgress},
		"viewer":   {xapp.PermReservationRead, xapp.PermPricingRead},
		"outsider": xapp.Permissions(),
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
	xm.RegisterRoutes(mux)
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

// do sends a request and decodes the answer into a fresh value of out.
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
		switch o := out.(type) { // omitted fields must not keep what a previous answer left
		case *xapp.ReservationDTO:
			*o = xapp.ReservationDTO{}
		case *xapp.QuoteDTO:
			*o = xapp.QuoteDTO{}
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
	acme, globex, customer, office := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
	for _, u := range []string{"pricer", "clerk", "desk", "viewer"} {
		h.grant(u, acme)
	}
	h.grant("outsider", globex)
	now := fw.Now().UTC()
	pickup := now.Add(48 * time.Hour).Truncate(time.Second)
	today := vocab.DateOf(now)

	// Currencies, rates and margins: the numbers of the C# quote tests.
	usd := map[string]any{"company": acme, "code": "usd", "name": "Dólar", "facial": "5"}
	h.must(h.do("PUT", "/api/exchange/currencies", "viewer", usd, nil), 403, "viewer")
	h.must(h.do("PUT", "/api/exchange/currencies", "outsider", usd, nil), 404, "outsider")
	h.must(h.do("PUT", "/api/exchange/currencies", "pricer", map[string]any{"company": acme, "code": "EUR", "name": "Euro"}, nil), 400, "the euro")
	h.must(h.do("PUT", "/api/exchange/currencies", "pricer", usd, nil), 200, "USD")
	h.must(h.do("PUT", "/api/exchange/currencies", "pricer", map[string]any{"company": acme, "code": "GBP", "name": "Libra", "facial": "5"}, nil), 200, "GBP")
	h.must(h.do("PUT", "/api/exchange/currencies", "pricer", map[string]any{"company": acme, "code": "BTC", "name": "Bitcoin", "crypto": true}, nil), 200, "BTC")
	h.must(h.do("PUT", "/api/exchange/currencies", "pricer", map[string]any{"company": acme, "code": "CHF", "name": "Franco", "facial": "10"}, nil), 200, "CHF")
	rate := func(code, r string) map[string]any { return map[string]any{"company": acme, "code": code, "rate": r} }
	h.must(h.do("POST", "/api/exchange/rates", "pricer", rate("USD", "0"), nil), 422, "zero rate")
	h.must(h.do("POST", "/api/exchange/rates", "pricer", rate("JPY", "0.0062"), nil), 422, "a currency the company does not exchange")
	for code, r := range map[string]string{"USD": "0.92", "GBP": "1.17", "BTC": "60000"} {
		h.must(h.do("POST", "/api/exchange/rates", "pricer", rate(code, r), nil), 200, "rate of "+code)
	}
	margin := map[string]any{"company": acme, "currency": "USD", "kind": "percent", "level1": "1.5", "level2": "2", "level3": "2.5"}
	h.must(h.do("PUT", "/api/exchange/margins", "pricer", map[string]any{"company": acme, "currency": "USD", "kind": "tip", "level1": "1", "level2": "1", "level3": "1"}, nil), 400, "kind")
	h.must(h.do("PUT", "/api/exchange/margins", "pricer", map[string]any{"company": acme, "currency": "JPY", "kind": "percent", "level1": "1", "level2": "1", "level3": "1"}, nil), 422, "margin of an unknown currency")
	h.must(h.do("PUT", "/api/exchange/margins", "pricer", margin, nil), 200, "margin")
	var currencies []xapp.CurrencyDTO
	h.must(h.do("GET", "/api/exchange/currencies?company="+acme, "viewer", nil, &currencies), 200, "currencies")
	if len(currencies) != 4 || currencies[0].Code != "BTC" || currencies[1].Code != "CHF" || currencies[1].Rate != "" || currencies[3].Rate != "0.920000" ||
		currencies[3].RateOn != today.String() || currencies[3].Facial != "5.00" {
		t.Fatalf("currencies: %+v", currencies)
	}
	var set xapp.SettingsDTO
	h.must(h.do("GET", "/api/exchange/settings?company="+acme, "viewer", nil, &set), 200, "settings")
	if !set.Defaults || set.Level != 2 || set.PromotionMode != "optional" || !set.ValidatePromotion || set.ExpiryHours != 4 {
		t.Fatalf("default settings: %+v", set)
	}

	// Quotes.
	quote := func(currency, amount string) string {
		return "/api/exchange/quote?company=" + acme + "&currency=" + currency + "&amount=" + amount
	}
	var q xapp.QuoteDTO
	h.must(h.do("GET", quote("USD", "500"), "pricer", nil, nil), 403, "setting prices is not quoting")
	h.must(h.do("GET", quote("USD", "500"), "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", quote("USD", "0"), "viewer", nil, nil), 422, "amount")
	h.must(h.do("GET", quote("JPY", "500"), "viewer", nil, nil), 422, "unknown currency")
	h.must(h.do("GET", quote("CHF", "500"), "viewer", nil, nil), 422, "no rate")
	h.must(h.do("GET", quote("BTC", "1"), "viewer", nil, nil), 422, "crypto")
	h.must(h.do("GET", quote("USD", "500"), "viewer", nil, &q), 200, "USD 500")
	if q.BaseRate != "0.920000" || q.OfferedRate != "0.938400" || q.Eur != "469.20" || q.Level != 2 || q.MarginValue != "2" || q.NoMargin || q.Segment != "WEB" {
		t.Fatalf("USD 500: %+v", q)
	}
	h.must(h.do("GET", quote("usd", "123"), "viewer", nil, &q), 200, "USD 123")
	if q.Delivered != "120.00" || q.Eur != "112.61" || q.Facial != "5.00" {
		t.Fatalf("whole notes: %+v", q)
	}
	h.must(h.do("GET", quote("GBP", "500"), "viewer", nil, &q), 200, "GBP 500")
	if q.OfferedRate != "1.170000" || q.Eur != "585.00" || !q.NoMargin {
		t.Fatalf("no margin: %+v", q)
	}
	settings := func(level int, mode string) map[string]any {
		return map[string]any{"company": acme, "level": level, "promotionMode": mode, "validatePromotion": true, "expiryHours": 4}
	}
	h.must(h.do("PUT", "/api/exchange/settings", "viewer", settings(3, "optional"), nil), 403, "viewer")
	h.must(h.do("PUT", "/api/exchange/settings", "pricer", settings(4, "optional"), nil), 400, "level")
	h.must(h.do("PUT", "/api/exchange/settings", "pricer", settings(3, "optional"), &set), 200, "third level")
	h.must(h.do("GET", quote("USD", "500"), "viewer", nil, &q), 200, "third level")
	if q.OfferedRate != "0.943000" || q.Eur != "471.50" || set.Defaults {
		t.Fatalf("third level: %+v", q)
	}
	h.must(h.do("PUT", "/api/exchange/settings", "pricer", settings(2, "required"), nil), 200, "second level, code required")

	// A reservation: the rates are quoted by the server and kept.
	order := func(code string, lines ...map[string]any) map[string]any {
		return map[string]any{"company": acme, "customer": customer, "channel": "web", "pickup": pickup.Format(time.RFC3339), "facility": office,
			"promotionCode": code, "lines": lines}
	}
	line := func(currency, amount string) map[string]any {
		return map[string]any{"currency": currency, "amount": amount}
	}
	var res, cancelled, late xapp.ReservationDTO
	h.must(h.do("POST", "/api/exchange/reservations", "desk", order("PROMO1", line("USD", "500")), nil), 403, "moving is not reserving")
	h.must(h.do("POST", "/api/exchange/reservations", "outsider", order("PROMO1", line("USD", "500")), nil), 404, "outsider")
	h.must(h.do("POST", "/api/exchange/reservations", "clerk", order("", line("USD", "500")), nil), 422, "a code is required")
	h.must(h.do("POST", "/api/exchange/reservations", "clerk", order("nope", line("USD", "500")), nil), 422, "unknown code")
	h.must(h.do("POST", "/api/exchange/reservations", "clerk", order("PROMO1", line("USD", "500"), line("usd", "100")), nil), 400, "a currency twice")
	h.must(h.do("POST", "/api/exchange/reservations", "clerk", order("PROMO1", map[string]any{"currency": "USD", "amount": "500", "collector": true}), nil), 400,
		"a collector's piece without a note")
	h.must(h.do("POST", "/api/exchange/reservations", "clerk", order("PROMO1", line("JPY", "500")), nil), 422, "unknown currency")
	yesterday := order("PROMO1", line("USD", "500"))
	yesterday["pickup"] = now.Add(-time.Hour).Format(time.RFC3339)
	h.must(h.do("POST", "/api/exchange/reservations", "clerk", yesterday, nil), 422, "pickup in the past")
	h.must(h.do("POST", "/api/exchange/reservations", "clerk", order("promo1", line("USD", "503"), line("GBP", "300")), &res), 201, "reservation")
	if !strings.HasPrefix(res.Reference, "RES-") || res.Status != "registered" || res.Total != "820.20" || res.Collaborator != h.partner.String() ||
		res.PromotionCode != "promo1" || res.ExpiresAt != pickup.Add(4*time.Hour).Format(time.RFC3339) || res.Facility != office || len(res.Lines) != 2 ||
		res.Lines[0].Requested != "503.00" || res.Lines[0].Delivered != "500.00" || res.Lines[0].OfferedRate != "0.938400" || res.Lines[0].Eur != "469.20" ||
		res.Lines[1].Eur != "351.00" || res.Lines[1].MarginValue != "0" {
		t.Fatalf("reservation: %+v", res)
	}
	// A new rate does not touch what is reserved.
	h.must(h.do("POST", "/api/exchange/rates", "pricer", map[string]any{"company": acme, "code": "USD", "rate": "0.96", "on": today.AddDays(1).String()}, nil), 422, "a rate for tomorrow")
	h.must(h.do("POST", "/api/exchange/rates", "pricer", map[string]any{"company": acme, "code": "USD", "rate": "0.90", "on": today.AddDays(-1).String()}, nil), 422, "an older rate")
	h.must(h.do("POST", "/api/exchange/rates", "pricer", rate("USD", "0.95"), nil), 200, "new rate")

	path := func(r xapp.ReservationDTO, action string) string {
		return "/api/exchange/reservations/" + r.ID + "/" + action
	}
	to := func(s string) map[string]any { return map[string]any{"to": s} }
	h.must(h.do("POST", path(res, "advance"), "clerk", to("email-verified"), nil), 403, "reserving is not moving")
	h.must(h.do("POST", path(res, "advance"), "outsider", to("email-verified"), nil), 404, "outsider")
	h.must(h.do("POST", path(res, "advance"), "desk", to("sent"), nil), 400, "status")
	h.must(h.do("POST", path(res, "advance"), "desk", to("completed"), nil), 422, "collected before being verified")
	h.must(h.do("POST", path(res, "advance"), "desk", to("email-verified"), &res), 200, "email verified")
	version := res.Version
	h.must(h.do("POST", path(res, "advance"), "desk", to("email-verified"), &res), 200, "again")
	if res.Version != version || res.Status != "email-verified" {
		t.Fatalf("the same status again changes nothing: %+v", res)
	}
	h.must(h.do("POST", path(res, "advance"), "desk", to("notified"), nil), 200, "notified")
	h.must(h.do("POST", path(res, "advance"), "desk", to("completed"), &res), 200, "collected")
	if res.Status != "completed" || len(res.History) != 4 || res.Lines[0].OfferedRate != "0.938400" || res.Total != "820.20" {
		t.Fatalf("collected: %+v", res)
	}
	h.must(h.do("POST", path(res, "cancel"), "desk", nil, nil), 422, "cancel what was collected")

	// Codes optional again: one reservation cancelled, one nobody collects.
	h.must(h.do("PUT", "/api/exchange/settings", "pricer", settings(2, "optional"), nil), 200, "codes optional")
	h.must(h.do("POST", "/api/exchange/reservations", "clerk", order("", line("USD", "100")), &cancelled), 201, "second")
	h.must(h.do("POST", "/api/exchange/reservations", "clerk", order("", line("USD", "200")), &late), 201, "third")
	if cancelled.Total != "96.90" || cancelled.Lines[0].BaseRate != "0.950000" || cancelled.Collaborator != "" {
		t.Fatalf("second, at the new rate: %+v", cancelled)
	}
	h.must(h.do("POST", path(cancelled, "cancel"), "clerk", map[string]any{"reason": "x"}, nil), 403, "reserving is not cancelling")
	h.must(h.do("POST", path(cancelled, "cancel"), "desk", map[string]any{"reason": "El cliente desiste"}, &cancelled), 200, "cancel")
	version = cancelled.Version
	h.must(h.do("POST", path(cancelled, "cancel"), "desk", nil, &cancelled), 200, "cancel again")
	if cancelled.Status != "cancelled" || cancelled.Reason != "El cliente desiste" || cancelled.Version != version {
		t.Fatalf("cancelled: %+v", cancelled)
	}
	h.must(h.do("POST", path(cancelled, "advance"), "desk", to("email-verified"), nil), 422, "cancelled does not move")
	// Five hours after the pickup nobody came: the job closes it (on a clock moved forward).
	func() {
		defer fw.SetClock(fake.New(pickup.Add(5 * time.Hour)))()
		expired, err := h.exg.Service.ExpireDue.Handle(h.adminCtx, xapp.ExpireDue{Company: acme})
		if err != nil || expired.Expired != 1 || !slices.Equal(expired.References, []string{late.Reference}) {
			t.Fatalf("expire: %+v %v", expired, err)
		}
		if again, err := h.exg.Service.ExpireDue.Handle(h.adminCtx, xapp.ExpireDue{Company: acme}); err != nil || again.Expired != 0 {
			t.Fatalf("expire again: %+v %v", again, err)
		}
	}()
	h.must(h.do("POST", "/api/exchange/reservations/expire-due", "clerk", map[string]any{"company": acme}, nil), 403, "reserving is not expiring")
	h.must(h.do("POST", path(late, "advance"), "desk", to("email-verified"), nil), 422, "expired does not move")

	var got xapp.ReservationDTO
	h.must(h.do("GET", "/api/exchange/reservations/by-reference/"+strings.ToLower(late.Reference), "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/exchange/reservations/by-reference/RES-2000-XXXXXXXXXXX", "viewer", nil, nil), 404, "unknown reference")
	h.must(h.do("GET", "/api/exchange/reservations/by-reference/"+strings.ToLower(late.Reference), "viewer", nil, &got), 200, "by reference")
	if got.ID != late.ID || got.Status != "expired" || len(got.History) != 2 || got.Pickup != pickup.Format(time.RFC3339) {
		t.Fatalf("by reference: %+v", got)
	}
	h.must(h.do("GET", "/api/exchange/reservations/"+res.ID, "viewer", nil, &got), 200, "get")
	if got.History[3].Status != "completed" || got.Lines[1].Currency != "GBP" || got.Channel != "web" {
		t.Fatalf("get: %+v", got)
	}
	count := func(query string, n int) {
		t.Helper()
		var p fw.Page[xapp.ReservationDTO]
		h.must(h.do("GET", "/api/exchange/reservations?company="+acme+"&"+query, "viewer", nil, &p), 200, query)
		if len(p.Items) != n {
			t.Fatalf("%s: %d reservations, want %d", query, len(p.Items), n)
		}
	}
	count("", 3)
	count("status=completed", 1)
	count("currency=gbp", 1)
	count("customer="+customer, 3)
	count("facility="+office, 3)
	count("createdFrom="+today.String()+"&createdTo="+today.String(), 3)
	count("pickupTo="+today.String(), 0)
	h.must(h.do("GET", "/api/exchange/reservations?status=lost", "viewer", nil, nil), 400, "status")
	var page fw.Page[xapp.ReservationDTO]
	h.must(h.do("GET", "/api/exchange/reservations", "outsider", nil, &page), 200, "outsider's reservations")
	if len(page.Items) != 0 {
		t.Fatalf("outsider: %+v", page.Items)
	}

	// The dashboard counts what is alive or collected.
	var dash xapp.DashboardDTO
	period := "&from=" + today.AddDays(-1).String() + "&to=" + today.AddDays(1).String()
	h.must(h.do("GET", "/api/exchange/dashboard?company="+acme, "viewer", nil, nil), 400, "period")
	h.must(h.do("GET", "/api/exchange/dashboard?company="+acme+period, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/exchange/dashboard?company="+acme+period, "viewer", nil, &dash), 200, "dashboard")
	if dash.Reservations != 1 || dash.Eur != "820.20" || dash.ByStatus["completed"] != 1 || dash.ByStatus["cancelled"] != 1 || dash.ByStatus["expired"] != 1 ||
		len(dash.Daily) != 1 || dash.Daily[0].Date != today.String() || len(dash.ByCurrency) != 2 || dash.ByCurrency[0].Currency != "USD" ||
		dash.ByCurrency[0].Amount != "500.00" || dash.ByCurrency[1].Eur != "351.00" || len(dash.ByFacility) != 1 || dash.ByFacility[0].Facility != office {
		t.Fatalf("dashboard: %+v", dash)
	}

	// 100 × 0.95 × 1.02 = 96.90 above. Three registrations, three steps of the first, a cancellation and an expiry.
	if n, err := h.exg.Relay(inprocess.NewBroker()).RelayOnce(context.Background()); err != nil || n != 8 {
		t.Fatalf("published: %d %v", n, err)
	}
}

func TestExchange_QuotesAndReserves_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{xinfra.Migrations()})
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

func TestExchange_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/exchange/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
