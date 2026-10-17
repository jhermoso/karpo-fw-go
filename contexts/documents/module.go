// Package documents compone el contexto delimitado Documents (Documentos: registro de documentos
// emitidos) sobre un backend intercambiable en caliente.
package documents

import (
	"fmt"
	"net/http"
	"strconv"

	dapp "github.com/jhermoso/karpo-fw-go/contexts/documents/application"
	"github.com/jhermoso/karpo-fw-go/contexts/documents/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/documents/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/documents/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module es el contexto compuesto.
type Module struct {
	Service *dapp.Service
	Audit   application.AuditLog
	// Consumer recibe los documentos que emiten los otros contextos: hay que suscribirlo al transporte.
	Consumer *messaging.Consumer
	// Register es el puerto que usan otros contextos para resolver el documento de un hecho.
	Register contracts.Register
}

// Compose construye el contexto sobre sw.
func Compose(sw *hotswap.Switch) *Module {
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := dapp.Deps{Documents: hotswap.Repository(sw, infrastructure.DocumentRepositoryFactory), UoW: sw, Audit: audit}
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	dapp.Subscribe(consumer, d)
	return &Module{Service: dapp.NewService(d), Audit: audit, Consumer: consumer, Register: dapp.Register{Documents: d.Documents}}
}

func badID(w http.ResponseWriter, r *http.Request) {
	distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
}

// RegisterRoutes implementa distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service

	mux.HandleFunc("GET /api/documents", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.Search.Handle(r.Context(), dapp.SearchDocuments{Company: q.Get("company"), Type: q.Get("type"), Party: q.Get("party"),
			Number: q.Get("number"), From: q.Get("from"), To: q.Get("to"), Live: q.Get("live") == "true", Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/documents/by-fact/{type}/{factId}", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Get.Handle(r.Context(), dapp.GetDocument{Type: r.PathValue("type"), FactID: r.PathValue("factId")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/documents/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseDocumentID(r.PathValue("id"))
		if err != nil || id.IsZero() {
			badID(w, r)
			return
		}
		out, err := svc.Get.Handle(r.Context(), dapp.GetDocument{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/documents/{id}/trail", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseDocumentID(r.PathValue("id"))
		if err != nil || id.IsZero() {
			badID(w, r)
			return
		}
		out, err := svc.Trail.Handle(r.Context(), dapp.GetTrail{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
}

var _ distribution.EndpointModule = (*Module)(nil)
