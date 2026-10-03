package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// CreditProfileKind is the stable aggregate type name.
const CreditProfileKind = "receivables.credit_profile"

// CreditProfileState is the persisted state of a credit profile.
type CreditProfileState struct {
	Seller   OrganizationID
	Customer PartyID
	Terms    TermsID       // default payment terms; zero: due on the invoice date
	Limit    vocab.Decimal // risk limit; meaningful only when Limited
	Limited  bool          // false: no limit (the C# read MaxRisk = 0 as "no limit" in one place and "limit 0" in another)
	Blocked  bool          // orders are refused
	Audit    traits.AuditStamp
}

// CreditProfile is the credit relationship of a seller with a customer: default terms, risk limit
// and block (the MaxRisk, BlockOrders and DefaultPaymentCondition of the C# customer commercial
// profile).
type CreditProfile struct {
	fw.BaseAggregateRoot[CreditProfileID]
	traits.Audited
	s CreditProfileState
}

func (s CreditProfileState) check(v *fw.Validation) {
	v.Require(!s.Seller.IsZero() && !s.Customer.IsZero(), "customer", "required", "seller and customer are required")
	v.Require(!s.Limited || (!s.Limit.IsNegative() && s.Limit.Equal(s.Limit.Round(2))), "limit", "range", "a non-negative limit in cents")
	v.Require(s.Limited || s.Limit.IsZero(), "limit", "unlimited", "an unlimited profile has no limit amount")
}

// ReconstituteCreditProfile rebuilds a credit profile.
func ReconstituteCreditProfile(id CreditProfileID, s CreditProfileState) (*CreditProfile, error) {
	base, err := fw.NewBaseAggregateRoot(CreditProfileKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.check(&v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &CreditProfile{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (p *CreditProfile) State() CreditProfileState { return p.s }

// Set replaces the terms, the limit and the block.
func (p *CreditProfile) Set(terms TermsID, limited bool, limit vocab.Decimal, blocked bool) error {
	s := p.s
	s.Terms, s.Limited, s.Limit, s.Blocked = terms, limited, limit, blocked
	var v fw.Validation
	s.check(&v)
	if err := v.Err(); err != nil {
		return err
	}
	p.s = s
	return nil
}

// Available returns what can still be sold on credit given the open exposure (ok false: no limit).
func (p *CreditProfile) Available(exposure vocab.Decimal) (vocab.Decimal, bool) {
	if !p.s.Limited {
		return vocab.Decimal{}, false
	}
	return p.s.Limit.Sub(exposure), true
}

// AuditSnapshot implements traits.Snapshotter.
func (p *CreditProfile) AuditSnapshot() map[string]any {
	return map[string]any{"terms": p.s.Terms.String(), "limited": p.s.Limited, "limit": p.s.Limit.String(), "blocked": p.s.Blocked}
}

// Credit profile fields.
var (
	CredFieldSeller   = spec.Comparable("seller", func(p *CreditProfile) OrganizationID { return p.s.Seller })
	CredFieldCustomer = spec.Comparable("customer", func(p *CreditProfile) PartyID { return p.s.Customer })
)

// Events of the context (the C# raised a generic FactIssued for payments that nobody consumed).
type (
	// ReceivableOpened is raised when an issued invoice becomes a receivable.
	ReceivableOpened struct {
		fw.EventMeta
		Customer string `json:"customer"`
		Number   string `json:"number"`
		Total    string `json:"total"`
	}
	// ReceivableSettled is raised when nothing remains to collect.
	ReceivableSettled struct {
		fw.EventMeta
		Customer string `json:"customer"`
		Number   string `json:"number"`
	}
	// CollectionRegistered is raised when money is received.
	CollectionRegistered struct {
		fw.EventMeta
		Payer  string `json:"payer"`
		Amount string `json:"amount"`
		Method string `json:"method"`
	}
	// CollectionAllocated is raised when part of a collection is applied to an installment.
	CollectionAllocated struct {
		fw.EventMeta
		Seller      string `json:"seller"`
		Payer       string `json:"payer"`
		Method      string `json:"method"`
		Receivable  string `json:"receivable"`
		Installment int    `json:"installment"`
		Amount      string `json:"amount"`
		On          string `json:"on"`
	}
	// AllocationReversed is raised when an allocation is reversed.
	AllocationReversed struct {
		fw.EventMeta
		Seller      string `json:"seller"`
		Payer       string `json:"payer"`
		Receivable  string `json:"receivable"`
		Installment int    `json:"installment"`
		Amount      string `json:"amount"`
	}
)

// EventType implementations.
func (ReceivableOpened) EventType() string     { return "receivables.receivable_opened" }
func (ReceivableSettled) EventType() string    { return "receivables.receivable_settled" }
func (CollectionRegistered) EventType() string { return "receivables.collection_registered" }
func (CollectionAllocated) EventType() string  { return "receivables.collection_allocated" }
func (AllocationReversed) EventType() string   { return "receivables.allocation_reversed" }

// New identities and parsing.
func NewTermsID() TermsID                 { return TermsID{fw.NewUUID()} }
func NewCollectionID() CollectionID       { return CollectionID{fw.NewUUID()} }
func NewCreditProfileID() CreditProfileID { return CreditProfileID{fw.NewUUID()} }

// ParseTermsID parses a textual identity.
func ParseTermsID(s string) (TermsID, error) { u, err := fw.ParseUUID(s); return TermsID{u}, err }

// ParseReceivableID parses a textual identity.
func ParseReceivableID(s string) (ReceivableID, error) {
	u, err := fw.ParseUUID(s)
	return ReceivableID{u}, err
}

// ParseCollectionID parses a textual identity.
func ParseCollectionID(s string) (CollectionID, error) {
	u, err := fw.ParseUUID(s)
	return CollectionID{u}, err
}

// ParseCreditProfileID parses a textual identity.
func ParseCreditProfileID(s string) (CreditProfileID, error) {
	u, err := fw.ParseUUID(s)
	return CreditProfileID{u}, err
}
