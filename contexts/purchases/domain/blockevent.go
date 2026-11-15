package domain

import (
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// BlockEventKind es el nombre estable del tipo de una entrada del historial de bloqueos.
const BlockEventKind = "purchases.supplier_block_event"

// BlockEventID identifica una entrada del historial de bloqueos.
type BlockEventID struct{ fw.UUID }

// BlockEventState es un bloqueo o un desbloqueo efectivo de un perfil de proveedor (D-156
// ampliada; el commercial_block_event de Trench): quién, cuándo y por qué. Si pasó por aprobación,
// Approval es la solicitud; el usuario es quien la aprobó.
type BlockEventState struct {
	Profile  SupplierID
	Company  OrganizationID
	Supplier PartyID
	Blocked  bool // true: bloqueo; false: desbloqueo
	Reason   string
	UserID   fw.UUID // la party del usuario; vacía si fue el sistema
	UserName string
	At       time.Time
	Approval fw.UUID // vacía si no hubo aprobación
}

// BlockEvent es una entrada del historial de bloqueos: se escribe una vez y no cambia.
type BlockEvent struct {
	fw.BaseAggregateRoot[BlockEventID]
	s BlockEventState
}

// NewBlockEvent construye una entrada del historial.
func NewBlockEvent(id BlockEventID, s BlockEventState) (*BlockEvent, error) {
	base, err := fw.NewBaseAggregateRoot(BlockEventKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Profile.IsZero() && !s.Company.IsZero() && !s.Supplier.IsZero(), "profile", "required", "profile, company and supplier are required")
	v.Require(!s.At.IsZero(), "at", "required", "when it happened is required")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &BlockEvent{BaseAggregateRoot: base, s: s}, nil
}

// State devuelve el estado.
func (e *BlockEvent) State() BlockEventState { return e.s }

// NewBlockEventID da una identidad nueva.
func NewBlockEventID() BlockEventID { return BlockEventID{fw.NewUUID()} }

// BlockEventRepository guarda el historial de bloqueos.
type BlockEventRepository = fw.Repository[BlockEventID, *BlockEvent]

// Campos del historial.
var (
	BlkFieldProfile = spec.Comparable("profile_id", func(e *BlockEvent) SupplierID { return e.s.Profile })
	BlkFieldAt      = spec.OrderedBy("occurred_at", func(e *BlockEvent) time.Time { return e.s.At }, time.Time.Compare)
	BlkFieldIDOrder = spec.Ordered("id", func(e *BlockEvent) string { return e.ID().String() })
)
