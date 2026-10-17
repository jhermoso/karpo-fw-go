// Package domain es el modelo del contexto delimitado Financial (Cuentas de clientes): las cuentas que
// una entidad lleva a sus clientes, quién es titular de cada una, para qué se usa y si puede
// operar. En el C# esto estaba partido en dos modelos que nunca se encontraban: FinancialAccount,
// una cáscara vacía sin datos, y BankAccount, que tenía todo lo que era real.
package domain

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// AccountKind es el nombre del tipo de agregado de una cuenta.
const AccountKind = "financial.account"

// Límites de una cuenta.
const (
	MaxHolders = 50
	MaxUses    = 50
)

// Identidades del contexto.
type (
	// AccountID identifica una cuenta.
	AccountID struct{ fw.UUID }
	// OrganizationID es la entidad, una organización interna de Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es un cliente o cualquiera relacionado con una cuenta.
	PartyID struct{ fw.UUID }
)

// NewAccountID devuelve una identidad nueva.
func NewAccountID() AccountID { return AccountID{fw.NewUUID()} }

// ParseAccountID interpreta una identidad en texto.
func ParseAccountID(s string) (AccountID, error) { u, err := fw.ParseUUID(s); return AccountID{u}, err }

// Status es si una cuenta puede operar.
type Status string

// Estados (Active, Blocked y Abandoned del C#, los de Apiscore, más el cierre que le faltaba).
const (
	Active    Status = "active"
	Blocked   Status = "blocked"   // no puede operar hasta que se libere
	Abandoned Status = "abandoned" // su titular dejó de usarla; el dinero espera aparte
	Closed    Status = "closed"
)

// Statuses enumera los estados válidos.
var Statuses = []Status{Active, Blocked, Abandoned, Closed}

// Papeles de una party en una cuenta (PartyBankAccountRole del C#).
const (
	RoleHolder      = "holder"
	RoleAuthorized  = "authorized"
	RoleBeneficiary = "beneficiary"
)

// Roles enumera los papeles válidos.
var Roles = []string{RoleHolder, RoleAuthorized, RoleBeneficiary}

// Uses son los usos que puede tener una cuenta (los diez que el C# sembraba como tabla; las cuentas de la propia
// entidad son las de Treasury).
var Uses = []string{"customer-payment", "virtual-multicurrency", "agent", "player", "player-operating", "player-transit", "cash-operating",
	"segregated", "abandonment"}

// Holder es una party relacionada con una cuenta entre dos días.
type Holder struct {
	Party   PartyID
	Role    string
	From    vocab.Date
	Thru    vocab.Date // el último día; cero mientras dura
	Primary bool       // aquél a cuyo nombre está archivada la cuenta
}

// Current dice si la relación dura.
func (h Holder) Current() bool { return h.Thru.IsZero() }

// Use es algo para lo que se usa una cuenta entre dos días.
type Use struct {
	Code string
	From vocab.Date
	Thru vocab.Date
}

// AccountState es el estado persistido de una cuenta.
type AccountState struct {
	Company   OrganizationID
	Number    string     // el IBAN sin espacios, o el identificador propio de la entidad cuando es virtual
	Virtual   bool       // no tiene IBAN: sólo existe en los libros de la entidad
	IBAN      vocab.IBAN // de una cuenta que no es virtual
	BIC       string
	Currency  vocab.CurrencyCode
	Name      string
	Product   fw.UUID // el producto financiero del que es una instancia
	Agreement fw.UUID // el acuerdo que la rige
	Status    Status
	Demo      bool // una cuenta de prueba de la entidad: nunca es dinero real
	Opened    vocab.Date
	Closed    vocab.Date
	Reason    string // del bloqueo, el abandono o el cierre
	Holders   []Holder
	Uses      []Use
	Audit     traits.AuditStamp
}

// Account es una cuenta que una entidad lleva a sus clientes.
type Account struct {
	fw.BaseAggregateRoot[AccountID]
	traits.Audited
	s AccountState
}

