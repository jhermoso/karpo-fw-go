package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// TrackingPlaceholder is where the tracking number goes in a carrier's tracking link.
const TrackingPlaceholder = "{tracking}"

// CarrierState is the persisted state of a carrier.
type CarrierState struct {
	Company     OrganizationID
	Code        string
	Name        string
	Party       PartyID // the carrier as a party, when it is one (it may also be a supplier)
	TrackingURL string  // link of a shipment, with {tracking} where its number goes
	Blocked     bool
	Audit       traits.AuditStamp
}

// Carrier is who carries the shipments of a company (in C# a party role whose table held only an
// id, and which no shipment referred to).
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

// ReconstituteCarrier rebuilds a carrier.
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

// State returns the state.
func (c *Carrier) State() CarrierState { return c.s }

// Change replaces the name, the party, the tracking link and whether the carrier is blocked.
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

// TrackingLink returns the link of a shipment with this carrier, or nothing without a number or
// a link.
func (c *Carrier) TrackingLink(tracking string) string {
	if tracking == "" || c.s.TrackingURL == "" {
		return ""
	}
	return strings.Replace(c.s.TrackingURL, TrackingPlaceholder, urlEscape(tracking), 1)
}

// urlEscape percent-encodes what is not unreserved in a URL (standard library only in the
// domain, and no dependency on net/url semantics for spaces).
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

// AuditSnapshot implements traits.Snapshotter.
func (c *Carrier) AuditSnapshot() map[string]any {
	return map[string]any{"code": c.s.Code, "name": c.s.Name, "blocked": c.s.Blocked}
}

// Carrier fields.
var (
	CarFieldCompany = spec.Comparable("company", func(c *Carrier) OrganizationID { return c.s.Company })
	CarFieldCode    = spec.Ordered("code", func(c *Carrier) string { return c.s.Code })
)

// NewCarrierID returns a new identity.
func NewCarrierID() CarrierID { return CarrierID{fw.NewUUID()} }

// ParseCarrierID parses a textual identity.
func ParseCarrierID(s string) (CarrierID, error) { u, err := fw.ParseUUID(s); return CarrierID{u}, err }

// CarrierRepository stores carriers.
type CarrierRepository = fw.Repository[CarrierID, *Carrier]
