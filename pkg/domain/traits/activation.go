package traits

// Activatable is implemented by aggregates that can be switched on and off
// (C# IActivable / IToggleable).
type Activatable interface {
	IsActive() bool
}

// Activation is the embeddable on/off state. The zero value is active, matching the C#
// default (IsActive = true), so embedding it needs no initialization.
//
// Activate and Deactivate report whether the state changed, so the aggregate can raise its own
// meaningful event only on real transitions:
//
//	func (p *Party) Deactivate(reason string) {
//		if p.Activation.Deactivate() {
//			p.Raise(PartyDeactivated{EventMeta: p.NewEventMeta(), Reason: reason})
//		}
//	}
type Activation struct {
	inactive bool
}

// RestoredActivation rebuilds the state from persistence.
func RestoredActivation(active bool) Activation { return Activation{inactive: !active} }

// IsActive reports whether the aggregate is active.
func (a Activation) IsActive() bool { return !a.inactive }

// Activate switches the state on and reports whether it changed.
func (a *Activation) Activate() bool {
	changed := a.inactive
	a.inactive = false
	return changed
}

// Deactivate switches the state off and reports whether it changed.
func (a *Activation) Deactivate() bool {
	changed := !a.inactive
	a.inactive = true
	return changed
}
