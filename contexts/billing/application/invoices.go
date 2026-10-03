package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// DetailsInput are the editable header fields of a draft.
type DetailsInput struct {
	Description          string     `json:"description,omitempty"`
	OperationDate        vocab.Date `json:"operationDate,omitzero"`
	DueDate              vocab.Date `json:"dueDate,omitzero"`
	EquivalenceSurcharge bool       `json:"equivalenceSurcharge,omitempty"`
}

// DraftInvoice drafts an invoice: ordinary (seller and customer) or corrective (the invoice it
// corrects and a reason R1–R5; the customer is the original one).
type DraftInvoice struct {
	Seller   string `json:"seller"`
	Customer string `json:"customer,omitempty"`
	Corrects string `json:"corrects,omitempty"`
	Reason   string `json:"reason,omitempty"`
	DetailsInput
}

// AddLine adds a line to a draft.
type AddLine struct {
	ID          domain.InvoiceID `json:"-"`
	Description string           `json:"description"`
	Quantity    string           `json:"quantity"`
	UnitPrice   string           `json:"unitPrice"`
	Discount    string           `json:"discount,omitempty"`
	TaxCode     string           `json:"taxCode,omitempty"`
	Treatment   string           `json:"treatment,omitempty"`
}

// RemoveLine removes a line of a draft.
type RemoveLine struct {
	ID   domain.InvoiceID `json:"-"`
	Line string           `json:"line"`
}

// SetDetails replaces the editable header fields of a draft.
type SetDetails struct {
	ID domain.InvoiceID `json:"-"`
	DetailsInput
}

// PreviewTaxes calculates the tax breakdown a draft would have if issued on a date (today by
// default).
type PreviewTaxes struct {
	ID domain.InvoiceID
	On string
}

// IssueInvoice issues a draft in a series (the date defaults to today).
type IssueInvoice struct {
	ID     domain.InvoiceID `json:"-"`
	Series string           `json:"series"`
	Date   vocab.Date       `json:"date,omitzero"`
}

// DiscardInvoice deletes a draft.
type DiscardInvoice struct{ ID domain.InvoiceID }

// GetInvoice loads an invoice.
type GetInvoice struct{ ID domain.InvoiceID }

// SearchInvoices searches invoices of the caller's scope.
type SearchInvoices struct {
	Seller, Customer, Status string
	From, To                 string // issue date range
	Page, Size               int
}

// LineDTO is the transport form of a line.
type LineDTO struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unitPrice"`
	Discount    string `json:"discount,omitempty"`
	TaxCode     string `json:"taxCode,omitempty"`
	Treatment   string `json:"treatment,omitempty"`
	Net         string `json:"net"`
}

// TaxLineDTO is a line of the tax breakdown.
type TaxLineDTO struct {
	TaxType         string `json:"taxType,omitempty"`
	TaxCode         string `json:"taxCode,omitempty"`
	Treatment       string `json:"treatment,omitempty"`
	TreatmentKind   string `json:"treatmentKind"`
	Rate            string `json:"rate"`
	Base            string `json:"base"`
	Amount          string `json:"amount"`
	SurchargeRate   string `json:"surchargeRate,omitempty"`
	SurchargeAmount string `json:"surchargeAmount,omitempty"`
}

// BreakdownDTO is the transport form of a tax breakdown.
type BreakdownDTO struct {
	Country   string       `json:"country,omitempty"`
	Lines     []TaxLineDTO `json:"lines"`
	Net       string       `json:"net"`
	Tax       string       `json:"tax"`
	Surcharge string       `json:"surcharge"`
	Total     string       `json:"total"`
}

