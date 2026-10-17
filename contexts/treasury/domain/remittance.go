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

// RemittanceKind es el nombre estable del tipo de agregado.
const RemittanceKind = "treasury.remittance"

// MaxItems limita los adeudos de una remesa.
const MaxItems = 5000

// RemittanceStatus es el estado de una remesa: un borrador se edita; una generada está congelada y
// su fichero se envía al banco; una liquidada se cobró (sus adeudos aún se pueden devolver).
type RemittanceStatus int

// Estados.
const (
	RemDraft RemittanceStatus = iota + 1
	RemGenerated
	RemSettled
	RemCancelled
)

var remStatuses = map[RemittanceStatus]string{RemDraft: "draft", RemGenerated: "generated", RemSettled: "settled", RemCancelled: "cancelled"}

// String devuelve el nombre estable.
func (s RemittanceStatus) String() string { return remStatuses[s] }

// Item es un adeudo de una remesa: un vencimiento de una factura, cobrado con un mandato.
type Item struct {
	EndToEnd    string // id único de la operación (el número de factura y el vencimiento)
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
	Sequence    string // FRST o RCUR, fijado al generar el fichero
	Returned    vocab.Date
	Reason      string // motivo de devolución ISO 20022 (AC04, AM04, MD01, MS02…)
}

// RemittanceState es el estado persistido de una remesa.
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

// Remittance es un lote de adeudos directos SEPA de un acreedor desde una de sus cuentas (nada de
// esto existía en el C#).
type Remittance struct {
	fw.BaseAggregateRoot[RemittanceID]
	traits.Audited
	s RemittanceState
}

// ReconstituteRemittance reconstruye una remesa.
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

// DraftRemittance crea un borrador.
func DraftRemittance(id RemittanceID, creditor OrganizationID, account AccountID, scheme Scheme, collection vocab.Date) (*Remittance, error) {
	return ReconstituteRemittance(id, RemittanceState{Creditor: creditor, Account: account, Scheme: scheme, CollectionDate: collection, Status: RemDraft})
}

// State devuelve el estado (los adeudos son una copia).
func (r *Remittance) State() RemittanceState {
	s := r.s
	s.Items = slices.Clone(s.Items)
	return s
}

// Total devuelve la suma de los adeudos (devueltos incluidos).
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

// Add añade un adeudo. Invariantes: un importe positivo en céntimos, un adeudo por vencimiento, y
// el mandato es del esquema de la remesa (lo comprueba quien llama, con el mandato).
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

// endToEnd construye el id end-to-end: el número de factura y el vencimiento, en caracteres SEPA,
// como mucho 35.
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

// Remove quita el adeudo de un vencimiento.
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

// Creditor es lo que el fichero necesita del acreedor.
type Creditor struct {
	Name string
	ID   string // identificador de acreedor SEPA
	IBAN vocab.IBAN
	BIC  string
}

// Generate congela la remesa: los datos del acreedor y el tipo de secuencia de cada adeudo (de su
// mandato). Quien llama registra el uso de los mandatos.
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

// Settle registra que el banco cobró la remesa en una fecha.
func (r *Remittance) Settle(on vocab.Date) error {
	if r.s.Status != RemGenerated {
		return fw.Violation("treasury.remittance_not_generated", "only a generated remittance is settled")
	}
	r.s.Status, r.s.Settled = RemSettled, on
	r.Raise(RemittanceSettled{EventMeta: r.NewEventMeta(), Creditor: r.s.Creditor.String(), Settled: on.String(), Items: r.State().Items})
	return nil
}

// Return registra un adeudo devuelto por el banco del deudor (después de la liquidación).
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

