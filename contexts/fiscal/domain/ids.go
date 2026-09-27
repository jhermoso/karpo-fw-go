package domain

import fw "github.com/jhermoso/karpo-fw-go/pkg/domain"

// New identities.
func NewTaxRateID() TaxRateID     { return TaxRateID{fw.NewUUID()} }
func NewTreatmentID() TreatmentID { return TreatmentID{fw.NewUUID()} }
func NewTaxpayerID() TaxpayerID   { return TaxpayerID{fw.NewUUID()} }
func NewFilingID() FilingID       { return FilingID{fw.NewUUID()} }
func NewCounterID() CounterID     { return CounterID{fw.NewUUID()} }

// ParseTaxRateID parses a textual identity.
func ParseTaxRateID(s string) (TaxRateID, error) { u, err := fw.ParseUUID(s); return TaxRateID{u}, err }

// ParseTreatmentID parses a textual identity.
func ParseTreatmentID(s string) (TreatmentID, error) {
	u, err := fw.ParseUUID(s)
	return TreatmentID{u}, err
}

// ParseTaxpayerID parses a textual identity.
func ParseTaxpayerID(s string) (TaxpayerID, error) {
	u, err := fw.ParseUUID(s)
	return TaxpayerID{u}, err
}

// ParseFilingID parses a textual identity.
func ParseFilingID(s string) (FilingID, error) { u, err := fw.ParseUUID(s); return FilingID{u}, err }
