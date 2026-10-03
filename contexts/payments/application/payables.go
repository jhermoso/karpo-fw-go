package application

import (
	"context"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PayToInput is a bank account and how much of the payable is paid to it.
type PayToInput struct {
	IBAN   string `json:"iban"`
	Amount string `json:"amount"`
}

// SetPayTo replaces the accounts a payable is paid to.
type SetPayTo struct {
	ID    domain.PayableID `json:"-"`
	PayTo []PayToInput     `json:"payTo"`
}

// CancelPayable withdraws an unpaid payable.
type CancelPayable struct {
	ID     domain.PayableID `json:"-"`
	Reason string           `json:"reason"`
}

// GetPayable loads a payable.
type GetPayable struct{ ID domain.PayableID }

// SearchPayables searches payables of the caller's scope.
type SearchPayables struct {
	Company, Payee, Kind, DueTo string
	OpenOnly                    bool
	Page, Size                  int
}

// PayToDTO is the transport form of an account.
type PayToDTO struct {
	IBAN   string `json:"iban"`
	Amount string `json:"amount"`
}

// PayableDTO is the transport form of a payable.
type PayableDTO struct {
	ID          string     `json:"id"`
	Company     string     `json:"company"`
	Payee       string     `json:"payee"` // party id or tax authority
	Kind        string     `json:"kind"`
	Source      string     `json:"source"`
	SourceID    string     `json:"sourceId"`
	Document    string     `json:"document"`
	Description string     `json:"description,omitempty"`
	Issued      string     `json:"issued"`
	Due         string     `json:"due"`
	Amount      string     `json:"amount"`
	Paid        string     `json:"paid"`
	Open        string     `json:"open"`
	Settled     bool       `json:"settled"`
	Cancelled   bool       `json:"cancelled"`
	PayTo       []PayToDTO `json:"payTo"`
	Version     int64      `json:"version"`
}

func payableDTO(p *domain.Payable) PayableDTO {
	s := p.State()
	d := PayableDTO{ID: p.ID().String(), Company: s.Company.String(), Payee: p.Payee(), Kind: s.Kind.String(), Source: s.Source.Type, SourceID: s.Source.ID,
		Document: s.Document, Description: s.Description, Issued: s.Issued.String(), Due: s.Due.String(), Amount: money(s.Amount), Paid: money(s.Paid),
		Open: money(p.Open()), Settled: p.Settled(), Cancelled: s.Cancelled, PayTo: []PayToDTO{}, Version: p.Version()}
	for _, t := range s.PayTo {
		d.PayTo = append(d.PayTo, PayToDTO{IBAN: t.IBAN.String(), Amount: money(t.Amount)})
	}
	return d
}

func payTo(v *fw.Validation, in []PayToInput) []domain.PayTo {
	var out []domain.PayTo
	for _, t := range in {
		iban, err := vocab.NewIBAN(t.IBAN)
		v.Require(err == nil, "payTo.iban", "format", "a valid IBAN")
		out = append(out, domain.PayTo{IBAN: iban, Amount: parseDecimal(v, "payTo.amount", t.Amount)})
	}
	return out
}

func (s service) payableUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.PayableID, fn func(*domain.Payable) error) (PayableDTO, error) {
		sc := scopeOf(ctx)
		p, err := s.payables.Update(ctx, id, func(_ context.Context, p *domain.Payable) error {
			if err := sc.check(domain.PayableKind, p.ID(), p.State().Company, true); err != nil {
				return err
			}
			return fn(p)
		})
		if err != nil {
			return PayableDTO{}, err
		}
		return payableDTO(p), nil
	}
	svc.SetPayTo = guard(PermPayableWrite, func(ctx context.Context, c SetPayTo) (PayableDTO, error) {
		var v fw.Validation
		to := payTo(&v, c.PayTo)
		if err := v.Err(); err != nil {
			return PayableDTO{}, err
		}
		return update(ctx, c.ID, func(p *domain.Payable) error { return p.SetPayTo(to) })
	}, retry[SetPayTo, PayableDTO]())
	svc.CancelPayable = guard(PermPayableWrite, func(ctx context.Context, c CancelPayable) (PayableDTO, error) {
		return update(ctx, c.ID, func(p *domain.Payable) error { return p.Cancel(c.Reason) })
	}, retry[CancelPayable, PayableDTO]())

	svc.GetPayable = guard(PermPayableRead, func(ctx context.Context, q GetPayable) (PayableDTO, error) {
		p, err := s.Payables.Get(ctx, q.ID)
		if err != nil {
			return PayableDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.PayableKind, p.ID(), p.State().Company, false); err != nil {
			return PayableDTO{}, err
		}
		return payableDTO(p), nil
	})
	svc.SearchPayables = guard(PermPayableRead, func(ctx context.Context, q SearchPayables) (fw.Page[PayableDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Payable]{within(scopeOf(ctx), domain.PayFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.PayFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Payee != "" {
			parts = append(parts, domain.PayFieldPayee.Eq(domain.PartyID{UUID: parseID(&v, "payee", q.Payee)}))
		}
		if q.Kind != "" {
			k, ok := domain.ParseKind(strings.TrimSpace(q.Kind))
			v.Require(ok, "kind", "enum", "supplier-invoice, payroll or tax")
			parts = append(parts, domain.PayFieldKind.Eq(k.String()))
		}
		if q.DueTo != "" {
			to, err := vocab.ParseDate(q.DueTo)
			v.Require(err == nil, "dueTo", "format", "a date YYYY-MM-DD is required")
			parts = append(parts, domain.PayFieldDue.Le(to))
		}
		if q.OpenOnly {
			parts = append(parts, domain.PayFieldSettled.Eq(false), domain.PayFieldCancelled.Eq(false))
		}
		if err := v.Err(); err != nil {
			return fw.Page[PayableDTO]{}, err
		}
		page, err := s.Payables.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.PayFieldDue.Asc()))
		if err != nil {
			return fw.Page[PayableDTO]{}, err
		}
		return fw.MapPage(page, payableDTO), nil
	})
}
