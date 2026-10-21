package application

import (
	"context"
	"errors"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// RelationshipTransferred es la copia de Orders de parties.relationship-transferred.v1: sólo los
// campos que necesita.
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

// SubscribeParties copia las condiciones de venta de un cliente cuando su relación con la empresa
// pasa a otra party al fusionar fichas (docs/DECISIONES-COMUNES.md, D-93): tarifa, descuento y
// bloqueos de (empresa, la que cede) pasan a (empresa, la que recibe) si ésta no tiene condiciones
// con esa empresa. Las de la que cede no se tocan. Copiar a la superviviente no es operar con el
// duplicado (D-64, D-70). Al deshacer la fusión (Returning) no se copia nada: el perfil de la
// superviviente es suyo, y la que vuelve conserva el que tenía. Las reentregas no hacen daño.
func SubscribeParties(c *messaging.Consumer, d Deps) {
	s := newService(d)
	messaging.Handle(c, func(ctx context.Context, e RelationshipTransferred, _ app.Envelope) error {
		if e.TypeCode != "customer" || e.Returning {
			return nil
		}
		company, err1 := fw.ParseUUID(e.Organization)
		ceding, err2 := fw.ParseUUID(e.CedingParty)
		receiving, err3 := fw.ParseUUID(e.ReceivingParty)
		if err := errors.Join(err1, err2, err3); err != nil {
			return fw.Violation("orders.invalid_event", "parties.relationship-transferred.v1: "+err.Error())
		}
		org := domain.OrganizationID{UUID: company}
		terms := func(customer fw.UUID) ([]*domain.Terms, error) {
			return s.Terms.Find(ctx, spec.And(domain.TrmFieldCompany.Eq(org), domain.TrmFieldCustomer.Eq(domain.PartyID{UUID: customer})))
		}
		from, err := terms(ceding)
		if err != nil || len(from) == 0 {
			return err
		}
		if has, err := terms(receiving); err != nil || len(has) > 0 {
			return err
		}
		st := from[0].State()
		st.Customer, st.Audit = domain.PartyID{UUID: receiving}, traits.AuditStamp{}
		t, err := domain.ReconstituteTerms(domain.NewTermsID(), st)
		if err != nil {
			return err
		}
		return s.terms.Create(ctx, t)
	})
}
