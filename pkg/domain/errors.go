package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Los errores centinela clasifican los fallos con independencia de la capa que los produce.
// Se comprueban siempre con errors.Is; los tipos de error concretos de abajo envuelven estos centinelas.
// La capa de distribución los traduce a códigos de estado HTTP (404, 409, 400, 422, ...).
var (
	// ErrNotFound: el agregado o recurso pedido no existe.
	ErrNotFound = errors.New("not found")
	// ErrConflict: fallo de concurrencia optimista o identidad duplicada.
	ErrConflict = errors.New("conflict")
	// ErrValidation: los datos de entrada no cumplen las reglas de campo.
	ErrValidation = errors.New("validation failed")
	// ErrRuleViolation: la operación pedida rompería un invariante de negocio.
	ErrRuleViolation = errors.New("business rule violated")
	// ErrUnauthorized: quien llama no está autenticado.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden: quien llama está autenticado pero no puede hacer la operación.
	ErrForbidden = errors.New("forbidden")
	// ErrInvalidIdentity: un identificador es nulo o está mal formado.
	ErrInvalidIdentity = errors.New("invalid identity")
	// ErrUnsupported: un adaptador no puede atender la petición (p. ej. una especificación que no sabe traducir).
	// Los adaptadores deben devolverlo en lugar de degradarse en silencio (por ejemplo, filtrando en memoria).
	ErrUnsupported = errors.New("unsupported")
)

// NotFoundError informa de un agregado que no existe. Coincide con ErrNotFound.
type NotFoundError struct {
	Kind string
	ID   string
}

// NotFound construye un NotFoundError para el tipo de agregado y el identificador.
func NotFound(kind string, id fmt.Stringer) error {
	return &NotFoundError{Kind: kind, ID: id.String()}
}

func (e *NotFoundError) Error() string        { return fmt.Sprintf("%s %q not found", e.Kind, e.ID) }
func (e *NotFoundError) Is(target error) bool { return target == ErrNotFound }

// ConflictError informa de un fallo de concurrencia optimista o de una identidad duplicada. Coincide con ErrConflict.
type ConflictError struct {
	Kind            string
	ID              string
	ExpectedVersion int64
	Reason          string
}

// Conflict construye un ConflictError.
func Conflict(kind string, id fmt.Stringer, expectedVersion int64, reason string) error {
	return &ConflictError{Kind: kind, ID: id.String(), ExpectedVersion: expectedVersion, Reason: reason}
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s %q conflict (expected version %d): %s", e.Kind, e.ID, e.ExpectedVersion, e.Reason)
}
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// FieldError describe un único fallo de validación de un campo.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidationError agrupa errores de campo (patrón notificación). Coincide con ErrValidation.
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

// RuleViolationError informa de un invariante de negocio roto. Coincide con ErrRuleViolation.
type RuleViolationError struct {
	Code    string
	Message string
}

// Violation construye un RuleViolationError.
func Violation(code, message string) error {
	return &RuleViolationError{Code: code, Message: message}
}

func (e *RuleViolationError) Error() string        { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func (e *RuleViolationError) Is(target error) bool { return target == ErrRuleViolation }

// Validation acumula errores de campo para que los constructores informen de todos los problemas a la vez.
//
//	var v domain.Validation
//	v.Require(name != "", "name", "required", "name is required")
//	if err := v.Err(); err != nil { return Party{}, err }
type Validation struct {
	errs []FieldError
}

// Add registra un error de campo.
func (v *Validation) Add(field, code, message string) {
	v.errs = append(v.errs, FieldError{Field: field, Code: code, Message: message})
}

// Require registra un error de campo cuando ok es false. Devuelve ok para encadenarlo en condiciones.
func (v *Validation) Require(ok bool, field, code, message string) bool {
	if !ok {
		v.Add(field, code, message)
	}
	return ok
}

// Merge absorbe err: los ValidationError anidados se aplanan (con los campos precedidos de prefix);
// cualquier otro error no nulo se registra bajo prefix con el código "invalid".
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

// HasErrors dice si se ha registrado algún error.
func (v *Validation) HasErrors() bool { return len(v.errs) > 0 }

// Err devuelve un *ValidationError con los errores acumulados, o nil si no hay ninguno.
func (v *Validation) Err() error {
	if len(v.errs) == 0 {
		return nil
	}
	return &ValidationError{Errors: append([]FieldError(nil), v.errs...)}
}
