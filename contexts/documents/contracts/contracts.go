// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el puerto para
// buscar un documento en el registro por el hecho que representa.
package contracts

import "context"

// Source es el nombre del contexto delimitado.
const Source = "documents"

// Document es una anotación del registro tal como la ven otros contextos.
type Document struct {
	ID        string `json:"id"`
	Company   string `json:"company"`
	Type      string `json:"type"` // invoice | credit-note | order | delivery-note | received-invoice | payslip | tax-filing
	FactID    string `json:"factId"`
	Number    string `json:"number"`
	Date      string `json:"date"`
	Cancelled bool   `json:"cancelled"`
}

// Register resuelve el documento de un hecho (el IDocumentResolver del C#). El booleano es false
// cuando el registro no ha oído hablar de él.
type Register interface {
	ByFact(ctx context.Context, docType, factID string) (Document, bool, error)
}
