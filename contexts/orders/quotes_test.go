package orders_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	odomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	oinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
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
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// priceBook plays Products: what each product is and costs today.
type priceBook struct {
	mu    sync.Mutex
	items map[odomain.ProductID]odomain.Item
}

func (b *priceBook) Price(_ context.Context, _ odomain.OrganizationID, p odomain.ProductID, _ odomain.PriceListID, _ vocab.Decimal, _ vocab.Date) (odomain.Item, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it, ok := b.items[p]
	return it, ok, nil
}

func (b *priceBook) set(p odomain.ProductID, it odomain.Item) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items[p] = it
}

type quoteHost struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	ord      *orders.Module
	book     *priceBook
	adminCtx context.Context
}

func composeQuotes(t *testing.T) *quoteHost {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	book := &priceBook{items: map[odomain.ProductID]odomain.Item{}}
	om := orders.Compose(sw, book, nil)
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("quotes-test")})
	dir := authorization.NewMemoryDirectory()
	h := &quoteHost{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, ord: om, book: book}
	users := map[string][]authz.Permission{
		"seller":   {oapp.PermQuoteRead, oapp.PermQuoteUpdate},
		"manager":  {oapp.PermQuoteRead, oapp.PermQuoteUpdate, oapp.PermQuoteSend, oapp.PermQuoteResolve, oapp.PermOrderRead, oapp.PermOrderConfirm, oapp.PermTermsUpdate},
		"viewer":   {oapp.PermQuoteRead},
		"outsider": oapp.Permissions(),
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
	om.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	return h
}

func (h *quoteHost) grant(user, org string) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(org), Level: authz.Full})
	h.dir.Put(h.ids[user], s)
}

// do sends a request and decodes the answer into out, emptied first.
func (h *quoteHost) do(method, path, user string, body, out any) int {
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
		case *oapp.QuoteDTO:
			*o = oapp.QuoteDTO{}
		case *oapp.AcceptedQuoteDTO:
			*o = oapp.AcceptedQuoteDTO{}
		}
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			h.t.Fatal(err)
		}
	}
	return res.StatusCode
}

func (h *quoteHost) must(got, want int, what string) {
	h.t.Helper()
	if got != want {
		h.t.Fatalf("%s: status %d, want %d", what, got, want)
	}
}

