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
	mux.HandleFunc("PUT /api/parties/{id}/document-issue", m.flagDocumentIssue)
	mux.HandleFunc("DELETE /api/parties/{id}/document-issue", m.clearDocumentIssue)
	mux.HandleFunc("GET /api/internal-organizations", m.internalOrganizations)
	mux.HandleFunc("GET /api/internal-organizations/names", m.internalOrganizationNames)
	// El perfil de la empresa interna (D-133), en la ruta de Karpo C#; {id} es la party de la
	// organización interna, el id que da GET /api/internal-organizations (docs/PARTIES.md §25).
	mux.HandleFunc("GET /api/profiles/internal-organizations/{id}", m.internalOrganizationProfile)
	mux.HandleFunc("PUT /api/profiles/internal-organizations/{id}", m.setInternalOrganizationProfile)
	mux.HandleFunc("GET /api/profiles/internal-organizations/{id}/code-patterns/{codeKind}", m.internalOrganizationCodePattern)
	mux.HandleFunc("PUT /api/profiles/internal-organizations/{id}/code-patterns/{codeKind}", m.setInternalOrganizationCodePattern)
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

func (m *Module) flagDocumentIssue(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.FlagDocumentIssue{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.FlagDocumentIssue) (papp.PartyDTO, error) {
		c.ID = id
		return m.svc.FlagDocumentIssue.Handle(r.Context(), c)
	})
}

func (m *Module) clearDocumentIssue(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	dto, err := m.svc.ClearDocumentIssue.Handle(r.Context(), papp.ClearDocumentIssue{ID: id})
	distribution.Respond(w, r, dto, err, http.StatusOK)
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

func (m *Module) internalOrganizationProfile(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	out, err := m.svc.InternalOrganizationProfile.Handle(r.Context(), papp.GetInternalOrganizationProfile{ID: id})
	distribution.Respond(w, r, out, err, http.StatusOK)
}

func (m *Module) setInternalOrganizationProfile(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetInternalOrganizationProfile{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetInternalOrganizationProfile) (papp.InternalOrganizationProfileDTO, error) {
		c.ID = id
		return m.svc.SetInternalOrganizationProfile.Handle(r.Context(), c)
	})
}

func (m *Module) internalOrganizationCodePattern(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	out, err := m.svc.InternalOrganizationCodePattern.Handle(r.Context(), papp.GetInternalOrganizationCodePattern{ID: id, Kind: r.PathValue("codeKind")})
	distribution.Respond(w, r, out, err, http.StatusOK)
}

func (m *Module) setInternalOrganizationCodePattern(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetInternalOrganizationCodePattern{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetInternalOrganizationCodePattern) (papp.CodePatternDTO, error) {
		c.ID, c.Kind = id, r.PathValue("codeKind")
		return m.svc.SetInternalOrganizationCodePattern.Handle(r.Context(), c)
	})
}

func (m *Module) internalOrganizationNames(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.InternalOrganizationNames.Handle(r.Context(), papp.ListInternalOrganizationNames{})
	distribution.Respond(w, r, out, err, http.StatusOK)
}
