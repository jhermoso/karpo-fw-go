package distribution

import (
	"fmt"
	"net/http"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// registerDetails monta las rutas que cambian los detalles que lleva una relación por su tipo
// (docs/PARTIES-UDM.md).
func (m *Module) registerDetails(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/party-relationships/{id}/trial", m.setProspectTrial)
	mux.HandleFunc("PUT /api/party-relationships/{id}/ownership", m.setOwnershipShare)
	mux.HandleFunc("PUT /api/party-relationships/{id}/promotion-code", m.setPromotionCode)
	mux.HandleFunc("PUT /api/party-relationships/{id}/customer-number", m.setCustomerNumber)
}

func (m *Module) setCustomerNumber(w http.ResponseWriter, r *http.Request) {
	id, err := pathRelationship(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetCustomerNumber{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetCustomerNumber) (papp.RelationshipDTO, error) {
		c.ID = id
		return m.svc.SetCustomerNumber.Handle(r.Context(), c)
	})
}

func (m *Module) setPromotionCode(w http.ResponseWriter, r *http.Request) {
	id, err := pathRelationship(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetPromotionCode{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetPromotionCode) (papp.RelationshipDTO, error) {
		c.ID = id
		return m.svc.SetPromotionCode.Handle(r.Context(), c)
	})
}

func (m *Module) setOwnershipShare(w http.ResponseWriter, r *http.Request) {
	id, err := pathRelationship(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetOwnershipShare{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetOwnershipShare) (papp.RelationshipDTO, error) {
		c.ID = id
		return m.svc.SetOwnershipShare.Handle(r.Context(), c)
	})
}

func pathRelationship(r *http.Request) (domain.RelationshipID, error) {
	id, err := domain.ParseRelationshipID(r.PathValue("id"))
	if err != nil {
		return domain.RelationshipID{}, fmt.Errorf("%w: invalid relationship id", fw.ErrValidation)
	}
	return id, nil
}

func (m *Module) setProspectTrial(w http.ResponseWriter, r *http.Request) {
	id, err := pathRelationship(r)
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	c := papp.SetProspectTrial{}
	handle(w, r, &c, true, http.StatusOK, func(c papp.SetProspectTrial) (papp.RelationshipDTO, error) {
		c.ID = id
		return m.svc.SetProspectTrial.Handle(r.Context(), c)
	})
}
