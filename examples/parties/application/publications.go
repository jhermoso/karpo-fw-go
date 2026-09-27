package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/examples/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/examples/parties/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
)

// Publications registers the translation of Parties domain events into its Published Language.
// ContactAdded and PartyDeactivated are internal facts: no other context depends on them yet.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.PartyRegistered) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.PartyRegisteredV1{
			PartyID: e.AggregateID, Type: e.Type, LegalName: e.LegalName, TaxID: e.TaxID, RegisteredAt: e.OccurredAt,
		}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.PartyRenamed) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.PartyRenamedV1{PartyID: e.AggregateID, LegalName: e.To}}, nil
	})
	return r
}
