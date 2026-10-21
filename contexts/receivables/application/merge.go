package application

import (
	"context"
	"errors"

	"github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// RelationshipTransferred es la copia de Receivables de parties.relationship-transferred.v1: sólo
// los campos que necesita.
type RelationshipTransferred struct {
	TypeCode       string `json:"typeCode"`
	Organization   string `json:"organization"`
	CedingParty    string `json:"cedingParty"`
	ReceivingParty string `json:"receivingParty"`
	Returning      bool   `json:"returning"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (RelationshipTransferred) IntegrationEventType() string {
	return "parties.relationship-transferred.v1"
}

// SubscribeParties copia el perfil de crédito de un cliente cuando su relación con el vendedor
// pasa a otra party al fusionar fichas (docs/DECISIONES-COMUNES.md, D-93): condiciones, límite y
// bloqueo de (vendedor, la que cede) pasan a (vendedor, la que recibe) si ésta no tiene perfil con
// ese vendedor. El perfil de la que cede no se toca. Copiar a la superviviente no es operar con el
// duplicado (D-64, D-70). Al deshacer la fusión (Returning) no se copia nada: el perfil de la
// superviviente es suyo, y la que vuelve conserva el que tenía. Las reentregas no hacen daño.
func SubscribeParties(c *messaging.Consumer, d Deps) {
	s := newOrchestrators(d)
	messaging.Handle(c, func(ctx context.Context, e RelationshipTransferred, _ app.Envelope) error {
		if e.TypeCode != "customer" || e.Returning {
			return nil
		}
		seller, err1 := fw.ParseUUID(e.Organization)
		ceding, err2 := fw.ParseUUID(e.CedingParty)
		receiving, err3 := fw.ParseUUID(e.ReceivingParty)
		if err := errors.Join(err1, err2, err3); err != nil {
			return fw.Violation("receivables.invalid_event", "parties.relationship-transferred.v1: "+err.Error())
		}
		org := domain.OrganizationID{UUID: seller}
		profile := func(customer fw.UUID) ([]*domain.CreditProfile, error) {
			return s.Credit.Find(ctx, domain.CredFieldSeller.Eq(org).And(domain.CredFieldCustomer.Eq(domain.PartyID{UUID: customer})))
		}
		from, err := profile(ceding)
		if err != nil || len(from) == 0 {
			return err
		}
		if has, err := profile(receiving); err != nil || len(has) > 0 {
			return err
		}
		st := from[0].State()
		st.Customer, st.Audit = domain.PartyID{UUID: receiving}, traits.AuditStamp{}
		p, err := domain.ReconstituteCreditProfile(domain.NewCreditProfileID(), st)
		if err != nil {
			return err
		}
		return s.credit.Create(ctx, p)
	})
}
