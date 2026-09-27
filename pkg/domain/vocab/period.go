package vocab

import (
	"encoding/json"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// ValidPeriod is the validity ("vigencia") of a fact: it starts at From and, if closed, ends at
// To. It unifies the C# ValidPeriod, Milestone (start instant) and ExpirationDate (end instant).
//
// The interval is half-open, [From, To): an element valid until 2025-01-01T00:00Z is no longer
// valid at that instant, so consecutive periods never overlap. Every "now" check uses the
// domain clock (domain.Now), so it is deterministic in tests.
type ValidPeriod struct {
	from time.Time
	to   *time.Time
}

// NewValidPeriod creates a period starting at from; to is nil for an open-ended period.
func NewValidPeriod(from time.Time, to *time.Time) (ValidPeriod, error) {
	var v domain.Validation
	v.Require(!from.IsZero(), "from", "required", "start is required")
	if to != nil {
		v.Require(to.After(from), "to", "range", "end must be after start")
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

// OpenPeriodFrom creates an open-ended period starting at from.
func OpenPeriodFrom(from time.Time) (ValidPeriod, error) { return NewValidPeriod(from, nil) }

// OpenPeriodNow creates an open-ended period starting now (domain clock).
func OpenPeriodNow() ValidPeriod {
	p, _ := NewValidPeriod(domain.Now(), nil)
	return p
}

// PeriodBetweenDates creates the period covering whole civil days [first, last] in loc
// (UTC when nil): from midnight of first to midnight after last.
func PeriodBetweenDates(first, last Date, loc *time.Location) (ValidPeriod, error) {
	end := last.AddDays(1).Time(loc)
	return NewValidPeriod(first.Time(loc), &end)
}

// From returns the start instant (UTC).
func (p ValidPeriod) From() time.Time { return p.from }

// To returns the end instant and whether the period is closed.
func (p ValidPeriod) To() (time.Time, bool) {
	if p.to == nil {
		return time.Time{}, false
	}
	return *p.to, true
}

// IsZero reports whether the period is absent.
func (p ValidPeriod) IsZero() bool { return p.from.IsZero() }

// IsOpenEnded reports whether the period has no end.
func (p ValidPeriod) IsOpenEnded() bool { return p.to == nil }

// IsActiveAt reports whether t is inside [From, To).
func (p ValidPeriod) IsActiveAt(t time.Time) bool {
	return !t.Before(p.from) && (p.to == nil || t.Before(*p.to))
}

// IsActive reports whether the period is active now (domain clock).
func (p ValidPeriod) IsActive() bool { return p.IsActiveAt(domain.Now()) }

// HasExpiredAt reports whether the period ended at or before t.
func (p ValidPeriod) HasExpiredAt(t time.Time) bool { return p.to != nil && !t.Before(*p.to) }

// HasExpired reports whether the period has ended (domain clock).
func (p ValidPeriod) HasExpired() bool { return p.HasExpiredAt(domain.Now()) }

// Duration returns the length of a closed period.
func (p ValidPeriod) Duration() (time.Duration, bool) {
	if p.to == nil {
		return 0, false
	}
	return p.to.Sub(p.from), true
}

// Overlaps reports whether both periods share at least one instant.
func (p ValidPeriod) Overlaps(o ValidPeriod) bool {
	startsBeforeOtherEnds := o.to == nil || p.from.Before(*o.to)
	otherStartsBeforeEnd := p.to == nil || o.from.Before(*p.to)
	return startsBeforeOtherEnds && otherStartsBeforeEnd
}

// CloseAt returns the period ending at t (t must be after From).
func (p ValidPeriod) CloseAt(t time.Time) (ValidPeriod, error) { return NewValidPeriod(p.from, &t) }

// Equal compares both bounds as instants.
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

// MarshalJSON renders {"from":"...","to":"..."}.
func (p ValidPeriod) MarshalJSON() ([]byte, error) {
	return json.Marshal(periodJSON{From: p.from, To: p.to})
}

// UnmarshalJSON parses {"from":"...","to":"..."}.
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
