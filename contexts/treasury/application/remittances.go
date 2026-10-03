package application

import (
	"context"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// ProposeRemittance drafts a remittance with the open installments of the creditor due up to a
// date whose customers have a usable mandate of the scheme.
type ProposeRemittance struct {
	Creditor       string     `json:"creditor"`
	Account        string     `json:"account"`
	Scheme         string     `json:"scheme"`
	CollectionDate vocab.Date `json:"collectionDate"`
	DueTo          vocab.Date `json:"dueTo"`
}

// RemoveItem removes an installment from a draft.
type RemoveItem struct {
	ID          domain.RemittanceID `json:"-"`
	Invoice     string              `json:"invoice"`
	Installment int                 `json:"installment"`
}

// GenerateRemittance freezes a draft and records the use of its mandates.
type GenerateRemittance struct {
	ID domain.RemittanceID `json:"-"`
}

// GetFile returns the pain.008 file of a generated remittance.
type GetFile struct{ ID domain.RemittanceID }

// SettleRemittance records that the bank charged the remittance.
type SettleRemittance struct {
	ID domain.RemittanceID `json:"-"`
	On vocab.Date          `json:"on"`
}

// ReturnDebit records a returned direct debit.
type ReturnDebit struct {
	ID       domain.RemittanceID `json:"-"`
	EndToEnd string              `json:"endToEnd"`
	On       vocab.Date          `json:"on"`
	Reason   string              `json:"reason"`
}

// CancelRemittance cancels a remittance not settled.
type CancelRemittance struct {
	ID domain.RemittanceID `json:"-"`
}

// GetRemittance loads a remittance.
type GetRemittance struct{ ID domain.RemittanceID }

// SearchRemittances searches remittances of the caller's scope.
type SearchRemittances struct {
	Creditor   string
	Page, Size int
}

// ItemDTO is a direct debit of a remittance.
type ItemDTO struct {
	EndToEnd    string `json:"endToEnd"`
	Invoice     string `json:"invoice"`
	Number      string `json:"number"`
	Installment int    `json:"installment"`
	Debtor      string `json:"debtor"`
	DebtorName  string `json:"debtorName"`
	Mandate     string `json:"mandate"`
	IBAN        string `json:"iban"`
	Amount      string `json:"amount"`
	Sequence    string `json:"sequence,omitempty"`
	Returned    string `json:"returned,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// RemittanceDTO is the transport form of a remittance.
type RemittanceDTO struct {
	ID             string    `json:"id"`
	Creditor       string    `json:"creditor"`
	Account        string    `json:"account"`
	Scheme         string    `json:"scheme"`
	CollectionDate string    `json:"collectionDate"`
	Status         string    `json:"status"`
	Total          string    `json:"total"`
	CreditorID     string    `json:"creditorId,omitempty"`
	Settled        string    `json:"settled,omitempty"`
	Items          []ItemDTO `json:"items"`
	WithoutMandate int       `json:"withoutMandate,omitempty"` // installments left out of a proposal
	Version        int64     `json:"version"`
}

func remittanceDTO(r *domain.Remittance) RemittanceDTO {
	s := r.State()
	d := RemittanceDTO{ID: r.ID().String(), Creditor: s.Creditor.String(), Account: s.Account.String(), Scheme: s.Scheme.String(),
		CollectionDate: s.CollectionDate.String(), Status: s.Status.String(), Total: r.Total().StringFixed(2), CreditorID: s.CreditorID,
		Settled: dateText(s.Settled), Items: []ItemDTO{}, Version: r.Version()}
	for _, i := range s.Items {
		d.Items = append(d.Items, ItemDTO{EndToEnd: i.EndToEnd, Invoice: i.Invoice.String(), Number: i.Number, Installment: i.Installment,
			Debtor: i.Debtor.String(), DebtorName: i.DebtorName, Mandate: i.MandateRef, IBAN: i.IBAN.String(), Amount: i.Amount.StringFixed(2),
			Sequence: i.Sequence, Returned: dateText(i.Returned), Reason: i.Reason})
	}
	return d
}

// inRemittances returns the installments already in remittances not cancelled (and not returned).
func (s service) inRemittances(ctx context.Context, creditor domain.OrganizationID) (map[domain.InvoiceID][]int, error) {
	rs, err := s.Remittances.Find(ctx, spec.And(domain.RemFieldCreditor.Eq(creditor), domain.RemFieldStatus.Ne(int(domain.RemCancelled))))
	if err != nil {
		return nil, err
	}
	out := map[domain.InvoiceID][]int{}
	for _, r := range rs {
		for _, i := range r.State().Items {
			if i.Returned.IsZero() {
				out[i.Invoice] = append(out[i.Invoice], i.Installment)
			}
		}
	}
	return out, nil
}

func (s service) remittanceUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.RemittanceID, fn func(context.Context, *domain.Remittance) error) (RemittanceDTO, error) {
		sc := scopeOf(ctx)
		r, err := s.remittances.Update(ctx, id, func(ctx context.Context, r *domain.Remittance) error {
			if err := sc.check(domain.RemittanceKind, r.ID(), r.State().Creditor, true); err != nil {
				return err
			}
			return fn(ctx, r)
		})
		if err != nil {
			return RemittanceDTO{}, err
		}
		return remittanceDTO(r), nil
	}

	svc.Propose = guard(PermRemittanceEdit, func(ctx context.Context, c ProposeRemittance) (RemittanceDTO, error) {
		var v fw.Validation
		creditor := domain.OrganizationID{UUID: parseID(&v, "creditor", c.Creditor)}
		accountID := domain.AccountID{UUID: parseID(&v, "account", c.Account)}
		scheme, ok := domain.ParseScheme(c.Scheme)
		v.Require(ok, "scheme", "enum", "CORE or B2B")
		v.Require(!c.CollectionDate.IsZero() && !c.DueTo.IsZero(), "collectionDate", "required", "collection and due dates are required")
		if err := v.Err(); err != nil {
			return RemittanceDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", creditor, creditor, true); err != nil {
			return RemittanceDTO{}, err
		}
		acc, err := s.Accounts.Get(ctx, accountID)
		if err != nil {
			return RemittanceDTO{}, err
		}
		if acc.State().Owner != creditor {
			return RemittanceDTO{}, fw.NotFound(domain.AccountKind, accountID)
		}
		if !acc.State().Collections || !acc.OpenOn(c.CollectionDate) {
			return RemittanceDTO{}, fw.Violation("treasury.account_not_for_collections", "the account is not open for collections on that date")
		}
		due, err := s.Receivables.DueItems(ctx, creditor, c.DueTo)
		if err != nil {
			return RemittanceDTO{}, err
		}
		taken, err := s.inRemittances(ctx, creditor)
		if err != nil {
			return RemittanceDTO{}, err
		}
		ms, err := s.Mandates.Find(ctx, domain.ManFieldCreditor.Eq(creditor))
		if err != nil {
			return RemittanceDTO{}, err
		}
		mandateOf := map[domain.PartyID]*domain.Mandate{}
		for _, m := range ms {
			st := m.State()
			if st.Scheme != scheme || !m.UsableOn(c.CollectionDate) {
				continue
			}
			if cur, ok := mandateOf[st.Debtor]; !ok || st.Signed.After(cur.State().Signed) {
				mandateOf[st.Debtor] = m
			}
		}
		var debtors []domain.PartyID
		for _, d := range due {
			debtors = append(debtors, d.Customer)
		}
		names, err := s.Identities.Identities(ctx, debtors)
		if err != nil {
			return RemittanceDTO{}, err
		}
		r, err := domain.DraftRemittance(domain.NewRemittanceID(), creditor, accountID, scheme, c.CollectionDate)
		if err != nil {
			return RemittanceDTO{}, err
		}
		without := 0
		for _, d := range due {
			m, ok := mandateOf[d.Customer]
			if !ok {
				without++
				continue
			}
			if slices.Contains(taken[d.Invoice], d.Installment) {
				continue
			}
			ms := m.State()
			if err := r.Add(domain.Item{Invoice: d.Invoice, Number: d.Number, Installment: d.Installment, Debtor: d.Customer,
				DebtorName: names[d.Customer].Name, Mandate: m.ID(), MandateRef: ms.Reference, Signed: ms.Signed, IBAN: ms.IBAN, Amount: d.Open}); err != nil {
				return RemittanceDTO{}, err
			}
		}
		if err := s.remittances.Create(ctx, r); err != nil {
			return RemittanceDTO{}, err
		}
		d := remittanceDTO(r)
		d.WithoutMandate = without
		return d, nil
	}, pipeline.Transactional[ProposeRemittance, RemittanceDTO](s.UoW))

	svc.RemoveItem = guard(PermRemittanceEdit, func(ctx context.Context, c RemoveItem) (RemittanceDTO, error) {
		var v fw.Validation
		inv := domain.InvoiceID{UUID: parseID(&v, "invoice", c.Invoice)}
		if err := v.Err(); err != nil {
			return RemittanceDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, r *domain.Remittance) error { return r.Remove(inv, c.Installment) })
	}, retry[RemoveItem, RemittanceDTO]())

	svc.Generate = guard(PermRemittanceSend, func(ctx context.Context, c GenerateRemittance) (RemittanceDTO, error) {
		return update(ctx, c.ID, func(ctx context.Context, r *domain.Remittance) error {
			st := r.State()
			acc, err := s.Accounts.Get(ctx, st.Account)
			if err != nil {
				return err
			}
			ids, err := s.Identities.Identities(ctx, []domain.PartyID{{UUID: st.Creditor.UUID}})
			if err != nil {
				return err
			}
			me := ids[domain.PartyID{UUID: st.Creditor.UUID}]
			as := acc.State()
			creditorID, ok := domain.CreditorID(as.IBAN.Country(), as.CreditorSuffix, me.NIF)
			if !ok {
				return fw.Violation("treasury.creditor_id", "the creditor has no tax number for its SEPA identifier")
			}
			sequences := map[domain.MandateID]string{}
			for _, i := range st.Items {
				if _, done := sequences[i.Mandate]; done {
					continue
				}
				if _, err := s.mandates.Update(ctx, i.Mandate, func(_ context.Context, m *domain.Mandate) error {
					sequences[i.Mandate] = m.Sequence()
					return m.Use(st.CollectionDate)
				}); err != nil {
					return err
				}
			}
			return r.Generate(domain.Creditor{Name: me.Name, ID: creditorID, IBAN: as.IBAN, BIC: as.BIC}, sequences, fw.Now())
		})
	}, retry[GenerateRemittance, RemittanceDTO](), pipeline.Transactional[GenerateRemittance, RemittanceDTO](s.UoW))

	svc.File = guard(PermRemittanceRead, func(ctx context.Context, q GetFile) ([]byte, error) {
		r, err := s.Remittances.Get(ctx, q.ID)
		if err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check(domain.RemittanceKind, r.ID(), r.State().Creditor, false); err != nil {
			return nil, err
		}
		return r.Pain008()
	})

	svc.Settle = guard(PermRemittanceBank, func(ctx context.Context, c SettleRemittance) (RemittanceDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, r *domain.Remittance) error { return r.Settle(c.On) })
	}, retry[SettleRemittance, RemittanceDTO]())

	svc.Return = guard(PermRemittanceBank, func(ctx context.Context, c ReturnDebit) (RemittanceDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, r *domain.Remittance) error { return r.Return(c.EndToEnd, c.On, c.Reason) })
	}, retry[ReturnDebit, RemittanceDTO]())

	svc.Cancel = guard(PermRemittanceEdit, func(ctx context.Context, c CancelRemittance) (RemittanceDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, r *domain.Remittance) error { return r.Cancel() })
	}, retry[CancelRemittance, RemittanceDTO]())

	svc.Get = guard(PermRemittanceRead, func(ctx context.Context, q GetRemittance) (RemittanceDTO, error) {
		r, err := s.Remittances.Get(ctx, q.ID)
		if err != nil {
			return RemittanceDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.RemittanceKind, r.ID(), r.State().Creditor, false); err != nil {
			return RemittanceDTO{}, err
		}
		return remittanceDTO(r), nil
	})

	svc.Search = guard(PermRemittanceRead, func(ctx context.Context, q SearchRemittances) (fw.Page[RemittanceDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Remittance]{within(scopeOf(ctx), domain.RemFieldCreditor)}
		if q.Creditor != "" {
			parts = append(parts, domain.RemFieldCreditor.Eq(domain.OrganizationID{UUID: parseID(&v, "creditor", q.Creditor)}))
		}
		if err := v.Err(); err != nil {
			return fw.Page[RemittanceDTO]{}, err
		}
		page, err := s.Remittances.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.RemFieldDate.Asc()))
		if err != nil {
			return fw.Page[RemittanceDTO]{}, err
		}
		return fw.MapPage(page, remittanceDTO), nil
	})
}
