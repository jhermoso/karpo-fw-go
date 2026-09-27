package distribution

import (
	"fmt"
	"net/http"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

func (m *Module) registerPhase2(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/parties/{id}/identifications", m.addIdentification)
	mux.HandleFunc("DELETE /api/parties/{id}/identifications/{identId}", m.removeIdentification)
	mux.HandleFunc("POST /api/parties/{id}/contacts", m.addContact)
	mux.HandleFunc("PUT /api/parties/{id}/contacts/{contactId}/purposes", m.setContactPurposes)
	mux.HandleFunc("POST /api/parties/{id}/contacts/{contactId}/end", m.endContact)
	mux.HandleFunc("POST /api/parties/{id}/classifications", m.classify)
	mux.HandleFunc("POST /api/parties/{id}/classifications/{classId}/end", m.endClassification)
	mux.HandleFunc("GET /api/catalogs/document-types", m.documentOptions)
	mux.HandleFunc("GET /api/catalogs/party-classifications", m.classificationTypes)
}

func pathChild[T any](r *http.Request, name string, parse func(string) (T, error)) (T, error) {
	v, err := parse(r.PathValue(name))
	if err != nil {
		var zero T
		return zero, fmt.Errorf("%w: invalid %s", fw.ErrValidation, name)
	}
	return v, nil
}

func (m *Module) addIdentification(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.AddIdentification{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.AddIdentification) (papp.PartyDTO, error) {
		c.PartyID = id
		return m.svc.AddIdentification.Handle(r.Context(), c)
	})
}

func (m *Module) removeIdentification(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	identID, err := pathChild(r, "identId", domain.ParseIdentificationID)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	dto, err := m.svc.RemoveIdentification.Handle(r.Context(), papp.RemoveIdentification{PartyID: id, ID: identID})
	distribution.Respond(w, r, dto, err, http.StatusOK)
}

func (m *Module) addContact(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.AddContact{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.AddContact) (papp.PartyDTO, error) {
		c.PartyID = id
		return m.svc.AddContact.Handle(r.Context(), c)
	})
}

func (m *Module) setContactPurposes(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	contactID, err := pathChild(r, "contactId", domain.ParseContactID)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetContactPurposes{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetContactPurposes) (papp.PartyDTO, error) {
		c.PartyID, c.ContactID = id, contactID
		return m.svc.SetContactPurposes.Handle(r.Context(), c)
	})
}

func (m *Module) endContact(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	contactID, err := pathChild(r, "contactId", domain.ParseContactID)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.EndContact{}
	handle(w, r, &c, r.ContentLength > 0, http.StatusOK, func(c papp.EndContact) (papp.PartyDTO, error) {
		c.PartyID, c.ContactID = id, contactID
		return m.svc.EndContact.Handle(r.Context(), c)
	})
}

func (m *Module) classify(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.Classify{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.Classify) (papp.PartyDTO, error) {
		c.PartyID = id
		return m.svc.Classify.Handle(r.Context(), c)
	})
}

func (m *Module) endClassification(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	classID, err := pathChild(r, "classId", domain.ParseClassificationID)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.EndClassification{}
	handle(w, r, &c, r.ContentLength > 0, http.StatusOK, func(c papp.EndClassification) (papp.PartyDTO, error) {
		c.PartyID, c.ID = id, classID
		return m.svc.EndClassification.Handle(r.Context(), c)
	})
}

func (m *Module) documentOptions(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.DocumentOptions.Handle(r.Context(), papp.DocumentOptions{Country: r.URL.Query().Get("country")})
	distribution.Respond(w, r, out, err, http.StatusOK)
}

func (m *Module) classificationTypes(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.ListClassificationTypes.Handle(r.Context(), papp.ListClassificationTypes{})
	distribution.Respond(w, r, out, err, http.StatusOK)
}
