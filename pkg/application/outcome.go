package application

import (
	"errors"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Outcomes of an operation, for metric labels and span attributes. They follow the domain error
// taxonomy, the same one the HTTP layer maps to status codes.
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

// Outcome classifies err with errors.Is, never by its text. Everything outside the taxonomy
// (infrastructure failures, bugs, an indeterminate authorization) is OutcomeError.
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
