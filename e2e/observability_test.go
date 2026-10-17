// Package e2e contiene pruebas de extremo a extremo que componen contextos delimitados reales
// con el framework como lo hace un servicio. Viven fuera de contexts/ para no chocar nunca con
// el trabajo sobre un contexto.
package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	logvanilla "github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	metricsvanilla "github.com/jhermoso/karpo-fw-go/pkg/metrics/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
	tracevanilla "github.com/jhermoso/karpo-fw-go/pkg/trace/vanilla"
)

// stdout es la salida estándar del servicio de prueba.
type stdout struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *stdout) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *stdout) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// requests devuelve las líneas de petición; cada línea de la salida debe ser un objeto JSON.
func (b *stdout) requests(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(b.String()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("stdout is not one JSON object per line: %q", sc.Text())
		}
		if m["msg"] == distribution.RequestLogMessage {
			out = append(out, m)
		}
	}
	return out
}

type service struct {
	t        *testing.T
	api      *httptest.Server // el puerto público
	internal *httptest.Server // el puerto interno: /metrics
	out      *stdout
	spans    *tracevanilla.Recorder
	reg      *metricsvanilla.Registry
	sw       *hotswap.Switch
	tracer   trace.Tracer
	token    string
	username string
}

func openSQLite(t *testing.T, name string, opts ...sqlrepo.Option) *sqlrepo.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), name+".db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	return sqlite.Open(raw, append([]sqlrepo.Option{sqlrepo.WithName(name)}, opts...)...)
}

// compose construye el contexto Geography sobre SQLite detrás de la cadena estándar de un
// servicio: recuperación, correlación, telemetría y autenticación, con /metrics en un puerto
// aparte.
func compose(t *testing.T) *service {
	t.Helper()
	ctx := context.Background()
	s := &service{t: t, out: &stdout{}, spans: tracevanilla.NewRecorder(0), reg: metricsvanilla.NewRegistry(), username: "reader-jane"}
	logger := logvanilla.NewJSON(s.out, log.LevelInfo).With("service", "geography", "env", "test")
	s.tracer = tracevanilla.New(tracevanilla.WithExporter(s.spans))

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("e2e-observability")})
	dir := authorization.NewMemoryDirectory()
	reader := fw.NewUUID()
	dir.Put(reader, authz.Subject{Active: true, Permissions: []authz.Permission{gapp.PermBoundaryRead, gapp.PermReferenceRead}})
	tok, _ := jwt.Issue(jwtauth.Claims{Subject: reader.String(), Username: s.username, PartyID: fw.NewUUID().String(),
		ExpiresAt: fw.Now().Add(time.Hour).Unix()})
	s.token = "Bearer " + tok

	db := openSQLite(t, "geography", sqlrepo.WithTelemetry(s.tracer, s.reg))
	m, err := infrastructure.Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s.sw = hotswap.New(db)
	mod := geography.Compose(s.sw)

	api := http.NewServeMux()
	mod.HTTP.RegisterRoutes(api)
	s.api = httptest.NewServer(distribution.Chain(api,
		distribution.Recovery(logger),
		distribution.Correlation(),
		distribution.Observe(logger, s.tracer, s.reg, distribution.ObserveRoutes(api)),
		distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{})),
	))
	internal := http.NewServeMux()
	internal.Handle("GET /metrics", s.reg.Handler())
	s.internal = httptest.NewServer(internal)

	t.Cleanup(s.api.Close)
	t.Cleanup(s.internal.Close)
	t.Cleanup(func() { _ = s.sw.Close(ctx) })
	s.spans.Reset() // olvida la migración
	return s
}

func (s *service) get(path, correlation string, authenticated bool) (int, http.Header, string) {
	s.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, s.api.URL+path, nil)
	if authenticated {
		req.Header.Set("Authorization", s.token)
	}
	if correlation != "" {
		req.Header.Set(distribution.CorrelationHeader, correlation)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(body)
}

func (s *service) metricsText() string {
	s.t.Helper()
	res, err := http.Get(s.internal.URL + "/metrics")
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return string(body)
}

// eventually espera a lo que escribe el servidor después de que salga la respuesta (la línea de
// petición, el span y la observación se anotan cuando vuelve la cadena de manejadores).
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

const countryRoute = "/api/reference/countries/{alpha2}"

