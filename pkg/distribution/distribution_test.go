package distribution_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

func TestWriteResult_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/parties/123", nil)

	data := map[string]string{"name": "Acme Corp"}
	distribution.WriteResult(rec, req, result.Ok(data), http.StatusOK)

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

func TestWriteResult_NotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/parties/999", nil)

	failRes := result.FailMsg[string]("party not found")
	distribution.WriteResult(rec, req, failRes, http.StatusOK)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for not found error, got %d", rec.Code)
	}

	var details distribution.ProblemDetails
	_ = json.Unmarshal(rec.Body.Bytes(), &details)
	if details.Status != 404 || details.Title != "Resource Not Found" {
		t.Errorf("unexpected problem details: %+v", details)
	}
}

func TestWriteResult_BadRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/parties", nil)

	failRes := result.FailMsg[string]("tax_id is required")
	distribution.WriteResult(rec, req, failRes, http.StatusOK)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for validation error, got %d", rec.Code)
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
