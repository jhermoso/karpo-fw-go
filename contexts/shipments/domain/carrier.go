package domain

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// TrackingPlaceholder es dónde va el número de seguimiento en el enlace de seguimiento de un
// transportista.
const TrackingPlaceholder = "{tracking}"

// CarrierState es el estado persistido de un transportista.
type CarrierState struct {
	Company     OrganizationID
	Code        string
	Name        string
	Party       PartyID // el transportista como party, cuando lo es (también puede ser un proveedor)
	TrackingURL string  // enlace de un envío, con {tracking} donde va su número
	Blocked     bool
	// BlockReason dice por qué está bloqueado; se borra al desbloquear. UnblockReason dice por qué
	// se desbloqueó la última vez; se borra al bloquear (D-156).
	BlockReason   string
	UnblockReason string
	// BlockEvents es el historial de bloqueos y desbloqueos, del más antiguo al más reciente.
	BlockEvents []BlockEvent
	Audit       traits.AuditStamp
}

// BlockEvent es un bloqueo o un desbloqueo efectivo: quién, cuándo y por qué.
type BlockEvent struct {
	Blocked bool
	Reason  string
	At      time.Time
	By      vocab.Actor
}

// MaxBlockReason es lo que cabe en el motivo de un bloqueo o de un desbloqueo.
const MaxBlockReason = 200

// Códigos de las infracciones del bloqueo (D-156).
const (
	CodeBlockReasonRequired        = "shipments.block_reason_required"
	CodeUnblockReasonRequired      = "shipments.unblock_reason_required"
	CodeBlockChangeRequiresCommand = "shipments.block_change_requires_command"
)

// Carrier es quien transporta los envíos de una empresa (en C#, un rol de party cuya tabla sólo
// guardaba un id y al que ningún envío hacía referencia).
type Carrier struct {
	fw.BaseAggregateRoot[CarrierID]
	traits.Audited
	s CarrierState
}

func checkCarrier(v *fw.Validation, s *CarrierState) {
	s.Name, s.TrackingURL = strings.TrimSpace(s.Name), strings.TrimSpace(s.TrackingURL)
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 120, "name", "length", "a name of 1 to 120 characters")
	v.Require(s.TrackingURL == "" || (len(s.TrackingURL) <= 300 && strings.HasPrefix(s.TrackingURL, "https://") &&
		strings.Count(s.TrackingURL, TrackingPlaceholder) == 1), "trackingUrl", "format", "an https link with {tracking} once")
}

