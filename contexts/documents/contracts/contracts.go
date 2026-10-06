// Package contracts is what other bounded contexts may depend on: the port to look a document up
// in the register by the fact it stands for.
package contracts

import "context"

// Source is the name of the bounded context.
const Source = "documents"

// Document is an entry of the register as other contexts see it.
type Document struct {
	ID        string `json:"id"`
	Company   string `json:"company"`
	Type      string `json:"type"` // invoice | credit-note | order | delivery-note | received-invoice | payslip | tax-filing
	FactID    string `json:"factId"`
	Number    string `json:"number"`
	Date      string `json:"date"`
	Cancelled bool   `json:"cancelled"`
}

// Register resolves the document of a fact (the C# IDocumentResolver). The boolean is false when
// the register has not heard of it.
type Register interface {
	ByFact(ctx context.Context, docType, factID string) (Document, bool, error)
}
