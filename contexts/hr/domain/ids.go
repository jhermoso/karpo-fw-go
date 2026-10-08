package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// NewPositionID returns a new identity.
func NewPositionID() PositionID { return PositionID{fw.NewUUID()} }

// NewEmploymentID returns a new identity.
func NewEmploymentID() EmploymentID { return EmploymentID{fw.NewUUID()} }

// NewWorkCenterID returns a new identity.
func NewWorkCenterID() WorkCenterID { return WorkCenterID{fw.NewUUID()} }

func parse[T any](s string, wrap func(fw.UUID) T) (T, error) {
	u, err := fw.ParseUUID(s)
	return wrap(u), err
}

// ParsePositionID parses a textual identity.
func ParsePositionID(s string) (PositionID, error) {
	return parse(s, func(u fw.UUID) PositionID { return PositionID{u} })
}

// ParseEmploymentID parses a textual identity.
func ParseEmploymentID(s string) (EmploymentID, error) {
	return parse(s, func(u fw.UUID) EmploymentID { return EmploymentID{u} })
}

// ParseWorkCenterID parses a textual identity.
func ParseWorkCenterID(s string) (WorkCenterID, error) {
	return parse(s, func(u fw.UUID) WorkCenterID { return WorkCenterID{u} })
}

// ParseContractID parses a textual identity.
func ParseContractID(s string) (ContractID, error) {
	return parse(s, func(u fw.UUID) ContractID { return ContractID{u} })
}

// PositionsWithIDs matches the positions with the given identities.
func PositionsWithIDs(ids ...PositionID) spec.Spec[*Position] { return PosFieldID.In(ids...) }

// EmploymentsWithIDs matches the employments with the given identities.
func EmploymentsWithIDs(ids ...EmploymentID) spec.Spec[*Employment] {
	return EmpFieldID.In(ids...)
}

// WorkCentersWithIDs matches the work centers with the given identities.
func WorkCentersWithIDs(ids ...WorkCenterID) spec.Spec[*WorkCenter] {
	return WCFieldID.In(ids...)
}
