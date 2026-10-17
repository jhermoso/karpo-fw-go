package traits

import (
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Temporal lo implementan los agregados con vigencia: IExpirable, ITimeScoped,
// IHistoriableStartEnd e ITimeBounded de C#, que se solapaban, son aquí un solo rasgo.
type Temporal interface {
	ValidPeriod() vocab.ValidPeriod
}

// Validity es la vigencia incrustable de un agregado sobre un vocab.ValidPeriod semiabierto.
// Toda comprobación de «ahora» usa el reloj del dominio.
type Validity struct {
	period vocab.ValidPeriod
}

// NewValidity empieza una vigencia con un periodo.
func NewValidity(p vocab.ValidPeriod) Validity { return Validity{period: p} }

// ValidPeriod devuelve el periodo.
func (v Validity) ValidPeriod() vocab.ValidPeriod { return v.period }

// IsValidAt dice si el agregado está vigente en t.
func (v Validity) IsValidAt(t time.Time) bool { return v.period.IsActiveAt(t) }

// IsCurrentlyValid dice si el agregado está vigente ahora.
func (v Validity) IsCurrentlyValid() bool { return v.period.IsActive() }

// HasExpired dice si la vigencia ha terminado.
func (v Validity) HasExpired() bool { return v.period.HasExpired() }

// ExpiresWithin dice si una vigencia cerrada termina dentro de d desde ahora y aún no ha terminado
// (el «periodo de aviso» de ExpirationInfo de C#).
func (v Validity) ExpiresWithin(d time.Duration) bool {
	end, closed := v.period.To()
	now := domain.Now()
	return closed && end.After(now) && !end.After(now.Add(d))
}

// ExpireAt cierra la vigencia en t (SetTimeline/caducidad de C#). Falla si t es anterior al
// inicio. Dice si el final ha cambiado.
func (v *Validity) ExpireAt(t time.Time) (bool, error) {
	if end, closed := v.period.To(); closed && end.Equal(t) {
		return false, nil
	}
	p, err := v.period.CloseAt(t)
	if err != nil {
		return false, err
	}
	v.period = p
	return true, nil
}

// Reopen quita el final de la vigencia (vuelve a quedar abierta). Dice si ha cambiado.
func (v *Validity) Reopen() bool {
	if v.period.IsOpenEnded() {
		return false
	}
	p, _ := vocab.NewValidPeriod(v.period.From(), nil)
	v.period = p
	return true
}
