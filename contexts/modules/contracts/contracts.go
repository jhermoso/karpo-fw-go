// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el puerto
// para preguntar si una empresa tiene encendida una funcionalidad, y el Published Language de
// Modules.
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "modules"

// Clases de funcionalidad.
const (
	Module     = "module"
	Capability = "capability"
	Sector     = "sector"
)

// Features responde qué tiene encendido una empresa (IModuleActivationProvider,
// ICapabilityProvider e ISectorProfileProvider del C# en uno).
type Features interface {
	// Has dice si la empresa tiene la funcionalidad encendida.
	Has(ctx context.Context, organization, kind, code string) (bool, error)
	// Of lista, en orden, los códigos de las funcionalidades de una clase que la empresa tiene
	// encendidas.
	Of(ctx context.Context, organization, kind string) ([]string, error)
}

// FeatureActivatedV1 se publica cuando una empresa enciende una funcionalidad.
type FeatureActivatedV1 struct {
	Organization string `json:"organization"`
	Kind         string `json:"kind"` // module | capability | sector
	Code         string `json:"code"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (FeatureActivatedV1) IntegrationEventType() string { return "modules.feature-activated.v1" }

// FeatureDeactivatedV1 se publica cuando una empresa apaga una funcionalidad.
type FeatureDeactivatedV1 struct {
	Organization string `json:"organization"`
	Kind         string `json:"kind"`
	Code         string `json:"code"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (FeatureDeactivatedV1) IntegrationEventType() string { return "modules.feature-deactivated.v1" }
