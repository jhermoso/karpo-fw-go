package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors classify failures independently of the layer that produced them.
// Always test with errors.Is; concrete error types below wrap these sentinels.
// The distribution layer maps them to HTTP status codes (404, 409, 400, 422, ...).
var (
	// ErrNotFound: the requested aggregate or resource does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict: optimistic concurrency failure or duplicate identity.
	ErrConflict = errors.New("conflict")
	// ErrValidation: input data does not satisfy field-level rules.
	ErrValidation = errors.New("validation failed")
	// ErrRuleViolation: a business invariant would be broken by the requested operation.
	ErrRuleViolation = errors.New("business rule violated")
	// ErrUnauthorized: the caller is not authenticated.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden: the caller is authenticated but not allowed to perform the operation.
	ErrForbidden = errors.New("forbidden")
	// ErrInvalidIdentity: an identifier is zero or malformed.
	ErrInvalidIdentity = errors.New("invalid identity")
	// ErrUnsupported: an adapter cannot honour the request (e.g. a specification it cannot translate).
	// Adapters must return this instead of silently degrading (for example, filtering in memory).
	ErrUnsupported = errors.New("unsupported")
)

// NotFoundError reports a missing aggregate. It matches ErrNotFound.
type NotFoundError struct {
	Kind string
	ID   string
}

// NotFound builds a NotFoundError for the aggregate kind and identifier.
func NotFound(kind string, id fmt.Stringer) error {
	return &NotFoundError{Kind: kind, ID: id.String()}
}

func (e *NotFoundError) Error() string        { return fmt.Sprintf("%s %q not found", e.Kind, e.ID) }
func (e *NotFoundError) Is(target error) bool { return target == ErrNotFound }

// ConflictError reports an optimistic concurrency failure or a duplicate identity. It matches ErrConflict.
type ConflictError struct {
	Kind            string
	ID              string
	ExpectedVersion int64
	Reason          string
}

// Conflict builds a ConflictError.
func Conflict(kind string, id fmt.Stringer, expectedVersion int64, reason string) error {
	return &ConflictError{Kind: kind, ID: id.String(), ExpectedVersion: expectedVersion, Reason: reason}
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s %q conflict (expected version %d): %s", e.Kind, e.ID, e.ExpectedVersion, e.Reason)
}
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// FieldError describes a single field-level validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidationError aggregates field errors (notification pattern). It matches ErrValidation.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Errors))
	for _, fe := range e.Errors {
		parts = append(parts, fmt.Sprintf("%s: %s", fe.Field, fe.Message))
	}
	return "validation failed: " + strings.Join(parts, "; ")
}
func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// RuleViolationError reports a broken business invariant. It matches ErrRuleViolation.
type RuleViolationError struct {
	Code    string
	Message string
}

// Violation builds a RuleViolationError.
func Violation(code, message string) error {
	return &RuleViolationError{Code: code, Message: message}
}

func (e *RuleViolationError) Error() string        { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func (e *RuleViolationError) Is(target error) bool { return target == ErrRuleViolation }

// Validation accumulates field errors so constructors can report every problem at once.
//
//	var v domain.Validation
//	v.Require(name != "", "name", "required", "name is required")
//	if err := v.Err(); err != nil { return Party{}, err }
type Validation struct {
	errs []FieldError
}

// Add records a field error.
func (v *Validation) Add(field, code, message string) {
	v.errs = append(v.errs, FieldError{Field: field, Code: code, Message: message})
}

// Require records a field error when ok is false. It returns ok for chaining in conditions.
func (v *Validation) Require(ok bool, field, code, message string) bool {
	if !ok {
		v.Add(field, code, message)
	}
	return ok
}

// Merge absorbs err: nested ValidationErrors are flattened (fields prefixed with prefix),
// any other non-nil error is recorded under prefix with code "invalid".
func (v *Validation) Merge(prefix string, err error) {
	if err == nil {
		return
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		for _, fe := range ve.Errors {
			if prefix != "" {
				fe.Field = prefix + "." + fe.Field
			}
			v.errs = append(v.errs, fe)
		}
		return
	}
	v.Add(prefix, "invalid", err.Error())
}

// HasErrors reports whether any error was recorded.
func (v *Validation) HasErrors() bool { return len(v.errs) > 0 }

// Err returns a *ValidationError with the accumulated errors, or nil when there are none.
func (v *Validation) Err() error {
	if len(v.errs) == 0 {
		return nil
	}
	return &ValidationError{Errors: append([]FieldError(nil), v.errs...)}
}
