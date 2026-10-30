package distribution

import (
	"net/http"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
)

// registerIdentity monta las rutas de identidad y perfil de cada empresa (D-122, docs/PARTIES.md §23).
func (m *Module) registerIdentity(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/parties/{id}/language", m.setLanguage)
	mux.HandleFunc("PUT /api/parties/{id}/identifications/{identId}/primary", m.markPrimaryIdentification)
	mux.HandleFunc("PUT /api/party-relationships/{id}/supplier-number", m.setSupplierNumber)
	mux.HandleFunc("PUT /api/party-relationships/{id}/external-codes", m.setExternalCodes)
	mux.HandleFunc("POST /api/party-relationships/{id}/data-protection-consents", m.recordConsent)
	mux.HandleFunc("POST /api/party-relationships/{id}/data-protection-consents/{consentId}/withdraw", m.withdrawConsent)
}

func (m *Module) setLanguage(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetPreferredLanguage{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetPreferredLanguage) (papp.PartyDTO, error) {
		c.ID = id
		return m.svc.SetLanguage.Handle(r.Context(), c)
	})
}

func (m *Module) markPrimaryIdentification(w http.ResponseWriter, r *http.Request) {
	id, err := pathParty(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	ident, err := pathChild(r, "identId", domain.ParseIdentificationID)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	p, err := m.svc.MarkPrimary.Handle(r.Context(), papp.MarkPrimaryIdentification{PartyID: id, ID: ident})
	distribution.Respond(w, r, p, err, http.StatusOK)
}

func (m *Module) setSupplierNumber(w http.ResponseWriter, r *http.Request) {
	id, err := pathRelationship(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetSupplierNumber{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetSupplierNumber) (papp.RelationshipDTO, error) {
		c.ID = id
		return m.svc.SetSupplierNumber.Handle(r.Context(), c)
	})
}

func (m *Module) setExternalCodes(w http.ResponseWriter, r *http.Request) {
	id, err := pathRelationship(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetExternalCodes{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetExternalCodes) (papp.RelationshipDTO, error) {
		c.ID = id
		return m.svc.SetExternalCodes.Handle(r.Context(), c)
	})
}

func (m *Module) recordConsent(w http.ResponseWriter, r *http.Request) {
	id, err := pathRelationship(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.RecordConsent{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.RecordConsent) (papp.RelationshipDTO, error) {
		c.ID = id
		return m.svc.RecordConsent.Handle(r.Context(), c)
	})
}

func (m *Module) withdrawConsent(w http.ResponseWriter, r *http.Request) {
	id, err := pathRelationship(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	consent, err := pathChild(r, "consentId", domain.ParseConsentID)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.WithdrawConsent{}
	handle(w, r, &c, r.ContentLength > 0, http.StatusOK, func(c papp.WithdrawConsent) (papp.RelationshipDTO, error) {
		c.ID, c.ConsentID = id, consent
		return m.svc.WithdrawConsent.Handle(r.Context(), c)
	})
}
