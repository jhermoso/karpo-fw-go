package traits

import (
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Temporal is implemented by aggregates with a validity ("vigencia"): C# IExpirable,
// ITimeScoped, IHistoriableStartEnd and ITimeBounded, which overlapped, are one trait here.
type Temporal interface {
	ValidPeriod() vocab.ValidPeriod
}

// Validity is the embeddable validity of an aggregate over a half-open vocab.ValidPeriod.
// Every "now" check uses the domain clock.
type Validity struct {
	period vocab.ValidPeriod
}

// NewValidity starts a validity with a period.
func NewValidity(p vocab.ValidPeriod) Validity { return Validity{period: p} }

// ValidPeriod returns the period.
func (v Validity) ValidPeriod() vocab.ValidPeriod { return v.period }

// IsValidAt reports whether the aggregate is valid at t.
func (v Validity) IsValidAt(t time.Time) bool { return v.period.IsActiveAt(t) }

// IsCurrentlyValid reports whether the aggregate is valid now.
func (v Validity) IsCurrentlyValid() bool { return v.period.IsActive() }

// HasExpired reports whether the validity has ended.
func (v Validity) HasExpired() bool { return v.period.HasExpired() }

// ExpiresWithin reports whether a closed validity ends within d from now and has not ended yet
// (the C# ExpirationInfo "warning period").
func (v Validity) ExpiresWithin(d time.Duration) bool {
	end, closed := v.period.To()
	now := domain.Now()
	return closed && end.After(now) && !end.After(now.Add(d))
}

// ExpireAt closes the validity at t (the C# SetTimeline/expiration). It fails if t is not after
// the start. It reports whether the end changed.
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

// Reopen removes the end of the validity (open-ended again). It reports whether it changed.
func (v *Validity) Reopen() bool {
	if v.period.IsOpenEnded() {
		return false
	}
	p, _ := vocab.NewValidPeriod(v.period.From(), nil)
	v.period = p
	return true
}
