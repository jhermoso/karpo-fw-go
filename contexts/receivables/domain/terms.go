// Package domain es el modelo del contexto delimitado Receivables (Cobros): las condiciones de pago y
// los vencimientos que producen, el cobro pendiente de cada factura emitida con sus vencimientos,
// los cobros de los clientes y su aplicación a vencimientos, y el perfil de crédito de cada cliente
// (condiciones por defecto, límite de riesgo, bloqueo). Las facturas son de Billing, los clientes
// de Parties, las cuentas bancarias y las remesas de Treasury: este contexto los referencia por
// identidad.
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

type (
	// TermsID identifica unas condiciones de pago.
	TermsID struct{ fw.UUID }
	// ReceivableID identifica un cobro pendiente: el id de la factura de la que viene.
	ReceivableID struct{ fw.UUID }
	// CollectionID identifica un cobro.
	CollectionID struct{ fw.UUID }
	// CreditProfileID identifica un perfil de crédito.
	CreditProfileID struct{ fw.UUID }
	// OrganizationID es el vendedor, una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es el cliente, una party del contexto Parties.
	PartyID struct{ fw.UUID }
)

// TermsKind es el nombre estable del tipo de agregado.
const TermsKind = "receivables.payment_terms"

// MonthRange es un periodo de meses (ambos incluidos) en el que no vence nada (por ejemplo,
// agosto); puede dar la vuelta al año (de diciembre a enero).
type MonthRange struct{ From, To time.Month }

// IsZero dice si no se fijó ningún periodo.
func (r MonthRange) IsZero() bool { return r.From == 0 && r.To == 0 }

// Contains dice si un mes está en el periodo.
func (r MonthRange) Contains(m time.Month) bool {
	if r.IsZero() {
		return false
	}
	if r.From <= r.To {
		return m >= r.From && m <= r.To
	}
	return m >= r.From || m <= r.To
}

// TermsState es el estado persistido de unas condiciones de pago (el PaymentCondition de Parties
// del C#, que nada usaba y nada convertía en vencimientos).
type TermsState struct {
	Seller          OrganizationID
	Code            string
	Description     string
	Installments    int
	DaysToFirst     int
	DaysBetween     int
	FixedDays       []int // días del mes en que vencen los pagos (hasta 3)
	NoPayment       MonthRange
	CommercialMonth bool // los periodos de 30 días cuentan como meses naturales
	ControlHolidays bool // un vencimiento en festivo se mueve hacia atrás (como mucho BackwardDays) o hacia delante
	BackwardDays    int
	Active          bool
	Audit           traits.AuditStamp
}

func (s *TermsState) check(v *fw.Validation) {
	s.Code = strings.ToUpper(strings.TrimSpace(s.Code))
	v.Require(s.Code != "" && utf8.RuneCountInString(s.Code) <= 10, "code", "length", "a code of 1 to 10 characters")
	s.Description = strings.TrimSpace(s.Description)
	v.Require(s.Description != "" && utf8.RuneCountInString(s.Description) <= 100, "description", "length", "a description of 1 to 100 characters")
	v.Require(!s.Seller.IsZero(), "seller", "required", "terms belong to a seller")
	v.Require(s.Installments >= 1 && s.Installments <= 24, "installments", "range", "from 1 to 24 installments")
	v.Require(s.DaysToFirst >= 0 && s.DaysToFirst <= 720 && s.DaysBetween >= 0 && s.DaysBetween <= 365, "daysToFirst", "range", "days within limits")
	v.Require(s.Installments == 1 || s.DaysBetween > 0, "daysBetween", "required", "several installments need days between them")
	fixed := slices.Clone(s.FixedDays)
	slices.Sort(fixed)
	fixed = slices.Compact(fixed)
	v.Require(len(fixed) == len(s.FixedDays) && len(fixed) <= 3, "fixedDays", "count", "up to 3 different fixed days")
	for _, d := range fixed {
		v.Require(d >= 1 && d <= 31, "fixedDays", "range", "days from 1 to 31")
	}
	s.FixedDays = fixed
	okMonths := func(m time.Month) bool { return m >= 1 && m <= 12 }
	v.Require(s.NoPayment.IsZero() || (okMonths(s.NoPayment.From) && okMonths(s.NoPayment.To)), "noPayment", "range",
		"both months of the period, from 1 to 12, or none")
	v.Require(s.BackwardDays >= 0 && s.BackwardDays <= 31, "backwardDays", "range", "0 to 31 days")
}

// Terms son unas condiciones de pago de un vendedor.
type Terms struct {
	fw.BaseAggregateRoot[TermsID]
	traits.Audited
	s TermsState
}

