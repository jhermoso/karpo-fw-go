package parties_test

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
	"strings"
	"testing"
	"time"

	papp "github.com/jhermoso/karpo-fw-go/examples/parties/application"
	"github.com/jhermoso/karpo-fw-go/examples/parties/contracts"
	pdist "github.com/jhermoso/karpo-fw-go/examples/parties/distribution"
	"github.com/jhermoso/karpo-fw-go/examples/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	appoutbox "github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	logvanilla "github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	msgbroker "github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	metricsvanilla "github.com/jhermoso/karpo-fw-go/pkg/metrics/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
	tracevanilla "github.com/jhermoso/karpo-fw-go/pkg/trace/vanilla"
)

// observedEnv is Parties on SQLite with the whole telemetry on, as a service would compose it.
type observedEnv struct {
	t       *testing.T
	handler http.Handler
	raw     *sql.DB
	logs    *bytes.Buffer
	logger  log.Logger
	spans   *tracevanilla.Recorder
	tracer  trace.Tracer
	reg     *metricsvanilla.Registry
	outbox  application.OutboxStore
	integ   application.OutboxStore
}

func composeObserved(t *testing.T) *observedEnv {
	t.Helper()
	ctx := context.Background()
	e := &observedEnv{t: t, logs: &bytes.Buffer{}, spans: tracevanilla.NewRecorder(0), reg: metricsvanilla.NewRegistry()}
	e.logger = logvanilla.NewJSON(e.logs, log.LevelInfo).With("service", "parties")
	e.tracer = tracevanilla.New(tracevanilla.WithExporter(e.spans))

	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "parties.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	e.raw = raw
	db := sqlite.Open(raw, sqlrepo.WithName("parties"), sqlrepo.WithTelemetry(e.tracer, e.reg))
	if err := infrastructure.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	sw := hotswap.New(db)
	repo := hotswap.Repository(sw, infrastructure.RepositoryFactory)
	e.outbox = hotswap.Outbox(sw, infrastructure.OutboxFactory)
	e.integ = hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	recorder := appoutbox.Recorders(
		appoutbox.NewRecorder(e.outbox),
		papp.Publications(messaging.NewRecorder(contracts.Source, e.integ)),
	)
	svc := papp.NewService(repo, sw, recorder, memory.NewIdempotencyStore(), hotswap.AuditLog(sw, infrastructure.AuditLogFactory)).
		Observe(e.logger, e.tracer, e.reg)

	mux := http.NewServeMux()
	pdist.NewModule(svc).RegisterRoutes(mux)
	e.handler = distribution.Chain(mux,
		distribution.Recovery(e.logger),
		distribution.Correlation(),
		distribution.Observe(e.logger, e.tracer, e.reg, distribution.ObserveRoutes(mux)),
		distribution.TenantActorContext(),
	)
	e.reset() // forget what the migration left
	return e
}

func (e *observedEnv) reset() {
	e.logs.Reset()
	e.spans.Reset()
}

