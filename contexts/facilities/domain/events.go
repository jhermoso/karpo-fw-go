package domain

import (
	"context"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// FacilityRegistered is raised when a facility is registered.
type FacilityRegistered struct {
	fw.EventMeta
	Organization string `json:"organization"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	PartOf       string `json:"partOf,omitempty"`
	Address      string `json:"address,omitempty"`
}

// EventType implements domain.Event.
func (FacilityRegistered) EventType() string { return "facilities.facility_registered" }

// FacilityRenamed is raised when a facility changes its name.
type FacilityRenamed struct {
	fw.EventMeta
	Name string `json:"name"`
}

// EventType implements domain.Event.
func (FacilityRenamed) EventType() string { return "facilities.facility_renamed" }

// FacilityRelocated is raised when the location changes.
type FacilityRelocated struct {
	fw.EventMeta
	Address string `json:"address,omitempty"`
	Phone   string `json:"phone,omitempty"`
}

// EventType implements domain.Event.
func (FacilityRelocated) EventType() string { return "facilities.facility_relocated" }

// FacilityMoved is raised when the facility becomes part of another one (or is detached).
type FacilityMoved struct {
	fw.EventMeta
	PartOf string `json:"partOf,omitempty"`
}

// EventType implements domain.Event.
func (FacilityMoved) EventType() string { return "facilities.facility_moved" }

// FacilityActivationChanged is raised when a facility is activated or deactivated.
type FacilityActivationChanged struct {
	fw.EventMeta
	Active bool `json:"active"`
}

// EventType implements domain.Event.
func (FacilityActivationChanged) EventType() string { return "facilities.facility_activation_changed" }

// Repository stores facilities.
type Repository = fw.Repository[FacilityID, *Facility]

// Catalogs reads the facility type catalog.
type Catalogs interface {
	FacilityTypes(ctx context.Context) ([]FacilityType, error)
}
