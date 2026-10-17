package application

import (
	"errors"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Desenlaces de una operación, para las etiquetas de las métricas y los atributos de los spans.
// Siguen la taxonomía de errores del dominio, la misma que la capa HTTP traduce a códigos de estado.
const (
	OutcomeOK           = "ok"
	OutcomeValidation   = "validation"
	OutcomeRule         = "rule"
	OutcomeNotFound     = "not_found"
	OutcomeConflict     = "conflict"
	OutcomeUnauthorized = "unauthorized"
	OutcomeForbidden    = "forbidden"
	OutcomeError        = "error"
)

// Outcome clasifica err con errors.Is, nunca por su texto. Todo lo que queda fuera de la taxonomía
// (fallos de infraestructura, errores de programación, una autorización indeterminada) es OutcomeError.
func Outcome(err error) string {
	switch {
	case err == nil:
		return OutcomeOK
	case errors.Is(err, authz.ErrIndeterminate):
		return OutcomeError
	case errors.Is(err, domain.ErrValidation), errors.Is(err, domain.ErrInvalidIdentity):
		return OutcomeValidation
	case errors.Is(err, domain.ErrRuleViolation):
		return OutcomeRule
	case errors.Is(err, domain.ErrNotFound):
		return OutcomeNotFound
	case errors.Is(err, domain.ErrConflict):
		return OutcomeConflict
	case errors.Is(err, domain.ErrUnauthorized):
		return OutcomeUnauthorized
	case errors.Is(err, domain.ErrForbidden):
		return OutcomeForbidden
	}
	return OutcomeError
}