// Cancel anula una remesa que no se liquidó (un fichero generado y no enviado, o un borrador).
func (r *Remittance) Cancel() error {
	if r.s.Status == RemSettled || r.s.Status == RemCancelled {
		return fw.Violation("treasury.remittance_closed", "a settled or cancelled remittance does not change")
	}
	r.s.Status = RemCancelled
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (r *Remittance) AuditSnapshot() map[string]any {
	return map[string]any{"status": r.s.Status.String(), "items": len(r.s.Items), "total": r.Total().String()}
}

type fwText string

func (t fwText) String() string { return string(t) }

// Campos de la remesa.
var (
	RemFieldCreditor = spec.Comparable("creditor", func(r *Remittance) OrganizationID { return r.s.Creditor })
	RemFieldStatus   = spec.Comparable("status", func(r *Remittance) int { return int(r.s.Status) })
	RemFieldDate     = spec.OrderedBy("collection_date", func(r *Remittance) vocab.Date { return r.s.CollectionDate }, vocab.CompareDates)
	RemFieldItems    = spec.Collection("items", func(r *Remittance) []Item { return r.s.Items })
	ItemFieldInvoice = spec.Comparable("invoice", func(i Item) InvoiceID { return i.Invoice })
)

// Eventos del contexto.
type (
	// RemittanceGenerated se lanza al generar el fichero de una remesa.
	RemittanceGenerated struct {
		fw.EventMeta
		Items int    `json:"items"`
		Total string `json:"total"`
	}
	// RemittanceSettled se lanza cuando el banco cobra una remesa, con sus adeudos.
	RemittanceSettled struct {
		fw.EventMeta
		Creditor string `json:"creditor"`
		Settled  string `json:"settled"`
		Items    []Item `json:"items"`
	}
	// DirectDebitReturned se lanza al devolverse un adeudo.
	DirectDebitReturned struct {
		fw.EventMeta
		Creditor string `json:"creditor"`
		Item     Item   `json:"item"`
	}
)

// Implementaciones de EventType.
func (RemittanceGenerated) EventType() string { return "treasury.remittance_generated" }
func (RemittanceSettled) EventType() string   { return "treasury.remittance_settled" }
func (DirectDebitReturned) EventType() string { return "treasury.direct_debit_returned" }

// Repositorios y puertos del contexto.
type (
	AccountRepository    = fw.Repository[AccountID, *Account]
	MandateRepository    = fw.Repository[MandateID, *Mandate]
	RemittanceRepository = fw.Repository[RemittanceID, *Remittance]
)

// DueItem es un vencimiento abierto que Receivables puede cobrar.
type DueItem struct {
	Invoice     InvoiceID
	Number      string
	Customer    PartyID
	Installment int
	Due         vocab.Date
	Open        vocab.Decimal
}

// Receivables es lo que Treasury necesita de Receivables (un puerto propio; un adaptador lo
// implementa sobre los contratos de Receivables).
type Receivables interface {
	DueItems(ctx context.Context, seller OrganizationID, dueTo vocab.Date) ([]DueItem, error)
}

// Identity es el nombre y el número fiscal de una party.
type Identity struct {
	Name string
	NIF  string
}

// Identities resuelve nombres y números fiscales de parties (un puerto propio de Treasury sobre
// Parties).
type Identities interface {
	Identities(ctx context.Context, parties []PartyID) (map[PartyID]Identity, error)
}

// Identidades nuevas e interpretación.
func NewAccountID() AccountID       { return AccountID{fw.NewUUID()} }
func NewMandateID() MandateID       { return MandateID{fw.NewUUID()} }
func NewRemittanceID() RemittanceID { return RemittanceID{fw.NewUUID()} }

// ParseAccountID interpreta una identidad en texto.
func ParseAccountID(s string) (AccountID, error) { u, err := fw.ParseUUID(s); return AccountID{u}, err }

// ParseMandateID interpreta una identidad en texto.
func ParseMandateID(s string) (MandateID, error) { u, err := fw.ParseUUID(s); return MandateID{u}, err }

// ParseRemittanceID interpreta una identidad en texto.
func ParseRemittanceID(s string) (RemittanceID, error) {
	u, err := fw.ParseUUID(s)
	return RemittanceID{u}, err
}
