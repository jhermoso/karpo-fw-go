package domain

import fw "github.com/jhermoso/karpo-fw-go/pkg/domain"

// Identidades nuevas.
func NewTaxRateID() TaxRateID     { return TaxRateID{fw.NewUUID()} }
func NewTreatmentID() TreatmentID { return TreatmentID{fw.NewUUID()} }
func NewTaxpayerID() TaxpayerID   { return TaxpayerID{fw.NewUUID()} }
func NewFilingID() FilingID       { return FilingID{fw.NewUUID()} }
func NewCounterID() CounterID     { return CounterID{fw.NewUUID()} }

// ParseTaxRateID interpreta una identidad en texto.
func ParseTaxRateID(s string) (TaxRateID, error) { u, err := fw.ParseUUID(s); return TaxRateID{u}, err }

// ParseTreatmentID interpreta una identidad en texto.
func ParseTreatmentID(s string) (TreatmentID, error) {
	u, err := fw.ParseUUID(s)
	return TreatmentID{u}, err
}

// ParseTaxpayerID interpreta una identidad en texto.
func ParseTaxpayerID(s string) (TaxpayerID, error) {
	u, err := fw.ParseUUID(s)
	return TaxpayerID{u}, err
}

// ParseFilingID interpreta una identidad en texto.
func ParseFilingID(s string) (FilingID, error) { u, err := fw.ParseUUID(s); return FilingID{u}, err }