// ReconstituteTerms reconstruye unas condiciones de pago.
func ReconstituteTerms(id TermsID, s TermsState) (*Terms, error) {
	base, err := fw.NewBaseAggregateRoot(TermsKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.check(&v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Terms{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado.
func (t *Terms) State() TermsState {
	s := t.s
	s.FixedDays = slices.Clone(s.FixedDays)
	return s
}

// Retire desactiva las condiciones.
func (t *Terms) Retire() { t.s.Active = false }

// AuditSnapshot implementa traits.Snapshotter.
func (t *Terms) AuditSnapshot() map[string]any {
	return map[string]any{"code": t.s.Code, "installments": t.s.Installments, "active": t.s.Active}
}

// Calendar dice los días en que un vendedor no cobra: los festivos de donde está y lo demás que
// cuente su calendario (un puerto; en el C# no existe calendario, el de por defecto no tiene festivos).
type Calendar interface {
	IsHoliday(ctx context.Context, seller OrganizationID, d vocab.Date) (bool, error)
}

// NoHolidays es un calendario sin festivos.
type NoHolidays struct{}

// IsHoliday implementa Calendar.
func (NoHolidays) IsHoliday(context.Context, OrganizationID, vocab.Date) (bool, error) {
	return false, nil
}

// Due es un vencimiento de un calendario de vencimientos.
type Due struct {
	Date   vocab.Date
	Amount vocab.Decimal
}

// Schedule calcula las fechas e importes de vencimiento de un importe emitido en una fecha:
//  1. el vencimiento k cae DaysToFirst + k × DaysBetween días después de la fecha de emisión (en
//     meses naturales cuando CommercialMonth y los días son múltiplo de 30);
//  2. con días fijos, se mueve al siguiente día fijo (como mucho, el último día del mes);
//  3. en un mes sin pagos, se mueve al primer día fijo (o al día 1) después del periodo;
//  4. con ControlHolidays, un festivo se mueve hacia atrás hasta BackwardDays; si no, hacia delante
//     a un día laborable;
//  5. el importe se reparte en partes iguales redondeadas al céntimo; la última se queda con el resto.
//
// Un importe negativo (un abono) tiene un único vencimiento en la fecha de emisión.
func (t *Terms) Schedule(ctx context.Context, issued vocab.Date, amount vocab.Decimal, cal Calendar) ([]Due, error) {
	if amount.IsNegative() {
		return []Due{{Date: issued, Amount: amount}}, nil
	}
	if cal == nil {
		cal = NoHolidays{}
	}
	n := t.s.Installments
	part := amount.Div(vocab.DecimalFromInt(int64(n))).RoundDown(2)
	out := make([]Due, 0, n)
	rest := amount
	for k := 0; k < n; k++ {
		days := t.s.DaysToFirst + k*t.s.DaysBetween
		d := issued.AddDays(days)
		if t.s.CommercialMonth && days%30 == 0 {
			d = issued.AddMonths(days / 30)
		}
		d = t.toFixedDay(d)
		for guard := 0; t.s.NoPayment.Contains(d.Month()) && guard < 12; guard++ {
			d = t.toFixedDay(vocab.MustDate(d.Year(), d.Month(), 1).AddMonths(1))
		}
		if t.s.ControlHolidays {
			var err error
			if d, err = t.avoidHolidays(ctx, d, cal); err != nil {
				return nil, err
			}
		}
		a := part
		if k == n-1 {
			a = rest
		}
		rest = rest.Sub(a)
		out = append(out, Due{Date: d, Amount: a})
	}
	return out, nil
}

// toFixedDay mueve una fecha al siguiente día fijo, sin pasar del final del mes.
func (t *Terms) toFixedDay(d vocab.Date) vocab.Date {
	if len(t.s.FixedDays) == 0 {
		return d
	}
	for month := 0; month < 2; month++ {
		first := vocab.MustDate(d.Year(), d.Month(), 1).AddMonths(month)
		last := first.AddMonths(1).AddDays(-1).Day()
		for _, fd := range t.s.FixedDays {
			c := vocab.MustDate(first.Year(), first.Month(), min(fd, last))
			if !c.Before(d) {
				return c
			}
		}
	}
	return d
}

func (t *Terms) avoidHolidays(ctx context.Context, d vocab.Date, cal Calendar) (vocab.Date, error) {
	holiday, err := cal.IsHoliday(ctx, t.s.Seller, d)
	if err != nil || !holiday {
		return d, err
	}
	for back := 1; back <= t.s.BackwardDays; back++ {
		c := d.AddDays(-back)
		h, err := cal.IsHoliday(ctx, t.s.Seller, c)
		if err != nil {
			return d, err
		}
		if !h {
			return c, nil
		}
	}
	for fwd := 1; fwd <= 366; fwd++ {
		c := d.AddDays(fwd)
		h, err := cal.IsHoliday(ctx, t.s.Seller, c)
		if err != nil {
			return d, err
		}
		if !h {
			return c, nil
		}
	}
	return d, nil
}

// Campos de las condiciones.
var (
	TermsFieldSeller = spec.Comparable("seller", func(t *Terms) OrganizationID { return t.s.Seller })
	TermsFieldCode   = spec.Ordered("code", func(t *Terms) string { return t.s.Code })
	TermsFieldActive = spec.Comparable("active", func(t *Terms) bool { return t.s.Active })
)
