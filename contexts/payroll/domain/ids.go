package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// NewPayslipID returns a new identity.
func NewPayslipID() PayslipID { return PayslipID{fw.NewUUID()} }

// NewProfileID returns a new identity.
func NewProfileID() ProfileID { return ProfileID{fw.NewUUID()} }

// NewEmployerAccountID returns a new identity.
func NewEmployerAccountID() EmployerAccountID { return EmployerAccountID{fw.NewUUID()} }

// ParsePayslipID parses a textual identity.
func ParsePayslipID(s string) (PayslipID, error) { u, err := fw.ParseUUID(s); return PayslipID{u}, err }

// ParseProfileID parses a textual identity.
func ParseProfileID(s string) (ProfileID, error) { u, err := fw.ParseUUID(s); return ProfileID{u}, err }

// ParseEmployerAccountID parses a textual identity.
func ParseEmployerAccountID(s string) (EmployerAccountID, error) {
	u, err := fw.ParseUUID(s)
	return EmployerAccountID{u}, err
}
