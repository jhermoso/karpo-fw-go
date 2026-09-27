package distribution

import (
	"net/http"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
)

func (m *Module) registerPhase3(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/parties/{id}/legal-form", m.setLegalForm)
	mux.HandleFunc("PUT /api/parties/{id}/shared", m.setShared)
	mux.HandleFunc("GET /api/internal-organizations", m.internalOrganizations)
	mux.HandleFunc("POST /api/parties/{id}/facility-roles", m.assignFacilityRole)
	mux.HandleFunc("POST /api/parties/{id}/facility-roles/{roleId}/end", m.endFacilityRole)
	mux.HandleFunc("GET /api/catalogs/facility-role-types", m.facilityRoleTypes)
}

func (m *Module) assignFacilityRole(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.AssignFacilityRole{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.AssignFacilityRole) (papp.PartyDTO, error) {
		c.PartyID = id
		return m.svc.AssignFacilityRole.Handle(r.Context(), c)
	})
}

func (m *Module) endFacilityRole(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	roleID, err := pathChild(r, "roleId", domain.ParseFacilityRoleID)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.EndFacilityRole{}
	handle(w, r, &c, r.ContentLength > 0, http.StatusOK, func(c papp.EndFacilityRole) (papp.PartyDTO, error) {
		c.PartyID, c.RoleID = id, roleID
		return m.svc.EndFacilityRole.Handle(r.Context(), c)
	})
}

func (m *Module) facilityRoleTypes(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.ListFacilityRoleTypes.Handle(r.Context(), papp.ListFacilityRoleTypes{})
	distribution.Respond(w, r, out, err, http.StatusOK)
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
