package domain

import "fmt"

// AggregateRoot es la frontera de consistencia de un grupo de entidades y objetos de valor.
// Sólo las raíces de agregado tienen repositorio.
//
// La interfaz está sellada: sus métodos no exportados sólo se satisfacen incrustando
// BaseAggregateRoot, que garantiza que todo agregado lleva una versión (concurrencia
// optimista) y un buffer de eventos.
type AggregateRoot[ID Identifier] interface {
	Entity[ID]
	// AggregateType es el nombre estable y persistido del tipo de agregado (p. ej. "parties.party").
	AggregateType() string
	// Version es la versión persistida: 0 significa «nunca persistido».
	Version() int64
	// PendingEvents devuelve los eventos de dominio lanzados desde que se cargó o se guardó por última vez el agregado.
	PendingEvents() []Event
	// ClearEvents descarta los eventos pendientes una vez registrados o despachados con seguridad.
	ClearEvents()

	aggregateRoot()
	setVersion(v int64)
}

// BaseAggregateRoot es la implementación incrustable de AggregateRoot. Se incrusta por valor y
// el tipo del agregado usa receptor puntero (p. ej. *Party implementa AggregateRoot[PartyID]).
type BaseAggregateRoot[ID Identifier] struct {
	BaseEntity[ID]
	kind    string
	version int64
	events  []Event
}

// NewBaseAggregateRoot valida la identidad y el tipo de un agregado nuevo o reconstituido.
func NewBaseAggregateRoot[ID Identifier](kind string, id ID) (BaseAggregateRoot[ID], error) {
	if kind == "" {
		return BaseAggregateRoot[ID]{}, fmt.Errorf("%w: aggregate kind must not be empty", ErrInvalidIdentity)
	}
	base, err := NewBaseEntity(id)
	if err != nil {
		return BaseAggregateRoot[ID]{}, err
	}
	return BaseAggregateRoot[ID]{BaseEntity: base, kind: kind}, nil
}

// AggregateType devuelve el tipo del agregado.
func (a *BaseAggregateRoot[ID]) AggregateType() string { return a.kind }

// Version devuelve la versión persistida (0 si es nuevo).
func (a *BaseAggregateRoot[ID]) Version() int64 { return a.version }

// IsNew dice si el agregado no se ha persistido nunca.
func (a *BaseAggregateRoot[ID]) IsNew() bool { return a.version == 0 }

// Raise registra un evento de dominio. Se llama desde los métodos de comportamiento del agregado, después del cambio de estado.
func (a *BaseAggregateRoot[ID]) Raise(evt Event) {
	if evt != nil {
		a.events = append(a.events, evt)
	}
}

// NewEventMeta devuelve metadatos de evento ya rellenos con la identidad y el tipo de este
// agregado y la versión que tendrá una vez guardados los cambios pendientes.
func (a *BaseAggregateRoot[ID]) NewEventMeta() EventMeta {
	m := NewEventMeta()
	m.AggregateType = a.kind
	m.AggregateID = a.ID().String()
	m.AggregateVersion = a.version + 1
	return m
}

// PendingEvents devuelve una copia de los eventos lanzados y aún no descartados.
func (a *BaseAggregateRoot[ID]) PendingEvents() []Event {
	return append([]Event(nil), a.events...)
}

// ClearEvents vacía los eventos pendientes.
func (a *BaseAggregateRoot[ID]) ClearEvents() { a.events = nil }

func (a *BaseAggregateRoot[ID]) aggregateRoot()     {}
func (a *BaseAggregateRoot[ID]) setVersion(v int64) { a.version = v }

// MarkPersisted fija la versión persistida de un agregado. Está reservada a las
// implementaciones de repositorio (tras un insert/update, o al reconstituir desde el almacenamiento).
func MarkPersisted[ID Identifier](agg AggregateRoot[ID], version int64) {
	agg.setVersion(version)
}
