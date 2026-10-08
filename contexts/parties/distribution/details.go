package distribution

import (
	"fmt"
	"net/http"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// registerDetails mounts the routes that change the details a relationship carries because of
// its type (docs/PARTIES-UDM.md).
func (m *Module) registerDetails(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/party-relationships/{id}/trial", m.setProspectTrial)
	mux.HandleFunc("PUT /api/party-relationships/{id}/ownership", m.setOwnershipShare)
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
