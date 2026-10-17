package vocab

import (
	"encoding/json"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// ValidPeriod es la vigencia de un hecho: empieza en From y, si está cerrado, termina en To.
// Unifica ValidPeriod, Milestone (instante de inicio) y ExpirationDate (instante de fin) de C#.
//
// El intervalo es semiabierto, [From, To): un elemento vigente hasta 2025-01-01T00:00Z ya no está
// vigente en ese instante, así que los periodos consecutivos nunca se solapan. Toda comprobación de
// «ahora» usa el reloj del dominio (domain.Now), de modo que es determinista en las pruebas.
//
// Un periodo puede terminar en el instante en que empieza: [t, t) es el periodo vacío de un hecho
// que se terminó en el acto, que nunca estuvo en vigor y no se solapa con nada. Dos lecturas de un
// reloj pueden dar el mismo instante (el reloj del sistema avanza a saltos y un almacén guarda menos
// dígitos de los que da el reloj), así que no se puede exigir «termina después de empezar» a lo que
// empieza y termina «ahora».
type ValidPeriod struct {
	from time.Time
	to   *time.Time
}

// NewValidPeriod crea un periodo que empieza en from; to es nil para un periodo abierto y nunca
// es anterior a from.
func NewValidPeriod(from time.Time, to *time.Time) (ValidPeriod, error) {
	var v domain.Validation
	v.Require(!from.IsZero(), "from", "required", "start is required")
	if to != nil {
		v.Require(!to.Before(from), "to", "range", "end must not be before start")
	}
	if err := v.Err(); err != nil {
		return ValidPeriod{}, err
	}
	p := ValidPeriod{from: from.UTC()}
	if to != nil {
		t := to.UTC()
		p.to = &t
	}
	return p, nil
}

// OpenPeriodFrom crea un periodo abierto que empieza en from.
func OpenPeriodFrom(from time.Time) (ValidPeriod, error) { return NewValidPeriod(from, nil) }

// OpenPeriodNow crea un periodo abierto que empieza ahora (reloj del dominio).
func OpenPeriodNow() ValidPeriod {
	p, _ := NewValidPeriod(domain.Now(), nil)
	return p
}

// PeriodBetweenDates crea el periodo que cubre los días civiles completos [first, last] en loc
// (UTC si es nil): desde la medianoche de first hasta la medianoche siguiente a last.
func PeriodBetweenDates(first, last Date, loc *time.Location) (ValidPeriod, error) {
	if last.Before(first) {
		var v domain.Validation
		v.Add("to", "range", "last day must not be before the first")
		return ValidPeriod{}, v.Err()
	}
	end := last.AddDays(1).Time(loc)
	return NewValidPeriod(first.Time(loc), &end)
}

// From devuelve el instante de inicio (UTC).
func (p ValidPeriod) From() time.Time { return p.from }

// To devuelve el instante de fin y si el periodo está cerrado.
func (p ValidPeriod) To() (time.Time, bool) {
	if p.to == nil {
		return time.Time{}, false
	}
	return *p.to, true
}

// IsZero dice si falta el periodo.
func (p ValidPeriod) IsZero() bool { return p.from.IsZero() }

// IsOpenEnded dice si el periodo no tiene fin.
func (p ValidPeriod) IsOpenEnded() bool { return p.to == nil }

// IsEmpty dice si el periodo termina en el instante en que empieza: no contiene ningún instante.
func (p ValidPeriod) IsEmpty() bool { return p.to != nil && !p.to.After(p.from) }

// IsActiveAt dice si t está dentro de [From, To).
func (p ValidPeriod) IsActiveAt(t time.Time) bool {
	return !t.Before(p.from) && (p.to == nil || t.Before(*p.to))
}

// IsActive dice si el periodo está vigente ahora (reloj del dominio).
func (p ValidPeriod) IsActive() bool { return p.IsActiveAt(domain.Now()) }

// HasExpiredAt dice si el periodo terminó en t o antes.
func (p ValidPeriod) HasExpiredAt(t time.Time) bool { return p.to != nil && !t.Before(*p.to) }

// HasExpired dice si el periodo ha terminado (reloj del dominio).
func (p ValidPeriod) HasExpired() bool { return p.HasExpiredAt(domain.Now()) }

// Duration devuelve la duración de un periodo cerrado.
func (p ValidPeriod) Duration() (time.Duration, bool) {
	if p.to == nil {
		return 0, false
	}
	return p.to.Sub(p.from), true
}

// Overlaps dice si ambos periodos comparten al menos un instante (un periodo vacío no comparte ninguno).
func (p ValidPeriod) Overlaps(o ValidPeriod) bool {
	if p.IsEmpty() || o.IsEmpty() {
		return false
	}
	startsBeforeOtherEnds := o.to == nil || p.from.Before(*o.to)
	otherStartsBeforeEnd := p.to == nil || o.from.Before(*p.to)
	return startsBeforeOtherEnds && otherStartsBeforeEnd
}

// CloseAt devuelve el periodo que termina en t (t no puede ser anterior a From).
func (p ValidPeriod) CloseAt(t time.Time) (ValidPeriod, error) { return NewValidPeriod(p.from, &t) }

// Equal compara ambos límites como instantes.
func (p ValidPeriod) Equal(o ValidPeriod) bool {
	if !p.from.Equal(o.from) || (p.to == nil) != (o.to == nil) {
		return false
	}
	return p.to == nil || p.to.Equal(*o.to)
}

type periodJSON struct {
	From time.Time  `json:"from"`
	To   *time.Time `json:"to,omitempty"`
}

// MarshalJSON escribe {"from":"...","to":"..."}.
func (p ValidPeriod) MarshalJSON() ([]byte, error) {
	return json.Marshal(periodJSON{From: p.from, To: p.to})
}

// UnmarshalJSON analiza {"from":"...","to":"..."}.
func (p *ValidPeriod) UnmarshalJSON(b []byte) error {
	var j periodJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	v, err := NewValidPeriod(j.From, j.To)
	if err == nil {
		*p = v
	}
	return err
}
