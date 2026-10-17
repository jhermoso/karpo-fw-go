package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// NewPayslipID devuelve una identidad nueva.
func NewPayslipID() PayslipID { return PayslipID{fw.NewUUID()} }

// NewProfileID devuelve una identidad nueva.
func NewProfileID() ProfileID { return ProfileID{fw.NewUUID()} }

// NewEmployerAccountID devuelve una identidad nueva.
func NewEmployerAccountID() EmployerAccountID { return EmployerAccountID{fw.NewUUID()} }

// ParsePayslipID interpreta una identidad en texto.
func ParsePayslipID(s string) (PayslipID, error) { u, err := fw.ParseUUID(s); return PayslipID{u}, err }

// ParseProfileID interpreta una identidad en texto.
func ParseProfileID(s string) (ProfileID, error) { u, err := fw.ParseUUID(s); return ProfileID{u}, err }

// ParseEmployerAccountID interpreta una identidad en texto.
func ParseEmployerAccountID(s string) (EmployerAccountID, error) {
	u, err := fw.ParseUUID(s)
	return EmployerAccountID{u}, err
}
