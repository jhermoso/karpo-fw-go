package application

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// DetailsInput son los campos editables de la cabecera de un borrador.
type DetailsInput struct {
	Description          string     `json:"description,omitempty"`
	OperationDate        vocab.Date `json:"operationDate,omitzero"`
	DueDate              vocab.Date `json:"dueDate,omitzero"`
	EquivalenceSurcharge bool       `json:"equivalenceSurcharge,omitempty"`
}

// DraftInvoice prepara el borrador de una factura: ordinaria (vendedor y cliente) o rectificativa
// (la factura que rectifica y un motivo R1–R5; el cliente es el de la original).
type DraftInvoice struct {
	Seller   string `json:"seller"`
	Customer string `json:"customer,omitempty"`
	Corrects string `json:"corrects,omitempty"`
	Reason   string `json:"reason,omitempty"`
	DetailsInput
}

// AddLine añade una línea a un borrador.
type AddLine struct {
	ID          domain.InvoiceID `json:"-"`
	Description string           `json:"description"`
	Quantity    string           `json:"quantity"`
	UnitPrice   string           `json:"unitPrice"`
	Discount    string           `json:"discount,omitempty"`
	TaxCode     string           `json:"taxCode,omitempty"`
	Treatment   string           `json:"treatment,omitempty"`
}

// RemoveLine quita una línea de un borrador.
type RemoveLine struct {
	ID   domain.InvoiceID `json:"-"`
	Line string           `json:"line"`
}

// SetDetails sustituye los campos editables de la cabecera de un borrador.
type SetDetails struct {
	ID domain.InvoiceID `json:"-"`
	DetailsInput
}

// PreviewTaxes calcula el desglose de impuestos que tendría un borrador si se emitiera en una fecha
// (hoy, por omisión).
type PreviewTaxes struct {
	ID domain.InvoiceID
	On string
}

// IssueInvoice emite un borrador en una serie (la fecha, por omisión, es hoy).
type IssueInvoice struct {
	ID     domain.InvoiceID `json:"-"`
	Series string           `json:"series"`
	Date   vocab.Date       `json:"date,omitzero"`
}

// DiscardInvoice borra un borrador.
type DiscardInvoice struct{ ID domain.InvoiceID }

// GetInvoice carga una factura.
type GetInvoice struct{ ID domain.InvoiceID }

// SearchInvoices busca facturas del ámbito de quien llama.
type SearchInvoices struct {
	Seller, Customer, Status string
	From, To                 string // rango de fechas de emisión
	IncludeMergedParties     bool   // también las de clientes fusionados en otra party (D-100)
	Page, Size               int
}

// LineDTO es la forma de transporte de una línea.
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

// TaxLineDTO es una línea del desglose de impuestos.
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

// BreakdownDTO es la forma de transporte de un desglose de impuestos.
type BreakdownDTO struct {
	Country   string       `json:"country,omitempty"`
	Lines     []TaxLineDTO `json:"lines"`
	Net       string       `json:"net"`
	Tax       string       `json:"tax"`
	Surcharge string       `json:"surcharge"`
	Total     string       `json:"total"`
}

// today es la fecha de hoy en la zona horaria de la empresa vendedora (Zones); en UTC sin Zones.
func (s *Deps) today(ctx context.Context, seller domain.PartyID, country string) (vocab.Date, error) {
	if s.Zones == nil {
		return vocab.Today(nil), nil
	}
	loc, err := s.Zones.Location(ctx, seller.String(), country)
	if err != nil {
		return vocab.Date{}, err
	}
	return vocab.Today(loc), nil
}

