package serve_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/host/mailbox"
	"github.com/jhermoso/karpo-fw-go/host/serve"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
)

// lines is a logger that keeps what it is told.
type lines struct{ said *[]string }

func (l lines) Debug(string, ...any) {}
func (l lines) Info(string, ...any)  {}
func (l lines) Warn(string, ...any)  {}
func (l lines) Error(msg string, args ...any) {
	*l.said = append(*l.said, msg)
}
func (l lines) With(...any) log.Logger                 { return l }
func (l lines) WithContext(context.Context) log.Logger { return l }

func TestAlerts(t *testing.T) {
	given := mailbox.DTO{ID: "d-1", Consumer: "accounting", EventType: "billing.invoice-issued.v1", Subject: "inv-1", Attempts: 10,
		Reason: "the database is down", Data: json.RawMessage(`{"secret":"the amount"}`)}
	var got map[string]any
	var raw string
	status := http.StatusOK
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		got = map[string]any{}
		_ = json.Unmarshal(b, &got)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		w.WriteHeader(status)
	}))
	defer hook.Close()

	// Without a webhook: the log, and nothing leaves.
	said := []string{}
	serve.Alerts{Logger: lines{&said}}.DeliveryGivenUp(context.Background(), given)
	if len(said) != 1 || got != nil {
		t.Fatalf("only the log: %v %v", said, got)
	}
	// With one: who, what and why, in words and as facts; never the message itself.
	said = said[:0]
	serve.Alerts{Webhook: hook.URL, Logger: lines{&said}}.DeliveryGivenUp(context.Background(), given)
	text, _ := got["text"].(string)
	if len(said) != 1 || got["kind"] != "delivery-given-up" || got["consumer"] != "accounting" || got["delivery"] != "d-1" ||
		got["eventType"] != "billing.invoice-issued.v1" || got["attempts"] != float64(10) || !strings.Contains(text, "accounting") ||
		!strings.Contains(text, "the database is down") || strings.Contains(raw, "the amount") {
		t.Fatalf("the alert: %s (log %v)", raw, said)
	}
	// A webhook that refuses, or that is not there, is logged and stops nothing; so is a context
	// that has ended: the alert of a round that is stopping is still sent.
	said, status = said[:0], http.StatusInternalServerError
	serve.Alerts{Webhook: hook.URL, Logger: lines{&said}}.DeliveryGivenUp(context.Background(), given)
	serve.Alerts{Webhook: "http://127.0.0.1:1/nowhere", Logger: lines{&said}}.DeliveryGivenUp(context.Background(), given)
	if len(said) != 4 {
		t.Fatalf("each failure is logged after its alert: %v", said)
	}
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	said, status, got = said[:0], http.StatusOK, nil
	serve.Alerts{Webhook: hook.URL, Logger: lines{&said}}.DeliveryGivenUp(ended, given)
	if len(said) != 1 || got == nil {
		t.Fatalf("sent although the round is stopping: %v %v", said, got)
	}
	serve.Alerts{Webhook: hook.URL}.DeliveryGivenUp(context.Background(), given) // without a logger it does not break
}

func TestLoad_AlertWebhook(t *testing.T) {
	t.Setenv("KARPO_JWT_SECRET", strings.Repeat("s", serve.MinSecret))
	for hook, ok := range map[string]bool{"": true, "https://hooks.example.test/T/1": true, "http://localhost:9/x": true, "hooks.example.test": false,
		"ftp://x/y": false, "https://": false} {
		t.Setenv("KARPO_ALERT_WEBHOOK", hook)
		cfg, err := serve.Load("exports")
		if (err == nil) != ok || (ok && cfg.AlertWebhook != hook) {
			t.Fatalf("%q: %+v %v", hook, cfg, err)
		}
	}
}
