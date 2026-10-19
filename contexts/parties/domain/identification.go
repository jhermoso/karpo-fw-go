package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// DocumentTypeID identifica un tipo de documento de identidad (los mismos GUID que el catálogo de C#).
type DocumentTypeID struct{ fw.UUID }

// ParseDocumentTypeID interpreta una identidad en texto.
func ParseDocumentTypeID(s string) (DocumentTypeID, error) {
	u, err := fw.ParseUUID(s)
	return DocumentTypeID{u}, err
}

// MustDocumentTypeID interpreta una identidad conocida.
func MustDocumentTypeID(s string) DocumentTypeID { return DocumentTypeID{fw.MustParseUUID(s)} }

// IdentificationID identifica una identificación de una party (entidad hija).
type IdentificationID struct{ fw.UUID }

// NewIdentificationID devuelve una identidad nueva.
func NewIdentificationID() IdentificationID { return IdentificationID{fw.NewUUID()} }

// ParseIdentificationID interpreta una identidad en texto.
func ParseIdentificationID(s string) (IdentificationID, error) {
	u, err := fw.ParseUUID(s)
	return IdentificationID{u}, err
}

// DocumentType es una entrada del catálogo de documentos de identidad (pasaporte, documento
// nacional, identificación fiscal...).
type DocumentType struct {
	ID                       DocumentTypeID
	Code                     string // al estilo ISO 20022: CCPT, NIDN, TXID, DRLC, SOSE, ARNU, OTHR
	Name                     vocab.Name
	DefaultPattern           string // se usa cuando no se aplica ninguna regla de país
	RequiresExpiry           bool
	RequiresIssuingAuthority bool
	Active                   bool
}

// CountryDocumentRule adapta un tipo de documento a un país: disponibilidad, formato y dígito de
// control (el country_document_rule de C#). Los indicadores de requisito nil heredan los del tipo
// de documento.
type CountryDocumentRule struct {
	ID                       string
	Country                  vocab.CountryCode
	DocumentType             DocumentTypeID
	Available                bool
	Default                  bool
	DisplayOrder             int
	Pattern                  string
	MinLength, MaxLength     int
	CheckDigit               string // clave de vocab.ValidCheckDigit
	RequiresExpiry           *bool
	RequiresIssuingAuthority *bool
	Active                   bool
}

// DocumentPolicy valida los números de identificación con el catálogo de documentos y las reglas
// de país. C# sólo marcaba un «estado de formato» después de guardar; aquí un número no válido se
// rechaza.
type DocumentPolicy struct {
	types    map[DocumentTypeID]DocumentType
	rules    map[string]CountryDocumentRule
	patterns map[string]*regexp.Regexp
}

func ruleKey(c vocab.CountryCode, t DocumentTypeID) string { return c.String() + "/" + t.String() }

// NewDocumentPolicy construye la política, compilando de antemano cada patrón y comprobando cada
// clave de dígito de control: un catálogo mal configurado falla al cargarse, no con el primer
// cliente.
func NewDocumentPolicy(types []DocumentType, rules []CountryDocumentRule) (*DocumentPolicy, error) {
	p := &DocumentPolicy{types: map[DocumentTypeID]DocumentType{}, rules: map[string]CountryDocumentRule{}, patterns: map[string]*regexp.Regexp{}}
	compile := func(pattern string) error {
		if pattern == "" || p.patterns[pattern] != nil {
			return nil
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("%w: document pattern %q: %v", fw.ErrValidation, pattern, err)
		}
		p.patterns[pattern] = re
		return nil
	}
	for _, t := range types {
		if err := compile(t.DefaultPattern); err != nil {
			return nil, err
		}
		p.types[t.ID] = t
	}
	for _, r := range rules {
		if _, ok := p.types[r.DocumentType]; !ok {
			return nil, fmt.Errorf("%w: rule %s references unknown document type %s", fw.ErrValidation, r.ID, r.DocumentType)
		}
		if err := compile(r.Pattern); err != nil {
			return nil, err
		}
		if r.CheckDigit != "" {
			if _, known := vocab.ValidCheckDigit(r.CheckDigit, ""); !known {
				return nil, fmt.Errorf("%w: rule %s uses unknown check digit %s", fw.ErrValidation, r.ID, r.CheckDigit)
			}
		}
		if r.Active {
			p.rules[ruleKey(r.Country, r.DocumentType)] = r
		}
	}
	return p, nil
}

// DocumentType devuelve un tipo de documento.
func (p *DocumentPolicy) DocumentType(id DocumentTypeID) (DocumentType, bool) {
	t, ok := p.types[id]
	return t, ok
}

