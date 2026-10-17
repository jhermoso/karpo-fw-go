// Package authz reúne los contratos de autorización de Karpo: el port a Go del «contrato de
// autorización v1» de C# (Fw.Application.Contracts/Authorization).
//
// Una petición se autoriza en dos pasos:
//  1. un Authenticator convierte las credenciales (un token bearer) en un Principal;
//  2. un Resolver convierte el Principal y el ámbito de organizaciones pedido en una Resolution:
//     Allow con un Context, Deny con un motivo, o Indeterminate cuando la fuente de seguridad no
//     está disponible (falla cerrado, nunca un contexto vacío «por defecto»).
//
// El Context viaja en context.Context (WithContext / FromContext) en lugar de en un accesor de DI
// con ámbito, y lo consume la capa de aplicación: pipeline.RequirePermission comprueba los permisos
// en cualquier punto de entrada (HTTP, trabajos, mensajes), CanWrite hace cumplir los niveles de
// acceso por organización y ScopeSpec restringe las consultas a las organizaciones efectivas como
// una especificación traducida a SQL. No hay modo «Legacy»: sin contexto, las operaciones protegidas fallan.
package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// ActorKind distingue usuarios humanos, principales de servicio y el sistema.
type ActorKind string

// Tipos de actor.
const (
	Human   ActorKind = "human"
	Service ActorKind = "service"
	System  ActorKind = "system"
)

// Principal es un llamante autenticado, tal como lo produce un Authenticator a partir de sus credenciales.
type Principal struct {
	Subject domain.UUID       // id del usuario de seguridad o del principal de servicio ("sub")
	Name    string            // nombre de usuario ("username")
	PartyID domain.UUID       // la Party que hay detrás de un usuario humano ("partyId")
	Kind    ActorKind         // humano o servicio ("actorKind")
	Claims  map[string]string // cualquier otro claim, para los resolutores que lo necesiten
}

// Complete dice si el principal tiene los datos mínimos que exige el contrato
// (si falta alguno, se resuelve en Deny "incomplete-principal" -> 401).
func (p Principal) Complete() bool {
	if p.Subject.IsZero() || strings.TrimSpace(p.Name) == "" {
		return false
	}
	return p.Kind == Service || !p.PartyID.IsZero()
}

// Errores del paso de autenticación.
var (
	// ErrNoCredentials: la petición no lleva credenciales.
	ErrNoCredentials = fmt.Errorf("%w: no credentials", domain.ErrUnauthorized)
	// ErrInvalidCredentials: las credenciales están mal formadas, han caducado o no son de confianza.
	ErrInvalidCredentials = fmt.Errorf("%w: invalid credentials", domain.ErrUnauthorized)
)

// Authenticator valida credenciales. Devuelve un error que coincide con domain.ErrUnauthorized
// cuando faltan o no son válidas (implementación: distribution/jwtauth).
type Authenticator interface {
	Authenticate(ctx context.Context, credentials string) (Principal, error)
}

// Request es lo que necesita un Resolver además del principal.
type Request struct {
	// Credentials son las credenciales en bruto, para los resolutores que las reenvían (modo HTTP).
	Credentials string
	// RequestedOrganizations es el X-Organization-Scope validado (nil si falta).
	RequestedOrganizations []domain.UUID
	// CorrelationID identifica el flujo de negocio.
	CorrelationID string
}

// Resolver resuelve el contexto de autorización de un principal. Nunca debe devolver un contexto
// parcial o por defecto: cuando la fuente de seguridad falla, devuelve Indeterminate.
type Resolver interface {
	Resolve(ctx context.Context, p Principal, req Request) Resolution
}

// ResolverFunc adapta una función a Resolver.
type ResolverFunc func(ctx context.Context, p Principal, req Request) Resolution

// Resolve llama a fn.
func (fn ResolverFunc) Resolve(ctx context.Context, p Principal, req Request) Resolution {
	return fn(ctx, p, req)
}

// Outcome es el desenlace de una resolución.
type Outcome string

// Desenlaces.
const (
	Allow         Outcome = "allow"
	Deny          Outcome = "deny"
	Indeterminate Outcome = "indeterminate"
)

// Motivos de denegación conocidos.
const (
	ReasonIncompletePrincipal = "incomplete-principal" // -> 401
	ReasonUnknownSubject      = "unknown-subject"
	ReasonInactiveUser        = "inactive-user"
	ReasonLockedUser          = "locked-user"
	ReasonUnknownService      = "unknown-service-principal"
)

// Resolution es el resultado de resolver una petición. Invariante: Allow <=> Context != nil.
type Resolution struct {
	Outcome Outcome  `json:"outcome"`
	Context *Context `json:"context,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

// Allowed construye una resolución Allow (c no puede ser nil).
func Allowed(c *Context) Resolution {
	if c == nil {
		panic("authz: Allow requires a context")
	}
	return Resolution{Outcome: Allow, Context: c}
}

// Denied construye una resolución Deny.
func Denied(reason string) Resolution { return Resolution{Outcome: Deny, Reason: reason} }

// Undetermined construye una resolución Indeterminate (fuente de seguridad no disponible -> 503).
func Undetermined(reason string) Resolution {
	return Resolution{Outcome: Indeterminate, Reason: reason}
}

// Err convierte una resolución que no es Allow en la taxonomía de errores del dominio
// (principal incompleto -> ErrUnauthorized, otras denegaciones -> ErrForbidden,
// indeterminada -> ErrIndeterminate).
func (r Resolution) Err() error {
	switch r.Outcome {
	case Allow:
		return nil
	case Deny:
		if r.Reason == ReasonIncompletePrincipal {
			return fmt.Errorf("%w: %s", domain.ErrUnauthorized, r.Reason)
		}
		return fmt.Errorf("%w: %s", domain.ErrForbidden, r.Reason)
	default:
		return fmt.Errorf("%w: %s", ErrIndeterminate, r.Reason)
	}
}

// ErrIndeterminate informa de que no se ha podido decidir la autorización (se traduce a 503).
var ErrIndeterminate = errors.New("authorization indeterminate")

type ctxKey struct{}

// WithContext guarda el contexto de autorización en ctx.
func WithContext(ctx context.Context, c *Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromContext devuelve el contexto de autorización de ctx.
func FromContext(ctx context.Context) (*Context, bool) {
	c, ok := ctx.Value(ctxKey{}).(*Context)
	return c, ok && c != nil
}

// Actor devuelve el vocab.Actor de un contexto de autorización (la identidad de auditoría).
func (c *Context) Actor() vocab.Actor {
	id := c.ActorPartyID
	if id.IsZero() {
		id = c.Subject
	}
	return vocab.Actor{PartyID: id, Name: c.SubjectName}
}

func containsUUID(xs []domain.UUID, x domain.UUID) bool { return slices.Contains(xs, x) }

// Subject es la instantánea de seguridad de un sujeto humano, tal como la guarda el contexto delimitado Security.
type Subject struct {
	Active      bool
	Locked      bool
	Roles       []string
	Permissions []Permission
	Grants      []Grant
}

// Directory es el puerto por el que el resolutor genérico lee la instantánea de seguridad de un
// sujeto (lo implementa el contexto delimitado Security sobre sus repositorios). Devuelve
// found=false para un sujeto desconocido y un error sólo cuando la fuente no está disponible.
type Directory interface {
	Subject(ctx context.Context, id domain.UUID) (s Subject, found bool, err error)
}
