// Package work composes the Work (Trabajos: proyectos, tareas y partes de horas) bounded context on
// a hot-swap backend.
package work

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	wapp "github.com/jhermoso/karpo-fw-go/contexts/work/application"
	"github.com/jhermoso/karpo-fw-go/contexts/work/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/work/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/work/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *wapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
}

// Compose builds the context on sw.
func Compose(sw *hotswap.Switch) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	svc := wapp.NewService(wapp.Deps{
		Works: hotswap.Repository(sw, infrastructure.WorkRepositoryFactory), Times: hotswap.Repository(sw, infrastructure.TimeEntryRepositoryFactory),
		UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			wapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	})
	return &Module{Service: svc, IntegrationOutbox: integration, Audit: audit}
}

// Relay forwards the Published Language to a transport.
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var val fw.Validation
		val.Add("body", "json", err.Error())
		return val.Err()
	}
	return nil
}

func badID(w http.ResponseWriter, r *http.Request) {
	distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
}

func command[ID, In, Out any](parse func(string) (ID, error), set func(*In, ID), run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		var c In
		if r.ContentLength != 0 {
			if err := decode(r, &c); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
		}
		set(&c, id)
		out, err := run(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusOK)
	}
}

func create[In, Out any](status int, run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var c In
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		out, err := run(r.Context(), c)
		distribution.Respond(w, r, out, err, status)
	}
}

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	wid, tid := domain.ParseWorkID, domain.ParseTimeEntryID
	atoi := func(r *http.Request, k string) int { n, _ := strconv.Atoi(r.URL.Query().Get(k)); return n }

	mux.HandleFunc("POST /api/work", create(http.StatusCreated, svc.Open.Handle))
	mux.HandleFunc("GET /api/work", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.SearchWorks.Handle(r.Context(), wapp.SearchWorks{Company: q.Get("company"), Kind: q.Get("kind"), Status: q.Get("status"),
			Parent: q.Get("parent"), Customer: q.Get("customer"), Page: atoi(r, "page"), Size: atoi(r, "size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/work/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := wid(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetWork.Handle(r.Context(), wapp.GetWork{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/work/{id}", command(wid, func(c *wapp.ChangeWork, id domain.WorkID) { c.ID = id }, svc.Change.Handle))
	mux.HandleFunc("POST /api/work/{id}/assign", command(wid, func(c *wapp.AssignPerson, id domain.WorkID) { c.ID = id }, svc.Assign.Handle))
	mux.HandleFunc("POST /api/work/{id}/release", command(wid, func(c *wapp.ReleasePerson, id domain.WorkID) { c.ID = id }, svc.Release.Handle))
	mux.HandleFunc("POST /api/work/{id}/progress", command(wid, func(c *wapp.ProgressWork, id domain.WorkID) { c.ID = id }, svc.Progress.Handle))

	mux.HandleFunc("POST /api/time", create(http.StatusCreated, svc.Record.Handle))
	mux.HandleFunc("GET /api/time", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.SearchTime.Handle(r.Context(), wapp.SearchTime{Company: q.Get("company"), Work: q.Get("work"), Person: q.Get("person"),
			From: q.Get("from"), To: q.Get("to"), Pending: q.Get("pending") == "true", Page: atoi(r, "page"), Size: atoi(r, "size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/time/sheet", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.Timesheet.Handle(r.Context(), wapp.GetTimesheet{Company: q.Get("company"), Person: q.Get("person"), From: q.Get("from"), To: q.Get("to")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/time/{id}", command(tid, func(c *wapp.CorrectTime, id domain.TimeEntryID) { c.ID = id }, svc.Correct.Handle))
	mux.HandleFunc("POST /api/time/{id}/approve", command(tid, func(c *wapp.ApproveTime, id domain.TimeEntryID) { c.ID = id }, svc.Approve.Handle))
	mux.HandleFunc("DELETE /api/time/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := tid(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		if _, err := svc.Withdraw.Handle(r.Context(), wapp.WithdrawTime{ID: id}); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

var _ distribution.EndpointModule = (*Module)(nil)
