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

// EmployeeTerminated is the Parties copy of hr.employee-terminated.v1: only the fields it needs.
type EmployeeTerminated struct {
	Person     string `json:"person"`
	Employer   string `json:"employer"`
	Terminated string `json:"terminated"` // civil date: the last working day
}

// IntegrationEventType implements application.IntegrationEvent.
func (EmployeeTerminated) IntegrationEventType() string { return "hr.employee-terminated.v1" }

// subscriber is the identity Parties uses when it reacts to other contexts.
var subscriber = fw.MustParseUUID("10000000-0000-0000-00ff-000000000001")

// systemContext authorizes a reaction: a service actor with only the permission it needs and no
// organization scope limit (the fact comes from a trusted context, not from a user).
func systemContext(ctx context.Context, name string, perms ...authz.Permission) (context.Context, error) {
	ac, err := authz.NewContext(authz.Context{Subject: subscriber, SubjectName: name, Kind: authz.Service, ActorPartyID: subscriber,
		Permissions: perms})
	if err != nil {
		return nil, err
	}
	ac.GlobalAdmin = true
	return authz.WithContext(ctx, ac), nil
}

// Subscribe registers the reactions of Parties to other contexts on c (approved decision 4 of
// docs/RRHH.md): when HR terminates an employment, the Employment relationship between the person
// and the employer ends at the end of the last working day, and with it the affiliation.
// Redeliveries are harmless: an already ended relationship is not found again.
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
				end = fw.Now() // affiliated after the last working day: it ends when HR reports the fact
			}
			if _, err := svc.TerminateRelationship.Handle(sys, TerminateRelationship{ID: r.ID(), At: timePtr(end)}); err != nil {
				return err
			}
		}
		return nil
	})
}

func timePtr(t time.Time) *time.Time { return &t }
