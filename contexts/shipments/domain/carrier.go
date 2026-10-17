package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
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
	Audit       traits.AuditStamp
}

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
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Carrier{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado.
func (c *Carrier) State() CarrierState { return c.s }

// Change sustituye el nombre, la party, el enlace de seguimiento y si el transportista está
// bloqueado.
func (c *Carrier) Change(name string, party PartyID, trackingURL string, blocked bool) error {
	s := c.s
	s.Name, s.Party, s.TrackingURL, s.Blocked = name, party, trackingURL, blocked
	var v fw.Validation
	checkCarrier(&v, &s)
	if err := v.Err(); err != nil {
		return err
	}
	c.s = s
	return nil
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
	return map[string]any{"code": c.s.Code, "name": c.s.Name, "blocked": c.s.Blocked}
}

// Campos del transportista.
var (
	CarFieldCompany = spec.Comparable("company", func(c *Carrier) OrganizationID { return c.s.Company })
	CarFieldCode    = spec.Ordered("code", func(c *Carrier) string { return c.s.Code })
)

// NewCarrierID devuelve una identidad nueva.
func NewCarrierID() CarrierID { return CarrierID{fw.NewUUID()} }

// ParseCarrierID analiza una identidad textual.
func ParseCarrierID(s string) (CarrierID, error) { u, err := fw.ParseUUID(s); return CarrierID{u}, err }

// CarrierRepository guarda transportistas.
type CarrierRepository = fw.Repository[CarrierID, *Carrier]