// InvoiceDTO es la forma de transporte de una factura.
type InvoiceDTO struct {
	ID                   string               `json:"id"`
	Number               string               `json:"number,omitempty"`
	Kind                 string               `json:"kind"`
	Status               string               `json:"status"`
	Seller               string               `json:"seller"`
	Customer             string               `json:"customer"`
	Corrects             string               `json:"corrects,omitempty"`
	Reason               string               `json:"reason,omitempty"`
	Currency             string               `json:"currency"`
	Description          string               `json:"description,omitempty"`
	IssueDate            string               `json:"issueDate,omitempty"`
	OperationDate        string               `json:"operationDate,omitempty"`
	DueDate              string               `json:"dueDate,omitempty"`
	EquivalenceSurcharge bool                 `json:"equivalenceSurcharge"`
	SellerNIF            string               `json:"sellerNif,omitempty"`
	CustomerNIF          string               `json:"customerNif,omitempty"`
	CustomerName         string               `json:"customerName,omitempty"`
	SellerAddress        *contracts.AddressV1 `json:"sellerAddress,omitempty"`
	CustomerAddress      *contracts.AddressV1 `json:"customerAddress,omitempty"`
	Lines                []LineDTO            `json:"lines"`
	Net                  string               `json:"net"`
	Taxes                *BreakdownDTO        `json:"taxes,omitempty"` // facturas emitidas
	// SourceType, SourceID y SourceRef son el primer origen; Sources, todos (D-146).
	SourceType string               `json:"sourceType,omitempty"`
	SourceID   string               `json:"sourceId,omitempty"`
	SourceRef  string               `json:"sourceRef,omitempty"`
	Sources    []contracts.SourceV1 `json:"sources"`
	// LineRounding y PriceDecimals son cómo se calculan las líneas: los ajustes de la empresa al
	// preparar el borrador, que se quedan en la factura (D-144).
	LineRounding  string `json:"lineRounding"`
	PriceDecimals int    `json:"priceDecimals"`
	Version       int64  `json:"version"`
	ModifiedBy    string `json:"modifiedBy,omitempty"`
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
		SellerAddress: addressV1(s.SellerIdentity.Address), CustomerAddress: addressV1(s.CustomerIdentity.Address),
		Net: money(i.Net()), Version: i.Version(), ModifiedBy: i.ModifiedBy().Name, Sources: []contracts.SourceV1{},
		LineRounding: string(s.Calculation.Rounding), PriceDecimals: s.Calculation.PriceDecimals}
	for k, src := range s.Sources {
		if k == 0 {
			d.SourceType, d.SourceID, d.SourceRef = src.Type, src.ID, src.Ref
		}
		d.Sources = append(d.Sources, contracts.SourceV1{Type: src.Type, ID: src.ID, Ref: src.Ref})
	}
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

