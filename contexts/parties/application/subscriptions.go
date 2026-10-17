package application

import (
	"context"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// EmployeeTerminated es la copia en Parties de hr.employee-terminated.v1: sólo los campos que
// necesita.
type EmployeeTerminated struct {
	Person     string `json:"person"`
	Employer   string `json:"employer"`
	Terminated string `json:"terminated"` // fecha civil: el último día trabajado
}

// IntegrationEventType implementa application.IntegrationEvent.
func (EmployeeTerminated) IntegrationEventType() string { return "hr.employee-terminated.v1" }

// subscriber es la identidad que usa Parties cuando reacciona a otros contextos.
var subscriber = fw.MustParseUUID("10000000-0000-0000-00ff-000000000001")

// systemContext autoriza una reacción: un actor de servicio con sólo el permiso que necesita y
// sin límite de ámbito de organización (el hecho viene de un contexto de confianza, no de un
// usuario).
func systemContext(ctx context.Context, name string, perms ...authz.Permission) (context.Context, error) {
	ac, err := authz.NewContext(authz.Context{Subject: subscriber, SubjectName: name, Kind: authz.Service, ActorPartyID: subscriber,
		Permissions: perms})
	if err != nil {
		return nil, err
	}
	ac.GlobalAdmin = true
	return authz.WithContext(ctx, ac), nil
}

// Subscribe registra en c las reacciones de Parties a otros contextos (decisión aprobada 4 de
// docs/RRHH.md): cuando HR da por terminado un empleo, la relación Employment entre la persona y
// el empleador termina al final del último día trabajado, y con ella la afiliación.
// Las reentregas no hacen daño: una relación ya terminada no se vuelve a encontrar.
func Subscribe(c *messaging.Consumer, svc *Service, relationships domain.RelationshipRepository) {
	messaging.Handle(c, func(ctx context.Context, e EmployeeTerminated, _ app.Envelope) error {
		person, err1 := domain.ParsePartyID(e.Person)
		employer, err2 := domain.ParsePartyID(e.Employer)
		last, err3 := vocab.ParseDate(e.Terminated)
		if err1 != nil || err2 != nil || err3 != nil {
			return fw.Violation("parties.invalid_event", "hr.employee-terminated.v1 without valid person, employer or date")
		}
		at := last.AddDays(1).BaseTime()
		pair := spec.Or(domain.RelFieldFrom.Eq(person).And(domain.RelFieldTo.Eq(employer)),
			domain.RelFieldTo.Eq(person).And(domain.RelFieldFrom.Eq(employer)))
		rs, err := relationships.Find(ctx, spec.And(domain.RelFieldType.Eq(domain.RelEmployment), pair,
			domain.RelFieldUntil.IsNull().Or(domain.RelFieldUntil.After(at))))
		if err != nil {
			return err
		}
		sys, err := systemContext(ctx, "parties:hr-subscription", PermRelationshipEnd)
		if err != nil {
			return err
		}
		for _, r := range rs {
			end := at
			if !end.After(r.Since()) {
				end = fw.Now() // afiliada después del último día trabajado: termina cuando HR comunica el hecho
			}
			if _, err := svc.TerminateRelationship.Handle(sys, TerminateRelationship{ID: r.ID(), At: timePtr(end)}); err != nil {
				return err
			}
		}
		return nil
	})
}

func timePtr(t time.Time) *time.Time { return &t }
