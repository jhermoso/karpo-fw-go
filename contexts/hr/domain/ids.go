package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// NewPositionID devuelve una identidad nueva.
func NewPositionID() PositionID { return PositionID{fw.NewUUID()} }

// NewEmploymentID devuelve una identidad nueva.
func NewEmploymentID() EmploymentID { return EmploymentID{fw.NewUUID()} }

// NewWorkCenterID devuelve una identidad nueva.
func NewWorkCenterID() WorkCenterID { return WorkCenterID{fw.NewUUID()} }

func parse[T any](s string, wrap func(fw.UUID) T) (T, error) {
	u, err := fw.ParseUUID(s)
	return wrap(u), err
}

// ParsePositionID interpreta una identidad en texto.
func ParsePositionID(s string) (PositionID, error) {
	return parse(s, func(u fw.UUID) PositionID { return PositionID{u} })
}

// ParseEmploymentID interpreta una identidad en texto.
func ParseEmploymentID(s string) (EmploymentID, error) {
	return parse(s, func(u fw.UUID) EmploymentID { return EmploymentID{u} })
}

// ParseWorkCenterID interpreta una identidad en texto.
func ParseWorkCenterID(s string) (WorkCenterID, error) {
	return parse(s, func(u fw.UUID) WorkCenterID { return WorkCenterID{u} })
}

// ParseContractID interpreta una identidad en texto.
func ParseContractID(s string) (ContractID, error) {
	return parse(s, func(u fw.UUID) ContractID { return ContractID{u} })
}

// PositionsWithIDs selecciona los puestos con las identidades dadas.
func PositionsWithIDs(ids ...PositionID) spec.Spec[*Position] { return PosFieldID.In(ids...) }

// EmploymentsWithIDs selecciona las relaciones laborales con las identidades dadas.
func EmploymentsWithIDs(ids ...EmploymentID) spec.Spec[*Employment] {
	return EmpFieldID.In(ids...)
}

// WorkCentersWithIDs selecciona los centros de trabajo con las identidades dadas.
func WorkCentersWithIDs(ids ...WorkCenterID) spec.Spec[*WorkCenter] {
	return WCFieldID.In(ids...)
}