// dispatchIssued entrega billing.invoice-issued.v1 de una factura recién emitida a InTransaction,
// con el contexto (y la unidad de trabajo) de la emisión.
func (s service) dispatchIssued(ctx context.Context, i *domain.Invoice) error {
	id := i.ID().String()
	e := issuedV1(id, i.State())
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return s.InTransaction.HandleMessage(ctx, app.Envelope{ID: fw.NewUUID().String(), Type: e.IntegrationEventType(), Source: contracts.Source,
		Subject: id, OccurredAt: fw.Now().UTC(), CorrelationID: app.CorrelationID(ctx), Data: data})
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
			// La rectificativa se calcula como la original, aunque la empresa haya cambiado sus
			// ajustes: así anula sus importes y admite sus precios (D-144).
			st.Customer, st.EquivalenceSurcharge, st.Calculation = os.Customer, os.EquivalenceSurcharge, os.Calculation
			if strings.TrimSpace(st.Description) == "" {
				st.Description = "Rectificación de la factura " + os.Number
			}
		}
		if st.Kind == domain.Ordinary {
			if err := s.notBlocked(ctx, st.Customer); err != nil {
				return InvoiceDTO{}, err
			}
		}
		if st.Kind == domain.Ordinary {
			settings, err := s.settings(ctx, seller)
			if err != nil {
				return InvoiceDTO{}, err
			}
			st.Calculation = settings.Calculation
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
		var on vocab.Date
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
		if on.IsZero() { // hoy en la zona del vendedor, como al emitir sin fecha
			ids, err := s.Identities.Identities(ctx, []domain.PartyID{{UUID: st.Seller.UUID}})
			if err != nil {
				return BreakdownDTO{}, err
			}
			if on, err = s.today(ctx, domain.PartyID{UUID: st.Seller.UUID}, ids[domain.PartyID{UUID: st.Seller.UUID}].Country); err != nil {
				return BreakdownDTO{}, err
			}
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
		var issued *domain.Invoice
		var withIssue bool
		out, err := update(ctx, c.ID, func(ctx context.Context, i *domain.Invoice) error {
			st := i.State()
			if st.Status != domain.Draft {
				return i.Issue(domain.Issuance{}) // informa de que no es un borrador
			}
			settings, err := s.settings(ctx, st.Seller)
			if err != nil {
				return err
			}
			issued, withIssue = i, settings.InstallmentsWithIssue
			if st.Kind == domain.Ordinary {
				if err := s.notBlocked(ctx, st.Customer); err != nil {
					return err
				}
			}
			ids, err := s.Identities.Identities(ctx, []domain.PartyID{{UUID: st.Seller.UUID}, st.Customer})
			if err != nil {
				return err
			}
			// El nombre y el NIF, los de Parties hoy. El domicilio de una rectificativa, el congelado
			// en la factura que rectifica; si aquélla no lo tenía (anterior a D-147), el de hoy.
			seller, customer := ids[domain.PartyID{UUID: st.Seller.UUID}], ids[st.Customer]
			if st.Kind == domain.Corrective {
				orig, err := s.Invoices.Get(ctx, st.Corrects)
				if err != nil {
					return err
				}
				os := orig.State()
				if os.SellerIdentity.Address.Complete() {
					seller.Address = os.SellerIdentity.Address
				}
				if os.CustomerIdentity.Address.Complete() {
					customer.Address = os.CustomerIdentity.Address
				}
			}
			// Antes de tomar número: así la serie se bloquea menos tiempo. Issue lo vuelve a comprobar.
			if err := domain.RequireAddresses(st.Kind, seller, customer); err != nil {
				return err
			}
			date := c.Date
			if date.IsZero() { // hoy en la zona horaria del vendedor
				if date, err = s.today(ctx, domain.PartyID{UUID: st.Seller.UUID}, ids[domain.PartyID{UUID: st.Seller.UUID}].Country); err != nil {
					return err
				}
			}
			taxes, err := s.Taxes.Calculate(ctx, st.Seller, i.AccrualDate(date), st.EquivalenceSurcharge, i.TaxableLines())
			if err != nil {
				return err
			}
			// El número se toma en la misma unidad de trabajo: si algo falla, no se consume nada.
			var number string
			if _, err := s.series.Update(ctx, seriesID, func(_ context.Context, sr *domain.Series) error {
				n, err := sr.Take(st.Seller, date, st.Kind == domain.Corrective)
				number = n
				return err
			}); err != nil {
				return err
			}
			return i.Issue(domain.Issuance{Series: seriesID, Number: number, Date: date, Seller: seller,
				Customer: customer, Taxes: taxes})
		})
		if err != nil || !withIssue || s.InTransaction == nil {
			return out, err
		}
		// Los vencimientos nacen con la factura (with_issue, D-130): Receivables recibe el mismo
		// hecho que publicará el outbox, en esta misma transacción; si falla, no se emite nada. Cuando
		// el outbox lo entregue después, Receivables ya tiene el cobro y no hace nada.
		return out, s.dispatchIssued(ctx, issued)
	}, issueRetry[IssueInvoice, InvoiceDTO](), pipeline.Transactional[IssueInvoice, InvoiceDTO](s.UoW))

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
		merged, err := withoutMerged(ctx, s.Merged, q.IncludeMergedParties || q.Customer != "", domain.InvFieldCustomer)
		if err != nil {
			return fw.Page[InvoiceDTO]{}, err
		}
		parts = append(parts, merged...)
		page, err := s.Invoices.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.InvFieldNumber.Asc()))
		if err != nil {
			return fw.Page[InvoiceDTO]{}, err
		}
		return fw.MapPage(page, invoiceDTO), nil
	})
}