// InvoiceDTO is the transport form of an invoice.
type InvoiceDTO struct {
	ID                   string        `json:"id"`
	Number               string        `json:"number,omitempty"`
	Kind                 string        `json:"kind"`
	Status               string        `json:"status"`
	Seller               string        `json:"seller"`
	Customer             string        `json:"customer"`
	Corrects             string        `json:"corrects,omitempty"`
	Reason               string        `json:"reason,omitempty"`
	Currency             string        `json:"currency"`
	Description          string        `json:"description,omitempty"`
	IssueDate            string        `json:"issueDate,omitempty"`
	OperationDate        string        `json:"operationDate,omitempty"`
	DueDate              string        `json:"dueDate,omitempty"`
	EquivalenceSurcharge bool          `json:"equivalenceSurcharge"`
	SellerNIF            string        `json:"sellerNif,omitempty"`
	CustomerNIF          string        `json:"customerNif,omitempty"`
	CustomerName         string        `json:"customerName,omitempty"`
	Lines                []LineDTO     `json:"lines"`
	Net                  string        `json:"net"`
	Taxes                *BreakdownDTO `json:"taxes,omitempty"` // issued invoices
	Version              int64         `json:"version"`
	ModifiedBy           string        `json:"modifiedBy,omitempty"`
}

func breakdownDTO(b domain.Breakdown) BreakdownDTO {
	d := BreakdownDTO{Country: b.Country, Lines: []TaxLineDTO{}, Net: money(b.Net), Tax: money(b.Tax), Surcharge: money(b.Surcharge), Total: money(b.Total())}
	for _, l := range b.Lines {
		d.Lines = append(d.Lines, TaxLineDTO{TaxType: l.TaxType, TaxCode: l.TaxCode, Treatment: l.Treatment, TreatmentKind: l.TreatmentKind,
			Rate: money(l.Rate), Base: money(l.Base), Amount: money(l.Amount), SurchargeRate: optMoney(l.SurchargeRate), SurchargeAmount: optMoney(l.SurchargeAmount)})
	}
	return d
}

func invoiceDTO(i *domain.Invoice) InvoiceDTO {
	s := i.State()
	d := InvoiceDTO{ID: i.ID().String(), Number: s.Number, Kind: s.Kind.String(), Status: s.Status.String(), Seller: s.Seller.String(),
		Customer: s.Customer.String(), Reason: s.Reason, Currency: s.Currency.String(), Description: s.Description, IssueDate: dateText(s.IssueDate),
		OperationDate: dateText(s.OperationDate), DueDate: dateText(s.DueDate), EquivalenceSurcharge: s.EquivalenceSurcharge,
		SellerNIF: s.SellerIdentity.NIF, CustomerNIF: s.CustomerIdentity.NIF, CustomerName: s.CustomerIdentity.Name, Lines: []LineDTO{},
		Net: money(i.Net()), Version: i.Version(), ModifiedBy: i.ModifiedBy().Name}
	if !s.Corrects.IsZero() {
		d.Corrects = s.Corrects.String()
	}
	for _, l := range s.Lines {
		d.Lines = append(d.Lines, LineDTO{ID: l.ID.String(), Description: l.Description, Quantity: l.Quantity.String(), UnitPrice: l.UnitPrice.String(),
			Discount: optMoney(l.Discount), TaxCode: l.TaxCode, Treatment: l.Treatment, Net: money(l.Net)})
	}
	if s.Status == domain.Issued {
		b := breakdownDTO(s.Taxes)
		d.Taxes = &b
	}
	return d
}