// Options devuelve los tipos de documento disponibles en un país, en el orden de presentación (la
// consulta de referencia por país de C# que usaban los formularios).
func (p *DocumentPolicy) Options(country vocab.CountryCode) []CountryDocumentRule {
	var out []CountryDocumentRule
	for _, r := range p.rules {
		if r.Country == country && r.Available {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b CountryDocumentRule) int { return a.DisplayOrder - b.DisplayOrder })
	return out
}

// IdentificationData es lo que un usuario aporta para una identificación.
type IdentificationData struct {
	Type             DocumentTypeID
	Country          vocab.CountryCode
	Number           string
	IssuingAuthority string
	IssuedOn         vocab.Date // opcional
	ExpiresOn        vocab.Date // obligatoria cuando lo dice la regla o el tipo
}

// Validate comprueba una identificación y devuelve su número canónico (en mayúsculas, sin
// espacios y sin separadores, salvo que el formato del país los necesite).
func (p *DocumentPolicy) Validate(d IdentificationData) (string, error) {
	var v fw.Validation
	t, ok := p.types[d.Type]
	if !ok || !t.Active {
		v.Add("documentType", "unknown", "unknown or inactive document type")
		return "", v.Err()
	}
	if d.Country.IsZero() {
		v.Add("country", "required", "issuing country is required")
		return "", v.Err()
	}
	compact := strings.ToUpper(strings.Join(strings.Fields(d.Number), ""))
	if compact == "" || utf8.RuneCountInString(compact) > 60 {
		v.Add("number", "length", "document number must have 1 to 60 characters")
		return "", v.Err()
	}
	pattern, requiresExpiry, requiresAuthority := t.DefaultPattern, t.RequiresExpiry, t.RequiresIssuingAuthority
	rule, hasRule := p.rules[ruleKey(d.Country, d.Type)]
	if hasRule {
		if !rule.Available {
			return "", fw.Violation("parties.document_not_available", t.Name.String()+" is not issued in "+d.Country.String())
		}
		if rule.Pattern != "" {
			pattern = rule.Pattern
		}
		if rule.RequiresExpiry != nil {
			requiresExpiry = *rule.RequiresExpiry
		}
		if rule.RequiresIssuingAuthority != nil {
			requiresAuthority = *rule.RequiresIssuingAuthority
		}
	}
	// El número canónico quita los separadores (12345678-Z es 12345678Z) salvo que el formato los
	// necesite (el signo de siglo finlandés, el guion opcional sueco): gana la primera forma que encaja.
	fits := func(n string) []string {
		var problems []string
		l := utf8.RuneCountInString(n)
		if hasRule && rule.MinLength > 0 && l < rule.MinLength {
			problems = append(problems, fmt.Sprintf("at least %d characters", rule.MinLength))
		}
		if hasRule && rule.MaxLength > 0 && l > rule.MaxLength {
			problems = append(problems, fmt.Sprintf("at most %d characters", rule.MaxLength))
		}
		if pattern != "" && !p.patterns[pattern].MatchString(n) {
			problems = append(problems, "document number does not have the format of "+t.Name.String()+" in "+d.Country.String())
		}
		return problems
	}
	number := vocab.NormalizeDocumentNumber(compact)
	if problems := fits(number); len(problems) > 0 {
		number = compact
		for _, msg := range fits(compact) {
			v.Add("number", "format", msg)
		}
	}
	if hasRule && rule.CheckDigit != "" && !v.HasErrors() {
		if ok, _ := vocab.ValidCheckDigit(rule.CheckDigit, number); !ok {
			v.Add("number", "check_digit", "document number check digit does not match")
		}
	}
	v.Require(!requiresExpiry || !d.ExpiresOn.IsZero(), "expiresOn", "required", t.Name.String()+" requires an expiry date")
	v.Require(!requiresAuthority || strings.TrimSpace(d.IssuingAuthority) != "", "issuingAuthority", "required",
		t.Name.String()+" requires the issuing authority")
	v.Require(d.IssuedOn.IsZero() || d.ExpiresOn.IsZero() || d.IssuedOn.Before(d.ExpiresOn), "expiresOn", "order", "expiry must follow issue")
	v.Require(utf8.RuneCountInString(d.IssuingAuthority) <= 200, "issuingAuthority", "length", "at most 200 characters")
	return number, v.Err()
}

// Identification es un documento de identidad de una party (entidad hija de Party).
type Identification struct {
	ID               IdentificationID
	Type             DocumentTypeID
	Country          vocab.CountryCode
	Number           string
	IssuingAuthority string
	IssuedOn         vocab.Date
	ExpiresOn        vocab.Date
	Primary          bool
}

// Matches dice si la identificación es el mismo documento (tipo, país, número).
func (i Identification) Matches(t DocumentTypeID, c vocab.CountryCode, number string) bool {
	return i.Type == t && i.Country == c && i.Number == number
}

var spain = vocab.MustCountryCode("ES")

