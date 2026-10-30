package domain

import (
	"bytes"
	"cmp"
	"slices"
	"strings"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// IssueSeverity dice cuánto pesa una incidencia de calidad de datos.
type IssueSeverity string

// Severidades. Hoy sólo nacen avisos: el dato se guardó y la ficha sigue sirviendo.
const (
	SeverityWarning IssueSeverity = "warning"
)

// Códigos de incidencia de calidad de datos (los mismos que usa Trench).
const (
	IssueInvalidEmail = "INVALID_EMAIL"
	IssueInvalidPhone = "INVALID_PHONE"
)

// DataQualityIssue es un dato de la ficha que se guardó aunque su formato es erróneo
// (docs/DECISIONES-COMUNES.md, D-92): avisa sin impedir guardar, y queda a la vista hasta que
// alguien lo corrija. Subject es el objeto de la ficha al que se refiere (hoy, el contacto) y
// Field, su campo. La incidencia desaparece con el dato: cuando el contacto termina.
type DataQualityIssue struct {
	ID       fw.UUID
	Subject  fw.UUID // el contacto
	Field    string  // "contact.value"
	Code     string
	Severity IssueSeverity
	Message  string
	Value    string // el valor tal como se guardó
	Since    time.Time
	// Owner es la organización dueña del objeto (la del contacto, D-122): la incidencia se ve donde
	// se ve el contacto. Cero: la ve quien ve la ficha.
	Owner PartyID
}

// OwnerOf devuelve la dueña de una incidencia, o nil si no la tiene (para las especificaciones).
func (i DataQualityIssue) OwnerOf() *PartyID {
	if i.Owner.IsZero() {
		return nil
	}
	o := i.Owner
	return &o
}

// Campo de un contacto al que se refieren sus incidencias.
const issueFieldContactValue = "contact.value"

// DataQualityIssues devuelve una copia de las incidencias de calidad de datos de la ficha.
func (p *Party) DataQualityIssues() []DataQualityIssue { return slices.Clone(p.qualityIssues) }

// HasDataQualityIssues dice si la ficha tiene alguna incidencia de calidad de datos.
func (p *Party) HasDataQualityIssues() bool { return len(p.qualityIssues) > 0 }

// DataQualityIssuesOf devuelve las incidencias de un objeto de la ficha (p. ej. un contacto).
func (p *Party) DataQualityIssuesOf(subject fw.UUID) []DataQualityIssue {
	var out []DataQualityIssue
	for _, i := range p.qualityIssues {
		if i.Subject == subject {
			out = append(out, i)
		}
	}
	return out
}

// raiseIssue anota una incidencia nueva de un objeto de la ficha.
func (p *Party) raiseIssue(subject fw.UUID, owner PartyID, field string, f formatIssue, value string) {
	p.qualityIssues = append(slices.Clone(p.qualityIssues), DataQualityIssue{ID: fw.NewUUID(), Subject: subject, Field: field,
		Code: f.code, Severity: SeverityWarning, Message: f.message, Value: value, Since: fw.Now(), Owner: owner})
	slices.SortFunc(p.qualityIssues, compareIssues)
}

// compareIssues da a las incidencias un orden que no depende del reloj (los motores guardan el
// instante con precisiones distintas): por objeto, campo, código y, al final, identidad. Así la
// ficha en memoria y la releída de cualquier base las enseñan en el mismo orden.
func compareIssues(a, b DataQualityIssue) int {
	if c := bytes.Compare(a.Subject.Bytes(), b.Subject.Bytes()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Field, b.Field); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Code, b.Code); c != 0 {
		return c
	}
	return bytes.Compare(a.ID.Bytes(), b.ID.Bytes())
}

// clearIssuesOf quita las incidencias de un objeto de la ficha: el dato se ha ido.
func (p *Party) clearIssuesOf(subject fw.UUID) {
	if slices.ContainsFunc(p.qualityIssues, func(i DataQualityIssue) bool { return i.Subject == subject }) {
		p.qualityIssues = slices.DeleteFunc(slices.Clone(p.qualityIssues), func(i DataQualityIssue) bool { return i.Subject == subject })
	}
}

// issuesSnapshot resume las incidencias para el registro de auditoría.
func (p *Party) issuesSnapshot() string {
	parts := make([]string, 0, len(p.qualityIssues))
	for _, i := range p.qualityIssues {
		parts = append(parts, i.Subject.String()+":"+i.Code)
	}
	return strings.Join(parts, ",")
}

// formatIssue es lo que un valor con formato erróneo deja dicho al guardarse.
type formatIssue struct{ code, message string }
