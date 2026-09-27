// Package distribution exposes the Parties use cases over HTTP (REST + RFC 9457 problems). The
// routes follow the C# publisher where the concept survived (/api/parties, /api/party-relationships,
// /api/catalogs/..., /api/parties/directory/...).
package distribution

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Module is the Parties HTTP endpoint module.
type Module struct {
	svc       *papp.Service
	directory contracts.Directory
}

// NewModule creates the endpoint module.
func NewModule(svc *papp.Service, directory contracts.Directory) *Module {
	return &Module{svc: svc, directory: directory}
}

var _ distribution.EndpointModule = (*Module)(nil)

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/persons", m.registerPerson)
	mux.HandleFunc("POST /api/organizations", m.registerOrganization)
	mux.HandleFunc("GET /api/parties", m.search)
	mux.HandleFunc("GET /api/parties/{id}", m.get)
	mux.HandleFunc("PUT /api/parties/{id}/name", m.rename)
	mux.HandleFunc("PUT /api/parties/{id}/active", m.setActive)
	mux.HandleFunc("POST /api/parties/{id}/roles", m.assignRole)
	mux.HandleFunc("POST /api/parties/{id}/roles/{roleId}/end", m.endRole)
	mux.HandleFunc("GET /api/parties/{id}/relationships", m.relationships)
	mux.HandleFunc("POST /api/party-relationships", m.establish)
	mux.HandleFunc("POST /api/party-relationships/{id}/terminate", m.terminate)
	mux.HandleFunc("GET /api/catalogs/party-role-types", m.roleTypes)
	mux.HandleFunc("GET /api/catalogs/party-relationship-types", m.relationshipTypes)
	mux.Handle("POST /api/parties/directory/resolve", distribution.RequirePermission(papp.PermPartyRead, http.HandlerFunc(m.resolve)))
	mux.Handle("GET /api/parties/directory/search-ids", distribution.RequirePermission(papp.PermPartyRead, http.HandlerFunc(m.searchIDs)))
	m.registerPhase2(mux)
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

func pathParty(r *http.Request) (domain.PartyID, error) {
	id, err := domain.ParsePartyID(r.PathValue("id"))
	if err != nil {
		return domain.PartyID{}, fmt.Errorf("%w: invalid party id", fw.ErrValidation)
	}
	return id, nil
}

// handle decodes an optional body into cmd, runs fn and writes the response.
func handle[C, R any](w http.ResponseWriter, r *http.Request, cmd *C, body bool, status int, fn func(C) (R, error)) {
	if body {
		if err := decode(r, cmd); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
	}
	res, err := fn(*cmd)
	distribution.Respond(w, r, res, err, status)
}

func (m *Module) registerPerson(w http.ResponseWriter, r *http.Request) {
	var c papp.RegisterPerson
	handle(w, r, &c, true, http.StatusCreated, func(c papp.RegisterPerson) (papp.PartyDTO, error) {
		c.RequestID = r.Header.Get("Idempotency-Key")
		dto, err := m.svc.RegisterPerson.Handle(r.Context(), c)
		if err == nil {
			w.Header().Set("Location", "/api/parties/"+dto.ID)
		}
		return dto, err
	})
}

func (m *Module) registerOrganization(w http.ResponseWriter, r *http.Request) {
	var c papp.RegisterOrganization
	handle(w, r, &c, true, http.StatusCreated, func(c papp.RegisterOrganization) (papp.PartyDTO, error) {
		c.RequestID = r.Header.Get("Idempotency-Key")
		dto, err := m.svc.RegisterOrganization.Handle(r.Context(), c)
		if err == nil {
			w.Header().Set("Location", "/api/parties/"+dto.ID)
		}
		return dto, err
	})
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	dto, err := m.svc.Get.Handle(r.Context(), papp.GetParty{ID: id})
	distribution.Respond(w, r, dto, err, http.StatusOK)
}

func (m *Module) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	page, err := m.svc.Search.Handle(r.Context(), papp.SearchParties{Text: q.Get("q"), Kind: q.Get("kind"), Role: q.Get("role"),
		Document: q.Get("document"), Classification: q.Get("classification"),
		ActiveOnly: q.Get("active") == "true", Page: atoi("page"), Size: atoi("size")})
	distribution.Respond(w, r, page, err, http.StatusOK)
}

func (m *Module) rename(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.RenameParty{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.RenameParty) (papp.PartyDTO, error) {
		c.ID = id
		return m.svc.Rename.Handle(r.Context(), c)
	})
}

func (m *Module) setActive(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetPartyActive{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetPartyActive) (papp.PartyDTO, error) {
		c.ID = id
		return m.svc.SetActive.Handle(r.Context(), c)
	})
}

func (m *Module) assignRole(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.AssignRole{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.AssignRole) (papp.PartyDTO, error) {
		c.PartyID = id
		return m.svc.AssignRole.Handle(r.Context(), c)
	})
}

func (m *Module) endRole(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	roleID, err := domain.ParsePartyRoleID(r.PathValue("roleId"))
	if err != nil {
		distribution.WriteError(w, r, fmt.Errorf("%w: invalid role id", fw.ErrValidation))
		return
	}
	c := papp.EndRole{}
	handle(w, r, &c, r.ContentLength > 0, http.StatusOK, func(c papp.EndRole) (papp.PartyDTO, error) {
		c.PartyID, c.RoleID = id, roleID
		return m.svc.EndRole.Handle(r.Context(), c)
	})
}

func (m *Module) relationships(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	rs, err := m.svc.Relationships.Handle(r.Context(), papp.PartyRelationships{PartyID: id, ActiveOnly: r.URL.Query().Get("active") == "true"})
	distribution.Respond(w, r, rs, err, http.StatusOK)
}

func (m *Module) establish(w http.ResponseWriter, r *http.Request) {
	c := papp.EstablishRelationship{}
	handle(w, r, &c, true, http.StatusCreated, func(c papp.EstablishRelationship) (papp.RelationshipDTO, error) {
		return m.svc.EstablishRelationship.Handle(r.Context(), c)
	})
}

func (m *Module) terminate(w http.ResponseWriter, r *http.Request) {
	id, err := domain.ParseRelationshipID(r.PathValue("id"))
	if err != nil {
		distribution.WriteError(w, r, fmt.Errorf("%w: invalid relationship id", fw.ErrValidation))
		return
	}
	c := papp.TerminateRelationship{}
	handle(w, r, &c, r.ContentLength > 0, http.StatusOK, func(c papp.TerminateRelationship) (papp.RelationshipDTO, error) {
		c.ID = id
		return m.svc.TerminateRelationship.Handle(r.Context(), c)
	})
}

func (m *Module) roleTypes(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.ListRoleTypes.Handle(r.Context(), papp.ListRoleTypes{})
	distribution.Respond(w, r, out, err, http.StatusOK)
}

func (m *Module) relationshipTypes(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.ListRelationshipTypes.Handle(r.Context(), papp.ListRelationshipTypes{})
	distribution.Respond(w, r, out, err, http.StatusOK)
}

func (m *Module) resolve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PartyIDs []string `json:"partyIds"`
	}
	if err := decode(r, &body); err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	refs, err := m.directory.Resolve(r.Context(), body.PartyIDs)
	list := make([]contracts.PartyRef, 0, len(refs))
	for _, ref := range refs {
		list = append(list, ref)
	}
	distribution.Respond(w, r, list, err, http.StatusOK)
}

func (m *Module) searchIDs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	ids, err := m.directory.SearchIDsByName(r.Context(), r.URL.Query().Get("q"), limit)
	distribution.Respond(w, r, ids, err, http.StatusOK)
}
