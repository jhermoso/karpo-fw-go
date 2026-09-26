package distribution

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// ProblemDetails is an RFC 9457 (formerly 7807) error response.
type ProblemDetails struct {
	Type     string              `json:"type,omitempty"`
	Title    string              `json:"title"`
	Status   int                 `json:"status"`
	Detail   string              `json:"detail,omitempty"`
	Instance string              `json:"instance,omitempty"`
	Code     string              `json:"code,omitempty"`
	Errors   []domain.FieldError `json:"errors,omitempty"`
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil && status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// Problem maps an error to ProblemDetails using the domain error taxonomy (errors.Is/As),
// never by inspecting message text. Unknown errors become 500 without leaking internals.
func Problem(r *http.Request, err error) ProblemDetails {
	p := ProblemDetails{Status: http.StatusInternalServerError, Title: "Internal Server Error",
		Detail: "An unexpected error occurred"}
	if r != nil {
		p.Instance = r.URL.Path
	}

	var (
		ve *domain.ValidationError
		rv *domain.RuleViolationError
	)
	switch {
	case errors.As(err, &ve):
		p.Status, p.Title, p.Detail, p.Errors = http.StatusBadRequest, "Validation Failed", "One or more fields are invalid", ve.Errors
	case errors.Is(err, domain.ErrValidation), errors.Is(err, domain.ErrInvalidIdentity):
		p.Status, p.Title, p.Detail = http.StatusBadRequest, "Bad Request", err.Error()
	case errors.As(err, &rv):
		p.Status, p.Title, p.Detail, p.Code = http.StatusUnprocessableEntity, "Business Rule Violated", rv.Message, rv.Code
	case errors.Is(err, domain.ErrRuleViolation):
		p.Status, p.Title, p.Detail = http.StatusUnprocessableEntity, "Business Rule Violated", err.Error()
	case errors.Is(err, domain.ErrNotFound):
		p.Status, p.Title, p.Detail = http.StatusNotFound, "Resource Not Found", err.Error()
	case errors.Is(err, domain.ErrConflict):
		p.Status, p.Title, p.Detail = http.StatusConflict, "Conflict", err.Error()
	case errors.Is(err, domain.ErrUnauthorized):
		p.Status, p.Title, p.Detail = http.StatusUnauthorized, "Unauthorized", ""
	case errors.Is(err, domain.ErrForbidden):
		p.Status, p.Title, p.Detail = http.StatusForbidden, "Forbidden", ""
	case errors.Is(err, domain.ErrUnsupported):
		p.Status, p.Title, p.Detail = http.StatusNotImplemented, "Not Implemented", err.Error()
	}
	return p
}

// WriteError writes err as an application/problem+json response.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	p := Problem(r, err)
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// Respond writes the (value, error) pair returned by a use case: the error as problem details,
// or v with successStatus (200 when <= 0).
func Respond[T any](w http.ResponseWriter, r *http.Request, v T, err error, successStatus int) {
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if successStatus <= 0 {
		successStatus = http.StatusOK
	}
	WriteJSON(w, successStatus, v)
}
