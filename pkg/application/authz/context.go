package authz

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// AccessLevel is the access a user has to an organization.
type AccessLevel string

// Access levels (contract v1, P2: Full writes; ReadOnly reads; Restricted behaves as ReadOnly).
const (
	Full       AccessLevel = "Full"
	ReadOnly   AccessLevel = "ReadOnly"
	Restricted AccessLevel = "Restricted"
)

// ParseAccessLevel parses a stored level; unknown or empty values are Restricted (never fails).
func ParseAccessLevel(s string) AccessLevel {
	switch AccessLevel(s) {
	case Full, ReadOnly:
		return AccessLevel(s)
	}
	return Restricted
}

// CanWrite reports whether the level allows writing.
func (l AccessLevel) CanWrite() bool { return l == Full }

// Grant is one organization access of the subject, as stored by Security. IncludeSubsidiaries
// is transported but does not expand anything in v1 (business decision P2).
type Grant struct {
	OrganizationID      domain.UUID `json:"organizationId"`
	Level               AccessLevel `json:"level"`
	IncludeSubsidiaries bool        `json:"includeSubsidiaries"`
}

// Context is the resolved authorization of one request (contract v1 AuthorizationContext).
// Build it with NewContext, which enforces the contract invariants.
type Context struct {
	TenantID               string        `json:"tenantId"`
	Subject                domain.UUID   `json:"subjectId"`
	SubjectName            string        `json:"subjectName"`
	Kind                   ActorKind     `json:"actorKind"`
	ActorPartyID           domain.UUID   `json:"actorPartyId,omitzero"`
	GlobalAdmin            bool          `json:"isGlobalAdmin"`
	Grants                 []Grant       `json:"grants"`
	Permissions            []Permission  `json:"permissions"`
	RequestedOrganizations []domain.UUID `json:"requestedOrganizationIds,omitempty"`
	EffectiveOrganizations []domain.UUID `json:"effectiveOrganizationIds"`
	PolicyVersion          string        `json:"policyVersion"`
	CorrelationID          string        `json:"correlationId,omitempty"`
}

// NewContext validates the contract invariants: a human needs a party id; effective
// organizations must be a subset of the grants. When effective is nil it is computed as
// grants ∩ requested (all grants when nothing was requested).
func NewContext(c Context) (*Context, error) {
	if c.Subject.IsZero() || strings.TrimSpace(c.SubjectName) == "" {
		return nil, fmt.Errorf("%w: subject and name are required", domain.ErrValidation)
	}
	if c.Kind == Human && c.ActorPartyID.IsZero() {
		return nil, fmt.Errorf("%w: a human actor requires a party id (contract v1, invariant 5)", domain.ErrValidation)
	}
	granted := make([]domain.UUID, 0, len(c.Grants))
	for _, g := range c.Grants {
		if !containsUUID(granted, g.OrganizationID) {
			granted = append(granted, g.OrganizationID)
		}
	}
	if c.EffectiveOrganizations == nil {
		c.EffectiveOrganizations = EffectiveOrganizations(c.Grants, c.RequestedOrganizations)
	}
	for _, id := range c.EffectiveOrganizations {
		if !containsUUID(granted, id) {
			return nil, fmt.Errorf("%w: effective organizations must be a subset of the grants (contract v1, invariant 2)", domain.ErrValidation)
		}
	}
	c.Grants = slices.Clone(c.Grants)
	c.Permissions = slices.Clone(c.Permissions)
	return &c, nil
}

// EffectiveOrganizations computes grants ∩ requested (all granted organizations when nothing
// was requested), without expanding subsidiaries.
func EffectiveOrganizations(grants []Grant, requested []domain.UUID) []domain.UUID {
	out := []domain.UUID{}
	for _, g := range grants {
		if containsUUID(out, g.OrganizationID) {
			continue
		}
		if len(requested) == 0 || containsUUID(requested, g.OrganizationID) {
			out = append(out, g.OrganizationID)
		}
	}
	return out
}

// HasPermission reports whether the context grants a permission: global admins and the
// wildcard grant everything; malformed codes are never granted.
func (c *Context) HasPermission(p Permission) bool {
	if !p.WellFormed() {
		return false
	}
	return c.GlobalAdmin || slices.Contains(c.Permissions, Wildcard) || slices.Contains(c.Permissions, p)
}

// CanRead reports whether the organization is inside the effective scope.
func (c *Context) CanRead(org domain.UUID) bool {
	return c.GlobalAdmin || containsUUID(c.EffectiveOrganizations, org)
}

// CanWrite reports whether the subject may write data owned by org: global admins always; others
// need a Full grant on org and org inside the effective scope (P2).
func (c *Context) CanWrite(org domain.UUID) bool {
	if c.GlobalAdmin {
		return true
	}
	if !containsUUID(c.EffectiveOrganizations, org) {
		return false
	}
	for _, g := range c.Grants {
		if g.OrganizationID == org && g.Level.CanWrite() {
			return true
		}
	}
	return false
}

// PolicyVersion computes the deterministic version of a subject's policy (SHA-256, hex) from
// its active flag, roles, permissions and grants, all sorted. Resolvers include it in the
// context so clients can detect revocations; it changes with any relevant change.
func PolicyVersion(active bool, roles []string, permissions []Permission, grants []Grant) string {
	r := slices.Clone(roles)
	slices.Sort(r)
	p := make([]string, len(permissions))
	for i, x := range permissions {
		p[i] = string(x)
	}
	slices.Sort(p)
	g := make([]string, len(grants))
	for i, x := range grants {
		g[i] = fmt.Sprintf("%s:%s:%t", x.OrganizationID, x.Level, x.IncludeSubsidiaries)
	}
	slices.Sort(g)
	h := sha256.Sum256([]byte(fmt.Sprintf("active=%t|roles=%s|perms=%s|grants=%s",
		active, strings.Join(r, ","), strings.Join(p, ","), strings.Join(g, ","))))
	return hex.EncodeToString(h[:])
}
