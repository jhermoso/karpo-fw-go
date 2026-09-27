package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// scope is the organization scope of the caller (decision P1 of the authorization contract):
//   - a party is visible to the internal organizations of the effective scope when it is one of
//     them, a shared catalog entry, or affiliated with one of them (an active relationship);
//   - out of scope, a party does not exist (uniform 404);
//   - writing needs a Full grant on the party itself or on an organization it is affiliated with;
//     shared catalog entries are written by global administrators only.
type scope struct {
	global bool
	ac     *authz.Context
	orgs   []domain.PartyID
}

func scopeOf(ctx context.Context) scope {
	ac, ok := authz.FromContext(ctx)
	if !ok {
		return scope{}
	}
	s := scope{global: ac.GlobalAdmin, ac: ac}
	for _, u := range ac.EffectiveOrganizations {
		s.orgs = append(s.orgs, domain.PartyID{UUID: u})
	}
	return s
}

// parties restricts a party query to the scope.
func (s scope) parties(at time.Time) spec.Spec[*domain.Party] {
	if s.global {
		return spec.All[*domain.Party]()
	}
	if len(s.orgs) == 0 {
		return spec.None[*domain.Party]()
	}
	return domain.VisibleTo(s.orgs, at)
}

// relationships restricts a relationship query to those owned by the scope.
func (s scope) relationships() spec.Spec[*domain.Relationship] {
	if s.global {
		return spec.All[*domain.Relationship]()
	}
	if len(s.orgs) == 0 {
		return spec.None[*domain.Relationship]()
	}
	return domain.OwnedBy(s.orgs)
}

func (s scope) sees(p *domain.Party, at time.Time) bool {
	return s.global || s.parties(at).IsSatisfiedBy(p)
}

func (s scope) canWriteOrg(org domain.PartyID) bool {
	return s.global || (s.ac != nil && s.ac.CanWrite(org.UUID))
}

func (s scope) canWrite(p *domain.Party, at time.Time) bool {
	if s.global {
		return true
	}
	if p.IsShared() {
		return false
	}
	if s.canWriteOrg(p.ID()) {
		return true
	}
	return slices.ContainsFunc(p.OrganizationsAt(at), s.canWriteOrg)
}

// visible returns the party or a uniform not-found error when it is out of scope.
func (s scope) visible(p *domain.Party, err error) (*domain.Party, error) {
	if err != nil {
		return nil, err
	}
	if !s.sees(p, fw.Now()) {
		return nil, fw.NotFound(domain.PartyKind, p.ID())
	}
	return p, nil
}

// writable checks a party the caller is about to change: 404 out of scope, 403 without Full.
func (s scope) writable(p *domain.Party) error {
	now := fw.Now()
	if !s.sees(p, now) {
		return fw.NotFound(domain.PartyKind, p.ID())
	}
	if !s.canWrite(p, now) {
		return fmt.Errorf("%w: the party is read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}