// Una petición HTTP a un contexto existente deja una línea JSON, una métrica y un id de traza,
// todos con la misma correlación; un 500 deja su causa.
func TestObservability_EndToEnd(t *testing.T) {
	s := compose(t)

	// --- Una petición normal ----------------------------------------------------------------
	status, header, body := s.get("/api/reference/countries/ES", "e2e-flow-1", true)
	if status != http.StatusOK {
		t.Fatalf("status = %d %s", status, body)
	}
	if header.Get(distribution.CorrelationHeader) != "e2e-flow-1" {
		t.Fatalf("the response must echo the correlation, got %q", header.Get(distribution.CorrelationHeader))
	}
	eventually(t, "the request line", func() bool { return len(s.out.requests(t)) == 1 })

	// La línea: JSON en stdout, una por petición.
	if n := strings.Count(strings.TrimSpace(s.out.String()), "\n"); n != 0 {
		t.Fatalf("a request must leave exactly one line:\n%s", s.out.String())
	}
	line := s.out.requests(t)[0]
	if line["level"] != "INFO" || line["service"] != "geography" || line["method"] != "GET" ||
		line["route"] != countryRoute || line["status"] != float64(200) || line["correlation_id"] != "e2e-flow-1" {
		t.Fatalf("unexpected request line: %v", line)
	}
	if _, ok := line["duration_ms"].(float64); !ok {
		t.Fatalf("the line must carry the duration: %v", line)
	}
	if line["actor"] == nil || strings.Contains(s.out.String(), s.username) || strings.Contains(s.out.String(), "Bearer") {
		t.Fatalf("the line carries the actor id and nothing personal or secret:\n%s", s.out.String())
	}

	// La traza: el span del servidor tiene el id de traza de la línea y la misma correlación, y
	// de él cuelgan las sentencias SQL de la petición.
	eventually(t, "the server span", func() bool { return len(s.spans.Named("GET "+countryRoute)) == 1 })
	server := s.spans.Named("GET " + countryRoute)[0]
	if line["trace_id"] != server.Context.TraceIDString() || line["span_id"] != server.Context.SpanIDString() {
		t.Fatalf("the line and the span must share the trace: %v", line)
	}
	if server.Attr("correlation_id") != "e2e-flow-1" || server.Kind != trace.KindServer || server.Err != nil {
		t.Fatalf("unexpected server span: %+v", server)
	}
	selects := s.spans.Named("db SELECT")
	if len(selects) == 0 {
		t.Fatal("the request must leave its SQL statements as spans")
	}
	for _, sp := range selects {
		if sp.Context.TraceID != server.Context.TraceID || sp.ParentID != server.Context.SpanID {
			t.Fatalf("the statement must be a child of the request: %+v", sp)
		}
		if dump := fmt.Sprint(sp.Attributes); strings.Contains(dump, "ES") {
			t.Fatalf("a statement span must not carry arguments: %s", dump)
		}
	}

	// La métrica: en el puerto interno, por patrón de ruta.
	if n := s.reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "GET", "route", countryRoute, "status", "200"); n != 1 {
		t.Fatalf("expected one observation: %v", s.reg.SeriesLabels(metrics.HTTPServerRequestDuration))
	}
	text := s.metricsText()
	for _, want := range []string{
		`http_server_request_duration_seconds_count{method="GET",route="` + countryRoute + `",status="200"} 1`,
		`db_client_operation_duration_seconds_count{engine="sqlite",operation="SELECT",outcome="ok"}`,
		`db_client_connections{engine="sqlite",db="geography",state="open"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("/metrics lacks %q", want)
		}
	}
	if strings.Contains(text, "e2e-flow-1") || strings.Contains(text, "/countries/ES") {
		t.Fatal("no correlation and no raw path may become a metric label")
	}

	// --- Una petición sin autenticar: también una línea, sin causa ---------------------------
	if status, _, _ := s.get("/api/reference/countries/ES", "e2e-flow-401", false); status != http.StatusUnauthorized {
		t.Fatalf("status = %d", status)
	}
	eventually(t, "the 401 line", func() bool { return len(s.out.requests(t)) == 2 })
	if l := s.out.requests(t)[1]; l["level"] != "INFO" || l["status"] != float64(401) || l["route"] != countryRoute ||
		l["correlation_id"] != "e2e-flow-401" || l["error"] != nil || l["actor"] != nil {
		t.Fatalf("unexpected 401 line: %v", l)
	}

	// --- Un 500: la base de datos pierde su esquema mientras el servicio funciona -------------
	if err := s.sw.Swap(context.Background(), openSQLite(t, "empty", sqlrepo.WithTelemetry(s.tracer, s.reg))); err != nil {
		t.Fatal(err)
	}
	status, _, body = s.get("/api/reference/countries/ES", "e2e-flow-500", true)
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d %s", status, body)
	}
	var problem distribution.ProblemDetails
	if err := json.Unmarshal([]byte(body), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.CorrelationID != "e2e-flow-500" || strings.Contains(body, "no such table") {
		t.Fatalf("the caller gets the correlation and never the cause: %s", body)
	}
	eventually(t, "the 500 line", func() bool { return len(s.out.requests(t)) == 3 })
	l := s.out.requests(t)[2]
	if l["level"] != "ERROR" || l["status"] != float64(500) || l["route"] != countryRoute || l["correlation_id"] != "e2e-flow-500" {
		t.Fatalf("unexpected 500 line: %v", l)
	}
	if cause := fmt.Sprint(l["error"]); !strings.Contains(cause, "no such table") {
		t.Fatalf("the 500 line must carry its cause, got %q", cause)
	}
	if l["error_type"] == nil || l["trace_id"] == nil || l["trace_id"] == line["trace_id"] {
		t.Fatalf("the 500 line must carry the error type and its own trace: %v", l)
	}
	eventually(t, "the failed span", func() bool { return len(s.spans.Named("GET "+countryRoute)) == 3 })
	if failed := s.spans.Named("GET " + countryRoute)[2]; failed.Err == nil || failed.Attr("correlation_id") != "e2e-flow-500" ||
		failed.Context.TraceIDString() != l["trace_id"] {
		t.Fatalf("the failed span must match the line: %+v", failed)
	}
	if !strings.Contains(s.metricsText(), `http_server_request_duration_seconds_count{method="GET",route="`+countryRoute+`",status="500"} 1`) {
		t.Fatal("/metrics must count the 500")
	}

	// La página de métricas no está en el puerto público.
	if res, err := http.Get(s.api.URL + "/metrics"); err == nil {
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			t.Fatal("/metrics must not be served on the public port")
		}
	}
}