func (e *observedEnv) do(method, path string, body any, headers ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	for k, v := range asAna {
		req.Header.Set(k, v)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *observedEnv) register(name, taxID, correlation string) papp.PartyDTO {
	e.t.Helper()
	res := e.do("POST", "/parties", map[string]string{"type": "organization", "legalName": name, "taxId": taxID},
		distribution.CorrelationHeader, correlation)
	if res.Code != http.StatusCreated {
		e.t.Fatalf("register %s: %d %s", name, res.Code, res.Body.String())
	}
	var dto papp.PartyDTO
	_ = json.Unmarshal(res.Body.Bytes(), &dto)
	return dto
}

// lines returns the log lines with the given message.
func (e *observedEnv) lines(msg string) []map[string]any {
	e.t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(e.logs.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			e.t.Fatalf("the output must be one JSON object per line: %v\n%s", err, l)
		}
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

func one(t *testing.T, spans []tracevanilla.FinishedSpan, what string) tracevanilla.FinishedSpan {
	t.Helper()
	if len(spans) != 1 {
		t.Fatalf("expected one %s span, got %d", what, len(spans))
	}
	return spans[0]
}

// One request leaves one JSON line, one observation per layer and one trace that links
// HTTP -> use case -> transaction -> INSERT, all found by the same correlation id.
func TestObservability_ARequestLeavesLineMetricAndTrace(t *testing.T) {
	e := composeObserved(t)

	acme := e.register("Acme Observed", "B12345674", "flow-register-1")

	// The line.
	lines := e.lines(distribution.RequestLogMessage)
	if len(lines) != 1 || len(strings.Split(strings.TrimSpace(e.logs.String()), "\n")) != 1 {
		t.Fatalf("a request must leave exactly one line:\n%s", e.logs.String())
	}
	l := lines[0]
	if l["level"] != "INFO" || l["service"] != "parties" || l["method"] != "POST" || l["route"] != "/parties" || l["status"] != float64(201) {
		t.Fatalf("unexpected request line: %v", l)
	}
	if l["correlation_id"] != "flow-register-1" || l["actor"] != anaID.String() {
		t.Fatalf("the line must carry the correlation and the actor id: %v", l)
	}
	if _, ok := l["duration_ms"].(float64); !ok {
		t.Fatalf("the line must carry the duration: %v", l)
	}
	for _, personal := range []string{"Ana", "Acme Observed", "B12345674"} {
		if strings.Contains(e.logs.String(), personal) {
			t.Fatalf("nothing personal may reach the log (%q):\n%s", personal, e.logs.String())
		}
	}

	// The trace.
	server := one(t, e.spans.Named("POST /parties"), "server")
	useCase := one(t, e.spans.Named("application.RegisterParty"), "use case")
	tx := one(t, e.spans.Named("db transaction"), "transaction")
	if server.Kind != trace.KindServer || server.HasParent() {
		t.Fatalf("unexpected server span: %+v", server)
	}
	if useCase.ParentID != server.Context.SpanID || tx.ParentID != useCase.Context.SpanID {
		t.Fatal("the chain must be HTTP -> use case -> transaction")
	}
	inserts := e.spans.Named("db INSERT")
	if len(inserts) == 0 {
		t.Fatal("the registration must leave INSERT spans")
	}
	for _, s := range e.spans.Spans() {
		if s.Context.TraceID != server.Context.TraceID {
			t.Fatalf("span %q belongs to another trace", s.Name)
		}
		if strings.HasPrefix(s.Name, "db ") && s.Name != "db transaction" && s.ParentID != tx.Context.SpanID {
			t.Fatalf("statement %q must hang from the transaction", s.Name)
		}
		if dump := fmt.Sprint(s.Attributes); strings.Contains(dump, "Acme") || strings.Contains(dump, "B12345674") || strings.Contains(dump, "Ana") {
			t.Fatalf("span %q carries request data: %s", s.Name, dump)
		}
	}
	if l["trace_id"] != server.Context.TraceIDString() || server.Attr("correlation_id") != "flow-register-1" {
		t.Fatalf("line, span and correlation must match: %v / %v", l, server.Attributes)
	}
	if tx.Attr(sqlrepo.AttrTransaction) != "committed" || useCase.Attr("outcome") != "ok" {
		t.Fatalf("unexpected outcomes: %v / %v", tx.Attributes, useCase.Attributes)
	}

	// The metrics.
	if n := e.reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "POST", "route", "/parties", "status", "201"); n != 1 {
		t.Fatalf("expected one HTTP observation: %v", e.reg.SeriesLabels(metrics.HTTPServerRequestDuration))
	}
	if n := e.reg.HistogramCount(metrics.UseCaseDuration, "request", "application.RegisterParty", "outcome", "ok"); n != 1 {
		t.Fatalf("expected one use case observation: %v", e.reg.SeriesLabels(metrics.UseCaseDuration))
	}
	// The migration that prepared the database is measured too, hence "at least".
	if n := e.reg.HistogramCount(metrics.DBOperationDuration, "engine", "sqlite", "operation", "INSERT", "outcome", "ok"); int(n) < len(inserts) {
		t.Fatalf("expected at least %d INSERT observations, got %d", len(inserts), n)
	}

	// A second request continues the trace of its caller, and its route is the pattern.
	e.reset()
	res := e.do("GET", "/parties/"+acme.ID.String(), nil,
		distribution.CorrelationHeader, "flow-get-1", trace.TraceParentHeader, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if res.Code != http.StatusOK {
		t.Fatalf("get: %d %s", res.Code, res.Body.String())
	}
	l = e.lines(distribution.RequestLogMessage)[0]
	if l["route"] != "/parties/{id}" || l["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || l["correlation_id"] != "flow-get-1" {
		t.Fatalf("unexpected line: %v", l)
	}
	if strings.Contains(e.logs.String(), acme.ID.String()) {
		t.Fatalf("the identifier in the path must not be logged:\n%s", e.logs.String())
	}
	if n := e.reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "GET", "route", "/parties/{id}", "status", "200"); n != 1 {
		t.Fatalf("the metric must use the pattern: %v", e.reg.SeriesLabels(metrics.HTTPServerRequestDuration))
	}

	// The exposition a collector scrapes.
	var text bytes.Buffer
	if err := e.reg.WriteText(&text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`http_server_request_duration_seconds_count{method="POST",route="/parties",status="201"} 1`,
		`karpo_usecase_duration_seconds_count{request="application.RegisterParty",outcome="ok"} 1`,
		`db_client_connections{engine="sqlite",db="parties",state="open"} 1`,
	} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("the exposition lacks %q", want)
		}
	}
}

