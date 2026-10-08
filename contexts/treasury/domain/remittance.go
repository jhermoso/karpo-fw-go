package domain

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// RemittanceKind is the stable aggregate type name.
const RemittanceKind = "treasury.remittance"

// MaxItems bounds the items of a remittance.
const MaxItems = 5000

// RemittanceStatus is the status of a remittance: a draft is edited; a generated one is frozen and
// its file sent to the bank; a settled one was charged (its items can still be returned).
type RemittanceStatus int

// Statuses.
const (
	RemDraft RemittanceStatus = iota + 1
	RemGenerated
	RemSettled
	RemCancelled
)

var remStatuses = map[RemittanceStatus]string{RemDraft: "draft", RemGenerated: "generated", RemSettled: "settled", RemCancelled: "cancelled"}

// String returns the stable name.
func (s RemittanceStatus) String() string { return remStatuses[s] }

// Item is a direct debit of a remittance: an installment of an invoice, collected with a mandate.
type Item struct {
	EndToEnd    string // unique id of the transaction (the invoice number and installment)
	Invoice     InvoiceID
	Number      string
	Installment int
	Debtor      PartyID
	DebtorName  string
	Mandate     MandateID
	MandateRef  string
	Signed      vocab.Date
	IBAN        vocab.IBAN
	Amount      vocab.Decimal
	Sequence    string // FRST or RCUR, fixed when the file is generated
	Returned    vocab.Date
	Reason      string // ISO 20022 return reason (AC04, AM04, MD01, MS02…)
}

// RemittanceState is the persisted state of a remittance.
type RemittanceState struct {
	Creditor       OrganizationID
	Account        AccountID
	Scheme         Scheme
	CollectionDate vocab.Date
	Status         RemittanceStatus
	Items          []Item
	GeneratedAt    time.Time
	CreditorName   string
	CreditorID     string
	CreditorIBAN   vocab.IBAN
	CreditorBIC    string
	Settled        vocab.Date
	Audit          traits.AuditStamp
}

// Remittance is a batch of SEPA direct debits of a creditor from one of its accounts (nothing of
// this existed in the C#).
type Remittance struct {
	fw.BaseAggregateRoot[RemittanceID]
	traits.Audited
	s RemittanceState
}

