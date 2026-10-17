package domain

import (
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PersonalName es el nombre de una persona: nombre de pila más hasta dos apellidos (en España se
// usan dos; muchos países usan uno). Sustituye al PersonalName de C#, que admitía un solo apellido
// y por defecto valía "JOHN DOE" (ver docs/LENGUAJE-UBICUO.md).
type PersonalName struct {
	given, firstSurname, secondSurname string
}

// NewPersonalName valida y normaliza un nombre de persona (sin espacios en los extremos y con los
// interiores reducidos a uno; se conservan mayúsculas y minúsculas: "de la Fuente", "McDonald" y
// "O'Neill" los escriben las personas, no las reglas).
func NewPersonalName(given, firstSurname, secondSurname string) (PersonalName, error) {
	n := PersonalName{given: clean(given), firstSurname: clean(firstSurname), secondSurname: clean(secondSurname)}
	var v fw.Validation
	v.Require(n.given != "", "givenName", "required", "given name is required")
	v.Require(n.firstSurname != "", "firstSurname", "required", "first surname is required")
	v.Require(n.secondSurname == "" || n.firstSurname != "", "secondSurname", "order", "second surname requires a first surname")
	for field, s := range map[string]string{"givenName": n.given, "firstSurname": n.firstSurname, "secondSurname": n.secondSurname} {
		v.Require(len([]rune(s)) <= 60, field, "length", "at most 60 characters")
	}
	if err := v.Err(); err != nil {
		return PersonalName{}, err
	}
	return n, nil
}

// Given devuelve el nombre de pila.
func (n PersonalName) Given() string { return n.given }

// FirstSurname devuelve el primer apellido.
func (n PersonalName) FirstSurname() string { return n.firstSurname }

// SecondSurname devuelve el segundo apellido ("" si no lo hay).
func (n PersonalName) SecondSurname() string { return n.secondSurname }

// IsZero dice si falta el nombre.
func (n PersonalName) IsZero() bool { return n == PersonalName{} }

// String devuelve el nombre visible "Nombre Primero Segundo".
func (n PersonalName) String() string {
	return strings.TrimSpace(strings.Join([]string{n.given, n.firstSurname, n.secondSurname}, " "))
}

// Surnames devuelve "Primero Segundo".
func (n PersonalName) Surnames() string {
	return strings.TrimSpace(n.firstSurname + " " + n.secondSurname)
}

// OrganizationName es la razón social de una organización más un nombre comercial opcional.
type OrganizationName struct {
	legal vocab.Name
	trade string
}

// NewOrganizationName valida el nombre de una organización.
func NewOrganizationName(legal, trade string) (OrganizationName, error) {
	var v fw.Validation
	name, err := vocab.NewName(clean(legal))
	if err != nil {
		v.Merge("legalName", err)
	}
	trade = clean(trade)
	v.Require(len([]rune(trade)) <= 120, "tradeName", "length", "at most 120 characters")
	if err := v.Err(); err != nil {
		return OrganizationName{}, err
	}
	if strings.EqualFold(trade, name.String()) {
		trade = ""
	}
	return OrganizationName{legal: name, trade: trade}, nil
}

// Legal devuelve la razón social.
func (n OrganizationName) Legal() string { return n.legal.String() }

// Trade devuelve el nombre comercial ("" cuando es la razón social).
func (n OrganizationName) Trade() string { return n.trade }

// String devuelve el nombre visible: el comercial si lo hay; si no, la razón social.
func (n OrganizationName) String() string {
	if n.trade != "" {
		return n.trade
	}
	return n.legal.String()
}

// Gender es el género de una persona (tal como se registró; «unspecified» si no se indicó).
type Gender string

// Géneros.
const (
	GenderUnspecified Gender = ""
	GenderFemale      Gender = "female"
	GenderMale        Gender = "male"
	GenderOther       Gender = "other"
)

// ParseGender valida un género.
func ParseGender(s string) (Gender, error) {
	switch g := Gender(strings.ToLower(strings.TrimSpace(s))); g {
	case GenderUnspecified, GenderFemale, GenderMale, GenderOther:
		return g, nil
	}
	var v fw.Validation
	v.Add("gender", "enum", "gender must be female, male, other or empty")
	return "", v.Err()
}

// MaritalStatus es el estado civil de una persona.
type MaritalStatus string

// Estados civiles.
const (
	MaritalUnspecified    MaritalStatus = ""
	MaritalSingle         MaritalStatus = "single"
	MaritalMarried        MaritalStatus = "married"
	MaritalCivilPartner   MaritalStatus = "civil-partnership"
	MaritalSeparated      MaritalStatus = "separated"
	MaritalDivorced       MaritalStatus = "divorced"
	MaritalWidowed        MaritalStatus = "widowed"
	maritalStatusesString               = "single, married, civil-partnership, separated, divorced, widowed or empty"
)

// ParseMaritalStatus valida un estado civil.
func ParseMaritalStatus(s string) (MaritalStatus, error) {
	switch m := MaritalStatus(strings.ToLower(strings.TrimSpace(s))); m {
	case MaritalUnspecified, MaritalSingle, MaritalMarried, MaritalCivilPartner, MaritalSeparated, MaritalDivorced, MaritalWidowed:
		return m, nil
	}
	var v fw.Validation
	v.Add("maritalStatus", "enum", "marital status must be "+maritalStatusesString)
	return "", v.Err()
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }
