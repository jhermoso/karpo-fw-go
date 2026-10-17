// Package distribution proporciona las primitivas de la capa de transporte y entrega para servicios HTTP/REST.
// Está en lo alto de la jerarquía de la arquitectura limpia (Distribution -> Application -> Domain).
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

// WithTenantID devuelve un contexto nuevo con el ID de tenant dado.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey, tenantID)
}

// TenantID obtiene del contexto el ID de tenant, o devuelve la cadena vacía si no está.
func TenantID(ctx context.Context) string {
	if val, ok := ctx.Value(tenantKey).(string); ok {
		return val
	}
	return ""
}

// WithActorID devuelve un contexto nuevo con el ID de actor dado.
func WithActorID(ctx context.Context, actorID string) context.Context {
	return context.WithValue(ctx, actorKey, actorID)
}

// ActorID obtiene del contexto el ID de actor, o devuelve la cadena vacía si no está.
func ActorID(ctx context.Context) string {
	if val, ok := ctx.Value(actorKey).(string); ok {
		return val
	}
	return ""
}

// WithOrganizationID devuelve un contexto nuevo con el ID de organización dado.
func WithOrganizationID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, organizationKey, orgID)
}

// OrganizationID obtiene del contexto el ID de organización, o devuelve la cadena vacía si no está.
func OrganizationID(ctx context.Context) string {
	if val, ok := ctx.Value(organizationKey).(string); ok {
		return val
	}
	return ""
}
