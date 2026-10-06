// Package contracts is what other bounded contexts may depend on: the port to ask whether a
// company has a feature on, and the Published Language of Modules.
package contracts

import "context"

// Source is the name of the publishing bounded context.
const Source = "modules"

// Kinds of feature.
const (
	Module     = "module"
	Capability = "capability"
	Sector     = "sector"
)

// Features answers what a company has switched on (the C# IModuleActivationProvider,
// ICapabilityProvider and ISectorProfileProvider in one).
type Features interface {
	// Has reports whether the company has the feature on.
	Has(ctx context.Context, organization, kind, code string) (bool, error)
	// Of lists the codes of the features of a kind the company has on, in order.
	Of(ctx context.Context, organization, kind string) ([]string, error)
}

// FeatureActivatedV1 is published when a company switches a feature on.
type FeatureActivatedV1 struct {
	Organization string `json:"organization"`
	Kind         string `json:"kind"` // module | capability | sector
	Code         string `json:"code"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (FeatureActivatedV1) IntegrationEventType() string { return "modules.feature-activated.v1" }

// FeatureDeactivatedV1 is published when a company switches a feature off.
type FeatureDeactivatedV1 struct {
	Organization string `json:"organization"`
	Kind         string `json:"kind"`
	Code         string `json:"code"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (FeatureDeactivatedV1) IntegrationEventType() string { return "modules.feature-deactivated.v1" }
