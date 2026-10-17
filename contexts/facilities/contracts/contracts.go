// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Facilities y su directorio (el stock de Product, Shipments, WorkEffort, las reservas
// de FinancialDetail y Parties referencian instalaciones por identidad y muestran sus nombres).
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "facilities"

// FacilityRef es lo que otros contextos necesitan de una instalación.
type FacilityRef struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Organization string `json:"organization"` // organización interna dueña (una party de Parties)
	Active       bool   `json:"active"`
}

// MaxBatch es el número máximo de ids por llamada a Resolve.
const MaxBatch = 900

// Directory resuelve instalaciones por lotes (sustituye las consultas directas DbSet<Facility> del
// C# en FinancialDetail y los joins de Product y Shipments). Las instalaciones que no existen no
// aparecen.
type Directory interface {
	Resolve(ctx context.Context, ids []string) (map[string]FacilityRef, error)
}

// FacilityRegisteredV1 se publica cuando se da de alta una instalación.
type FacilityRegisteredV1 struct {
	FacilityID   string `json:"facilityId"`
	Organization string `json:"organization"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	PartOf       string `json:"partOf,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (FacilityRegisteredV1) IntegrationEventType() string { return "facilities.facility-registered.v1" }

// FacilityRenamedV1 se publica cuando una instalación cambia de nombre (cachés de nombres de otros
// contextos).
type FacilityRenamedV1 struct {
	FacilityID string `json:"facilityId"`
	Name       string `json:"name"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (FacilityRenamedV1) IntegrationEventType() string { return "facilities.facility-renamed.v1" }

// FacilityRelocatedV1 se publica cuando una instalación cambia de ubicación.
type FacilityRelocatedV1 struct {
	FacilityID string `json:"facilityId"`
	Address    string `json:"address,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (FacilityRelocatedV1) IntegrationEventType() string { return "facilities.facility-relocated.v1" }

// FacilityActivationChangedV1 se publica cuando una instalación se activa o se desactiva.
type FacilityActivationChangedV1 struct {
	FacilityID string `json:"facilityId"`
	Active     bool   `json:"active"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (FacilityActivationChangedV1) IntegrationEventType() string {
	return "facilities.facility-activation-changed.v1"
}
