package distribution

import (
	"net/http"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
)

func (m *Module) registerPhase3(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/parties/{id}/legal-form", m.setLegalForm)
	mux.HandleFunc("PUT /api/parties/{id}/shared", m.setShared)
	mux.HandleFunc("GET /api/internal-organizations", m.internalOrganizations)
}

func (m *Module) setLegalForm(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetLegalForm{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetLegalForm) (papp.PartyDTO, error) {
		c.ID = id
		return m.svc.SetLegalForm.Handle(r.Context(), c)
	})
}

func (m *Module) setShared(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetShared{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetShared) (papp.PartyDTO, error) {
		c.ID = id
		return m.svc.SetShared.Handle(r.Context(), c)
	})
}

func (m *Module) internalOrganizations(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.InternalOrganizations.Handle(r.Context(), papp.ListInternalOrganizations{})
	distribution.Respond(w, r, out, err, http.StatusOK)
}