// A 500 leaves its cause with the correlation the caller received, and the caller never sees it.
func TestObservability_A500LeavesItsCause(t *testing.T) {
	e := composeObserved(t)
	acme := e.register("Acme Observed", "B12345674", "flow-ok")
	e.reset()
	_ = e.raw.Close() // the database goes away

	res := e.do("GET", "/parties/"+acme.ID.String(), nil, distribution.CorrelationHeader, "flow-500")
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d %s", res.Code, res.Body.String())
	}
	var problem distribution.ProblemDetails
	if err := json.Unmarshal(res.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.CorrelationID != "flow-500" || strings.Contains(res.Body.String(), "closed") {
		t.Fatalf("the caller gets the correlation and never the cause: %s", res.Body.String())
	}

	lines := e.lines(distribution.RequestLogMessage)
	if len(lines) != 1 {
		t.Fatalf("expected one request line:\n%s", e.logs.String())
	}
	l := lines[0]
	if l["level"] != "ERROR" || l["status"] != float64(500) || l["correlation_id"] != "flow-500" || l["route"] != "/parties/{id}" {
		t.Fatalf("unexpected 500 line: %v", l)
	}
	if cause := fmt.Sprint(l["error"]); !strings.Contains(cause, "database is closed") {
		t.Fatalf("the 500 line must carry its cause, got %q", cause)
	}
	if l["error_type"] == nil || l["trace_id"] == nil {
		t.Fatalf("the 500 line must carry the error type and the trace: %v", l)
	}

	// The use case says the same, with the same correlation and trace.
	failed := e.lines("use case failed")
	if len(failed) != 1 || failed[0]["correlation_id"] != "flow-500" || failed[0]["trace_id"] != l["trace_id"] || failed[0]["outcome"] != "error" {
		t.Fatalf("unexpected use case line: %v", failed)
	}
	server := one(t, e.spans.Named("GET /parties/{id}"), "server")
	if server.Err == nil || server.Attr("status") != 500 {
		t.Fatalf("the server span must be failed: %+v", server)
	}
	if n := e.reg.HistogramCount(metrics.UseCaseDuration, "request", "application.GetParty", "outcome", "error"); n != 1 {
		t.Fatalf("the failure must be measured: %v", e.reg.SeriesLabels(metrics.UseCaseDuration))
	}
	if n := e.reg.HistogramCount(metrics.DBOperationDuration, "engine", "sqlite", "operation", "SELECT", "outcome", "error"); n == 0 {
		t.Fatalf("the failing statement must be measured: %v", e.reg.SeriesLabels(metrics.DBOperationDuration))
	}

	// An expected rejection is not an error: Info line, no cause.
	e.reset()
	if res := e.do("GET", "/parties/not-an-id", nil); res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", res.Code)
	}
	if l := e.lines(distribution.RequestLogMessage)[0]; l["level"] != "INFO" || l["error"] != nil {
		t.Fatalf("a 4xx is an Info line without a cause: %v", l)
	}
}