// ReconstituteRemittance rebuilds a remittance.
func ReconstituteRemittance(id RemittanceID, s RemittanceState) (*Remittance, error) {
	base, err := fw.NewBaseAggregateRoot(RemittanceKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Creditor.IsZero() && !s.Account.IsZero(), "account", "required", "creditor and account are required")
	_, ok := schemes[s.Scheme]
	v.Require(ok, "scheme", "enum", "CORE or B2B")
	v.Require(!s.CollectionDate.IsZero(), "collectionDate", "required", "the collection date is required")
	_, ok = remStatuses[s.Status]
	v.Require(ok, "status", "enum", "unknown status")
	v.Require(len(s.Items) <= MaxItems, "items", "count", "too many items")
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Items = slices.Clone(s.Items)
	return &Remittance{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// DraftRemittance creates a draft.
func DraftRemittance(id RemittanceID, creditor OrganizationID, account AccountID, scheme Scheme, collection vocab.Date) (*Remittance, error) {
	return ReconstituteRemittance(id, RemittanceState{Creditor: creditor, Account: account, Scheme: scheme, CollectionDate: collection, Status: RemDraft})
}

// State returns the state (items are a copy).
func (r *Remittance) State() RemittanceState {
	s := r.s
	s.Items = slices.Clone(s.Items)
	return s
}

// Total returns the sum of the items (returned ones included).
func (r *Remittance) Total() vocab.Decimal {
	t := vocab.DecimalFromInt(0)
	for _, i := range r.s.Items {
		t = t.Add(i.Amount)
	}
	return t
}

func (r *Remittance) mustBeDraft() error {
	if r.s.Status != RemDraft {
		return fw.Violation("treasury.remittance_not_draft", "only a draft remittance changes")
	}
	return nil
}

// Add adds a direct debit. Invariants: a positive amount in cents, one item per installment, and
// the mandate is of the remittance scheme (checked by the caller with the mandate).
func (r *Remittance) Add(i Item) error {
	if err := r.mustBeDraft(); err != nil {
		return err
	}
	if !i.Amount.IsPositive() || !i.Amount.Equal(i.Amount.Round(2)) {
		return fw.Violation("treasury.item_amount", "a positive amount in cents")
	}
	if slices.ContainsFunc(r.s.Items, func(x Item) bool { return x.Invoice == i.Invoice && x.Installment == i.Installment }) {
		return fw.Violation("treasury.item_duplicate", "the installment is already in the remittance")
	}
	if len(r.s.Items) >= MaxItems {
		return fw.Violation("treasury.too_many_items", "the remittance is full")
	}
	i.EndToEnd = endToEnd(i.Number, i.Installment)
	i.Sequence, i.Returned, i.Reason = "", vocab.Date{}, ""
	r.s.Items = append(slices.Clone(r.s.Items), i)
	return nil
}

// endToEnd builds the end-to-end id: the invoice number and the installment, in SEPA characters,
// at most 35.
func endToEnd(number string, installment int) string {
	id := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, number) + "-" + vocab.DecimalFromInt(int64(installment)).String()
	if utf8.RuneCountInString(id) > 35 {
		id = id[len(id)-35:]
	}
	return id
}

// Remove removes the item of an installment.
func (r *Remittance) Remove(invoice InvoiceID, installment int) error {
	if err := r.mustBeDraft(); err != nil {
		return err
	}
	k := slices.IndexFunc(r.s.Items, func(x Item) bool { return x.Invoice == invoice && x.Installment == installment })
	if k < 0 {
		return fw.NotFound("treasury.remittance_item", invoice)
	}
	r.s.Items = slices.Delete(slices.Clone(r.s.Items), k, k+1)
	return nil
}

// Creditor is what the file needs of the creditor.
type Creditor struct {
	Name string
	ID   string // SEPA creditor identifier
	IBAN vocab.IBAN
	BIC  string
}

// Generate freezes the remittance: the creditor data and the sequence type of each item (from its
// mandate). The caller records the use of the mandates.
func (r *Remittance) Generate(c Creditor, sequences map[MandateID]string, at time.Time) error {
	if err := r.mustBeDraft(); err != nil {
		return err
	}
	if len(r.s.Items) == 0 {
		return fw.Violation("treasury.empty_remittance", "a remittance needs at least one direct debit")
	}
	if c.ID == "" || c.Name == "" || c.IBAN.IsZero() {
		return fw.Violation("treasury.creditor_incomplete", "the creditor needs a name, an identifier and an account")
	}
	items := slices.Clone(r.s.Items)
	for k := range items {
		seq := sequences[items[k].Mandate]
		if seq != "FRST" && seq != "RCUR" {
			return fw.Violation("treasury.sequence", "each direct debit needs its sequence type")
		}
		items[k].Sequence = seq
	}
	r.s.Items, r.s.Status, r.s.GeneratedAt = items, RemGenerated, at.UTC()
	r.s.CreditorName, r.s.CreditorID, r.s.CreditorIBAN, r.s.CreditorBIC = c.Name, c.ID, c.IBAN, c.BIC
	r.Raise(RemittanceGenerated{EventMeta: r.NewEventMeta(), Items: len(items), Total: r.Total().StringFixed(2)})
	return nil
}

// Settle records that the bank charged the remittance on a date.
func (r *Remittance) Settle(on vocab.Date) error {
	if r.s.Status != RemGenerated {
		return fw.Violation("treasury.remittance_not_generated", "only a generated remittance is settled")
	}
	r.s.Status, r.s.Settled = RemSettled, on
	r.Raise(RemittanceSettled{EventMeta: r.NewEventMeta(), Creditor: r.s.Creditor.String(), Settled: on.String(), Items: r.State().Items})
	return nil
}

// Return records a direct debit returned by the debtor's bank (after settlement).
func (r *Remittance) Return(endToEnd string, on vocab.Date, reason string) error {
	if r.s.Status != RemSettled {
		return fw.Violation("treasury.remittance_not_settled", "only a settled remittance has returns")
	}
	k := slices.IndexFunc(r.s.Items, func(x Item) bool { return x.EndToEnd == endToEnd })
	if k < 0 {
		return fw.NotFound("treasury.remittance_item", fwText(endToEnd))
	}
	if !r.s.Items[k].Returned.IsZero() {
		return fw.Violation("treasury.already_returned", "the direct debit was already returned")
	}
	reason = strings.ToUpper(strings.TrimSpace(reason))
	if len(reason) != 4 || on.Before(r.s.Settled) {
		return fw.Violation("treasury.return_invalid", "a 4-character ISO reason and a date not before the settlement")
	}
	r.s.Items = slices.Clone(r.s.Items)
	r.s.Items[k].Returned, r.s.Items[k].Reason = on, reason
	it := r.s.Items[k]
	r.Raise(DirectDebitReturned{EventMeta: r.NewEventMeta(), Creditor: r.s.Creditor.String(), Item: it})
	return nil
}

// Cancel cancels a remittance that was not settled (a generated file not sent, or a draft).
func (r *Remittance) Cancel() error {
	if r.s.Status == RemSettled || r.s.Status == RemCancelled {
		return fw.Violation("treasury.remittance_closed", "a settled or cancelled remittance does not change")
	}
	r.s.Status = RemCancelled
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (r *Remittance) AuditSnapshot() map[string]any {
	return map[string]any{"status": r.s.Status.String(), "items": len(r.s.Items), "total": r.Total().String()}
}

type fwText string

func (t fwText) String() string { return string(t) }

// Remittance fields.
var (
	RemFieldCreditor = spec.Comparable("creditor", func(r *Remittance) OrganizationID { return r.s.Creditor })
	RemFieldStatus   = spec.Comparable("status", func(r *Remittance) int { return int(r.s.Status) })
	RemFieldDate     = spec.OrderedBy("collection_date", func(r *Remittance) vocab.Date { return r.s.CollectionDate }, vocab.CompareDates)
	RemFieldItems    = spec.Collection("items", func(r *Remittance) []Item { return r.s.Items })
	ItemFieldInvoice = spec.Comparable("invoice", func(i Item) InvoiceID { return i.Invoice })
)

// Events of the context.
type (
	// RemittanceGenerated is raised when the file of a remittance is generated.
	RemittanceGenerated struct {
		fw.EventMeta
		Items int    `json:"items"`
		Total string `json:"total"`
	}
	// RemittanceSettled is raised when the bank charges a remittance, with its items.
	RemittanceSettled struct {
		fw.EventMeta
		Creditor string `json:"creditor"`
		Settled  string `json:"settled"`
		Items    []Item `json:"items"`
	}
	// DirectDebitReturned is raised when a direct debit is returned.
	DirectDebitReturned struct {
		fw.EventMeta
		Creditor string `json:"creditor"`
		Item     Item   `json:"item"`
	}
)

// EventType implementations.
func (RemittanceGenerated) EventType() string { return "treasury.remittance_generated" }
func (RemittanceSettled) EventType() string   { return "treasury.remittance_settled" }
func (DirectDebitReturned) EventType() string { return "treasury.direct_debit_returned" }

// Repositories and ports of the context.
type (
	AccountRepository    = fw.Repository[AccountID, *Account]
	MandateRepository    = fw.Repository[MandateID, *Mandate]
	RemittanceRepository = fw.Repository[RemittanceID, *Remittance]
)

// DueItem is an open installment Receivables can collect.
type DueItem struct {
	Invoice     InvoiceID
	Number      string
	Customer    PartyID
	Installment int
	Due         vocab.Date
	Open        vocab.Decimal
}

// Receivables is what Treasury needs of Receivables (a port it owns; an adapter implements it over
// the Receivables contracts).
type Receivables interface {
	DueItems(ctx context.Context, seller OrganizationID, dueTo vocab.Date) ([]DueItem, error)
}

// Identity is the name and tax number of a party.
type Identity struct {
	Name string
	NIF  string
}

// Identities resolves names and tax numbers of parties (a port Treasury owns over Parties).
type Identities interface {
	Identities(ctx context.Context, parties []PartyID) (map[PartyID]Identity, error)
}

// New identities and parsing.
func NewAccountID() AccountID       { return AccountID{fw.NewUUID()} }
func NewMandateID() MandateID       { return MandateID{fw.NewUUID()} }
func NewRemittanceID() RemittanceID { return RemittanceID{fw.NewUUID()} }

// ParseAccountID parses a textual identity.
func ParseAccountID(s string) (AccountID, error) { u, err := fw.ParseUUID(s); return AccountID{u}, err }

// ParseMandateID parses a textual identity.
func ParseMandateID(s string) (MandateID, error) { u, err := fw.ParseUUID(s); return MandateID{u}, err }

// ParseRemittanceID parses a textual identity.
func ParseRemittanceID(s string) (RemittanceID, error) {
	u, err := fw.ParseUUID(s)
	return RemittanceID{u}, err
}
