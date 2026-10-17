package distribution

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// ProblemDetails es una respuesta de error RFC 9457 (antes 7807).
type ProblemDetails struct {
	Type     string              `json:"type,omitempty"`
	Title    string              `json:"title"`
	Status   int                 `json:"status"`
	Detail   string              `json:"detail,omitempty"`
	Instance string              `json:"instance,omitempty"`
	Code     string              `json:"code,omitempty"`
	Errors   []domain.FieldError `json:"errors,omitempty"`
	// CorrelationID es lo que cita quien llama para encontrar la petición en los logs.
	CorrelationID string `json:"correlationId,omitempty"`
}

// WriteJSON escribe v como JSON con el estado dado.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil && status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// Problem traduce un error a ProblemDetails con la taxonomía de errores del dominio (errors.Is/As),
// nunca mirando el texto del mensaje. Los errores desconocidos pasan a 500 sin filtrar detalles internos.
func Problem(r *http.Request, err error) ProblemDetails {
	p := ProblemDetails{Status: http.StatusInternalServerError, Title: "Internal Server Error",
		Detail: "An unexpected error occurred"}
	if r != nil {
		p.Instance = r.URL.Path
		p.CorrelationID = application.CorrelationID(r.Context())
	}

	// El kind de un NotFoundError o un ConflictError puede ser el tipo Go del agregado (los
	// repositorios lo toman del tipo), así que a quien llama se le dice qué ha pasado, nunca el kind.
	var (
		ve *domain.ValidationError
		rv *domain.RuleViolationError
		ce *domain.ConflictError
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
		p.Status, p.Title, p.Detail = http.StatusNotFound, "Resource Not Found", "The requested resource was not found"
	case errors.As(err, &ce):
		p.Status, p.Title, p.Detail = http.StatusConflict, "Conflict", ce.Reason
	case errors.Is(err, domain.ErrConflict):
		p.Status, p.Title, p.Detail = http.StatusConflict, "Conflict", err.Error()
	case errors.Is(err, domain.ErrUnauthorized):
		p.Status, p.Title, p.Detail = http.StatusUnauthorized, "Unauthorized", ""
	case errors.Is(err, domain.ErrForbidden):
		p.Status, p.Title, p.Detail = http.StatusForbidden, "Forbidden", ""
	case errors.Is(err, authz.ErrIndeterminate):
		p.Status, p.Title, p.Detail = http.StatusServiceUnavailable, "Service Unavailable", "authorization could not be determined"
	case errors.Is(err, domain.ErrUnsupported):
		p.Status, p.Title, p.Detail = http.StatusNotImplemented, "Not Implemented", err.Error()
	}
	return p
}

// WriteError escribe err como respuesta application/problem+json. Quien llama nunca ve la causa
// de un 5xx; Observe, si está montado, la registra en el log con el id de correlación que se devuelve aquí.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	p := Problem(r, err)
	RecordError(r, err)
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// Respond escribe el par (valor, error) que devuelve un caso de uso: el error como problem details,
// o v con successStatus (200 cuando es <= 0).
func Respond[T any](w http.ResponseWriter, r *http.Request, v T, err error, successStatus int) {
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if successStatus <= 0 {
		successStatus = http.StatusOK
	}
	noteRoute(r)
	WriteJSON(w, successStatus, v)
}
