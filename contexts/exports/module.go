// Package exports composes the Exports (Exportación de listados) bounded context on a hot-swap
// backend.
package exports

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	eapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	"github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/exports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service  *eapp.Service
	Registry *eapp.Registry
	Audit    application.AuditLog
}

// Compose builds the context on sw, keeping its files in files (a folder every instance shares,
// a bucket). The host says which lists can be exported with Offer, and runs a worker that calls
// RunNext and Purge.
func Compose(sw *hotswap.Switch, files eapp.Files) *Module {
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	registry := eapp.NewRegistry()
	svc := eapp.NewService(eapp.Deps{Jobs: hotswap.Repository(sw, infrastructure.JobRepositoryFactory), Files: files, Registry: registry, UoW: sw, Audit: audit})
	return &Module{Service: svc, Registry: registry, Audit: audit}
}

// Offer adds lists that can be exported.
func (m *Module) Offer(ds ...eapp.Dataset) *Module {
	m.Registry.Offer(ds...)
	return m
}

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	job := func(run func(r *http.Request, id domain.JobID) (any, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id, err := domain.ParseJobID(r.PathValue("id"))
			if err != nil || id.IsZero() {
				distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
				return
			}
			out, err := run(r, id)
			distribution.Respond(w, r, out, err, http.StatusOK)
		}
	}
	mux.HandleFunc("GET /api/exports/datasets", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Datasets.Handle(r.Context(), eapp.ListDatasets{})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/exports", func(w http.ResponseWriter, r *http.Request) {
		var c eapp.StartExport
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			var v fw.Validation
			v.Add("body", "json", err.Error())
			distribution.WriteError(w, r, v.Err())
			return
		}
		out, err := svc.Start.Handle(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusAccepted)
	})
	mux.HandleFunc("GET /api/exports", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.List.Handle(r.Context(), eapp.ListJobs{All: q.Get("all") == "true", Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/exports/run-next", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.RunNext.Handle(r.Context(), eapp.RunNext{})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/exports/purge", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Purge.Handle(r.Context(), eapp.Purge{})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/exports/{id}", job(func(r *http.Request, id domain.JobID) (any, error) {
		return svc.Get.Handle(r.Context(), eapp.GetJob{ID: id})
	}))
	mux.HandleFunc("POST /api/exports/{id}/cancel", job(func(r *http.Request, id domain.JobID) (any, error) {
		return svc.Cancel.Handle(r.Context(), eapp.CancelJob{ID: id})
	}))
	mux.HandleFunc("GET /api/exports/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseJobID(r.PathValue("id"))
		if err != nil || id.IsZero() {
			distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
			return
		}
		f, err := svc.Download.Handle(r.Context(), eapp.DownloadJob{ID: id})
		if err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		defer f.Content.Close()
		w.Header().Set("Content-Type", f.ContentType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
		w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
		w.Header().Set("Cache-Control", "no-store") // personal data: nobody in between keeps it
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = io.Copy(w, f.Content)
	})
}

var _ distribution.EndpointModule = (*Module)(nil)
