package domain

import (
	"context"
	"encoding/xml"
	"slices"
	"strings"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// TransferOrderKind is the stable aggregate type name.
const TransferOrderKind = "treasury.transfer_order"

// PayableID identifies a payable of the Payments context.
type PayableID struct{ fw.UUID }

// TransferOrderID identifies a transfer order.
type TransferOrderID struct{ fw.UUID }

// Transfer is a credit transfer of an order: one account of a payable.
type Transfer struct {
	EndToEnd  string // the payable and the account line, unique
	Payable   PayableID
	Line      int
	Document  string
	Payee     string // party id, or the tax authority
	PayeeName string
	IBAN      vocab.IBAN
	Amount    vocab.Decimal
	Rejected  vocab.Date
	Reason    string // ISO 20022 reason (AC01, AC04, AM05…)
}

// TransferOrderState is the persisted state of a transfer order.
type TransferOrderState struct {
	Debtor        OrganizationID
	Account       AccountID
	ExecutionDate vocab.Date
	Status        RemittanceStatus // draft, generated, settled or cancelled, as a remittance
	Transfers     []Transfer
	GeneratedAt   time.Time
	DebtorName    string
	DebtorIBAN    vocab.IBAN
	DebtorBIC     string
	Settled       vocab.Date
	Audit         traits.AuditStamp
}

// TransferOrder is a batch of SEPA credit transfers of a company from one of its accounts, paying
// what Payments owes (nothing of this existed in the C#: no pain.001, no Norma 34).
type TransferOrder struct {
	fw.BaseAggregateRoot[TransferOrderID]
	traits.Audited
	s TransferOrderState
}

// ReconstituteTransferOrder rebuilds a transfer order.
func ReconstituteTransferOrder(id TransferOrderID, s TransferOrderState) (*TransferOrder, error) {
	base, err := fw.NewBaseAggregateRoot(TransferOrderKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Debtor.IsZero() && !s.Account.IsZero(), "account", "required", "debtor and account are required")
	v.Require(!s.ExecutionDate.IsZero(), "executionDate", "required", "the execution date is required")
	_, ok := remStatuses[s.Status]
	v.Require(ok, "status", "enum", "unknown status")
	v.Require(len(s.Transfers) <= MaxItems, "transfers", "count", "too many transfers")
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Transfers = slices.Clone(s.Transfers)
	return &TransferOrder{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// DraftTransferOrder creates a draft.
func DraftTransferOrder(id TransferOrderID, debtor OrganizationID, account AccountID, execution vocab.Date) (*TransferOrder, error) {
	return ReconstituteTransferOrder(id, TransferOrderState{Debtor: debtor, Account: account, ExecutionDate: execution, Status: RemDraft})
}

// State returns the state (transfers are a copy).
func (o *TransferOrder) State() TransferOrderState {
	s := o.s
	s.Transfers = slices.Clone(s.Transfers)
	return s
}

// Total returns the sum of the transfers (rejected ones included).
func (o *TransferOrder) Total() vocab.Decimal {
	t := vocab.DecimalFromInt(0)
	for _, x := range o.s.Transfers {
		t = t.Add(x.Amount)
	}
	return t
}

func (o *TransferOrder) mustBeDraft() error {
	if o.s.Status != RemDraft {
		return fw.Violation("treasury.order_not_draft", "only a draft transfer order changes")
	}
	return nil
}

// TransferEndToEnd builds the end-to-end id of a transfer: the payable (32 hex digits) and the
// account line, 35 characters at most.
func TransferEndToEnd(p PayableID, line int) string {
	return strings.ReplaceAll(p.String(), "-", "") + "-" + vocab.DecimalFromInt(int64(line)).String()
}

// Add adds a transfer. Invariants: a positive amount in cents, an IBAN, one transfer per account
// line of a payable.
func (o *TransferOrder) Add(t Transfer) error {
	if err := o.mustBeDraft(); err != nil {
		return err
	}
	if !t.Amount.IsPositive() || !t.Amount.Equal(t.Amount.Round(2)) || t.IBAN.IsZero() {
		return fw.Violation("treasury.transfer_invalid", "a positive amount in cents and an IBAN")
	}
	if slices.ContainsFunc(o.s.Transfers, func(x Transfer) bool { return x.Payable == t.Payable && x.Line == t.Line }) {
		return fw.Violation("treasury.transfer_duplicate", "the payable is already in the order")
	}
	if len(o.s.Transfers) >= MaxItems {
		return fw.Violation("treasury.too_many_items", "the order is full")
	}
	t.EndToEnd = TransferEndToEnd(t.Payable, t.Line)
	t.Rejected, t.Reason = vocab.Date{}, ""
	o.s.Transfers = append(slices.Clone(o.s.Transfers), t)
	return nil
}

// Remove removes the transfers of a payable.
func (o *TransferOrder) Remove(p PayableID) error {
	if err := o.mustBeDraft(); err != nil {
		return err
	}
	n := len(o.s.Transfers)
	o.s.Transfers = slices.DeleteFunc(slices.Clone(o.s.Transfers), func(x Transfer) bool { return x.Payable == p })
	if len(o.s.Transfers) == n {
		return fw.NotFound("treasury.transfer", p)
	}
	return nil
}

// Debtor is what the file needs of the paying company.
type Debtor struct {
	Name string
	IBAN vocab.IBAN
	BIC  string
}

// Generate freezes the order with the debtor data.
func (o *TransferOrder) Generate(d Debtor, at time.Time) error {
	if err := o.mustBeDraft(); err != nil {
		return err
	}
	if len(o.s.Transfers) == 0 {
		return fw.Violation("treasury.empty_order", "an order needs at least one transfer")
	}
	if d.Name == "" || d.IBAN.IsZero() {
		return fw.Violation("treasury.debtor_incomplete", "the debtor needs a name and an account")
	}
	o.s.Status, o.s.GeneratedAt = RemGenerated, at.UTC()
	o.s.DebtorName, o.s.DebtorIBAN, o.s.DebtorBIC = d.Name, d.IBAN, d.BIC
	o.Raise(TransferOrderGenerated{EventMeta: o.NewEventMeta(), Transfers: len(o.s.Transfers), Total: o.Total().StringFixed(2)})
	return nil
}

// Settle records that the bank executed the order on a date.
func (o *TransferOrder) Settle(on vocab.Date) error {
	if o.s.Status != RemGenerated {
		return fw.Violation("treasury.order_not_generated", "only a generated order is settled")
	}
	if on.Before(vocab.DateOf(o.s.GeneratedAt)) {
		return fw.Violation("treasury.settle_date", "the order is executed after being generated")
	}
	o.s.Status, o.s.Settled = RemSettled, on
	o.Raise(TransfersExecuted{EventMeta: o.NewEventMeta(), Debtor: o.s.Debtor.String(), Executed: on.String(), Transfers: o.State().Transfers})
	return nil
}

// Reject records a transfer rejected or returned by the payee's bank (after execution).
func (o *TransferOrder) Reject(endToEnd string, on vocab.Date, reason string) error {
	if o.s.Status != RemSettled {
		return fw.Violation("treasury.order_not_settled", "only an executed order has rejections")
	}
	k := slices.IndexFunc(o.s.Transfers, func(x Transfer) bool { return x.EndToEnd == endToEnd })
	if k < 0 {
		return fw.NotFound("treasury.transfer", fwText(endToEnd))
	}
	if !o.s.Transfers[k].Rejected.IsZero() {
		return fw.Violation("treasury.already_rejected", "the transfer was already rejected")
	}
	reason = strings.ToUpper(strings.TrimSpace(reason))
	if len(reason) != 4 || on.Before(o.s.Settled) {
		return fw.Violation("treasury.reject_invalid", "a 4-character ISO reason and a date not before the execution")
	}
	o.s.Transfers = slices.Clone(o.s.Transfers)
	o.s.Transfers[k].Rejected, o.s.Transfers[k].Reason = on, reason
	o.Raise(TransferRejected{EventMeta: o.NewEventMeta(), Debtor: o.s.Debtor.String(), Transfer: o.s.Transfers[k]})
	return nil
}

// Cancel cancels an order that was not executed.
func (o *TransferOrder) Cancel() error {
	if o.s.Status == RemSettled || o.s.Status == RemCancelled {
		return fw.Violation("treasury.order_closed", "an executed or cancelled order does not change")
	}
	o.s.Status = RemCancelled
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (o *TransferOrder) AuditSnapshot() map[string]any {
	return map[string]any{"status": o.s.Status.String(), "transfers": len(o.s.Transfers), "total": o.Total().String()}
}

// Transfer order fields.
var (
	TrfFieldDebtor    = spec.Comparable("debtor", func(o *TransferOrder) OrganizationID { return o.s.Debtor })
	TrfFieldStatus    = spec.Comparable("status", func(o *TransferOrder) int { return int(o.s.Status) })
	TrfFieldDate      = spec.OrderedBy("execution_date", func(o *TransferOrder) vocab.Date { return o.s.ExecutionDate }, vocab.CompareDates)
	TrfFieldTransfers = spec.Collection("transfers", func(o *TransferOrder) []Transfer { return o.s.Transfers })
	TransferFieldPay  = spec.Comparable("payable", func(t Transfer) PayableID { return t.Payable })
)

// Events of transfer orders.
type (
	// TransferOrderGenerated is raised when the file of an order is generated.
	TransferOrderGenerated struct {
		fw.EventMeta
		Transfers int    `json:"transfers"`
		Total     string `json:"total"`
	}
	// TransfersExecuted is raised when the bank executes an order, with its transfers.
	TransfersExecuted struct {
		fw.EventMeta
		Debtor    string     `json:"debtor"`
		Executed  string     `json:"executed"`
		Transfers []Transfer `json:"transfers"`
	}
	// TransferRejected is raised when a transfer is rejected.
	TransferRejected struct {
		fw.EventMeta
		Debtor   string   `json:"debtor"`
		Transfer Transfer `json:"transfer"`
	}
)

// EventType implementations.
func (TransferOrderGenerated) EventType() string { return "treasury.transfer_order_generated" }
func (TransfersExecuted) EventType() string      { return "treasury.transfers_executed" }
func (TransferRejected) EventType() string       { return "treasury.transfer_rejected" }

// TransferOrderRepository stores transfer orders.
type TransferOrderRepository = fw.Repository[TransferOrderID, *TransferOrder]

// PayableDue is a payable Payments has to pay by transfer, with its accounts.
type PayableDue struct {
	Payable  PayableID
	Kind     string
	Document string
	Payee    string
	Due      vocab.Date
	PayTo    []PayableAccount
}

// PayableAccount is an account a payable is paid to.
type PayableAccount struct {
	IBAN   vocab.IBAN
	Amount vocab.Decimal
}

// Payables is what Treasury needs of Payments (a port it owns; an adapter implements it over the
// Payments contracts).
type Payables interface {
	DueForTransfer(ctx context.Context, debtor OrganizationID, dueTo vocab.Date) (items []PayableDue, withoutAccount int, err error)
}

// NewTransferOrderID returns a new identity.
func NewTransferOrderID() TransferOrderID { return TransferOrderID{fw.NewUUID()} }

// ParseTransferOrderID parses a textual identity.
func ParseTransferOrderID(s string) (TransferOrderID, error) {
	u, err := fw.ParseUUID(s)
	return TransferOrderID{u}, err
}

// The ISO 20022 customer credit transfer initiation (pain.001.001.03), the file SEPA banks accept
// for credit transfers. One payment information block per order.

type trfDocument struct {
	XMLName xml.Name      `xml:"Document"`
	Xmlns   string        `xml:"xmlns,attr"`
	Init    trfInitiation `xml:"CstmrCdtTrfInitn"`
}

type trfInitiation struct {
	Header  painHeader `xml:"GrpHdr"`
	Payment trfPayment `xml:"PmtInf"`
}

type trfPayment struct {
	ID        string           `xml:"PmtInfId"`
	Method    string           `xml:"PmtMtd"`
	Batch     bool             `xml:"BtchBookg"`
	Count     int              `xml:"NbOfTxs"`
	Sum       string           `xml:"CtrlSum"`
	Service   string           `xml:"PmtTpInf>SvcLvl>Cd"`
	Execution string           `xml:"ReqdExctnDt"`
	Debtor    painParty        `xml:"Dbtr"`
	IBAN      string           `xml:"DbtrAcct>Id>IBAN"`
	Bank      painAgent        `xml:"DbtrAgt"`
	Charges   string           `xml:"ChrgBr"`
	Credits   []trfTransaction `xml:"CdtTrfTxInf"`
}

type trfTransaction struct {
	EndToEnd   string     `xml:"PmtId>EndToEndId"`
	Amount     painAmount `xml:"Amt>InstdAmt"`
	Creditor   painParty  `xml:"Cdtr"`
	IBAN       string     `xml:"CdtrAcct>Id>IBAN"`
	Remittance string     `xml:"RmtInf>Ustrd"`
}

// Pain001 renders the file of a generated order.
func (o *TransferOrder) Pain001() ([]byte, error) {
	if o.s.Status == RemDraft || o.s.Status == RemCancelled {
		return nil, fw.Violation("treasury.order_not_generated", "the file exists once the order is generated")
	}
	msg := strings.ReplaceAll(o.ID().String(), "-", "")
	p := trfPayment{ID: msg + "-TRF", Method: "TRF", Batch: true, Count: len(o.s.Transfers), Sum: o.Total().StringFixed(2), Service: "SEPA",
		Execution: o.s.ExecutionDate.String(), Debtor: painParty{Name: sepaName(o.s.DebtorName)}, IBAN: o.s.DebtorIBAN.String(),
		Bank: agent(o.s.DebtorBIC), Charges: "SLEV"}
	for _, t := range o.s.Transfers {
		p.Credits = append(p.Credits, trfTransaction{EndToEnd: t.EndToEnd, Amount: painAmount{Currency: "EUR", Value: t.Amount.StringFixed(2)},
			Creditor: painParty{Name: sepaName(t.PayeeName)}, IBAN: t.IBAN.String(), Remittance: sepaName(t.Document)})
	}
	doc := trfDocument{Xmlns: "urn:iso:std:iso:20022:tech:xsd:pain.001.001.03", Init: trfInitiation{Header: painHeader{
		MsgID: msg, Created: o.s.GeneratedAt.Format("2006-01-02T15:04:05"), Count: len(o.s.Transfers), Sum: o.Total().StringFixed(2),
		Initiator: painParty{Name: sepaName(o.s.DebtorName)}}, Payment: p}}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}
