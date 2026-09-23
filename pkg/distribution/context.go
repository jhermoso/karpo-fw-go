// Package distribution provides transport and delivery layer primitives for HTTP/REST services.
// It sits at the top of the Clean Architecture hierarchy (Distribution -> Application -> Domain).
package distribution

import (
	"context"
)

type contextKey string

const (
	tenantKey       contextKey = "karpo.tenant_id"
	actorKey        contextKey = "karpo.actor_id"
	organizationKey contextKey = "karpo.organization_id"
)

// WithTenantID returns a new context with the given tenant ID.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey, tenantID)
}

// TenantID retrieves the tenant ID from the context, or returns empty string if not present.
func TenantID(ctx context.Context) string {
	if val, ok := ctx.Value(tenantKey).(string); ok {
		return val
	}
	return ""
}

// WithActorID returns a new context with the given actor ID.
func WithActorID(ctx context.Context, actorID string) context.Context {
	return context.WithValue(ctx, actorKey, actorID)
}

// ActorID retrieves the actor ID from the context, or returns empty string if not present.
func ActorID(ctx context.Context) string {
	if val, ok := ctx.Value(actorKey).(string); ok {
		return val
	}
	return ""
}

// WithOrganizationID returns a new context with the given organization ID.
func WithOrganizationID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, organizationKey, orgID)
}

// OrganizationID retrieves the organization ID from the context, or returns empty string if not present.
func OrganizationID(ctx context.Context) string {
	if val, ok := ctx.Value(organizationKey).(string); ok {
		return val
	}
	return ""
}
