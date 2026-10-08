// Package authz holds the authorization contracts of Karpo: the Go port of the C# "contrato de
// autorización v1" (Fw.Application.Contracts/Authorization).
//
// A request is authorized in two steps:
//  1. an Authenticator turns credentials (a bearer token) into a Principal;
//  2. a Resolver turns the Principal and the requested organization scope into a Resolution:
//     Allow with a Context, Deny with a reason, or Indeterminate when the security source is
//     unavailable (fail closed, never an empty "default" context).
//
// The Context travels in context.Context (WithContext / FromContext) instead of a scoped DI
// accessor, and is consumed by the application layer: pipeline.RequirePermission checks
// permissions for any entry point (HTTP, jobs, messages), CanWrite enforces organization access
// levels and ScopeSpec restricts queries to the effective organizations as a specification
// translated to SQL. There is no "Legacy" mode: without a context, protected operations fail.
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

// ActorKind distinguishes human users, service principals and the system.
type ActorKind string

// Actor kinds.
const (
	Human   ActorKind = "human"
	Service ActorKind = "service"
	System  ActorKind = "system"
)

// Principal is an authenticated caller, as produced by an Authenticator from its credentials.
type Principal struct {
	Subject domain.UUID       // security user or service principal id ("sub")
	Name    string            // user name ("username")
	PartyID domain.UUID       // the Party behind a human user ("partyId")
	Kind    ActorKind         // human or service ("actorKind")
	Claims  map[string]string // any other claim, for resolvers that need it
}

// Complete reports whether the principal has the minimum data the contract requires
// (a missing one resolves to Deny "incomplete-principal" -> 401).
func (p Principal) Complete() bool {
	if p.Subject.IsZero() || strings.TrimSpace(p.Name) == "" {
		return false
	}
	return p.Kind == Service || !p.PartyID.IsZero()
}

// Errors of the authentication step.
var (
	// ErrNoCredentials: the request carries no credentials.
	ErrNoCredentials = fmt.Errorf("%w: no credentials", domain.ErrUnauthorized)
	// ErrInvalidCredentials: the credentials are malformed, expired or not trusted.
	ErrInvalidCredentials = fmt.Errorf("%w: invalid credentials", domain.ErrUnauthorized)
)

// Authenticator validates credentials. It returns an error matching domain.ErrUnauthorized when
// they are missing or invalid (implementation: distribution/jwtauth).
type Authenticator interface {
	Authenticate(ctx context.Context, credentials string) (Principal, error)
}

// Request is what a Resolver needs besides the principal.
type Request struct {
	// Credentials are the raw credentials, for resolvers that forward them (HTTP mode).
	Credentials string
	// RequestedOrganizations is the validated X-Organization-Scope (nil when absent).
	RequestedOrganizations []domain.UUID
	// CorrelationID identifies the business flow.
	CorrelationID string
}

// Resolver resolves the authorization context of a principal. It must never return a partial
// or default context: when the security source fails it returns Indeterminate.
type Resolver interface {
	Resolve(ctx context.Context, p Principal, req Request) Resolution
}

// ResolverFunc adapts a function into a Resolver.
type ResolverFunc func(ctx context.Context, p Principal, req Request) Resolution

// Resolve calls fn.
func (fn ResolverFunc) Resolve(ctx context.Context, p Principal, req Request) Resolution {
	return fn(ctx, p, req)
}

// Outcome of a resolution.
type Outcome string

// Outcomes.
const (
	Allow         Outcome = "allow"
	Deny          Outcome = "deny"
	Indeterminate Outcome = "indeterminate"
)

// Well-known deny reasons.
const (
	ReasonIncompletePrincipal = "incomplete-principal" // -> 401
	ReasonUnknownSubject      = "unknown-subject"
	ReasonInactiveUser        = "inactive-user"
	ReasonLockedUser          = "locked-user"
	ReasonUnknownService      = "unknown-service-principal"
)

// Resolution is the result of resolving a request. Invariant: Allow <=> Context != nil.
type Resolution struct {
	Outcome Outcome  `json:"outcome"`
	Context *Context `json:"context,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

// Allowed builds an Allow resolution (c must not be nil).
func Allowed(c *Context) Resolution {
	if c == nil {
		panic("authz: Allow requires a context")
	}
	return Resolution{Outcome: Allow, Context: c}
}

// Denied builds a Deny resolution.
func Denied(reason string) Resolution { return Resolution{Outcome: Deny, Reason: reason} }

// Undetermined builds an Indeterminate resolution (security source unavailable -> 503).
func Undetermined(reason string) Resolution {
	return Resolution{Outcome: Indeterminate, Reason: reason}
}

// Err converts a non-Allow resolution into the domain error taxonomy
// (incomplete principal -> ErrUnauthorized, other denials -> ErrForbidden,
// indeterminate -> ErrIndeterminate).
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

// ErrIndeterminate reports that authorization could not be decided (maps to 503).
var ErrIndeterminate = errors.New("authorization indeterminate")

type ctxKey struct{}

// WithContext stores the authorization context in ctx.
func WithContext(ctx context.Context, c *Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromContext returns the authorization context of ctx.
func FromContext(ctx context.Context) (*Context, bool) {
	c, ok := ctx.Value(ctxKey{}).(*Context)
	return c, ok && c != nil
}

// Actor returns the vocab.Actor of an authorization context (the audit identity).
func (c *Context) Actor() vocab.Actor {
	id := c.ActorPartyID
	if id.IsZero() {
		id = c.Subject
	}
	return vocab.Actor{PartyID: id, Name: c.SubjectName}
}

func containsUUID(xs []domain.UUID, x domain.UUID) bool { return slices.Contains(xs, x) }

// Subject is the security snapshot of a human subject, as stored by the Security bounded context.
type Subject struct {
	Active      bool
	Locked      bool
	Roles       []string
	Permissions []Permission
	Grants      []Grant
}

// Directory is the port through which the generic resolver reads the security snapshot of a
// subject (implemented by the Security bounded context over its repositories). It returns
// found=false for an unknown subject and an error only when the source is unavailable.
type Directory interface {
	Subject(ctx context.Context, id domain.UUID) (s Subject, found bool, err error)
}
