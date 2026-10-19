package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// RoleDetails son los datos que lleva un rol de una party por su tipo (el perfil del rol). Como
// RelationshipDetails en las relaciones, el tipo es un dato y los detalles de cada tipo son objetos
// de valor; los de los otros tipos se quedan a cero.
type RoleDetails struct {
	SelfEmployed SelfEmployedDetails // roles Self Employed o por debajo de él
}

// SelfEmployedDetails es el perfil de actividad de un autónomo (docs/DECISIONES-COMUNES.md, D-85):
// el nombre con el que trabaja, el epígrafe de su actividad y cuándo se dio de alta en ella. Todos
// los datos son opcionales; el valor cero es un perfil vacío. Su NIF no está aquí: es su documento
// de identidad.
type SelfEmployedDetails struct {
	// TradeName es el nombre comercial, con los espacios normalizados.
	TradeName string
	// ActivityCode es el epígrafe de la actividad (el del IAE en España), en mayúsculas.
	ActivityCode string
	// RegisteredOn es la fecha de alta en la actividad; cero si no se conoce. Nunca futura.
	RegisteredOn vocab.Date
}

// Límites del perfil de un autónomo.
const (
	MaxSelfEmployedTradeName = 120 // como el nombre comercial de una organización
	MaxActivityCode          = 10
)

// NewSelfEmployedDetails construye un perfil de autónomo: recorta el nombre comercial y escribe el
// epígrafe en mayúsculas y sin espacios alrededor.
func NewSelfEmployedDetails(tradeName, activityCode string, registeredOn vocab.Date) (SelfEmployedDetails, error) {
	d := SelfEmployedDetails{TradeName: strings.Join(strings.Fields(tradeName), " "),
		ActivityCode: strings.ToUpper(strings.TrimSpace(activityCode)), RegisteredOn: registeredOn}
	var v fw.Validation
	v.Require(utf8.RuneCountInString(d.TradeName) <= MaxSelfEmployedTradeName, "tradeName", "length", "trade name must have at most 120 characters")
	v.Require(utf8.RuneCountInString(d.ActivityCode) <= MaxActivityCode, "activityCode", "length", "activity code must have at most 10 characters")
	v.Require(d.RegisteredOn.IsZero() || !d.RegisteredOn.After(vocab.DateOf(fw.Now())), "registeredOn", "future",
		"the registration in the activity cannot be in the future")
	return d, v.Err()
}

// IsZero dice si el perfil está vacío.
func (d SelfEmployedDetails) IsZero() bool { return d == SelfEmployedDetails{} }

// SetSelfEmployedDetails fija el perfil de actividad de un rol Self Employed (o de un rol por
// debajo de él). Invariantes: la party está activa y no es un duplicado (D-64), el rol existe, no
// está anulado y es de un tipo que lleva ese perfil; el perfil cumple sus reglas aunque se haya
// construido sin NewSelfEmployedDetails (se normaliza y se valida aquí). Un perfil vacío lo borra.
// El rol tiene que estar vigente ahora: el perfil de un rol terminado ya no cambia, y así todo
// cambio queda en el registro de auditoría, que lleva el perfil de los roles vigentes.
func (p *Party) SetSelfEmployedDetails(c RoleCatalog, id PartyRoleID, d SelfEmployedDetails) error {
	if err := p.requireActive("change its roles"); err != nil {
		return err
	}
	d, err := NewSelfEmployedDetails(d.TradeName, d.ActivityCode, d.RegisteredOn)
	if err != nil {
		return err
	}
	if p.duplicate {
		return fw.Violation("parties.duplicate", "the party is the duplicate of another: it takes part in nothing but queries")
	}
	i := slices.IndexFunc(p.roles, func(r PartyRole) bool { return r.ID == id })
	if i < 0 {
		return fw.NotFound("parties.party_role", id)
	}
	r := p.roles[i]
	if r.Annulled {
		return fw.Violation("parties.role_annulled", "the role was annulled: it was given by mistake")
	}
	if !c.IsA(r.RoleType, RoleSelfEmployed) {
		return fw.Violation("parties.role_details_not_for_type", "only a self-employed role carries a self-employed profile")
	}
	if !r.IsActiveAt(fw.Now()) {
		return fw.Violation("parties.role_ended", "the role is not in force now: its profile no longer changes")
	}
	if r.Details.SelfEmployed == d {
		return nil
	}
	p.roles = slices.Clone(p.roles)
	p.roles[i].Details.SelfEmployed = d
	e := SelfEmployedDetailsSet{EventMeta: p.NewEventMeta(), RoleID: id.String(), TradeName: d.TradeName, ActivityCode: d.ActivityCode}
	if !d.RegisteredOn.IsZero() {
		e.RegisteredOn = d.RegisteredOn.String()
	}
	p.Raise(e)
	return nil
}