// ReconstituteCarrier reconstruye un transportista.
func ReconstituteCarrier(id CarrierID, s CarrierState) (*Carrier, error) {
	base, err := fw.NewBaseAggregateRoot(CarrierKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	s.Code = strings.ToUpper(strings.TrimSpace(s.Code))
	valid := s.Code != "" && len(s.Code) <= 20
	for _, r := range s.Code {
		valid = valid && ((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_')
	}
	v.Require(valid, "code", "format", "1 to 20 letters, digits, dashes or underscores")
	checkCarrier(&v, &s)
	if s.Blocked {
		s.UnblockReason = ""
	} else {
		s.BlockReason = ""
	}
	s.BlockReason, s.UnblockReason = strings.TrimSpace(s.BlockReason), strings.TrimSpace(s.UnblockReason)
	checkReason(&v, "blockReason", s.BlockReason)
	checkReason(&v, "unblockReason", s.UnblockReason)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Carrier{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado.
func (c *Carrier) State() CarrierState { return c.s }

func checkReason(v *fw.Validation, field, r string) {
	v.Require(utf8.RuneCountInString(r) <= MaxBlockReason, field, "length", "the reason has at most 200 characters")
	v.Require(!strings.ContainsFunc(r, unicode.IsControl), field, "format", "no control characters")
}

// Change sustituye el nombre, la party y el enlace de seguimiento. El bloqueo no cambia aquí: tiene
// sus órdenes, Block y Unblock (D-156).
func (c *Carrier) Change(name string, party PartyID, trackingURL string) error {
	s := c.s
	s.Name, s.Party, s.TrackingURL = name, party, trackingURL
	var v fw.Validation
	checkCarrier(&v, &s)
	if err := v.Err(); err != nil {
		return err
	}
	c.s = s
	return nil
}

// CheckBlockReason comprueba el motivo de bloquear (blocked) o desbloquear: obligatorio, de 200
// caracteres como mucho y sin caracteres de control.
func CheckBlockReason(blocked bool, reason string) error {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "" && blocked:
		return fw.Violation(CodeBlockReasonRequired, "blocking a carrier needs a reason")
	case reason == "":
		return fw.Violation(CodeUnblockReasonRequired, "unblocking a carrier needs a reason")
	}
	var v fw.Validation
	checkReason(&v, "reason", reason)
	return v.Err()
}

// SetBlocked bloquea (blocked) o desbloquea el transportista con su motivo, que es obligatorio, y
// deja el cambio en el historial con quién y cuándo. Si ya está así, no cambia nada y devuelve
// false.
func (c *Carrier) SetBlocked(blocked bool, reason string, by vocab.Actor, at time.Time) (bool, error) {
	if err := CheckBlockReason(blocked, reason); err != nil {
		return false, err
	}
	reason = strings.TrimSpace(reason)
	if c.s.Blocked == blocked {
		return false, nil
	}
	c.s.Blocked = blocked
	if blocked {
		c.s.BlockReason, c.s.UnblockReason = reason, ""
	} else {
		c.s.BlockReason, c.s.UnblockReason = "", reason
	}
	c.s.BlockEvents = append(c.s.BlockEvents, BlockEvent{Blocked: blocked, Reason: reason, At: at, By: by})
	return true, nil
}

// TrackingLink devuelve el enlace de un envío con este transportista, o nada si no hay número o
// enlace.
func (c *Carrier) TrackingLink(tracking string) string {
	if tracking == "" || c.s.TrackingURL == "" {
		return ""
	}
	return strings.Replace(c.s.TrackingURL, TrackingPlaceholder, urlEscape(tracking), 1)
}

// urlEscape codifica con porcentajes lo que no es «unreserved» en una URL (en el dominio sólo
// biblioteca estándar, y sin depender de la semántica de net/url para los espacios).
func urlEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// AuditSnapshot implementa traits.Snapshotter.
func (c *Carrier) AuditSnapshot() map[string]any {
	s := map[string]any{"code": c.s.Code, "name": c.s.Name, "blocked": c.s.Blocked}
	if c.s.BlockReason != "" {
		s["blockReason"] = c.s.BlockReason
	}
	if c.s.UnblockReason != "" {
		s["unblockReason"] = c.s.UnblockReason
	}
	return s
}

// Campos del transportista.
var (
	CarFieldCompany = spec.Comparable("company", func(c *Carrier) OrganizationID { return c.s.Company })
	CarFieldCode    = spec.Ordered("code", func(c *Carrier) string { return c.s.Code })
	CarFieldName    = spec.Text("name", func(c *Carrier) string { return c.s.Name })
	CarFieldBlocked = spec.Comparable("blocked", func(c *Carrier) bool { return c.s.Blocked })
	// CarFieldIDOrder ordena por la identidad: deshace los empates de una lista paginada.
	CarFieldIDOrder = spec.Ordered("id", func(c *Carrier) string { return c.ID().String() })
)

// NewCarrierID devuelve una identidad nueva.
func NewCarrierID() CarrierID { return CarrierID{fw.NewUUID()} }

// ParseCarrierID analiza una identidad textual.
func ParseCarrierID(s string) (CarrierID, error) { u, err := fw.ParseUUID(s); return CarrierID{u}, err }

// CarrierRepository guarda transportistas.
type CarrierRepository = fw.Repository[CarrierID, *Carrier]
