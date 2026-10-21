package application

import (
	"context"
	"errors"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// RelationshipTransferred es la copia de Purchases de parties.relationship-transferred.v1: sólo
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

// Subscribe registra en c las reacciones de Purchases a otros contextos: cuando la relación de un
// proveedor con una empresa pasa a otra party al fusionar fichas (docs/DECISIONES-COMUNES.md,
// D-93), el perfil de proveedor de (empresa, la que cede) se copia a (empresa, la que recibe) si
// ésta no tiene perfil con esa empresa. El de la que cede no se toca. Copiar a la superviviente no
// es operar con el duplicado (D-64, D-70). Al deshacer la fusión (Returning) no se copia nada: el
// perfil de la superviviente es suyo, y la que vuelve conserva el que tenía. Las reentregas no
// hacen daño.
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newService(d)
	messaging.Handle(c, func(ctx context.Context, e RelationshipTransferred, _ app.Envelope) error {
		if e.TypeCode != "supplier" || e.Returning {
			return nil
		}
		company, err1 := fw.ParseUUID(e.Organization)
		ceding, err2 := fw.ParseUUID(e.CedingParty)
		receiving, err3 := fw.ParseUUID(e.ReceivingParty)
		if err := errors.Join(err1, err2, err3); err != nil {
			return fw.Violation("purchases.invalid_event", "parties.relationship-transferred.v1: "+err.Error())
		}
		org := domain.OrganizationID{UUID: company}
		profile := func(supplier fw.UUID) ([]*domain.SupplierProfile, error) {
			return s.Suppliers.Find(ctx, spec.And(domain.SupFieldCompany.Eq(org), domain.SupFieldSupplier.Eq(domain.PartyID{UUID: supplier})))
		}
		from, err := profile(ceding)
		if err != nil || len(from) == 0 {
			return err
		}
		if has, err := profile(receiving); err != nil || len(has) > 0 {
			return err
		}
		st := from[0].State()
		st.Supplier, st.Audit = domain.PartyID{UUID: receiving}, traits.AuditStamp{}
		p, err := domain.ReconstituteSupplier(domain.NewSupplierID(), st)
		if err != nil {
			return err
		}
		return s.suppliers.Create(ctx, p)
	})
}
