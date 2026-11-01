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

// scope es el ámbito de organizaciones del llamante (decisión P1 del contrato de autorización):
//   - una party es visible para las organizaciones internas del ámbito efectivo cuando es una de
//     ellas, una entrada compartida del catálogo, o está afiliada a una de ellas (una relación
//     activa);
//   - fuera del ámbito, una party no existe (404 uniforme);
//   - escribir exige un permiso Full sobre la propia party, o sobre una organización a la que
//     está afiliada y que la dio de alta; una organización que sólo se relaciona con ella necesita
//     además PermPartyUpdateRelated, y nadie salvo sus propios usuarios escribe una party que es
//     organización interna (docs/DECISIONES-COMUNES.md, D-65, D-66 y D-74); las entradas
//     compartidas del catálogo sólo las escriben los administradores globales.
type scope struct {
	global  bool
	ac      *authz.Context
	orgs    []domain.PartyID
	related bool // el llamante puede cambiar las parties con las que sus organizaciones sólo se relacionan
	// registered dice si la party se dio de alta junto con la relación que hay detrás de la
	// afiliación; lo responde el registro de auditoría (D-74). nil: nunca.
	registered func(p *domain.Party, a domain.Affiliation) bool
}

func scopeOf(ctx context.Context) scope {
	ac, ok := authz.FromContext(ctx)
	if !ok {
		return scope{}
	}
	s := scope{global: ac.GlobalAdmin, ac: ac, related: authz.Require(ctx, PermPartyUpdateRelated) == nil}
	for _, u := range ac.EffectiveOrganizations {
		s.orgs = append(s.orgs, domain.PartyID{UUID: u})
	}
	return s
}

// parties limita una consulta de parties al ámbito.
func (s scope) parties(at time.Time) spec.Spec[*domain.Party] {
	if s.global {
		return spec.All[*domain.Party]()
	}
	if len(s.orgs) == 0 {
		return spec.None[*domain.Party]()
	}
	return domain.VisibleTo(s.orgs, at)
}

// relationships limita una consulta de relaciones a las que son del ámbito.
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
	// (Internal Organization por su id: un tipo de rol añadido por debajo no contaría. El catálogo
	// todavía no rechaza nada ahí; véase docs/PARTIES.md §18.)
	if slices.ContainsFunc(p.Roles(), func(r domain.PartyRole) bool {
		return r.RoleType == domain.RoleInternalOrganization && !r.Annulled && r.IsActiveAt(at)
	}) {
		return false // una organización interna de otros: sólo la escriben sus propios usuarios
	}
	return slices.ContainsFunc(p.Affiliations(), func(a domain.Affiliation) bool {
		return a.Period.IsActiveAt(at) && s.canWriteOrg(a.Organization) && (s.related || (s.registered != nil && s.registered(p, a)))
	})
}

// seesOwned dice si el llamante ve lo que es de una organización (un contacto con dueña, D-122):
// lo de todos (dueña cero), y lo de las organizaciones de su ámbito, con cualquier acceso.
func (s scope) seesOwned(owner domain.PartyID) bool {
	return owner.IsZero() || s.global || (s.ac != nil && s.ac.CanRead(owner.UUID))
}

// writableOrgs son las organizaciones del ámbito en las que el llamante escribe (acceso Full).
func (s scope) writableOrgs() []domain.PartyID {
	var out []domain.PartyID
	for _, o := range s.orgs {
		if s.canWriteOrg(o) {
			out = append(out, o)
		}
	}
	return out
}

// isInternalOrganization dice si la party es ahora una organización interna (por el tipo exacto,
// como canWrite).
func isInternalOrganization(p *domain.Party, at time.Time) bool {
	return slices.ContainsFunc(p.Roles(), func(r domain.PartyRole) bool {
		return r.RoleType == domain.RoleInternalOrganization && !r.Annulled && r.IsActiveAt(at)
	})
}

// mayCorrectIdentity dice si el llamante, que ya puede escribir la party, puede además corregir su
// documento de identidad o su nombre o razón social (docs/DECISIONES-COMUNES.md, D-122, regla 3):
// el administrador global; los usuarios con acceso Full a la propia party, si es una organización
// interna; y, si no lo es, quien tiene además PermPartyUpdateRelated (el administrador de
// organización: así se lee «el administrador» de D-122 en Go).
func (s scope) mayCorrectIdentity(p *domain.Party) error {
	if s.global || s.canWriteOrg(p.ID()) {
		return nil
	}
	if isInternalOrganization(p, fw.Now()) {
		return fmt.Errorf("%w: only the users of the internal organization correct its identity", fw.ErrForbidden)
	}
	if !s.related {
		return fmt.Errorf("%w: the identity document and the name are corrected by an administrator", fw.ErrForbidden)
	}
	return nil
}

// visible devuelve la party, o un error uniforme de no encontrada si está fuera del ámbito.
func (s scope) visible(p *domain.Party, err error) (*domain.Party, error) {
	if err != nil {
		return nil, err
	}
	if !s.sees(p, fw.Now()) {
		return nil, fw.NotFound(domain.PartyKind, p.ID())
	}
	return p, nil
}

// writable comprueba una party que el llamante va a cambiar: 404 fuera del ámbito, 403 sin Full.
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