// A correlation header longer than the columns that store it must not break the write.
func TestObservability_AnOversizedCorrelationDoesNotBreakTheWrite(t *testing.T) {
	e := composeObserved(t)
	long := strings.Repeat("x", 200)

	res := e.do("POST", "/parties", map[string]string{"type": "organization", "legalName": "Acme Long", "taxId": "B12345674"},
		distribution.CorrelationHeader, long)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", res.Code, res.Body.String())
	}
	echoed := res.Header().Get(distribution.CorrelationHeader)
	if echoed == long || !distribution.ValidCorrelationID(echoed) {
		t.Fatalf("the oversized value must be replaced, got %q", echoed)
	}
	msgs, err := e.outbox.Pending(context.Background(), 10, 10)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("outbox: %v, %v", msgs, err)
	}
	for _, m := range msgs {
		if m.CorrelationID != echoed {
			t.Fatalf("the outbox must store the replaced correlation, got %q", m.CorrelationID)
		}
	}
	if l := e.lines(distribution.RequestLogMessage)[0]; l["correlation_id"] != echoed || strings.Contains(e.logs.String(), long) {
		t.Fatalf("the line must carry the replaced correlation only: %v", l)
	}
}

// The outbox is watched: deliveries, failures with their correlation, and the age of the oldest
// pending message, which grows while nobody relays and returns to zero once delivered.
func TestObservability_OutboxRelayAndConsumers(t *testing.T) {
	ctx := context.Background()
	e := composeObserved(t)
	e.register("Acme Observed", "B12345674", "flow-acme")
	e.register("Globex Observed", "A58818501", "flow-globex")
	e.reset()
	const registered = "parties.party-registered.v1"

	// Billing fails the first time it sees Acme; the CRM always succeeds, so the redelivery
	// reaches it as a duplicate.
	billingStore := memory.NewStore("billing")
	billing := messaging.NewConsumer("billing", memory.NewInbox(billingStore), billingStore).WithTelemetry(e.logger, e.tracer, e.reg)
	failures := 0
	messaging.Handle(billing, func(_ context.Context, evt contracts.PartyRegisteredV1, _ application.Envelope) error {
		if evt.TaxID == "B12345674" && failures == 0 {
			failures++
			return errors.New("billing temporarily unavailable")
		}
		return nil
	})
	crmStore := memory.NewStore("crm")
	crm := messaging.NewConsumer("crm", memory.NewInbox(crmStore), crmStore).WithTelemetry(e.logger, e.tracer, e.reg)
	messaging.Handle(crm, func(context.Context, crmPartyRegistered, application.Envelope) error { return nil })

	broker := msgbroker.NewBroker()
	broker.Subscribe(crm.Name(), crm, crm.Types()...)
	broker.Subscribe(billing.Name(), billing, billing.Types()...)
	relay := messaging.NewRelay(contracts.Source, e.integ, broker,
		appoutbox.WithRelayLogger(e.logger), appoutbox.WithRelayTelemetry(e.tracer, e.reg))

	// Nobody relays: the age of the oldest pending message grows.
	age := func() float64 {
		v, ok := e.reg.GaugeValue(metrics.OutboxOldestPendingAge, "outbox", contracts.Source)
		if !ok {
			t.Fatal("the relay must register the age of the oldest pending message")
		}
		return v
	}
	first := age()
	time.Sleep(30 * time.Millisecond)
	if second := age(); first <= 0 || second <= first {
		t.Fatalf("the age must grow while the relay does not run: %v then %v", first, second)
	}

	// First pass: Globex is delivered, Acme fails in billing and stays pending.
	if n, err := relay.RelayOnce(ctx); err != nil || n != 1 {
		t.Fatalf("first pass: %d, %v", n, err)
	}
	relayed := func(outcome string) float64 {
		return e.reg.CounterValue(metrics.OutboxRelayed, "event_type", registered, "outcome", outcome)
	}
	handled := func(consumer, outcome string) float64 {
		return e.reg.CounterValue(metrics.MessagingHandled, "consumer", consumer, "event_type", registered, "outcome", outcome)
	}
	if relayed("ok") != 1 || relayed("error") != 1 {
		t.Fatalf("relayed: ok=%v error=%v", relayed("ok"), relayed("error"))
	}
	if handled("billing", "ok") != 1 || handled("billing", "error") != 1 || handled("crm", "ok") != 2 {
		t.Fatalf("handled: %v", e.reg.SeriesLabels(metrics.MessagingHandled))
	}
	if age() <= 0 {
		t.Fatal("a message is still pending: its age must be positive")
	}

	// The failure left its lines, with the correlation of the request that caused it.
	for _, msg := range []string{"integration message failed", "outbox delivery failed"} {
		lines := e.lines(msg)
		if len(lines) != 1 {
			t.Fatalf("expected one %q line:\n%s", msg, e.logs.String())
		}
		if l := lines[0]; l["level"] != "WARN" || l["correlation_id"] != "flow-acme" || l["event_type"] != registered ||
			!strings.Contains(fmt.Sprint(l["error"]), "billing temporarily unavailable") {
			t.Fatalf("unexpected %q line: %v", msg, l)
		}
	}
	if l := e.lines("outbox delivery failed")[0]; l["outbox"] != contracts.Source || l["attempt"] != float64(1) {
		t.Fatalf("unexpected relay line: %v", l)
	}

	// Second pass: Acme is delivered; the CRM skips it as a duplicate.
	if n, err := relay.RelayOnce(ctx); err != nil || n != 1 {
		t.Fatalf("second pass: %d, %v", n, err)
	}
	if relayed("ok") != 2 || relayed("error") != 1 {
		t.Fatalf("relayed: ok=%v error=%v", relayed("ok"), relayed("error"))
	}
	if handled("billing", "ok") != 2 || handled("crm", "duplicate") != 1 {
		t.Fatalf("handled: %v", e.reg.SeriesLabels(metrics.MessagingHandled))
	}
	if n := e.reg.HistogramCount(metrics.MessagingLag, "consumer", "billing", "event_type", registered); n != 2 {
		t.Fatalf("the lag must be measured once per handled message, got %d", n)
	}
	if got := age(); got != 0 {
		t.Fatalf("the outbox is empty: the age must be zero, got %v", got)
	}

	// Relay and consumer spans carry the correlation of the flow, in traces of their own.
	deliveries := e.spans.Named("outbox relay " + registered)
	consumed := e.spans.Named("consume " + registered)
	if len(deliveries) != 3 || len(consumed) != 6 {
		t.Fatalf("expected 3 delivery and 6 consumer spans, got %d and %d", len(deliveries), len(consumed))
	}
	for _, s := range append(deliveries, consumed...) {
		if c := s.Attr("correlation_id"); c != "flow-acme" && c != "flow-globex" {
			t.Fatalf("span %q lacks the correlation of its flow: %v", s.Name, s.Attributes)
		}
	}
	if deliveries[0].Kind != trace.KindProducer || consumed[0].Kind != trace.KindConsumer || consumed[0].HasParent() {
		t.Fatalf("unexpected span kinds or parents: %+v / %+v", deliveries[0], consumed[0])
	}
}
