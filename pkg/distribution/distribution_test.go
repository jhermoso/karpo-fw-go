package distribution_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

type stringID string

func (s stringID) String() string { return string(s) }

func TestRespond_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/parties/123", nil)

	distribution.Respond(rec, req, map[string]string{"name": "Acme Corp"}, nil, http.StatusOK)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed decoding json response: %v", err)
	}
	if body["name"] != "Acme Corp" {
		t.Errorf("expected name 'Acme Corp', got %s", body["name"])
	}
}

func TestWriteError_MapsDomainTaxonomy(t *testing.T) {
	var v domain.Validation
	v.Add("taxId", "required", "tax id is required")

	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"not found", domain.NotFound("parties.party", stringID("999")), http.StatusNotFound},
		{"wrapped not found", fmt.Errorf("loading: %w", domain.NotFound("x", stringID("1"))), http.StatusNotFound},
		{"validation", v.Err(), http.StatusBadRequest},
		{"rule", domain.Violation("credit_limit", "limit exceeded"), http.StatusUnprocessableEntity},
		{"conflict", domain.Conflict("x", stringID("1"), 3, "stale version"), http.StatusConflict},
		{"forbidden", domain.ErrForbidden, http.StatusForbidden},
		{"unsupported", domain.ErrUnsupported, http.StatusNotImplemented},
		// Message text is irrelevant: an unknown error mentioning "not found" is still a 500.
		{"unknown", errors.New("dependency not found in cache"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			distribution.WriteError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), tc.err)
			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d", tc.status, rec.Code)
			}
			var p distribution.ProblemDetails
			_ = json.Unmarshal(rec.Body.Bytes(), &p)
			if p.Status != tc.status {
				t.Fatalf("problem status mismatch: %+v", p)
			}
		})
	}
}

func TestWriteError_ValidationCarriesFieldErrors(t *testing.T) {
	var v domain.Validation
	v.Add("taxId", "required", "tax id is required")
	rec := httptest.NewRecorder()
	distribution.WriteError(rec, httptest.NewRequest(http.MethodPost, "/parties", nil), v.Err())

	var p distribution.ProblemDetails
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if len(p.Errors) != 1 || p.Errors[0].Field != "taxId" || p.Errors[0].Code != "required" {
		t.Fatalf("field errors not propagated: %+v", p)
	}
}

func TestCorrelationMiddleware(t *testing.T) {
	var seen string
	h := distribution.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = application.CorrelationID(r.Context())
	}), distribution.Correlation())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(distribution.CorrelationHeader, "corr-1")
	h.ServeHTTP(rec, req)
	if seen != "corr-1" || rec.Header().Get(distribution.CorrelationHeader) != "corr-1" {
		t.Fatalf("correlation not propagated: ctx=%q header=%q", seen, rec.Header().Get(distribution.CorrelationHeader))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if seen == "" || seen != rec.Header().Get(distribution.CorrelationHeader) {
		t.Fatalf("expected generated correlation id, got %q", seen)
	}
}

func TestTenantActorContextMiddleware(t *testing.T) {
	handler := distribution.Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			tenant := distribution.TenantID(ctx)
			actor := distribution.ActorID(ctx)
			org := distribution.OrganizationID(ctx)

			_ = json.NewEncoder(w).Encode(map[string]string{
				"tenant": tenant,
				"actor":  actor,
				"org":    org,
			})
		}),
		distribution.TenantActorContext(),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Tenant-ID", "tenant-100")
	req.Header.Set("X-Actor-ID", "actor-500")
	req.Header.Set("X-Organization-ID", "org-900")

	handler.ServeHTTP(rec, req)

	var res map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &res)

	if res["tenant"] != "tenant-100" || res["actor"] != "actor-500" || res["org"] != "org-900" {
		t.Fatalf("context values not extracted properly: %+v", res)
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	handler := distribution.Chain(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			panic("something went terribly wrong")
		}),
		distribution.Recovery(nil),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 status from recovered panic, got %d", rec.Code)
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := distribution.Chain(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		distribution.CORS(),
	)

	// Pre-flight OPTIONS request
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/data", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS preflight, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected CORS header to be set")
	}
}

func TestHealthRegistry(t *testing.T) {
	health := distribution.NewHealthRegistry()
	mux := http.NewServeMux()
	health.Mount(mux)

	// 1. Liveness
	recLive := httptest.NewRecorder()
	reqLive := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	mux.ServeHTTP(recLive, reqLive)

	if recLive.Code != http.StatusOK {
		t.Fatalf("expected 200 for liveness, got %d", recLive.Code)
	}

	// 2. Readiness with OK probe
	health.RegisterReadinessProbe("db", func(_ context.Context) error {
		return nil
	})

	recReady := httptest.NewRecorder()
	reqReady := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	mux.ServeHTTP(recReady, reqReady)

	if recReady.Code != http.StatusOK {
		t.Fatalf("expected 200 for readiness with healthy db, got %d", recReady.Code)
	}

	// 3. Readiness with failing probe
	health.RegisterReadinessProbe("cache", func(_ context.Context) error {
		return errors.New("connection refused")
	})

	recFailed := httptest.NewRecorder()
	mux.ServeHTTP(recFailed, reqReady)

	if recFailed.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for readiness with failing probe, got %d", recFailed.Code)
	}
}
