package application

import (
	"context"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// LineInput is a line of a received invoice. Category defaults to the supplier's.
type LineInput struct {
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Base        string `json:"base"`
	TaxCode     string `json:"taxCode,omitempty"`
	Treatment   string `json:"treatment,omitempty"`
}

// PayToInput is an account the invoice is paid to.
type PayToInput struct {
	IBAN   string `json:"iban"`
	Amount string `json:"amount"`
}

// RegisterInvoice books an invoice received from a supplier as printed. The due date, the
// withholding rate, the categories and the account default to the supplier profile.
type RegisterInvoice struct {
	Company         string       `json:"company"`
	Supplier        string       `json:"supplier"`
	SupplierNumber  string       `json:"supplierNumber"`
	Issued          vocab.Date   `json:"issued"`
	Received        vocab.Date   `json:"received"`
	Due             vocab.Date   `json:"due,omitzero"`
	Lines           []LineInput  `json:"lines"`
	NonDeductible   bool         `json:"nonDeductible,omitempty"`
	WithholdingRate *string      `json:"withholdingRate,omitempty"`
	Total           string       `json:"total"`
	PayTo           []PayToInput `json:"payTo,omitempty"`
	Corrects        string       `json:"corrects,omitempty"`
}

// CancelInvoice annuls a received invoice booked by mistake.
type CancelInvoice struct {
	ID     domain.InvoiceID `json:"-"`
	Reason string           `json:"reason"`
}

// GetInvoice loads a received invoice.
type GetInvoice struct{ ID domain.InvoiceID }

// SearchInvoices searches the register of the caller's scope.
type SearchInvoices struct {
	Company, Supplier, From, To string
	Page, Size                  int
}

// LineDTO is the transport form of a line.
type LineDTO struct {
	No          int    `json:"no"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category"`
	Base        string `json:"base"`
	TaxCode     string `json:"taxCode,omitempty"`
	Treatment   string `json:"treatment,omitempty"`
}

// TaxDTO is the transport form of a line of the breakdown.
type TaxDTO struct {
	TaxCode   string `json:"taxCode,omitempty"`
	Treatment string `json:"treatment,omitempty"`
	Rate      string `json:"rate"`
	Base      string `json:"base"`
	Amount    string `json:"amount"`
}

// InvoiceDTO is the transport form of a received invoice.
type InvoiceDTO struct {
	ID              string       `json:"id"`
	Company         string       `json:"company"`
	Supplier        string       `json:"supplier"`
	SupplierNumber  string       `json:"supplierNumber"`
	Register        string       `json:"register"`
	Issued          string       `json:"issued"`
	Received        string       `json:"received"`
	Due             string       `json:"due"`
	Corrects        string       `json:"corrects,omitempty"`
	Lines           []LineDTO    `json:"lines"`
	Taxes           []TaxDTO     `json:"taxes"`
	Net             string       `json:"net"`
	Tax             string       `json:"tax"`
	Total           string       `json:"total"`
	NonDeductible   bool         `json:"nonDeductible"`
	WithholdingRate string       `json:"withholdingRate"`
	Withholding     string       `json:"withholding"`
	Payable         string       `json:"payable"`
	PayTo           []PayToInput `json:"payTo"`
	Cancelled       bool         `json:"cancelled"`
	CancelReason    string       `json:"cancelReason,omitempty"`
	Version         int64        `json:"version"`
}

func invoiceDTO(i *domain.Invoice) InvoiceDTO {
	s := i.State()
	d := InvoiceDTO{ID: i.ID().String(), Company: s.Company.String(), Supplier: s.Supplier.String(), SupplierNumber: s.SupplierNumber, Register: s.Register,
		Issued: s.Issued.String(), Received: s.Received.String(), Due: s.Due.String(), Lines: []LineDTO{}, Taxes: []TaxDTO{},
		Net: money(s.Breakdown.Net), Tax: money(s.Breakdown.Tax), Total: money(i.Total()), NonDeductible: s.NonDeductible,
		WithholdingRate: money(s.WithholdingRate), Withholding: money(s.Withholding), Payable: money(i.Payable()), PayTo: []PayToInput{},
		Cancelled: s.Cancelled, CancelReason: s.CancelReason, Version: i.Version()}
	if !s.Corrects.IsZero() {
		d.Corrects = s.Corrects.String()
	}
	for _, l := range s.Lines {
		d.Lines = append(d.Lines, LineDTO{No: l.No, Description: l.Description, Category: string(l.Category), Base: money(l.Base), TaxCode: l.TaxCode,
			Treatment: l.Treatment})
	}
	for _, t := range s.Breakdown.Lines {
		d.Taxes = append(d.Taxes, TaxDTO{TaxCode: t.TaxCode, Treatment: t.Treatment, Rate: money(t.Rate), Base: money(t.Base), Amount: money(t.Amount)})
	}
	for _, p := range s.PayTo {
		d.PayTo = append(d.PayTo, PayToInput{IBAN: p.IBAN.String(), Amount: money(p.Amount)})
	}
	return d
}

// counter takes the next number of the register of a company and year (same unit of work).
func (s service) counter(ctx context.Context, company domain.OrganizationID, year int) (int64, error) {
	cs, err := s.Counters.Find(ctx, spec.And(domain.CntFieldCompany.Eq(company), domain.CntFieldYear.Eq(year)))
	if err != nil {
		return 0, err
	}
	var c *domain.Counter
	if len(cs) > 0 {
		c = cs[0]
	} else if c, err = domain.ReconstituteCounter(domain.NewCounterID(), company, year, 0); err != nil {
		return 0, err
	}
	n := c.Next()
	return n, s.Counters.Save(ctx, c)
}

func (s service) invoiceUseCases(svc *Service) {
	svc.RegisterInvoice = guard(PermInvoiceRegister, func(ctx context.Context, c RegisterInvoice) (InvoiceDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		supplier := domain.PartyID{UUID: parseID(&v, "supplier", c.Supplier)}
		total := parseDecimal(&v, "total", c.Total)
		var corrects domain.InvoiceID
		if c.Corrects != "" {
			corrects = domain.InvoiceID{UUID: parseID(&v, "corrects", c.Corrects)}
		}
		if err := v.Err(); err != nil {
			return InvoiceDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return InvoiceDTO{}, err
		}
		profile := domain.SupplierState{}
		ps, err := s.Suppliers.Find(ctx, spec.And(domain.SupFieldCompany.Eq(company), domain.SupFieldSupplier.Eq(supplier)))
		if err != nil {
			return InvoiceDTO{}, err
		}
		if len(ps) > 0 {
			profile = ps[0].State()
		}
		if profile.Blocked {
			return InvoiceDTO{}, fw.Violation("purchases.supplier_blocked", "the supplier is blocked for new invoices")
		}

		d := domain.Draft{Company: company, Supplier: supplier, SupplierNumber: c.SupplierNumber, Issued: c.Issued, Received: c.Received, Due: c.Due,
			NonDeductible: c.NonDeductible, WithholdingRate: profile.WithholdingRate, DeclaredTotal: total, Corrects: corrects}
		if d.Due.IsZero() && !d.Issued.IsZero() {
			d.Due = d.Issued.AddDays(profile.PaymentDays)
		}
		if c.WithholdingRate != nil {
			d.WithholdingRate = parseDecimal(&v, "withholdingRate", *c.WithholdingRate)
		}
		var taxable []domain.TaxableLine
		for k, l := range c.Lines {
			cat := domain.Category(strings.TrimSpace(l.Category))
			if cat == "" {
				cat = profile.Category
			}
			line := domain.Line{No: k + 1, Description: strings.TrimSpace(l.Description), Category: cat, Base: parseDecimal(&v, "base", l.Base),
				TaxCode: strings.ToUpper(strings.TrimSpace(l.TaxCode)), Treatment: strings.TrimSpace(l.Treatment)}
			d.Lines = append(d.Lines, line)
			taxable = append(taxable, domain.TaxableLine{Ref: vocab.DecimalFromInt(int64(k + 1)).String(), Base: line.Base, TaxCode: line.TaxCode,
				Treatment: line.Treatment})
		}
		for _, p := range c.PayTo {
			iban, err := vocab.NewIBAN(p.IBAN)
			v.Require(err == nil, "payTo.iban", "format", "a valid IBAN")
			d.PayTo = append(d.PayTo, domain.PayTo{IBAN: iban, Amount: parseDecimal(&v, "payTo.amount", p.Amount)})
		}
		if err := v.Err(); err != nil {
			return InvoiceDTO{}, err
		}
		if !d.Corrects.IsZero() {
			orig, err := s.Invoices.Get(ctx, d.Corrects)
			if err != nil {
				return InvoiceDTO{}, err
			}
			if os := orig.State(); os.Company != company || os.Supplier != supplier || os.Cancelled {
				return InvoiceDTO{}, fw.Violation("purchases.corrects", "a corrective invoice rectifies a booked invoice of the same supplier")
			}
		}
		dup, err := s.Invoices.Exists(ctx, spec.And(domain.InvFieldCompany.Eq(company), domain.InvFieldSupplier.Eq(supplier),
			domain.InvFieldNumber.Eq(strings.TrimSpace(c.SupplierNumber)), domain.InvFieldCancelled.Eq(false)))
		if err != nil {
			return InvoiceDTO{}, err
		}
		if dup {
			return InvoiceDTO{}, fw.Violation("purchases.duplicate_invoice", "that supplier invoice is already booked")
		}
		var b domain.Breakdown
		if len(taxable) > 0 && !d.Issued.IsZero() {
			if b, err = s.Taxes.Calculate(ctx, company, d.Issued, taxable); err != nil {
				return InvoiceDTO{}, err
			}
		}
		// The supplier's account takes what is paid when none is given.
		if len(d.PayTo) == 0 && !profile.IBAN.IsZero() {
			withholding := b.Net.Mul(d.WithholdingRate).Div(vocab.DecimalFromInt(100)).Round(2)
			if payable := b.Net.Add(b.Tax).Sub(withholding); payable.IsPositive() {
				d.PayTo = []domain.PayTo{{IBAN: profile.IBAN, Amount: payable}}
			}
		}
		year := d.Received.Year()
		n, err := s.counter(ctx, company, year)
		if err != nil {
			return InvoiceDTO{}, err
		}
		inv, err := domain.Register(domain.NewInvoiceID(), d, b, year, n)
		if err != nil {
			return InvoiceDTO{}, err
		}
		if err := s.invoices.Create(ctx, inv); err != nil {
			return InvoiceDTO{}, err
		}
		return invoiceDTO(inv), nil
	}, retry[RegisterInvoice, InvoiceDTO](), pipeline.Transactional[RegisterInvoice, InvoiceDTO](s.UoW))

	svc.CancelInvoice = guard(PermInvoiceCancel, func(ctx context.Context, c CancelInvoice) (InvoiceDTO, error) {
		sc := scopeOf(ctx)
		inv, err := s.invoices.Update(ctx, c.ID, func(_ context.Context, i *domain.Invoice) error {
			if err := sc.check(domain.InvoiceKind, i.ID(), i.State().Company, true); err != nil {
				return err
			}
			return i.Cancel(c.Reason)
		})
		if err != nil {
			return InvoiceDTO{}, err
		}
		return invoiceDTO(inv), nil
	}, retry[CancelInvoice, InvoiceDTO]())

	svc.GetInvoice = guard(PermInvoiceRead, func(ctx context.Context, q GetInvoice) (InvoiceDTO, error) {
		i, err := s.Invoices.Get(ctx, q.ID)
		if err != nil {
			return InvoiceDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.InvoiceKind, i.ID(), i.State().Company, false); err != nil {
			return InvoiceDTO{}, err
		}
		return invoiceDTO(i), nil
	})

	svc.SearchInvoices = guard(PermInvoiceRead, func(ctx context.Context, q SearchInvoices) (fw.Page[InvoiceDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Invoice]{within(scopeOf(ctx), domain.InvFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.InvFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Supplier != "" {
			parts = append(parts, domain.InvFieldSupplier.Eq(domain.PartyID{UUID: parseID(&v, "supplier", q.Supplier)}))
		}
		if q.From != "" {
			d, err := vocab.ParseDate(q.From)
			v.Require(err == nil, "from", "format", "a date YYYY-MM-DD is required")
			parts = append(parts, domain.InvFieldReceived.Ge(d))
		}
		if q.To != "" {
			d, err := vocab.ParseDate(q.To)
			v.Require(err == nil, "to", "format", "a date YYYY-MM-DD is required")
			parts = append(parts, domain.InvFieldReceived.Le(d))
		}
		if err := v.Err(); err != nil {
			return fw.Page[InvoiceDTO]{}, err
		}
		page, err := s.Invoices.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.InvFieldRegister.Asc()))
		if err != nil {
			return fw.Page[InvoiceDTO]{}, err
		}
		return fw.MapPage(page, invoiceDTO), nil
	})
}
