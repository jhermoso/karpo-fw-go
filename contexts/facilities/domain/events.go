package domain

import (
	"context"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// FacilityRegistered se lanza cuando se da de alta una instalación.
type FacilityRegistered struct {
	fw.EventMeta
	Organization string `json:"organization"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	PartOf       string `json:"partOf,omitempty"`
	Address      string `json:"address,omitempty"`
}

// EventType implementa domain.Event.
func (FacilityRegistered) EventType() string { return "facilities.facility_registered" }

// FacilityRenamed se lanza cuando una instalación cambia de nombre.
type FacilityRenamed struct {
	fw.EventMeta
	Name string `json:"name"`
}

// EventType implementa domain.Event.
func (FacilityRenamed) EventType() string { return "facilities.facility_renamed" }

// FacilityRelocated se lanza cuando cambia la ubicación.
type FacilityRelocated struct {
	fw.EventMeta
	Address string `json:"address,omitempty"`
	Phone   string `json:"phone,omitempty"`
}

// EventType implementa domain.Event.
func (FacilityRelocated) EventType() string { return "facilities.facility_relocated" }

// FacilityMoved se lanza cuando la instalación pasa a formar parte de otra (o se separa).
type FacilityMoved struct {
	fw.EventMeta
	PartOf string `json:"partOf,omitempty"`
}

// EventType implementa domain.Event.
func (FacilityMoved) EventType() string { return "facilities.facility_moved" }

// FacilityActivationChanged se lanza cuando una instalación se activa o se desactiva.
type FacilityActivationChanged struct {
	fw.EventMeta
	Active bool `json:"active"`
}

// EventType implementa domain.Event.
func (FacilityActivationChanged) EventType() string { return "facilities.facility_activation_changed" }

// Repository guarda las instalaciones.
type Repository = fw.Repository[FacilityID, *Facility]

// Catalogs lee el catálogo de tipos de instalación.
type Catalogs interface {
	FacilityTypes(ctx context.Context) ([]FacilityType, error)
}
