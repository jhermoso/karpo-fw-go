package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Domain events (the C# raised a generic FactIssued only when Documents numbered a TaxFiling).
type (
	// FilingDrafted is raised when a filing is generated.
	FilingDrafted struct {
		fw.EventMeta
		Declarant string `json:"declarant"`
		Form      string `json:"form"`
		Year      int    `json:"year"`
		Period    string `json:"period"`
	}
	// FilingSubmitted is raised when a filing is submitted, with its number and totals.
	FilingSubmitted struct {
		fw.EventMeta
		Declarant   string `json:"declarant"`
		Form        string `json:"form"`
		Year        int    `json:"year"`
		Period      string `json:"period"`
		Number      int64  `json:"number"`
		Recipients  int    `json:"recipients"`
		Perceptions string `json:"perceptions"`
		Withheld    string `json:"withheld"`
	}
	// FilingReverted is raised when a submitted filing is reverted.
	FilingReverted struct {
		fw.EventMeta
		Reason string `json:"reason"`
	}
)

// EventType implementations.
func (FilingDrafted) EventType() string   { return "fiscal.filing_drafted" }
func (FilingSubmitted) EventType() string { return "fiscal.filing_submitted" }
func (FilingReverted) EventType() string  { return "fiscal.filing_reverted" }