// NormalizeNumber quita los espacios y pasa a mayúsculas las letras de un número de cuenta.
func NormalizeNumber(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

func validBIC(s string) bool {
	if len(s) != 8 && len(s) != 11 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func validNumber(s string) bool {
	if len(s) < 8 || len(s) > 34 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// ReconstituteAccount reconstruye una cuenta.
func ReconstituteAccount(id AccountID, s AccountState) (*Account, error) {
	base, err := fw.NewBaseAggregateRoot(AccountKind, id)
	if err != nil {
		return nil, err
	}
	s.Number, s.BIC = NormalizeNumber(s.Number), strings.ToUpper(strings.TrimSpace(s.BIC))
	s.Name, s.Reason = strings.TrimSpace(s.Name), strings.TrimSpace(s.Reason)
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the institution that keeps the account")
	if s.Virtual {
		v.Require(s.IBAN.IsZero() && validNumber(s.Number), "number", "format", "an identifier of 8 to 34 letters, digits and dashes")
	} else {
		v.Require(!s.IBAN.IsZero() && s.Number == s.IBAN.String(), "iban", "required", "an account that is not virtual has an IBAN")
	}
	v.Require(s.BIC == "" || validBIC(s.BIC), "bic", "format", "a BIC of 8 or 11 characters")
	v.Require(!s.Currency.IsZero(), "currency", "required", "the currency is required")
	v.Require(utf8.RuneCountInString(s.Name) <= 200 && utf8.RuneCountInString(s.Reason) <= 200, "name", "length",
		"a name and a reason of at most 200 characters")
	v.Require(slices.Contains(Statuses, s.Status), "status", "enum", "a status")
	v.Require(!s.Opened.IsZero(), "opened", "required", "the day it was opened")
	v.Require((s.Status == Closed) == !s.Closed.IsZero() && (s.Closed.IsZero() || !s.Closed.Before(s.Opened)), "closed", "state",
		"a closed account, and only it, has the day it was closed, not before it was opened")
	v.Require(len(s.Holders) <= MaxHolders && len(s.Uses) <= MaxUses, "holders", "count", "too many holders or uses")
	primaries, holders := 0, 0
	seen := map[string]bool{}
	for i, h := range s.Holders {
		f := fmt.Sprintf("holders[%d]", i)
		v.Require(!h.Party.IsZero() && slices.Contains(Roles, h.Role), f, "required", "a party with a role: holder, authorized or beneficiary")
		v.Require(!h.From.IsZero() && (h.Thru.IsZero() || !h.Thru.Before(h.From)), f, "range", "from a day, and not until before it")
		if h.Current() {
			k := h.Party.String() + "|" + h.Role
			v.Require(!seen[k], f, "duplicate", "a party has each role once at a time")
			seen[k] = true
			if h.Role == RoleHolder {
				holders++
			}
			if h.Primary {
				primaries++
				v.Require(h.Role == RoleHolder, f, "primary", "the account is filed under one of its holders")
			}
		} else {
			v.Require(!h.Primary, f, "primary", "the account is filed under someone who still holds it")
		}
	}
	// Una cuenta cerrada conserva su historia, no sus titulares.
	v.Require(s.Status == Closed || holders >= 1 && primaries == 1, "holders", "required", "an account has at least one holder, and is filed under one of them")
	open := map[string]bool{}
	for i, u := range s.Uses {
		f := fmt.Sprintf("uses[%d]", i)
		v.Require(slices.Contains(Uses, u.Code), f, "enum", "a use of the catalogue")
		v.Require(!u.From.IsZero() && (u.Thru.IsZero() || !u.Thru.Before(u.From)), f, "range", "from a day, and not until before it")
		if u.Thru.IsZero() {
			v.Require(!open[u.Code], f, "duplicate", "an account has each use once at a time")
			open[u.Code] = true
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Holders, s.Uses = slices.Clone(s.Holders), slices.Clone(s.Uses)
	return &Account{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// Opening es aquello con lo que se abre una cuenta.
type Opening struct {
	Company   OrganizationID
	Number    string
	Virtual   bool
	BIC       string
	Currency  vocab.CurrencyCode
	Name      string
	Product   fw.UUID
	Agreement fw.UUID
	Demo      bool
	Opened    vocab.Date
	Holder    PartyID
	Uses      []string
}

// Open abre una cuenta a un cliente, que es su titular desde ese día y a cuyo nombre se archiva.
// El número de una cuenta que no es virtual es un IBAN, y sus dígitos de control tienen que ser
// correctos (el C# sólo comprobaba su longitud).
func Open(id AccountID, o Opening) (*Account, error) {
	s := AccountState{Company: o.Company, Number: NormalizeNumber(o.Number), Virtual: o.Virtual, BIC: o.BIC, Currency: o.Currency, Name: o.Name,
		Product: o.Product, Agreement: o.Agreement, Status: Active, Demo: o.Demo, Opened: o.Opened,
		Holders: []Holder{{Party: o.Holder, Role: RoleHolder, From: o.Opened, Primary: true}}}
	if !o.Virtual {
		iban, err := vocab.NewIBAN(s.Number)
		if err != nil {
			var v fw.Validation
			v.Add("number", "iban", "the number of an account that is not virtual is an IBAN: "+err.Error())
			return nil, v.Err()
		}
		s.IBAN, s.Number = iban, iban.String()
	}
	for _, code := range o.Uses {
		s.Uses = append(s.Uses, Use{Code: code, From: o.Opened})
	}
	a, err := ReconstituteAccount(id, s)
	if err != nil {
		return nil, err
	}
	a.Raise(AccountOpened{EventMeta: a.NewEventMeta(), Company: s.Company.String(), Number: a.s.Number, Currency: s.Currency.String(), Holder: o.Holder.String(),
		Virtual: s.Virtual, Demo: s.Demo, Opened: s.Opened})
	return a, nil
}

// State devuelve el estado (los slices son copias).
func (a *Account) State() AccountState {
	s := a.s
	s.Holders, s.Uses = slices.Clone(s.Holders), slices.Clone(s.Uses)
	return s
}

// PrimaryHolder devuelve el titular a cuyo nombre está archivada la cuenta (cero en una cuenta
// cerrada que los terminó a todos).
func (a *Account) PrimaryHolder() PartyID {
	for _, h := range a.s.Holders {
		if h.Primary {
			return h.Party
		}
	}
	return PartyID{}
}

// try aplica un cambio si la cuenta sigue siendo válida.
func (a *Account) try(change func(*AccountState)) error {
	s := a.State()
	change(&s)
	n, err := ReconstituteAccount(a.ID(), s)
	if err != nil {
		return err
	}
	a.s = n.s
	return nil
}

func (a *Account) open() error {
	if a.s.Status == Closed {
		return fw.Violation("financial.closed", "a closed account does not change")
	}
	return nil
}

// Describe cambia el nombre, el BIC, el producto y el acuerdo de una cuenta.
func (a *Account) Describe(name, bic string, product, agreement fw.UUID) error {
	if err := a.open(); err != nil {
		return err
	}
	return a.try(func(s *AccountState) { s.Name, s.BIC, s.Product, s.Agreement = name, bic, product, agreement })
}

// Relate da a una party un papel en la cuenta desde un día. Si la party es principal, la cuenta se archiva a
// su nombre desde entonces.
func (a *Account) Relate(party PartyID, role string, from vocab.Date, primary bool) error {
	if err := a.open(); err != nil {
		return err
	}
	for _, h := range a.s.Holders {
		if h.Current() && h.Party == party && h.Role == role {
			return fw.Violation("financial.already_related", "the party already has that role on the account")
		}
	}
	return a.try(func(s *AccountState) {
		if primary {
			for i := range s.Holders {
				s.Holders[i].Primary = false
			}
		}
		s.Holders = append(s.Holders, Holder{Party: party, Role: role, From: from, Primary: primary})
	})
}

// Unrelate termina el papel de una party en un día. El último titular no puede irse, y aquél a
// cuyo nombre está archivada la cuenta sólo se va después de que otro ocupe su lugar.
func (a *Account) Unrelate(party PartyID, role string, on vocab.Date) error {
	if err := a.open(); err != nil {
		return err
	}
	k := slices.IndexFunc(a.s.Holders, func(h Holder) bool { return h.Current() && h.Party == party && h.Role == role })
	if k < 0 {
		return fw.Violation("financial.not_related", "the party does not have that role on the account")
	}
	if a.s.Holders[k].Primary {
		return fw.Violation("financial.primary_holder", "the account is filed under this holder: file it under another first")
	}
	return a.try(func(s *AccountState) { s.Holders[k].Thru = on })
}

// FileUnder archiva la cuenta a nombre de otro de sus titulares.
func (a *Account) FileUnder(party PartyID) error {
	if err := a.open(); err != nil {
		return err
	}
	k := slices.IndexFunc(a.s.Holders, func(h Holder) bool { return h.Current() && h.Party == party && h.Role == RoleHolder })
	if k < 0 {
		return fw.Violation("financial.not_related", "the party does not hold the account")
	}
	return a.try(func(s *AccountState) {
		for i := range s.Holders {
			s.Holders[i].Primary = i == k
		}
	})
}

// Assign da a la cuenta un uso desde un día.
func (a *Account) Assign(code string, from vocab.Date) error {
	if err := a.open(); err != nil {
		return err
	}
	if a.Used(code) {
		return fw.Violation("financial.already_used", "the account already has that use")
	}
	return a.try(func(s *AccountState) { s.Uses = append(s.Uses, Use{Code: code, From: from}) })
}

// Withdraw termina un uso de la cuenta en un día.
func (a *Account) Withdraw(code string, on vocab.Date) error {
	if err := a.open(); err != nil {
		return err
	}
	k := slices.IndexFunc(a.s.Uses, func(u Use) bool { return u.Code == code && u.Thru.IsZero() })
	if k < 0 {
		return fw.Violation("financial.not_used", "the account does not have that use")
	}
	return a.try(func(s *AccountState) { s.Uses[k].Thru = on })
}

// Used dice si la cuenta tiene ahora un uso.
func (a *Account) Used(code string) bool {
	return slices.ContainsFunc(a.s.Uses, func(u Use) bool { return u.Code == code && u.Thru.IsZero() })
}

func (a *Account) become(to Status, reason string, closed vocab.Date) error {
	from := a.s.Status
	if err := a.try(func(s *AccountState) { s.Status, s.Reason, s.Closed = to, reason, closed }); err != nil {
		return err
	}
	a.Raise(AccountStatusChanged{EventMeta: a.NewEventMeta(), Company: a.s.Company.String(), Number: a.s.Number, Holder: a.PrimaryHolder().String(),
		From: string(from), To: string(to), Reason: a.s.Reason})
	return nil
}

func reasoned(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fw.Violation("financial.reason", "say why")
	}
	return nil
}

// Block impide operar a una cuenta activa, diciendo por qué.
func (a *Account) Block(reason string) error {
	if a.s.Status != Active {
		return fw.Violation("financial.transition", "only an active account is blocked; this one is "+string(a.s.Status))
	}
	if err := reasoned(reason); err != nil {
		return err
	}
	return a.become(Blocked, reason, vocab.Date{})
}

// Abandon aparta una cuenta que su titular dejó de usar, diciendo por qué.
func (a *Account) Abandon(reason string) error {
	if a.s.Status != Active && a.s.Status != Blocked {
		return fw.Violation("financial.transition", "an account "+string(a.s.Status)+" is not set apart as abandoned")
	}
	if err := reasoned(reason); err != nil {
		return err
	}
	return a.become(Abandoned, reason, vocab.Date{})
}

// Release deja que una cuenta bloqueada o abandonada vuelva a operar.
func (a *Account) Release() error {
	if a.s.Status != Blocked && a.s.Status != Abandoned {
		return fw.Violation("financial.transition", "only a blocked or abandoned account is released; this one is "+string(a.s.Status))
	}
	return a.become(Active, "", vocab.Date{})
}

// Close termina la cuenta en un día: sus titulares y sus usos terminan con ella, y ya no cambia más.
func (a *Account) Close(on vocab.Date, reason string) error {
	if err := a.open(); err != nil {
		return err
	}
	if on.Before(a.s.Opened) {
		return fw.Violation("financial.closing_date", "an account is not closed before it was opened")
	}
	holder := a.PrimaryHolder()
	from := a.s.Status
	if err := a.try(func(s *AccountState) {
		s.Status, s.Reason, s.Closed = Closed, reason, on
		for i := range s.Holders {
			if s.Holders[i].Current() {
				s.Holders[i].Thru, s.Holders[i].Primary = later(on, s.Holders[i].From), false
			}
		}
		for i := range s.Uses {
			if s.Uses[i].Thru.IsZero() {
				s.Uses[i].Thru = later(on, s.Uses[i].From)
			}
		}
	}); err != nil {
		return err
	}
	a.Raise(AccountStatusChanged{EventMeta: a.NewEventMeta(), Company: a.s.Company.String(), Number: a.s.Number, Holder: holder.String(), From: string(from),
		To: string(Closed), Reason: a.s.Reason})
	return nil
}

func later(a, b vocab.Date) vocab.Date {
	if a.Before(b) {
		return b
	}
	return a
}

// AuditSnapshot implementa traits.Snapshotter.
func (a *Account) AuditSnapshot() map[string]any {
	holders, uses := []string{}, []string{}
	for _, h := range a.s.Holders {
		if h.Current() {
			holders = append(holders, h.Role+":"+h.Party.String())
		}
	}
	for _, u := range a.s.Uses {
		if u.Thru.IsZero() {
			uses = append(uses, u.Code)
		}
	}
	slices.Sort(holders)
	slices.Sort(uses)
	return map[string]any{"number": a.s.Number, "status": string(a.s.Status), "reason": a.s.Reason, "name": a.s.Name, "holders": strings.Join(holders, ","),
		"uses": strings.Join(uses, ","), "primary": a.PrimaryHolder().String()}
}

// Campos de la cuenta.
var (
	AccFieldCompany   = spec.Comparable("company", func(a *Account) OrganizationID { return a.s.Company })
	AccFieldNumber    = spec.Ordered("account_number", func(a *Account) string { return a.s.Number })
	AccFieldStatus    = spec.Comparable("status", func(a *Account) string { return string(a.s.Status) })
	AccFieldCurrency  = spec.Comparable("currency", func(a *Account) string { return a.s.Currency.String() })
	AccFieldDemo      = spec.Comparable("demo", func(a *Account) bool { return a.s.Demo })
	AccFieldVirtual   = spec.Comparable("virtual_account", func(a *Account) bool { return a.s.Virtual })
	AccFieldProduct   = spec.Comparable("product", func(a *Account) fw.UUID { return a.s.Product })
	AccFieldAgreement = spec.Comparable("agreement", func(a *Account) fw.UUID { return a.s.Agreement })
	AccFieldName      = spec.Text("account_name", func(a *Account) string { return a.s.Name })
	AccFieldHolders   = spec.Collection("holders", func(a *Account) []Holder { return a.s.Holders })
	HolFieldParty     = spec.Comparable("party", func(h Holder) PartyID { return h.Party })
	HolFieldCurrent   = spec.Comparable("current_holder", func(h Holder) bool { return h.Current() })
	AccFieldUses      = spec.Collection("uses", func(a *Account) []Use { return a.s.Uses })
	UseFieldCode      = spec.Comparable("use_code", func(u Use) string { return u.Code })
	UseFieldCurrent   = spec.Comparable("current_use", func(u Use) bool { return u.Thru.IsZero() })
)

// AccountRepository guarda las cuentas.
type AccountRepository = fw.Repository[AccountID, *Account]

// Eventos de una cuenta.
type (
	// AccountOpened se lanza cuando se abre una cuenta.
	AccountOpened struct {
		fw.EventMeta
		Company  string     `json:"company"`
		Number   string     `json:"number"`
		Currency string     `json:"currency"`
		Holder   string     `json:"holder"`
		Virtual  bool       `json:"virtual"`
		Demo     bool       `json:"demo"`
		Opened   vocab.Date `json:"opened"`
	}
	// AccountStatusChanged se lanza cuando una cuenta se bloquea, se aparta, se libera o se cierra.
	AccountStatusChanged struct {
		fw.EventMeta
		Company string `json:"company"`
		Number  string `json:"number"`
		Holder  string `json:"holder"`
		From    string `json:"from"`
		To      string `json:"to"`
		Reason  string `json:"reason"`
	}
)

// Implementaciones de EventType.
func (AccountOpened) EventType() string        { return "financial.account_opened" }
func (AccountStatusChanged) EventType() string { return "financial.account_status_changed" }
