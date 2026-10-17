package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Eventos de dominio (el C# sólo lanzaba un FactIssued genérico cuando Documents numeraba un
// TaxFiling).
type (
	// FilingDrafted se lanza cuando se genera una declaración.
	FilingDrafted struct {
		fw.EventMeta
		Declarant string `json:"declarant"`
		Form      string `json:"form"`
		Year      int    `json:"year"`
		Period    string `json:"period"`
	}
	// FilingSubmitted se lanza cuando se presenta una declaración, con su número y sus totales.
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
	// FilingReverted se lanza cuando se revierte una declaración presentada.
	FilingReverted struct {
		fw.EventMeta
		Reason string `json:"reason"`
	}
)

// Implementaciones de EventType.
func (FilingDrafted) EventType() string   { return "fiscal.filing_drafted" }
func (FilingSubmitted) EventType() string { return "fiscal.filing_submitted" }
func (FilingReverted) EventType() string  { return "fiscal.filing_reverted" }
