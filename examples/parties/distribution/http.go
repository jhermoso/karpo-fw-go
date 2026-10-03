// Package distribution exposes the Parties use cases over HTTP (REST + RFC 9457 problems).
package distribution

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	papp "github.com/jhermoso/karpo-fw-go/examples/parties/application"
	"github.com/jhermoso/karpo-fw-go/examples/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Module is the Parties HTTP endpoint module.
type Module struct {
	svc *papp.Service
}

// NewModule creates the endpoint module.
func NewModule(svc *papp.Service) *Module { return &Module{svc: svc} }

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /parties", m.register)
	mux.HandleFunc("GET /parties", m.search)
	mux.HandleFunc("GET /parties/{id}", m.get)
	mux.HandleFunc("PUT /parties/{id}/legal-name", m.rename)
	mux.HandleFunc("POST /parties/{id}/contacts", m.addContact)
}

var _ distribution.EndpointModule = (*Module)(nil)

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

func pathID(r *http.Request) (domain.PartyID, error) {
	id, err := domain.ParsePartyID(r.PathValue("id"))
	if err != nil {
		return domain.PartyID{}, fmt.Errorf("%w: %v", fw.ErrValidation, err)
	}
	return id, nil
}

func (m *Module) register(w http.ResponseWriter, r *http.Request) {
	var cmd papp.RegisterParty
	if err := decode(r, &cmd); err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	cmd.RequestID = r.Header.Get("Idempotency-Key")
	dto, err := m.svc.Register.Handle(r.Context(), cmd)
	if err == nil {
		w.Header().Set("Location", "/parties/"+dto.ID.String())
	}
	distribution.Respond(w, r, dto, err, http.StatusCreated)
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	dto, err := m.svc.Get.Handle(r.Context(), papp.GetParty{ID: id})
	distribution.Respond(w, r, dto, err, http.StatusOK)
}

func (m *Module) rename(w http.ResponseWriter, r *http.Request) {
	var cmd papp.RenameParty
	id, err := pathID(r)
	if err == nil {
		err = decode(r, &cmd)
	}
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	cmd.ID = id
	dto, err := m.svc.Rename.Handle(r.Context(), cmd)
	distribution.Respond(w, r, dto, err, http.StatusOK)
}

func (m *Module) addContact(w http.ResponseWriter, r *http.Request) {
	var cmd papp.AddContact
	id, err := pathID(r)
	if err == nil {
		err = decode(r, &cmd)
	}
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	cmd.ID = id
	dto, err := m.svc.AddContact.Handle(r.Context(), cmd)
	distribution.Respond(w, r, dto, err, http.StatusOK)
}

func (m *Module) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	query := papp.SearchParties{
		Text:        q.Get("q"),
		ActiveOnly:  q.Get("active") == "true",
		ReachableBy: q.Get("reachableBy"),
		MinContacts: atoi("minContacts"),
		Page:        atoi("page"),
		Size:        atoi("size"),
	}
	page, err := m.svc.Search.Handle(r.Context(), query)
	distribution.Respond(w, r, page, err, http.StatusOK)
}
