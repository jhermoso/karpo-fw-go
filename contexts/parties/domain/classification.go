package domain

import (
	"fmt"
	"slices"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// ClassificationTypeID identifica una clasificación (los mismos GUID que las filas party_type de C#).
type ClassificationTypeID struct{ fw.UUID }

// ParseClassificationTypeID interpreta una identidad en texto.
func ParseClassificationTypeID(s string) (ClassificationTypeID, error) {
	u, err := fw.ParseUUID(s)
	return ClassificationTypeID{u}, err
}

// MustClassificationTypeID interpreta una identidad conocida.
func MustClassificationTypeID(s string) ClassificationTypeID {
	return ClassificationTypeID{fw.MustParseUUID(s)}
}

// ClassificationID identifica una clasificación de una party (entidad hija).
type ClassificationID struct{ fw.UUID }

// NewClassificationID devuelve una identidad nueva.
func NewClassificationID() ClassificationID { return ClassificationID{fw.NewUUID()} }

// ParseClassificationID interpreta una identidad en texto.
func ParseClassificationID(s string) (ClassificationID, error) {
	u, err := fw.ParseUUID(s)
	return ClassificationID{u}, err
}

// ClassificationType es una entrada del catálogo de clasificaciones: las familias (segmento de
// cliente, sector de actividad, tamaño de organización, canal de captación, riesgo AML) agrupan
// las hojas que se asignan a las parties. Una familia inactiva (AML hasta que cumplimiento la
// habilite) y sus hojas no se pueden asignar. En una familia exclusiva una party tiene una sola
// hoja vigente a la vez.
type ClassificationType struct {
	ID          ClassificationTypeID
	Name        vocab.Name
	Description string
	Family      *ClassificationTypeID // nil en una familia
	Active      bool
	AppliesTo   Applicability
	Exclusive   bool
}

// ClassificationCatalog responde a preguntas sobre las clasificaciones.
type ClassificationCatalog struct {
	types map[ClassificationTypeID]ClassificationType
	order []ClassificationTypeID
}

// NewClassificationCatalog construye el catálogo: dos niveles, primero las familias.
func NewClassificationCatalog(types []ClassificationType) (*ClassificationCatalog, error) {
	c := &ClassificationCatalog{types: map[ClassificationTypeID]ClassificationType{}}
	for _, t := range types {
		c.types[t.ID] = t
		c.order = append(c.order, t.ID)
	}
	for _, t := range types {
		if t.Family == nil {
			continue
		}
		f, ok := c.types[*t.Family]
		if !ok || f.Family != nil {
			return nil, fmt.Errorf("%w: classification %s must belong to a known family", fw.ErrValidation, t.ID)
		}
	}
	return c, nil
}

// Type devuelve un tipo de clasificación.
func (c *ClassificationCatalog) Type(id ClassificationTypeID) (ClassificationType, bool) {
	t, ok := c.types[id]
	return t, ok
}

// All devuelve todos los tipos en el orden en que se declararon.
func (c *ClassificationCatalog) All() []ClassificationType {
	out := make([]ClassificationType, len(c.order))
	for i, id := range c.order {
		out[i] = c.types[id]
	}
	return out
}

// Classification es una clasificación de una party durante un periodo (entidad hija de Party).
type Classification struct {
	ID     ClassificationID
	Type   ClassificationTypeID
	Period vocab.ValidPeriod
}

// Classify clasifica la party a partir de un instante. Invariantes: el tipo es una hoja activa de
// una familia activa, encaja con la clase de party, no está ya vigente y, en una familia
// exclusiva, ninguna otra hoja de la familia está vigente en el periodo.
func (p *Party) Classify(c *ClassificationCatalog, typ ClassificationTypeID, from time.Time) (ClassificationID, error) {
	if err := p.requireActive("be classified"); err != nil {
		return ClassificationID{}, err
	}
	t, ok := c.Type(typ)
	if !ok {
		var v fw.Validation
		v.Add("classification", "unknown", "unknown classification")
		return ClassificationID{}, v.Err()
	}
	if t.Family == nil {
		return ClassificationID{}, fw.Violation("parties.classification_is_family", t.Name.String()+" is a family and cannot be assigned")
	}
	family, _ := c.Type(*t.Family)
	if !t.Active || !family.Active {
		return ClassificationID{}, fw.Violation("parties.classification_inactive", t.Name.String()+" is not active")
	}
	if a := family.AppliesTo; (a == AppliesToPerson && p.kind != KindPerson) || (a == AppliesToOrganization && p.kind != KindOrganization) {
		return ClassificationID{}, fw.Violation("parties.classification_incompatible", family.Name.String()+" does not apply to this kind of party")
	}
	period, err := vocab.OpenPeriodFrom(from)
	if err != nil {
		return ClassificationID{}, err
	}
	for _, x := range p.classifications {
		if !x.Period.Overlaps(period) {
			continue
		}
		if x.Type == typ {
			return ClassificationID{}, fw.Violation("parties.classification_overlap", "the party already has this classification in that period")
		}
		if other, ok := c.Type(x.Type); ok && family.Exclusive && other.Family != nil && *other.Family == family.ID {
			return ClassificationID{}, fw.Violation("parties.classification_exclusive",
				"the party already has a "+family.Name.String()+" in that period: end it first")
		}
	}
	cl := Classification{ID: NewClassificationID(), Type: typ, Period: period}
	p.classifications = append(slices.Clone(p.classifications), cl)
	p.Raise(PartyClassified{EventMeta: p.NewEventMeta(), ClassificationID: cl.ID.String(), Type: typ.String(), From: period.From()})
	return cl.ID, nil
}

// EndClassification termina una clasificación en un instante (no antes de que empezara).
func (p *Party) EndClassification(id ClassificationID, at time.Time) error {
	k := slices.IndexFunc(p.classifications, func(c Classification) bool { return c.ID == id })
	if k < 0 {
		return fw.NotFound("parties.classification", id)
	}
	c := p.classifications[k]
	if end, closed := c.Period.To(); closed && !at.Before(end) {
		return nil
	}
	period, err := vocab.NewValidPeriod(c.Period.From(), &at)
	if err != nil {
		return fw.Violation("parties.classification_end_before_start", "a classification cannot end before it starts")
	}
	p.classifications = slices.Clone(p.classifications)
	p.classifications[k].Period = period
	p.Raise(PartyClassificationEnded{EventMeta: p.NewEventMeta(), ClassificationID: id.String(), Type: c.Type.String(), At: at.UTC()})
	return nil
}

// Classifications devuelve una copia de las clasificaciones, actuales o pasadas.
func (p *Party) Classifications() []Classification { return slices.Clone(p.classifications) }
