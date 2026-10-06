package contracts

import (
	"context"
	"time"
)

// ProspectTrialChangedV1 is published when the free trial an internal organization grants to a
// prospect is granted, extended, shortened or withdrawn (TrialUntil absent). The end of the
// relationship itself is RelationshipTerminatedV1: a trial is in force only while the prospect
// relationship is.
type ProspectTrialChangedV1 struct {
	RelationshipID string     `json:"relationshipId"`
	Prospect       string     `json:"prospect"`
	Organization   string     `json:"organization"`
	TrialUntil     *time.Time `json:"trialUntil,omitempty"`
	ChangedAt      time.Time  `json:"changedAt"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (ProspectTrialChangedV1) IntegrationEventType() string {
	return "parties.prospect-trial-changed.v1"
}

// OwnershipShareChangedV1 is published when the direct stake of a shareholder in an organization
// is recorded, corrected or cleared (Share absent). Share is in points with two decimals
// ("30.00" is 30 %).
type OwnershipShareChangedV1 struct {
	RelationshipID string    `json:"relationshipId"`
	Shareholder    string    `json:"shareholder"`
	Organization   string    `json:"organization"`
	Share          string    `json:"share,omitempty"`
	ChangedAt      time.Time `json:"changedAt"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (OwnershipShareChangedV1) IntegrationEventType() string {
	return "parties.ownership-share-changed.v1"
}

// Trial is the free trial of a prospect with an internal organization.
type Trial struct {
	PartyID        string     `json:"partyId"`
	RelationshipID string     `json:"relationshipId"`
	Since          time.Time  `json:"since"`
	Until          *time.Time `json:"until,omitempty"` // absent: no trial granted
	InForce        bool       `json:"inForce"`
}

// Trials answers whether the trial of a prospect is still in force (what a subscriptions context
// asks before letting a prospect work), in batches of at most MaxDirectoryBatch ids. A party
// without a current prospect relationship with the organization is absent from the result.
type Trials interface {
	Trials(ctx context.Context, organization string, partyIDs []string) (map[string]Trial, error)
}
