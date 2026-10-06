package shipments_test

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

	"github.com/jhermoso/karpo-fw-go/contexts/shipments"
	sapp "github.com/jhermoso/karpo-fw-go/contexts/shipments/application"
	"github.com/jhermoso/karpo-fw-go/contexts/shipments/domain"
	sinfra "github.com/jhermoso/karpo-fw-go/contexts/shipments/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
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

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func TestShipment_Rules(t *testing.T) {
	company, customer := domain.OrganizationID{UUID: fw.NewUUID()}, domain.PartyID{UUID: fw.NewUUID()}
	carrier := domain.CarrierID{UUID: fw.NewUUID()}
	day := func(d int) vocab.Date { return vocab.MustDate(2026, 5, d) }
	lines := []domain.Line{{Description: "Tornillos", Quantity: vocab.MustDecimal("100"), UoM: "ud"}}
	schedule := func(p domain.Plan, ls []domain.Line) (*domain.Shipment, error) {
		return domain.ScheduleShipment(domain.NewShipmentID(), company, customer, domain.Source{}, p, ls, day(6))
	}
	for i, p := range []domain.Plan{
		{Method: "bike"},
		{Method: domain.Pickup, Carrier: carrier},
		{Packages: -1},
		{WeightKg: vocab.MustDecimal("1.2345")},
		{Cost: vocab.MustDecimal("1.001")},
		{EstimatedShip: day(8), EstimatedArrival: day(7)},
	} {
		if _, err := schedule(p, lines); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("plan %d: %v", i, err)
		}
	}
	if _, err := schedule(domain.Plan{}, nil); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("no lines: %v", err)
	}
	if _, err := schedule(domain.Plan{}, []domain.Line{{Description: "x", Quantity: vocab.MustDecimal("0")}}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("quantity: %v", err)
	}

	s, err := schedule(domain.Plan{}, lines)
	if err != nil || s.State().Status != domain.Scheduled || s.State().Lines[0].No != 1 {
		t.Fatalf("scheduled: %v", err)
	}
	if err := s.Dispatch(day(7), ""); !isViolation(err, "shipments.incomplete") {
		t.Fatalf("incomplete: %v", err)
	}
	if err := s.Deliver(day(7), ""); !isViolation(err, "shipments.transition") {
		t.Fatalf("deliver before dispatch: %v", err)
	}
	plan := domain.Plan{Method: domain.Courier, Carrier: carrier, Destination: "Calle Mayor 1, Madrid", Packages: 2, WeightKg: vocab.MustDecimal("12.5")}
	if err := s.Change(plan); err != nil {
		t.Fatal(err)
	}
	if err := s.Dispatch(day(5), "SEUR"); !isViolation(err, "shipments.date") {
		t.Fatalf("before it was planned: %v", err)
	}
	if err := s.Dispatch(day(7), "SEUR"); err != nil || s.State().Dispatched != day(7) {
		t.Fatalf("dispatch: %v", err)
	}
	moved := plan
	moved.Destination = "Otra calle"
	if err := s.Change(moved); !isViolation(err, "shipments.in_transit") {
		t.Fatalf("destination in transit: %v", err)
	}
	tracked := plan
	tracked.Tracking, tracked.Cost = "AB123", vocab.MustDecimal("18.90")
	if err := s.Change(tracked); err != nil || s.State().Tracking != "AB123" {
		t.Fatalf("tracking in transit: %v", err)
	}
	if err := s.Cancel(day(7), "tarde"); !isViolation(err, "shipments.transition") {
		t.Fatalf("cancel in transit: %v", err)
	}
	if err := s.Return(day(8), " "); !isViolation(err, "shipments.reason") {
		t.Fatalf("return without reason: %v", err)
	}
	if err := s.Deliver(day(8), "Portería"); err != nil || s.State().Closed != day(8) || len(s.State().History) != 3 {
		t.Fatalf("deliver: %v", err)
	}
	if err := s.Change(plan); !isViolation(err, "shipments.closed") {
		t.Fatalf("closed: %v", err)
	}

	c, err := domain.ReconstituteCarrier(domain.NewCarrierID(), domain.CarrierState{Company: company, Code: "seur", Name: "SEUR",
		TrackingURL: "https://seur.example/track?n={tracking}"})
	if err != nil || c.State().Code != "SEUR" || c.TrackingLink("AB 123/9") != "https://seur.example/track?n=AB%20123%2F9" || c.TrackingLink("") != "" {
		t.Fatalf("carrier: %v %s", err, c.TrackingLink("AB 123/9"))
	}
	for _, url := range []string{"http://seur.example/{tracking}", "https://seur.example/track", "https://x/{tracking}/{tracking}"} {
		if err := c.Change("SEUR", domain.PartyID{}, url, false); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("link %s: %v", url, err)
		}
	}
}