func (s service) invoiceUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.InvoiceID, fn func(context.Context, *domain.Invoice) error) (InvoiceDTO, error) {
		sc := scopeOf(ctx)
		i, err := s.invoices.Update(ctx, id, func(ctx context.Context, i *domain.Invoice) error {
			if err := sc.check(domain.InvoiceKind, i.ID(), i.State().Seller, true); err != nil {
				return err
			}
			return fn(ctx, i)
		})
		if err != nil {
			return InvoiceDTO{}, err
		}
		return invoiceDTO(i), nil
	}

	svc.DraftInvoice = guard(PermInvoiceCreate, func(ctx context.Context, c DraftInvoice) (InvoiceDTO, error) {
		var v fw.Validation
		seller := domain.OrganizationID{UUID: parseID(&v, "seller", c.Seller)}
		st := domain.InvoiceState{Seller: seller, Kind: domain.Ordinary, Description: c.Description, OperationDate: c.OperationDate,
			DueDate: c.DueDate, EquivalenceSurcharge: c.EquivalenceSurcharge}
		if c.Corrects != "" {
			st.Kind, st.Corrects, st.Reason = domain.Corrective, domain.InvoiceID{UUID: parseID(&v, "corrects", c.Corrects)}, upper(c.Reason)
		} else {
			st.Customer = domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}
		}
		if err := v.Err(); err != nil {
			return InvoiceDTO{}, err
		}
		sc := scopeOf(ctx)
		if err := sc.check("parties.party", seller, seller, true); err != nil {
			return InvoiceDTO{}, err
		}
		if st.Kind == domain.Corrective {
			orig, err := s.Invoices.Get(ctx, st.Corrects)
			if err != nil {
				return InvoiceDTO{}, err
			}
			os := orig.State()
			if err := sc.check(domain.InvoiceKind, orig.ID(), os.Seller, false); err != nil {
				return InvoiceDTO{}, err
			}
			if os.Seller != seller {
				return InvoiceDTO{}, fw.Violation("billing.correction_other_seller", "an invoice is corrected by its own seller")
			}
			if os.Status != domain.Issued {
				return InvoiceDTO{}, fw.Violation("billing.correcting_draft", "only an issued invoice is corrected; a draft is edited")
			}
			st.Customer, st.EquivalenceSurcharge = os.Customer, os.EquivalenceSurcharge
		}
		inv, err := domain.DraftInvoice(domain.NewInvoiceID(), st)
		if err != nil {
			return InvoiceDTO{}, err
		}
		if err := s.invoices.Create(ctx, inv); err != nil {
			return InvoiceDTO{}, err
		}
		return invoiceDTO(inv), nil
	}, pipeline.Transactional[DraftInvoice, InvoiceDTO](s.UoW))

	svc.AddLine = guard(PermInvoiceUpdate, func(ctx context.Context, c AddLine) (InvoiceDTO, error) {
		var v fw.Validation
		in := domain.LineInput{Description: c.Description, Quantity: parseDecimal(&v, "quantity", c.Quantity),
			UnitPrice: parseDecimal(&v, "unitPrice", c.UnitPrice), Discount: parseDecimal(&v, "discount", c.Discount), TaxCode: c.TaxCode, Treatment: c.Treatment}
		if err := v.Err(); err != nil {
			return InvoiceDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, i *domain.Invoice) error { _, err := i.AddLine(in); return err })
	}, retry[AddLine, InvoiceDTO]())

	svc.RemoveLine = guard(PermInvoiceUpdate, func(ctx context.Context, c RemoveLine) (InvoiceDTO, error) {
		var v fw.Validation
		id := domain.LineID{UUID: parseID(&v, "line", c.Line)}
		if err := v.Err(); err != nil {
			return InvoiceDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, i *domain.Invoice) error { return i.RemoveLine(id) })
	}, retry[RemoveLine, InvoiceDTO]())

	svc.SetDetails = guard(PermInvoiceUpdate, func(ctx context.Context, c SetDetails) (InvoiceDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, i *domain.Invoice) error {
			return i.SetDetails(domain.Details{Description: c.Description, OperationDate: c.OperationDate, DueDate: c.DueDate,
				EquivalenceSurcharge: c.EquivalenceSurcharge})
		})
	}, retry[SetDetails, InvoiceDTO]())

	svc.PreviewTaxes = guard(PermInvoiceRead, func(ctx context.Context, q PreviewTaxes) (BreakdownDTO, error) {
		on := vocab.DateOf(fw.Now())
		if q.On != "" {
			d, err := vocab.ParseDate(q.On)
			if err != nil {
				var v fw.Validation
				v.Add("on", "format", "a date YYYY-MM-DD is required")
				return BreakdownDTO{}, v.Err()
			}
			on = d
		}
		i, err := s.Invoices.Get(ctx, q.ID)
		if err != nil {
			return BreakdownDTO{}, err
		}
		st := i.State()
		if err := scopeOf(ctx).check(domain.InvoiceKind, i.ID(), st.Seller, false); err != nil {
			return BreakdownDTO{}, err
		}
		if st.Status == domain.Issued {
			return breakdownDTO(st.Taxes), nil
		}
		b, err := s.Taxes.Calculate(ctx, st.Seller, i.AccrualDate(on), st.EquivalenceSurcharge, i.TaxableLines())
		if err != nil {
			return BreakdownDTO{}, err
		}
		return breakdownDTO(b), nil
	})

	svc.Issue = guard(PermInvoiceIssue, func(ctx context.Context, c IssueInvoice) (InvoiceDTO, error) {
		var v fw.Validation
		seriesID := domain.SeriesID{UUID: parseID(&v, "series", c.Series)}
		if err := v.Err(); err != nil {
			return InvoiceDTO{}, err
		}
		date := c.Date
		if date.IsZero() {
			date = vocab.DateOf(fw.Now())
		}
		return update(ctx, c.ID, func(ctx context.Context, i *domain.Invoice) error {
			st := i.State()
			if st.Status != domain.Draft {
				return i.Issue(domain.Issuance{}) // reports that it is not a draft
			}
			ids, err := s.Identities.Identities(ctx, []domain.PartyID{{UUID: st.Seller.UUID}, st.Customer})
			if err != nil {
				return err
			}
			taxes, err := s.Taxes.Calculate(ctx, st.Seller, i.AccrualDate(date), st.EquivalenceSurcharge, i.TaxableLines())
			if err != nil {
				return err
			}
			// The number is taken in the same unit of work: if anything fails, nothing is consumed.
			var number string
			if _, err := s.series.Update(ctx, seriesID, func(_ context.Context, sr *domain.Series) error {
				n, err := sr.Take(st.Seller, date.Year(), st.Kind == domain.Corrective)
				number = n
				return err
			}); err != nil {
				return err
			}
			return i.Issue(domain.Issuance{Series: seriesID, Number: number, Date: date, Seller: ids[domain.PartyID{UUID: st.Seller.UUID}],
				Customer: ids[st.Customer], Taxes: taxes})
		})
	}, retry[IssueInvoice, InvoiceDTO](), pipeline.Transactional[IssueInvoice, InvoiceDTO](s.UoW))

	svc.Discard = guard(PermInvoiceUpdate, func(ctx context.Context, c DiscardInvoice) (struct{}, error) {
		sc := scopeOf(ctx)
		return struct{}{}, s.invoices.Delete(ctx, c.ID, func(_ context.Context, i *domain.Invoice) error {
			if err := sc.check(domain.InvoiceKind, i.ID(), i.State().Seller, true); err != nil {
				return err
			}
			return i.Discard()
		})
	})

	svc.GetInvoice = guard(PermInvoiceRead, func(ctx context.Context, q GetInvoice) (InvoiceDTO, error) {
		i, err := s.Invoices.Get(ctx, q.ID)
		if err != nil {
			return InvoiceDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.InvoiceKind, i.ID(), i.State().Seller, false); err != nil {
			return InvoiceDTO{}, err
		}
		return invoiceDTO(i), nil
	})

	svc.SearchInvoices = guard(PermInvoiceRead, func(ctx context.Context, q SearchInvoices) (fw.Page[InvoiceDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Invoice]{within(scopeOf(ctx), domain.InvFieldSeller)}
		if q.Seller != "" {
			parts = append(parts, domain.InvFieldSeller.Eq(domain.OrganizationID{UUID: parseID(&v, "seller", q.Seller)}))
		}
		if q.Customer != "" {
			parts = append(parts, domain.InvFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", q.Customer)}))
		}
		if q.Status != "" {
			st, ok := domain.ParseStatus(q.Status)
			v.Require(ok, "status", "enum", "draft or issued")
			parts = append(parts, domain.InvFieldStatus.Eq(int(st)))
		}
		for field, s := range map[string]string{"from": q.From, "to": q.To} {
			if s == "" {
				continue
			}
			d, err := vocab.ParseDate(s)
			v.Require(err == nil, field, "format", "a date YYYY-MM-DD is required")
			if field == "from" {
				parts = append(parts, domain.InvFieldIssued.Ge(d))
			} else {
				parts = append(parts, domain.InvFieldIssued.Le(d))
			}
		}
		if err := v.Err(); err != nil {
			return fw.Page[InvoiceDTO]{}, err
		}
		page, err := s.Invoices.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.InvFieldNumber.Asc()))
		if err != nil {
			return fw.Page[InvoiceDTO]{}, err
		}
		return fw.MapPage(page, invoiceDTO), nil
	})
}
