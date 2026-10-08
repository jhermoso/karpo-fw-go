package domain

import (
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PersonalName is the name of a person: given name plus up to two surnames (Spanish naming uses
// two; many countries use one). It replaces the C# PersonalName, which allowed one surname and
// defaulted to "JOHN DOE" (see docs/LENGUAJE-UBICUO.md).
type PersonalName struct {
	given, firstSurname, secondSurname string
}

// NewPersonalName validates and normalizes a personal name (trimmed, inner spaces collapsed;
// case is preserved: "de la Fuente", "McDonald" and "O'Neill" are written by people, not rules).
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

// Given returns the given name.
func (n PersonalName) Given() string { return n.given }

// FirstSurname returns the first surname.
func (n PersonalName) FirstSurname() string { return n.firstSurname }

// SecondSurname returns the second surname ("" when there is none).
func (n PersonalName) SecondSurname() string { return n.secondSurname }

// IsZero reports whether the name is absent.
func (n PersonalName) IsZero() bool { return n == PersonalName{} }

// String returns the display name "Given First Second".
func (n PersonalName) String() string {
	return strings.TrimSpace(strings.Join([]string{n.given, n.firstSurname, n.secondSurname}, " "))
}

// Surnames returns "First Second".
func (n PersonalName) Surnames() string {
	return strings.TrimSpace(n.firstSurname + " " + n.secondSurname)
}

// OrganizationName is the legal name of an organization plus an optional trade name.
type OrganizationName struct {
	legal vocab.Name
	trade string
}

// NewOrganizationName validates an organization name.
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

// Legal returns the legal name.
func (n OrganizationName) Legal() string { return n.legal.String() }

// Trade returns the trade name ("" when it is the legal name).
func (n OrganizationName) Trade() string { return n.trade }

// String returns the display name: the trade name when there is one, else the legal name.
func (n OrganizationName) String() string {
	if n.trade != "" {
		return n.trade
	}
	return n.legal.String()
}

// Gender of a person (as registered; "unspecified" when not provided).
type Gender string

// Genders.
const (
	GenderUnspecified Gender = ""
	GenderFemale      Gender = "female"
	GenderMale        Gender = "male"
	GenderOther       Gender = "other"
)

// ParseGender validates a gender.
func ParseGender(s string) (Gender, error) {
	switch g := Gender(strings.ToLower(strings.TrimSpace(s))); g {
	case GenderUnspecified, GenderFemale, GenderMale, GenderOther:
		return g, nil
	}
	var v fw.Validation
	v.Add("gender", "enum", "gender must be female, male, other or empty")
	return "", v.Err()
}

// MaritalStatus of a person.
type MaritalStatus string

// Marital statuses.
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

// ParseMaritalStatus validates a marital status.
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