// host composes Shipments on a hot-swappable backend; the test plays Orders by sending its
// delivery notes to the broker.
type host struct {
	t      *testing.T
	srv    *httptest.Server
	sw     *hotswap.Switch
	dir    *authorization.MemoryDirectory
	ids    map[string]fw.UUID
	tokens map[string]string
	shp    *shipments.Module
	broker *inprocess.Broker
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	sm := shipments.Compose(sw)
	broker := inprocess.NewBroker()
	broker.Subscribe("shipments", sm.Consumer)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("shipments-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, shp: sm, broker: broker}
	users := map[string][]authz.Permission{
		"planner":  {sapp.PermShipmentRead, sapp.PermShipmentUpdate, sapp.PermCarrierRead, sapp.PermCarrierUpdate},
		"dock":     {sapp.PermShipmentRead, sapp.PermShipmentDispatch},
		"viewer":   {sapp.PermShipmentRead, sapp.PermCarrierRead},
		"outsider": sapp.Permissions(),
	}
	for u, perms := range users {
		h.ids[u] = fw.NewUUID()
		dir.Put(h.ids[u], authz.Subject{Active: true, Permissions: perms})
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	mux := http.NewServeMux()
	sm.RegisterRoutes(mux)
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

// deliveryNote plays Orders: a delivery note reaches the broker.
func (h *host) deliveryNote(msg, id, company, customer, number string, lines []map[string]any) {
	h.t.Helper()
	raw, _ := json.Marshal(map[string]any{"deliveryId": id, "orderId": fw.NewUUID().String(), "company": company, "customer": customer, "number": number,
		"date": "2026-05-06", "total": "242.00", "lines": lines})
	if err := h.broker.Send(context.Background(), application.Envelope{ID: msg, Type: "orders.delivery-issued.v1", Source: "orders",
		OccurredAt: time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC), Data: raw}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *host) scenario(tag string) {
	t := h.t
	id := func() string { return fw.NewUUID().String() }
	acme, globex, customer, delivery := id(), id(), id(), id()
	for _, u := range []string{"planner", "dock", "viewer"} {
		h.grant(u, acme)
	}
	h.grant("outsider", globex)

	// Carriers.
	seurBody := map[string]any{"company": acme, "code": "seur", "name": "SEUR", "trackingUrl": "https://seur.example/track?n={tracking}"}
	var seur, old sapp.CarrierDTO
	h.must(h.do("POST", "/api/carriers", "viewer", seurBody, nil), 403, "viewer")
	h.must(h.do("POST", "/api/carriers", "outsider", seurBody, nil), 404, "outsider")
	h.must(h.do("POST", "/api/carriers", "planner", map[string]any{"company": acme, "code": "X", "name": "X", "trackingUrl": "http://x/{tracking}"}, nil), 400, "link")
	h.must(h.do("POST", "/api/carriers", "planner", seurBody, &seur), 201, "carrier")
	h.must(h.do("POST", "/api/carriers", "planner", seurBody, nil), 422, "code used once")
	h.must(h.do("POST", "/api/carriers", "planner", map[string]any{"company": acme, "code": "OLD", "name": "Transportes Viejos"}, &old), 201, "old carrier")
	h.must(h.do("PUT", "/api/carriers/"+old.ID, "planner", map[string]any{"name": "Transportes Viejos", "blocked": true}, &old), 200, "block")
	var carriers []sapp.CarrierDTO
	h.must(h.do("GET", "/api/carriers?company="+acme, "viewer", nil, &carriers), 200, "carriers")
	if len(carriers) != 2 || carriers[0].Code != "OLD" || !carriers[0].Blocked || carriers[1].Code != "SEUR" {
		t.Fatalf("carriers: %+v", carriers)
	}

	// The delivery note of Orders plans the shipment of its goods, once; services are not shipped.
	goods := []map[string]any{{"line": 1, "sku": "TOR-01", "description": "Tornillos", "uom": "ud", "stocked": true, "quantity": "100"},
		{"line": 2, "sku": "MON-01", "description": "Montaje", "uom": "h", "stocked": false, "quantity": "2"},
		{"line": 3, "sku": "TUE-01", "description": "Tuercas", "uom": "ud", "stocked": true, "quantity": "50.5"}}
	h.deliveryNote(tag+"-1", delivery, acme, customer, "ALB-2026-000001", goods)
	h.deliveryNote(tag+"-1", delivery, acme, customer, "ALB-2026-000001", goods)
	h.deliveryNote(tag+"-1b", delivery, acme, customer, "ALB-2026-000001", goods)
	h.deliveryNote(tag+"-2", id(), acme, customer, "ALB-2026-000002", goods[1:2])
	var page fw.Page[sapp.ShipmentDTO]
	h.must(h.do("GET", "/api/shipments?company="+acme, "viewer", nil, &page), 200, "shipments")
	if len(page.Items) != 1 || page.Items[0].Status != "scheduled" || page.Items[0].SourceRef != "ALB-2026-000001" || page.Items[0].SourceID != delivery {
		t.Fatalf("planned from the delivery note: %+v", page.Items)
	}
	sid := page.Items[0].ID
	var shp sapp.ShipmentDTO
	h.must(h.do("GET", "/api/shipments/"+sid, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/shipments/"+sid, "viewer", nil, &shp), 200, "get")
	if len(shp.Lines) != 2 || shp.Lines[1].SKU != "TUE-01" || shp.Lines[1].Line != 2 || shp.Lines[1].Quantity != "50.5" || len(shp.History) != 1 ||
		shp.History[0].On != "2026-05-06" || shp.EstimatedShip != "2026-05-06" || shp.Customer != customer {
		t.Fatalf("shipment: %+v", shp)
	}

	// How it travels.
	move := func(to, on string) map[string]any { return map[string]any{"to": to, "on": on} }
	h.must(h.do("POST", "/api/shipments/"+sid+"/move", "planner", move("in-transit", "2026-05-07"), nil), 403, "preparing is not dispatching")
	h.must(h.do("POST", "/api/shipments/"+sid+"/move", "dock", move("lost", "2026-05-07"), nil), 400, "status")
	h.must(h.do("POST", "/api/shipments/"+sid+"/move", "dock", move("in-transit", "2026-05-07"), nil), 422, "nothing decided yet")
	plan := map[string]any{"method": "courier", "carrier": seur.ID, "recipient": "Talleres Vega, att. Marta", "destination": "Calle Mayor 1, 28013 Madrid",
		"packages": 2, "weightKg": "12.5", "cost": "18.90", "tracking": "AB 123/9", "estimatedShip": "2026-05-07", "estimatedArrival": "2026-05-08"}
	with := func(k string, v any) map[string]any {
		out := map[string]any{}
		for a, b := range plan {
			out[a] = b
		}
		out[k] = v
		return out
	}
	h.must(h.do("PUT", "/api/shipments/"+sid, "dock", plan, nil), 403, "dispatching is not preparing")
	h.must(h.do("PUT", "/api/shipments/"+sid, "planner", with("carrier", old.ID), nil), 422, "blocked carrier")
	h.must(h.do("PUT", "/api/shipments/"+sid, "planner", with("method", "pickup"), nil), 400, "a collection has no carrier")
	h.must(h.do("PUT", "/api/shipments/"+sid, "planner", plan, &shp), 200, "plan")
	if shp.CarrierName != "SEUR" || shp.TrackingLink != "https://seur.example/track?n=AB%20123%2F9" || shp.WeightKg != "12.500" || shp.Cost != "18.90" ||
		shp.Packages != 2 {
		t.Fatalf("plan: %+v", shp)
	}

	// Dispatch, tracking, delivery.
	h.must(h.do("POST", "/api/shipments/"+sid+"/move", "dock", move("in-transit", "2026-05-05"), nil), 422, "before it was planned")
	h.must(h.do("POST", "/api/shipments/"+sid+"/move", "dock", move("in-transit", "2026-05-07"), &shp), 200, "dispatch")
	h.must(h.do("PUT", "/api/shipments/"+sid, "planner", with("destination", "Otra calle"), nil), 422, "destination in transit")
	h.must(h.do("PUT", "/api/shipments/"+sid, "planner", with("tracking", "AB124"), &shp), 200, "tracking in transit")
	h.must(h.do("POST", "/api/shipments/"+sid+"/move", "dock", map[string]any{"to": "cancelled", "on": "2026-05-07", "reason": "tarde"}, nil), 422, "cancel in transit")
	h.must(h.do("POST", "/api/shipments/"+sid+"/move", "dock", move("delivered", "2026-05-06"), nil), 422, "delivered before leaving")
	h.must(h.do("POST", "/api/shipments/"+sid+"/move", "dock", map[string]any{"to": "delivered", "on": "2026-05-08", "receivedBy": "Marta"}, &shp), 200, "delivered")
	if shp.Status != "delivered" || shp.Dispatched != "2026-05-07" || shp.Closed != "2026-05-08" || shp.ReceivedBy != "Marta" || shp.Tracking != "AB124" ||
		len(shp.History) != 3 {
		t.Fatalf("delivered: %+v", shp)
	}
	h.must(h.do("POST", "/api/shipments/"+sid+"/move", "dock", move("delivered", "2026-05-09"), nil), 422, "delivered once")
	h.must(h.do("PUT", "/api/shipments/"+sid, "planner", plan, nil), 422, "closed")

	// Shipments planned by hand: one collected by the customer that comes back, one cancelled.
	byHand := map[string]any{"company": acme, "customer": customer, "method": "pickup", "packages": 1, "on": "2026-05-10",
		"lines": []map[string]any{{"description": "Muestras", "quantity": "3", "uom": "ud"}}}
	var samples, dropped sapp.ShipmentDTO
	h.must(h.do("POST", "/api/shipments", "dock", byHand, nil), 403, "dispatching is not preparing")
	h.must(h.do("POST", "/api/shipments", "planner", map[string]any{"company": acme, "customer": customer, "on": "2026-05-10"}, nil), 400, "no lines")
	h.must(h.do("POST", "/api/shipments", "planner", byHand, &samples), 201, "samples")
	h.must(h.do("POST", "/api/shipments", "planner", byHand, &dropped), 201, "another")
	h.must(h.do("POST", "/api/shipments/"+samples.ID+"/move", "dock", move("in-transit", "2026-05-11"), nil), 200, "collected")
	h.must(h.do("POST", "/api/shipments/"+samples.ID+"/move", "dock", move("returned", "2026-05-12"), nil), 422, "return without reason")
	h.must(h.do("POST", "/api/shipments/"+samples.ID+"/move", "dock", map[string]any{"to": "returned", "on": "2026-05-12", "reason": "No las quiere"}, &samples), 200, "returned")
	h.must(h.do("POST", "/api/shipments/"+dropped.ID+"/move", "dock", map[string]any{"to": "cancelled", "on": "2026-05-10", "reason": "Duplicado"}, &dropped), 200, "cancelled")
	if samples.Status != "returned" || samples.Reason != "No las quiere" || samples.SourceID != "" || dropped.Status != "cancelled" || dropped.Closed != "2026-05-10" {
		t.Fatalf("by hand: %+v / %+v", samples, dropped)
	}

	// Two dispatches, a delivery and a return leave for the other contexts.
	if n, err := h.shp.Relay(inprocess.NewBroker()).RelayOnce(context.Background()); err != nil || n != 4 {
		t.Fatalf("published: %d %v", n, err)
	}

	count := func(query string, n int) {
		t.Helper()
		var p fw.Page[sapp.ShipmentDTO]
		h.must(h.do("GET", "/api/shipments?company="+acme+"&"+query, "viewer", nil, &p), 200, query)
		if len(p.Items) != n {
			t.Fatalf("%s: %d shipments, want %d", query, len(p.Items), n)
		}
	}
	count("status=delivered", 1)
	count("tracking=ab1", 1)
	count("delivery="+delivery, 1)
	count("carrier="+seur.ID, 1)
	count("customer="+customer, 3)
	h.must(h.do("GET", "/api/shipments?status=lost", "viewer", nil, nil), 400, "status")
	h.must(h.do("GET", "/api/shipments", "outsider", nil, &page), 200, "outsider's shipments")
	if len(page.Items) != 0 {
		t.Fatalf("outsider: %+v", page.Items)
	}
}

func TestShipments_DispatchesDeliveryNotes_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{sinfra.Migrations()})
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

func TestShipments_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/shipments/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
