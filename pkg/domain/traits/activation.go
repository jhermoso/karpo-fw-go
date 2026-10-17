package traits

// Activatable lo implementan los agregados que se pueden activar y desactivar
// (IActivable / IToggleable de C#).
type Activatable interface {
	IsActive() bool
}

// Activation es el estado activo/inactivo incrustable. El valor cero es activo, como el valor por
// defecto de C# (IsActive = true), así que incrustarlo no necesita inicialización.
//
// Activate y Deactivate dicen si el estado ha cambiado, para que el agregado lance su propio
// evento con significado sólo en las transiciones reales:
//
//	func (p *Party) Deactivate(reason string) {
//		if p.Activation.Deactivate() {
//			p.Raise(PartyDeactivated{EventMeta: p.NewEventMeta(), Reason: reason})
//		}
//	}
type Activation struct {
	inactive bool
}

// RestoredActivation reconstruye el estado desde la persistencia.
func RestoredActivation(active bool) Activation { return Activation{inactive: !active} }

// IsActive dice si el agregado está activo.
func (a Activation) IsActive() bool { return !a.inactive }

// Activate activa el estado y dice si ha cambiado.
func (a *Activation) Activate() bool {
	changed := a.inactive
	a.inactive = false
	return changed
}

// Deactivate desactiva el estado y dice si ha cambiado.
func (a *Activation) Deactivate() bool {
	changed := !a.inactive
	a.inactive = true
	return changed
}