// DocumentFitsKind dice si una party de clase kind puede llevar un documento del tipo t expedido
// en country (docs/DECISIONES-COMUNES.md, D-85). Una organización sólo lleva identificaciones
// fiscales y «otros documentos»: el pasaporte, el documento nacional, el permiso de conducir, la
// Seguridad Social y el número de extranjero (NIE) son de personas. Una persona lleva todos salvo
// la identificación fiscal española, que es el CIF: el NIF de una persona es su DNI o su NIE. La
// identificación fiscal de otro país sí la lleva una persona. La regla vale al añadir un documento;
// lo que ya estaba guardado se rehidrata tal cual.
func DocumentFitsKind(kind Kind, t DocumentTypeID, country vocab.CountryCode) bool {
	switch kind {
	case KindOrganization:
		return t == DocTaxID || t == DocOther
	case KindPerson:
		return t != DocTaxID || country != spain
	}
	return false
}

// CheckKind rechaza un documento que la clase de party no puede llevar (DocumentFitsKind), con un
// mensaje que nombra el tipo de documento. Lo usa AddIdentification, y el caso de uso antes de
// buscar el documento en otras parties.
func (p *DocumentPolicy) CheckKind(kind Kind, d IdentificationData) error {
	if DocumentFitsKind(kind, d.Type, d.Country) {
		return nil
	}
	name := d.Type.String()
	if t, ok := p.types[d.Type]; ok {
		name = t.Name.String()
	}
	if kind == KindOrganization {
		return fw.Violation("parties.document_not_for_kind",
			"an organization is identified by a tax identification number or another document, not by a "+name)
	}
	return fw.Violation("parties.document_not_for_kind",
		"the "+name+" of "+d.Country.String()+" identifies organizations: a person is identified by a national identity or alien registration number")
}

// AddIdentification valida y añade un documento de identidad. Invariantes: un número válido para
// el tipo y el país, no registrado dos veces en la party, admitido por su clase (DocumentFitsKind),
// y una sola identificación principal (la primera es la principal; una nueva principal degrada a
// la anterior).
func (p *Party) AddIdentification(policy *DocumentPolicy, d IdentificationData, primary bool) (IdentificationID, error) {
	if err := p.requireActive("get identifications"); err != nil {
		return IdentificationID{}, err
	}
	number, err := policy.Validate(d)
	if err != nil {
		return IdentificationID{}, err
	}
	if err := policy.CheckKind(p.kind, d); err != nil {
		return IdentificationID{}, err
	}
	for _, i := range p.identifications {
		if i.Matches(d.Type, d.Country, number) {
			return IdentificationID{}, fw.Violation("parties.duplicate_identification", "the party already has this document")
		}
	}
	id := Identification{ID: NewIdentificationID(), Type: d.Type, Country: d.Country, Number: number,
		IssuingAuthority: strings.TrimSpace(d.IssuingAuthority), IssuedOn: d.IssuedOn, ExpiresOn: d.ExpiresOn,
		Primary: primary || len(p.identifications) == 0}
	ids := slices.Clone(p.identifications)
	if id.Primary {
		for k := range ids {
			ids[k].Primary = false
		}
	}
	p.identifications = append(ids, id)
	if d.Type == DocNationalID || d.Type == DocTaxID || d.Type == DocAlienRegistation {
		// Un documento nacional, fiscal o de extranjero que ha pasado sus comprobaciones responde a
		// la marca de uno rechazado. Un pasaporte, un permiso o un «otro documento» no responde a
		// nada: no es el documento que estaba mal.
		p.documentIssue = DocumentIssue{}
	}
	p.Raise(IdentificationAdded{EventMeta: p.NewEventMeta(), IdentificationID: id.ID.String(), DocumentType: d.Type.String(),
		Country: d.Country.String(), Number: number, Primary: id.Primary})
	return id.ID, nil
}

// RemoveIdentification quita un documento de identidad.
func (p *Party) RemoveIdentification(id IdentificationID) error {
	k := slices.IndexFunc(p.identifications, func(i Identification) bool { return i.ID == id })
	if k < 0 {
		return fw.NotFound("parties.identification", id)
	}
	removed := p.identifications[k]
	p.identifications = slices.Delete(slices.Clone(p.identifications), k, k+1)
	p.Raise(IdentificationRemoved{EventMeta: p.NewEventMeta(), IdentificationID: id.String(), DocumentType: removed.Type.String(),
		Country: removed.Country.String(), Number: removed.Number})
	return nil
}

// Identifications devuelve una copia de los documentos de identidad.
func (p *Party) Identifications() []Identification { return slices.Clone(p.identifications) }

// PrimaryIdentification devuelve el documento principal, si lo hay.
func (p *Party) PrimaryIdentification() (Identification, bool) {
	for _, i := range p.identifications {
		if i.Primary {
			return i, true
		}
	}
	return Identification{}, false
}
