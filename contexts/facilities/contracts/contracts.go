// Package contracts is what other bounded contexts may depend on: the Published Language of
// Facilities and its directory (Product stock, Shipments, WorkEffort, Maccorp reservations and
// Parties reference facilities by identity and show their names).
package contracts

import "context"

// Source is the name of the publishing bounded context.
const Source = "facilities"

// FacilityRef is what other contexts need of a facility.
type FacilityRef struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Organization string `json:"organization"` // owning internal organization (a Parties party)
	Active       bool   `json:"active"`
}

// MaxBatch is the maximum number of ids per Resolve call.
const MaxBatch = 900

// Directory resolves facilities in batches (it replaces the C# direct DbSet<Facility> queries of
// Maccorp and the joins of Product and Shipments). Missing facilities are absent.
type Directory interface {
	Resolve(ctx context.Context, ids []string) (map[string]FacilityRef, error)
}

// FacilityRegisteredV1 is published when a facility is registered.
type FacilityRegisteredV1 struct {
	FacilityID   string `json:"facilityId"`
	Organization string `json:"organization"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	PartOf       string `json:"partOf,omitempty"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (FacilityRegisteredV1) IntegrationEventType() string { return "facilities.facility-registered.v1" }

// FacilityRenamedV1 is published when a facility changes its name (name caches of other contexts).
type FacilityRenamedV1 struct {
	FacilityID string `json:"facilityId"`
	Name       string `json:"name"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (FacilityRenamedV1) IntegrationEventType() string { return "facilities.facility-renamed.v1" }

// FacilityRelocatedV1 is published when a facility changes its location.
type FacilityRelocatedV1 struct {
	FacilityID string `json:"facilityId"`
	Address    string `json:"address,omitempty"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (FacilityRelocatedV1) IntegrationEventType() string { return "facilities.facility-relocated.v1" }

// FacilityActivationChangedV1 is published when a facility is activated or deactivated.
type FacilityActivationChangedV1 struct {
	FacilityID string `json:"facilityId"`
	Active     bool   `json:"active"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (FacilityActivationChangedV1) IntegrationEventType() string {
	return "facilities.facility-activation-changed.v1"
}