func (h *quoteHost) scenario() {
	t := h.t
	acme, globex, customer, other, warehouse := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
	for _, u := range []string{"seller", "manager", "viewer"} {
		h.grant(u, acme)
	}
	h.grant("outsider", globex)
	company := odomain.OrganizationID{UUID: fw.MustParseUUID(acme)}
	dec := func(s string) vocab.Decimal { d, _ := vocab.ParseDecimal(s); return d }
	widget, service, blocked, untaxed := odomain.ProductID{UUID: fw.NewUUID()}, odomain.ProductID{UUID: fw.NewUUID()}, odomain.ProductID{UUID: fw.NewUUID()},
		odomain.ProductID{UUID: fw.NewUUID()}
	h.book.set(widget, odomain.Item{Company: company, SKU: "W-1", Name: "Tornillo", UoM: "ud", TaxCode: "G", Stocked: true, Sellable: true,
		UnitPrice: dec("10"), Discount: dec("10")})
	h.book.set(service, odomain.Item{Company: company, SKU: "S-1", Name: "Instalación", UoM: "h", TaxCode: "G", Sellable: true, UnitPrice: dec("100"), Discount: dec("0")})
	h.book.set(blocked, odomain.Item{Company: company, SKU: "B-1", Name: "Retirado", UoM: "ud", TaxCode: "G", UnitPrice: dec("1"), Discount: dec("0")})
	h.book.set(untaxed, odomain.Item{Company: company, SKU: "U-1", Name: "Sin IVA", UoM: "ud", Sellable: true, UnitPrice: dec("1"), Discount: dec("0")})
	today := vocab.DateOf(fw.Now())
	year := today.Year()
	path := func(q oapp.QuoteDTO, action string) string { return "/api/orders/quotes/" + q.ID + "/" + action }
	line := func(p odomain.ProductID, n string) map[string]any {
		return map[string]any{"product": p.String(), "quantity": n}
	}

	// The customer has a discount of 5 %: a quote takes it, as an order does.
	h.must(h.do("PUT", "/api/orders/terms", "manager", map[string]any{"company": acme, "customer": customer, "discount": "5"}, nil), 200, "terms")

	// A draft.
	draft := map[string]any{"company": acme, "customer": customer, "reference": "Obra 12", "notes": "Entrega en obra"}
	var q oapp.QuoteDTO
	h.must(h.do("POST", "/api/orders/quotes", "viewer", draft, nil), 403, "viewer")
	h.must(h.do("POST", "/api/orders/quotes", "outsider", draft, nil), 404, "outsider")
	h.must(h.do("POST", "/api/orders/quotes", "seller", map[string]any{"company": acme, "customer": customer, "date": today.String(),
		"validUntil": today.AddDays(-1).String()}, nil), 400, "valid until yesterday")
	h.must(h.do("POST", "/api/orders/quotes", "seller", draft, &q), 201, "draft")
	if q.Status != "draft" || q.Number != "" || q.Date != today.String() || q.ValidUntil != today.AddDays(30).String() || q.CustomerDiscount != "5.00" ||
		q.Total != "0.00" || q.Version != 1 || q.Reference != "Obra 12" {
		t.Fatalf("draft: %+v", q)
	}

	// Lines, priced as an order prices them: 10 − 10 % − 5 % = 8.55.
	h.must(h.do("POST", path(q, "lines"), "viewer", line(widget, "3"), nil), 403, "viewer")
	h.must(h.do("POST", path(q, "lines"), "outsider", line(widget, "3"), nil), 404, "outsider")
	h.must(h.do("POST", path(q, "lines"), "seller", line(widget, "0"), nil), 400, "nothing")
	h.must(h.do("POST", path(q, "lines"), "seller", line(blocked, "1"), nil), 422, "not for sale")
	h.must(h.do("POST", path(q, "lines"), "seller", line(untaxed, "1"), nil), 422, "no tax code")
	h.must(h.do("POST", path(q, "lines"), "seller", line(odomain.ProductID{UUID: fw.NewUUID()}, "1"), nil), 422, "unknown product")
	h.must(h.do("POST", path(q, "lines"), "seller", line(widget, "3"), &q), 200, "widgets")
	h.must(h.do("POST", path(q, "lines"), "seller", line(service, "1"), &q), 200, "one hour")
	h.must(h.do("POST", path(q, "lines/remove"), "seller", map[string]any{"line": 9}, nil), 404, "no such line")
	h.must(h.do("POST", path(q, "lines/remove"), "seller", map[string]any{"line": 2}, &q), 200, "remove")
	h.must(h.do("POST", path(q, "lines"), "seller", line(service, "2"), &q), 200, "two hours")
	if len(q.Lines) != 2 || q.Lines[0].NetPrice != "8.5500" || q.Lines[0].Amount != "25.65" || q.Lines[0].UnitPrice != "10.0000" || q.Lines[0].Discount != "10.00" ||
		!q.Lines[0].Stocked || q.Lines[1].No != 2 || q.Lines[1].NetPrice != "95.0000" || q.Lines[1].Amount != "190.00" || q.Total != "215.65" {
		t.Fatalf("lines: %+v", q)
	}

	// What a draft says of itself changes; what is left out is emptied.
	until := today.AddDays(10).String()
	h.must(h.do("PUT", "/api/orders/quotes/"+q.ID, "seller", map[string]any{"validUntil": today.AddDays(-3).String()}, nil), 400, "validity in the past")
	h.must(h.do("PUT", "/api/orders/quotes/"+q.ID, "seller", map[string]any{"validUntil": until, "reference": "Obra 12-B"}, &q), 200, "change")
	if q.ValidUntil != until || q.Reference != "Obra 12-B" || q.Notes != "" || q.Total != "215.65" {
		t.Fatalf("changed: %+v", q)
	}

	// Sending commits the company: a number, and nothing changes any more.
	h.must(h.do("POST", path(q, "accept"), "manager", nil, nil), 422, "a draft is not accepted")
	h.must(h.do("POST", path(q, "send"), "seller", nil, nil), 403, "preparing is not sending")
	h.must(h.do("POST", path(q, "send"), "manager", nil, &q), 200, "send")
	first := fmt.Sprintf("PRE-%d-000001", year)
	if q.Status != "sent" || q.Number != first {
		t.Fatalf("sent: %+v", q)
	}
	h.must(h.do("POST", path(q, "send"), "manager", nil, nil), 422, "sent twice")
	h.must(h.do("POST", path(q, "lines"), "seller", line(widget, "1"), nil), 422, "a sent quote does not change")
	h.must(h.do("POST", path(q, "lines/remove"), "seller", map[string]any{"line": 1}, nil), 422, "nor loses lines")
	h.must(h.do("PUT", "/api/orders/quotes/"+q.ID, "seller", map[string]any{"notes": "x"}, nil), 422, "nor its notes")

	// Prices go up; the customer accepts: the order is the offer, at the prices offered.
	h.book.set(widget, odomain.Item{Company: company, SKU: "W-1", Name: "Tornillo", UoM: "ud", TaxCode: "G", Stocked: true, Sellable: true,
		UnitPrice: dec("12"), Discount: dec("0")})
	var acc oapp.AcceptedQuoteDTO
	h.must(h.do("POST", path(q, "accept"), "seller", nil, nil), 403, "preparing is not accepting")
	h.must(h.do("POST", path(q, "accept"), "outsider", nil, nil), 404, "outsider")
	h.must(h.do("POST", path(q, "accept"), "manager", map[string]any{"warehouse": "nope"}, nil), 400, "warehouse")
	h.must(h.do("POST", path(q, "accept"), "manager", map[string]any{"warehouse": warehouse}, &acc), 200, "accept")
	o := acc.Order
	if acc.Quote.Status != "accepted" || acc.Quote.Order != o.ID || o.Status != "draft" || o.Number != "" || o.Customer != customer || o.Warehouse != warehouse ||
		o.CustomerDiscount != "5.00" || o.Reference != "Obra 12-B" || o.Date != today.String() || o.Total != "215.65" || len(o.Lines) != 2 ||
		o.Lines[0].NetPrice != "8.5500" || o.Lines[0].SKU != "W-1" || !o.Lines[0].Stocked || o.Lines[1].Amount != "190.00" {
		t.Fatalf("accepted: %+v", acc)
	}
	h.must(h.do("POST", path(q, "accept"), "manager", nil, nil), 422, "accepted twice")
	h.must(h.do("POST", path(q, "reject"), "manager", nil, nil), 422, "accepted is not rejected")
	h.must(h.do("POST", path(q, "withdraw"), "seller", nil, nil), 422, "nor withdrawn")
	// The order is an order like any other.
	var order oapp.OrderDTO
	h.must(h.do("POST", "/api/orders/orders/"+o.ID+"/confirm", "manager", nil, &order), 200, "confirm the order")
	if order.Status != "confirmed" || order.Number != fmt.Sprintf("PED-%d-000001", year) || order.Total != "215.65" {
		t.Fatalf("order: %+v", order)
	}

	// The customer says no.
	send := func(body map[string]any, lines ...map[string]any) oapp.QuoteDTO {
		t.Helper()
		var x oapp.QuoteDTO
		h.must(h.do("POST", "/api/orders/quotes", "seller", body, &x), 201, "draft")
		for _, l := range lines {
			h.must(h.do("POST", path(x, "lines"), "seller", l, &x), 200, "line")
		}
		h.must(h.do("POST", path(x, "send"), "manager", nil, &x), 200, "send")
		return x
	}
	no := send(map[string]any{"company": acme, "customer": other}, line(widget, "1"))
	if no.Number != fmt.Sprintf("PRE-%d-000002", year) || no.CustomerDiscount != "0.00" || no.Total != "12.00" {
		t.Fatalf("second: %+v", no)
	}
	h.must(h.do("POST", path(no, "reject"), "seller", nil, nil), 403, "preparing is not resolving")
	h.must(h.do("POST", path(no, "reject"), "manager", map[string]any{"reason": strings.Repeat("x", 201)}, nil), 422, "long reason")
	h.must(h.do("POST", path(no, "reject"), "manager", map[string]any{"reason": "Demasiado caro"}, &no), 200, "reject")
	if no.Status != "rejected" || no.Reason != "Demasiado caro" || no.Order != "" {
		t.Fatalf("rejected: %+v", no)
	}
	h.must(h.do("POST", path(no, "reject"), "manager", nil, nil), 422, "rejected twice")
	h.must(h.do("POST", path(no, "accept"), "manager", nil, nil), 422, "rejected is not accepted")

	// A draft nobody wants, and an offer taken back.
	var gone oapp.QuoteDTO
	h.must(h.do("POST", "/api/orders/quotes", "seller", draft, &gone), 201, "draft")
	h.must(h.do("POST", path(gone, "send"), "manager", nil, nil), 422, "nothing to offer")
	h.must(h.do("POST", path(gone, "withdraw"), "viewer", nil, nil), 403, "viewer")
	h.must(h.do("POST", path(gone, "withdraw"), "seller", nil, &gone), 200, "withdraw a draft")
	if gone.Status != "withdrawn" || gone.Number != "" {
		t.Fatalf("withdrawn draft: %+v", gone)
	}
	back := send(draft, line(service, "1"))
	h.must(h.do("POST", path(back, "withdraw"), "seller", map[string]any{"reason": "Error en el precio"}, &back), 200, "withdraw")
	if back.Status != "withdrawn" || back.Number != fmt.Sprintf("PRE-%d-000003", year) || back.Reason != "Error en el precio" {
		t.Fatalf("withdrawn: %+v", back)
	}

	// An offer holds until its last day, that day included.
	short := send(map[string]any{"company": acme, "customer": customer, "validUntil": today.String()}, line(service, "1"))
	late := send(map[string]any{"company": acme, "customer": customer, "validUntil": today.AddDays(1).String()}, line(service, "3"))
	var stale oapp.QuoteDTO
	h.must(h.do("POST", "/api/orders/quotes", "seller", map[string]any{"company": acme, "customer": customer, "validUntil": today.String()}, &stale), 201, "draft")
	h.must(h.do("POST", path(stale, "lines"), "seller", line(service, "1"), &stale), 200, "line")
	h.must(h.do("POST", "/api/orders/quotes/expire-due", "seller", map[string]any{"company": acme}, nil), 403, "preparing is not expiring")
	h.must(h.do("POST", "/api/orders/quotes/expire-due", "outsider", map[string]any{"company": acme}, nil), 404, "outsider")
	var expired oapp.ExpiredQuotesDTO
	h.must(h.do("POST", "/api/orders/quotes/expire-due", "manager", map[string]any{"company": acme}, &expired), 200, "nothing is due today")
	if len(expired.Numbers) != 0 {
		t.Fatalf("expired today: %+v", expired)
	}
	svc := h.ord.Service
	shortID, _ := odomain.ParseQuoteID(short.ID)
	staleID, _ := odomain.ParseQuoteID(stale.ID)
	func() {
		defer fw.SetClock(fake.New(fw.Now().Add(24 * time.Hour)))()
		var rv *fw.RuleViolationError
		if _, err := svc.AcceptQuote.Handle(h.adminCtx, oapp.AcceptQuote{ID: shortID}); !errors.As(err, &rv) || rv.Code != "orders.quote_expired" {
			t.Fatalf("accepting late: %v", err)
		}
		// A draft whose validity went by is not sent, and takes no number.
		if _, err := svc.SendQuote.Handle(h.adminCtx, oapp.SendQuote{ID: staleID}); !errors.As(err, &rv) || rv.Code != "orders.quote_expired" {
			t.Fatalf("sending late: %v", err)
		}
		out, err := svc.ExpireQuotes.Handle(h.adminCtx, oapp.ExpireQuotes{Company: acme})
		if err != nil || !slices.Equal(out.Numbers, []string{short.Number}) {
			t.Fatalf("expire: %+v %v", out, err)
		}
		if again, err := svc.ExpireQuotes.Handle(h.adminCtx, oapp.ExpireQuotes{Company: acme}); err != nil || len(again.Numbers) != 0 {
			t.Fatalf("expire again: %+v %v", again, err)
		}
	}()
	h.must(h.do("GET", "/api/orders/quotes/"+short.ID, "viewer", nil, &short), 200, "expired")
	if short.Status != "expired" || short.Order != "" {
		t.Fatalf("expired: %+v", short)
	}
	h.must(h.do("POST", path(short, "accept"), "manager", nil, nil), 422, "expired is not accepted")
	h.must(h.do("PUT", "/api/orders/quotes/"+stale.ID, "seller", map[string]any{"validUntil": today.AddDays(5).String()}, &stale), 200, "a new validity")
	h.must(h.do("POST", path(stale, "send"), "manager", nil, &stale), 200, "send")
	if stale.Number != fmt.Sprintf("PRE-%d-000006", year) {
		t.Fatalf("numbers run without gaps: %+v", stale)
	}

	// Reading and searching.
	var got oapp.QuoteDTO
	h.must(h.do("GET", "/api/orders/quotes/"+q.ID, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/orders/quotes/nope", "viewer", nil, nil), 400, "bad id")
	h.must(h.do("GET", "/api/orders/quotes/"+q.ID, "viewer", nil, &got), 200, "get")
	if got.Status != "accepted" || got.Order != o.ID || len(got.Lines) != 2 || got.Lines[1].Description != "Instalación" || got.Total != "215.65" {
		t.Fatalf("got: %+v", got)
	}
	var page fw.Page[oapp.QuoteDTO]
	h.must(h.do("GET", "/api/orders/quotes?company="+acme, "viewer", nil, &page), 200, "all")
	if page.Total != 7 || page.Items[0].Number != stale.Number || page.Items[5].Number != first || page.Items[6].Number != "" {
		t.Fatalf("all, the latest number first: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/orders/quotes?status=sent", "viewer", nil, &page), 200, "sent")
	if page.Total != 2 || page.Items[0].ID != stale.ID || page.Items[1].ID != late.ID {
		t.Fatalf("sent: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/orders/quotes?customer="+other, "viewer", nil, &page), 200, "by customer")
	if page.Total != 1 || page.Items[0].ID != no.ID {
		t.Fatalf("by customer: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/orders/quotes?status=odd", "viewer", nil, nil), 400, "status")
	h.must(h.do("GET", "/api/orders/quotes", "outsider", nil, &page), 200, "outsider")
	if page.Total != 0 {
		t.Fatalf("outsider: %+v", page.Items)
	}

	// Six sent; accepted, rejected, withdrawn and expired told once each (the draft withdrawn
	// tells nobody); and the order confirmed, with the stock it asks for.
	if n, err := h.ord.Relay(inprocess.NewBroker()).RelayOnce(context.Background()); err != nil || n != 12 {
		t.Fatalf("published: %d %v", n, err)
	}
}

func TestQuotes_OfferAcceptRejectExpire_MemoryThenSQLite(t *testing.T) {
	h := composeQuotes(t)
	h.scenario()

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{oinfra.Migrations()})
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
