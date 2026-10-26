package accounting_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// Un reintento que llega mientras otro está en marcha, y un descarte, esperan su turno: el hecho se
// contabiliza una vez y no queda descartado después de contabilizado.
func TestParking_RetriesAndDiscardsWaitForTheRetryInCourse(t *testing.T) {
	var calls atomic.Int32
	entered, again, release := make(chan struct{}), make(chan struct{}, 4), make(chan struct{})
	refusal := fw.Violation("accounting.no_ledger", "the company has no ledger")
	inner := &stubInner{answer: func(app.Envelope) error { calls.Add(1); return refusal }}
	svc, parking, actx, invoice, _ := parkingOver(t, inner)
	if err := parking.HandleMessage(context.Background(), invoice(fw.NewUUID().String(), 1)); err != nil {
		t.Fatal(err)
	}
	p, err := svc.SearchParked.Handle(actx, aapp.SearchParked{})
	if err != nil || len(p.Items) != 1 {
		t.Fatalf("parked: %+v %v", p.Items, err)
	}
	id, _ := domain.ParseParkedID(p.Items[0].ID)
	inner.answer = func(app.Envelope) error {
		if calls.Add(1) == 2 {
			close(entered)
			<-release
			return nil
		}
		again <- struct{}{}
		return nil
	}

	first, second := make(chan aapp.RetriedDTO, 1), make(chan aapp.RetriedDTO, 1)
	discarded := make(chan error, 1)
	go func() { r, _ := svc.RetryParked.Handle(actx, aapp.RetryParked{}); first <- r }()
	<-entered // el primer reintento está contabilizando el hecho
	go func() { r, _ := svc.RetryParked.Handle(actx, aapp.RetryParked{}); second <- r }()
	go func() {
		_, err := svc.DiscardParked.Handle(actx, aapp.DiscardParked{ID: id, Note: "de prueba"})
		discarded <- err
	}()
	select {
	case <-again:
		t.Fatal("the second retry tried the fact while the first was posting it")
	case err := <-discarded:
		t.Fatalf("the discard did not wait: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if r := <-first; r.Posted != 1 || r.Waiting != 0 {
		t.Fatalf("first retry: %+v", r)
	}
	var rule *fw.RuleViolationError
	if err := <-discarded; !errors.As(err, &rule) || rule.Code != "accounting.not_parked" {
		t.Fatalf("the discard after posting: %v", err)
	}
	if r := <-second; r.Posted != 0 || r.Waiting != 0 || calls.Load() != 2 {
		t.Fatalf("second retry: %+v, %d calls", r, calls.Load())
	}
	if p, _ := svc.SearchParked.Handle(actx, aapp.SearchParked{}); len(p.Items) != 1 || p.Items[0].Status != "posted" {
		t.Fatalf("posted, not discarded: %+v", p.Items)
	}
}

// Un reintento que se interrumpe mientras el consumidor trabaja no anota nada en el hecho.
func TestParking_AnInterruptedRetryWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inner := &stubInner{answer: func(app.Envelope) error { return fw.Violation("accounting.no_ledger", "the company has no ledger") }}
	svc, parking, actx, invoice, parked := parkingOver(t, inner)
	if err := parking.HandleMessage(ctx, invoice(fw.NewUUID().String(), 1)); err != nil {
		t.Fatal(err)
	}
	inner.answer = func(app.Envelope) error { cancel(); return context.Canceled }
	ac, _ := authz.FromContext(actx)
	if _, err := svc.RetryParked.Handle(authz.WithContext(ctx, ac), aapp.RetryParked{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted: %v", err)
	}
	p, _ := svc.SearchParked.Handle(actx, aapp.SearchParked{})
	if parked.saves != 1 || len(p.Items) != 1 || p.Items[0].Attempts != 1 || p.Items[0].Code != "accounting.no_ledger" {
		t.Fatalf("written: %d saves, %+v", parked.saves, p.Items)
	}
}

// Dos hechos distintos sin identidad no se toman por el mismo: se guardan los dos.
func TestParking_TwoFactsWithoutIdentityAreBothKept(t *testing.T) {
	tries := 0
	refusal := fw.Violation("accounting.no_ledger", "the company has no ledger")
	inner := &stubInner{answer: func(app.Envelope) error { tries++; return refusal }}
	svc, parking, actx, invoice, _ := parkingOver(t, inner)
	company := fw.NewUUID().String()
	one, other := invoice(company, 1), invoice(company, 2)
	one.ID, other.ID = "", ""
	for _, env := range []app.Envelope{one, other} {
		if err := parking.HandleMessage(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	p, err := svc.SearchParked.Handle(actx, aapp.SearchParked{})
	if err != nil || len(p.Items) != 2 || tries != 2 || p.Items[0].Code != domain.OversizedCode || p.Items[1].Code != domain.OversizedCode {
		t.Fatalf("kept: %+v %v, %d tries", p.Items, err, tries)
	}
}

// shopInvoice es la factura de una empresa que aún no tiene libro: el consumidor de verdad la rechaza.
func shopInvoice(company string) app.Envelope {
	id := fw.NewUUID().String()
	raw, _ := json.Marshal(map[string]any{"invoiceId": id, "number": "F-1", "seller": company, "customer": fw.NewUUID().String(),
		"issueDate": "2026-03-10", "net": "100.00", "total": "121.00", "taxes": []map[string]any{{"taxCode": "G", "amount": "21.00"}}})
	return app.Envelope{ID: fw.NewUUID().String(), Type: "billing.invoice-issued.v1", Source: "test", Subject: id,
		OccurredAt: time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC), Data: raw}
}

func globalAdmin(ctx context.Context) context.Context {
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	ac.GlobalAdmin = true
	return authz.WithContext(ctx, ac)
}

// Una fila de antes de la migración 5, con next_attempt nulo, toca ya.
func TestParking_ARowWithoutNextAttemptIsDue(t *testing.T) {
	stopTheClock(t)
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "parked.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{ainfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	am := accounting.Compose(hotswap.New(db))
	actx := globalAdmin(ctx)
	if err := am.Parking.HandleMessage(ctx, shopInvoice(fw.NewUUID().String())); err != nil {
		t.Fatal(err)
	}
	attempts := func() aapp.ParkedDTO {
		t.Helper()
		p, err := am.Service.SearchParked.Handle(actx, aapp.SearchParked{})
		if err != nil || len(p.Items) != 1 {
			t.Fatalf("kept: %+v %v", p.Items, err)
		}
		return p.Items[0]
	}
	// Aún no toca: la ronda no lo intenta.
	if r, err := am.Service.RetryParked.Handle(actx, aapp.RetryParked{Due: true}); err != nil || r.Waiting != 1 || attempts().Attempts != 1 {
		t.Fatalf("not due: %+v %v", r, err)
	}
	if _, err := raw.ExecContext(ctx, "UPDATE acc_parked_facts SET next_attempt = NULL"); err != nil {
		t.Fatal(err)
	}
	if f := attempts(); f.NextAttempt != "" {
		t.Fatalf("null: %+v", f)
	}
	if r, err := am.Service.RetryParked.Handle(actx, aapp.RetryParked{Due: true}); err != nil || r.Waiting != 1 {
		t.Fatalf("due: %+v %v", r, err)
	}
	if f := attempts(); f.Attempts != 2 || f.NextAttempt == "" {
		t.Fatalf("a null next attempt is due: %+v", f)
	}
}

// La ruta del reintento entiende {"due":true}: sólo lo que ya toca. Sin él, lo intenta todo, y la
// respuesta lleva lo contabilizado y lo que espera.
func TestParking_RetryRouteTakesDue(t *testing.T) {
	stopTheClock(t)
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(ctx) })
	am := accounting.Compose(sw)
	actx := globalAdmin(ctx)
	if err := am.Parking.HandleMessage(ctx, shopInvoice(fw.NewUUID().String())); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	am.RegisterRoutes(mux)
	post := func(body string) map[string]any {
		t.Helper()
		req := httptest.NewRequestWithContext(actx, http.MethodPost, "/api/accounting/parked/retry", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var out map[string]any
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
		return out
	}
	attempts := func() int {
		t.Helper()
		p, err := am.Service.SearchParked.Handle(actx, aapp.SearchParked{})
		if err != nil || len(p.Items) != 1 {
			t.Fatalf("kept: %+v %v", p.Items, err)
		}
		return p.Items[0].Attempts
	}
	if out := post(`{"due":true}`); out["posted"] != 0.0 || out["waiting"] != 1.0 || attempts() != 1 {
		t.Fatalf("due: %v, %d attempts", out, attempts())
	}
	if out := post(`{}`); out["posted"] != 0.0 || out["waiting"] != 1.0 || attempts() != 2 {
		t.Fatalf("all: %v, %d attempts", out, attempts())
	}
}
