package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Publications registers the translation of the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	one := func(e app.IntegrationEvent) ([]app.IntegrationEvent, error) { return []app.IntegrationEvent{e}, nil }
	messaging.On(r, func(_ context.Context, e domain.PartyRegistered) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyRegisteredV1{PartyID: e.AggregateID, Kind: e.Kind, Name: e.Name, RegisteredAt: e.OccurredAt})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyRenamed) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyRenamedV1{PartyID: e.AggregateID, Name: e.To})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyActivated) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyActivationChangedV1{PartyID: e.AggregateID, Active: true})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyDeactivated) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyActivationChangedV1{PartyID: e.AggregateID, Active: false})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyRoleAssigned) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyRoleAssignedV1{PartyID: e.AggregateID, RoleID: e.RoleID, RoleType: e.RoleType, From: e.From})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyRoleEnded) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyRoleEndedV1{PartyID: e.AggregateID, RoleID: e.RoleID, RoleType: e.RoleType, At: e.At})
	})
	messaging.On(r, func(_ context.Context, e domain.RelationshipEstablished) ([]app.IntegrationEvent, error) {
		return one(contracts.RelationshipEstablishedV1{RelationshipID: e.AggregateID, Type: e.Type, FromParty: e.From,
			ToParty: e.To, FromRole: e.FromRole, ToRole: e.ToRole, Since: e.Since})
	})
	messaging.On(r, func(_ context.Context, e domain.RelationshipTerminated) ([]app.IntegrationEvent, error) {
		return one(contracts.RelationshipTerminatedV1{RelationshipID: e.AggregateID, At: e.At})
	})
	messaging.On(r, func(_ context.Context, e domain.ProspectTrialChanged) ([]app.IntegrationEvent, error) {
		return one(contracts.ProspectTrialChangedV1{RelationshipID: e.AggregateID, Prospect: e.Prospect,
			Organization: e.Organization, TrialUntil: e.TrialUntil, ChangedAt: e.OccurredAt})
	})
	messaging.On(r, func(_ context.Context, e domain.OwnershipShareChanged) ([]app.IntegrationEvent, error) {
		return one(contracts.OwnershipShareChangedV1{RelationshipID: e.AggregateID, Shareholder: e.Shareholder,
			Organization: e.Organization, Share: e.Share, ChangedAt: e.OccurredAt})
	})
	messaging.On(r, func(_ context.Context, e domain.IdentificationAdded) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyIdentificationAddedV1{PartyID: e.AggregateID, IdentificationID: e.IdentificationID,
			DocumentType: e.DocumentType, Country: e.Country, Number: e.Number, Primary: e.Primary})
	})
	messaging.On(r, func(_ context.Context, e domain.IdentificationRemoved) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyIdentificationRemovedV1{PartyID: e.AggregateID, IdentificationID: e.IdentificationID})
	})
	messaging.On(r, func(_ context.Context, e domain.ContactAdded) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyContactAddedV1{PartyID: e.AggregateID, ContactID: e.ContactID, Kind: e.Kind, Value: e.Value,
			Address: e.Address, Purposes: e.Purposes, From: e.From})
	})
	messaging.On(r, func(_ context.Context, e domain.ContactPurposesChanged) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyContactPurposesChangedV1{PartyID: e.AggregateID, ContactID: e.ContactID, Purposes: e.Purposes})
	})
	messaging.On(r, func(_ context.Context, e domain.ContactEnded) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyContactEndedV1{PartyID: e.AggregateID, ContactID: e.ContactID, At: e.At})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyClassified) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyClassifiedV1{PartyID: e.AggregateID, ClassificationID: e.ClassificationID, Type: e.Type, From: e.From})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyClassificationEnded) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyClassificationEndedV1{PartyID: e.AggregateID, ClassificationID: e.ClassificationID, Type: e.Type, At: e.At})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyAffiliated) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyAffiliatedV1{PartyID: e.AggregateID, Organization: e.Organization, RelationshipID: e.Relationship, From: e.From})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyAffiliationEnded) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyAffiliationEndedV1{PartyID: e.AggregateID, Organization: e.Organization, RelationshipID: e.Relationship, At: e.At})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyFacilityRoleAssigned) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyFacilityRoleAssignedV1{PartyID: e.AggregateID, RoleID: e.RoleID, Facility: e.Facility, RoleType: e.RoleType, From: e.From})
	})
	messaging.On(r, func(_ context.Context, e domain.PartyFacilityRoleEnded) ([]app.IntegrationEvent, error) {
		return one(contracts.PartyFacilityRoleEndedV1{PartyID: e.AggregateID, RoleID: e.RoleID, Facility: e.Facility, At: e.At})
	})
	return r
}

// Directory is the in-process implementation of contracts.Directory (the C# EfPartyDirectory):
// one query per batch, whatever the number of ids. Remote contexts use an HTTP adapter over
// the same contract. It serves other contexts, so it does not require a Parties permission:
// the caller's own use case is what is authorized.
type Directory struct{ Parties domain.PartyRepository }

var _ contracts.Directory = Directory{}

// Resolve implements contracts.Directory.
func (d Directory) Resolve(ctx context.Context, ids []string) (map[string]contracts.PartyRef, error) {
	if len(ids) > contracts.MaxDirectoryBatch {
		return nil, fmt.Errorf("%w: at most %d ids per call", fw.ErrValidation, contracts.MaxDirectoryBatch)
	}
	want := make([]domain.PartyID, 0, len(ids))
	for _, s := range ids {
		if id, err := domain.ParsePartyID(s); err == nil && !id.IsZero() {
			want = append(want, id)
		}
	}
	out := make(map[string]contracts.PartyRef, len(want))
	if len(want) == 0 {
		return out, nil
	}
	ps, err := d.Parties.Find(ctx, domain.WithIDs(want...))
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		out[p.ID().String()] = contracts.PartyRef{ID: p.ID().String(), Name: p.Name(), Active: p.IsActive()}
	}
	return out, nil
}

// SearchIDsByName implements contracts.Directory: an empty fragment is an empty result, not
// "everyone".
func (d Directory) SearchIDsByName(ctx context.Context, text string, limit int) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{}, nil
	}
	if limit <= 0 || limit > contracts.MaxDirectoryBatch {
		limit = contracts.MaxDirectoryBatch
	}
	page, err := d.Parties.FindPage(ctx, spec.And(domain.NameContains(text)), fw.NewPageRequest(1, limit, domain.FieldName.Asc()))
	if err != nil {
		return nil, err
	}
	out := make([]string, len(page.Items))
	for i, p := range page.Items {
		out[i] = p.ID().String()
	}
	return out, nil
}
