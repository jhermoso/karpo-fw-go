package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/treasury/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions of transfer orders: preparing, generating the file and recording what the bank did
// are separate, as for remittances.
var (
	PermTransferRead = authz.MustPermission("Treasury.Transfer.Read")
	PermTransferEdit = authz.MustPermission("Treasury.Transfer.Update")
	PermTransferSend = authz.MustPermission("Treasury.Transfer.Generate")
	PermTransferBank = authz.MustPermission("Treasury.Transfer.Settle")
)

// TaxAuthorityName is the name of the tax authority on a transfer.
const TaxAuthorityName = "Agencia Estatal de Administracion Tributaria"

// ProposeTransfers drafts a transfer order with what Payments has to pay by transfer up to a due
// date.
type ProposeTransfers struct {
	Debtor        string     `json:"debtor"`
	Account       string     `json:"account"`
	ExecutionDate vocab.Date `json:"executionDate"`
	DueTo         vocab.Date `json:"dueTo"`
}

// RemoveTransfer removes the transfers of a payable from a draft.
type RemoveTransfer struct {
	ID      domain.TransferOrderID `json:"-"`
	Payable string                 `json:"payable"`
}

// GenerateTransfers freezes a draft order.
type GenerateTransfers struct {
	ID domain.TransferOrderID `json:"-"`
}

// GetTransferFile returns the pain.001 file of a generated order.
type GetTransferFile struct{ ID domain.TransferOrderID }

// SettleTransfers records that the bank executed the order.
type SettleTransfers struct {
	ID domain.TransferOrderID `json:"-"`
	On vocab.Date             `json:"on"`
}

// RejectTransfer records a transfer rejected by the payee's bank.
type RejectTransfer struct {
	ID       domain.TransferOrderID `json:"-"`
	EndToEnd string                 `json:"endToEnd"`
	On       vocab.Date             `json:"on"`
	Reason   string                 `json:"reason"`
}

// CancelTransfers cancels an order not executed.
type CancelTransfers struct {
	ID domain.TransferOrderID `json:"-"`
}

// GetTransferOrder loads an order.
type GetTransferOrder struct{ ID domain.TransferOrderID }

// SearchTransferOrders searches orders of the caller's scope.
type SearchTransferOrders struct {
	Debtor     string
	Page, Size int
}

// TransferDTO is the transport form of a transfer.
type TransferDTO struct {
	EndToEnd  string `json:"endToEnd"`
	Payable   string `json:"payable"`
	Document  string `json:"document"`
	Payee     string `json:"payee"`
	PayeeName string `json:"payeeName"`
	IBAN      string `json:"iban"`
	Amount    string `json:"amount"`
	Rejected  string `json:"rejected,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// TransferOrderDTO is the transport form of an order.
type TransferOrderDTO struct {
	ID             string        `json:"id"`
	Debtor         string        `json:"debtor"`
	Account        string        `json:"account"`
	ExecutionDate  string        `json:"executionDate"`
	Status         string        `json:"status"`
	Total          string        `json:"total"`
	Settled        string        `json:"settled,omitempty"`
	Transfers      []TransferDTO `json:"transfers"`
	WithoutAccount int           `json:"withoutAccount,omitempty"` // due payables without accounts, on a proposal
	Version        int64         `json:"version"`
}

func transferOrderDTO(o *domain.TransferOrder) TransferOrderDTO {
	s := o.State()
	d := TransferOrderDTO{ID: o.ID().String(), Debtor: s.Debtor.String(), Account: s.Account.String(), ExecutionDate: s.ExecutionDate.String(),
		Status: s.Status.String(), Total: o.Total().StringFixed(2), Settled: dateText(s.Settled), Transfers: []TransferDTO{}, Version: o.Version()}
	for _, t := range s.Transfers {
		d.Transfers = append(d.Transfers, TransferDTO{EndToEnd: t.EndToEnd, Payable: t.Payable.String(), Document: t.Document, Payee: t.Payee,
			PayeeName: t.PayeeName, IBAN: t.IBAN.String(), Amount: t.Amount.StringFixed(2), Rejected: dateText(t.Rejected), Reason: t.Reason})
	}
	return d
}

// inOrders returns the payables already in orders not cancelled (and not rejected).
func (s service) inOrders(ctx context.Context, debtor domain.OrganizationID) (map[domain.PayableID]bool, error) {
	os, err := s.Transfers.Find(ctx, spec.And(domain.TrfFieldDebtor.Eq(debtor), domain.TrfFieldStatus.Ne(int(domain.RemCancelled))))
	if err != nil {
		return nil, err
	}
	out := map[domain.PayableID]bool{}
	for _, o := range os {
		for _, t := range o.State().Transfers {
			if t.Rejected.IsZero() {
				out[t.Payable] = true
			}
		}
	}
	return out, nil
}

func (s service) transferUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.TransferOrderID, fn func(context.Context, *domain.TransferOrder) error) (TransferOrderDTO, error) {
		sc := scopeOf(ctx)
		o, err := s.transfers.Update(ctx, id, func(ctx context.Context, o *domain.TransferOrder) error {
			if err := sc.check(domain.TransferOrderKind, o.ID(), o.State().Debtor, true); err != nil {
				return err
			}
			return fn(ctx, o)
		})
		if err != nil {
			return TransferOrderDTO{}, err
		}
		return transferOrderDTO(o), nil
	}

	svc.ProposeTransfers = guard(PermTransferEdit, func(ctx context.Context, c ProposeTransfers) (TransferOrderDTO, error) {
		var v fw.Validation
		debtor := domain.OrganizationID{UUID: parseID(&v, "debtor", c.Debtor)}
		accountID := domain.AccountID{UUID: parseID(&v, "account", c.Account)}
		v.Require(!c.ExecutionDate.IsZero() && !c.DueTo.IsZero(), "executionDate", "required", "execution and due dates are required")
		if err := v.Err(); err != nil {
			return TransferOrderDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", debtor, debtor, true); err != nil {
			return TransferOrderDTO{}, err
		}
		if s.Payables == nil {
			return TransferOrderDTO{}, fw.Violation("treasury.no_payables", "Payments is not composed with Treasury")
		}
		acc, err := s.Accounts.Get(ctx, accountID)
		if err != nil {
			return TransferOrderDTO{}, err
		}
		if acc.State().Owner != debtor {
			return TransferOrderDTO{}, fw.NotFound(domain.AccountKind, accountID)
		}
		if !acc.State().Payments || !acc.OpenOn(c.ExecutionDate) {
			return TransferOrderDTO{}, fw.Violation("treasury.account_not_for_payments", "the account is not open for payments on that date")
		}
		due, without, err := s.Payables.DueForTransfer(ctx, debtor, c.DueTo)
		if err != nil {
			return TransferOrderDTO{}, err
		}
		taken, err := s.inOrders(ctx, debtor)
		if err != nil {
			return TransferOrderDTO{}, err
		}
		var parties []domain.PartyID
		for _, d := range due {
			if u, err := fw.ParseUUID(d.Payee); err == nil {
				parties = append(parties, domain.PartyID{UUID: u})
			}
		}
		names, err := s.Identities.Identities(ctx, parties)
		if err != nil {
			return TransferOrderDTO{}, err
		}
		o, err := domain.DraftTransferOrder(domain.NewTransferOrderID(), debtor, accountID, c.ExecutionDate)
		if err != nil {
			return TransferOrderDTO{}, err
		}
		for _, d := range due {
			if taken[d.Payable] {
				continue
			}
			name := TaxAuthorityName
			if u, err := fw.ParseUUID(d.Payee); err == nil {
				name = names[domain.PartyID{UUID: u}].Name
			}
			for k, to := range d.PayTo {
				if err := o.Add(domain.Transfer{Payable: d.Payable, Line: k + 1, Document: d.Document, Payee: d.Payee, PayeeName: name, IBAN: to.IBAN,
					Amount: to.Amount}); err != nil {
					return TransferOrderDTO{}, err
				}
			}
		}
		if err := s.transfers.Create(ctx, o); err != nil {
			return TransferOrderDTO{}, err
		}
		d := transferOrderDTO(o)
		d.WithoutAccount = without
		return d, nil
	}, pipeline.Transactional[ProposeTransfers, TransferOrderDTO](s.UoW))

	svc.RemoveTransfer = guard(PermTransferEdit, func(ctx context.Context, c RemoveTransfer) (TransferOrderDTO, error) {
		var v fw.Validation
		p := domain.PayableID{UUID: parseID(&v, "payable", c.Payable)}
		if err := v.Err(); err != nil {
			return TransferOrderDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, o *domain.TransferOrder) error { return o.Remove(p) })
	}, retry[RemoveTransfer, TransferOrderDTO]())

	svc.GenerateTransfers = guard(PermTransferSend, func(ctx context.Context, c GenerateTransfers) (TransferOrderDTO, error) {
		return update(ctx, c.ID, func(ctx context.Context, o *domain.TransferOrder) error {
			st := o.State()
			acc, err := s.Accounts.Get(ctx, st.Account)
			if err != nil {
				return err
			}
			me := domain.PartyID{UUID: st.Debtor.UUID}
			ids, err := s.Identities.Identities(ctx, []domain.PartyID{me})
			if err != nil {
				return err
			}
			as := acc.State()
			return o.Generate(domain.Debtor{Name: ids[me].Name, IBAN: as.IBAN, BIC: as.BIC}, fw.Now())
		})
	}, retry[GenerateTransfers, TransferOrderDTO](), pipeline.Transactional[GenerateTransfers, TransferOrderDTO](s.UoW))

	svc.TransferFile = guard(PermTransferRead, func(ctx context.Context, q GetTransferFile) ([]byte, error) {
		o, err := s.Transfers.Get(ctx, q.ID)
		if err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check(domain.TransferOrderKind, o.ID(), o.State().Debtor, false); err != nil {
			return nil, err
		}
		return o.Pain001()
	})

	svc.SettleTransfers = guard(PermTransferBank, func(ctx context.Context, c SettleTransfers) (TransferOrderDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, o *domain.TransferOrder) error { return o.Settle(c.On) })
	}, retry[SettleTransfers, TransferOrderDTO]())

	svc.RejectTransfer = guard(PermTransferBank, func(ctx context.Context, c RejectTransfer) (TransferOrderDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, o *domain.TransferOrder) error { return o.Reject(c.EndToEnd, c.On, c.Reason) })
	}, retry[RejectTransfer, TransferOrderDTO]())

	svc.CancelTransfers = guard(PermTransferEdit, func(ctx context.Context, c CancelTransfers) (TransferOrderDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, o *domain.TransferOrder) error { return o.Cancel() })
	}, retry[CancelTransfers, TransferOrderDTO]())

	svc.GetTransferOrder = guard(PermTransferRead, func(ctx context.Context, q GetTransferOrder) (TransferOrderDTO, error) {
		o, err := s.Transfers.Get(ctx, q.ID)
		if err != nil {
			return TransferOrderDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.TransferOrderKind, o.ID(), o.State().Debtor, false); err != nil {
			return TransferOrderDTO{}, err
		}
		return transferOrderDTO(o), nil
	})

	svc.SearchTransferOrders = guard(PermTransferRead, func(ctx context.Context, q SearchTransferOrders) (fw.Page[TransferOrderDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.TransferOrder]{within(scopeOf(ctx), domain.TrfFieldDebtor)}
		if q.Debtor != "" {
			parts = append(parts, domain.TrfFieldDebtor.Eq(domain.OrganizationID{UUID: parseID(&v, "debtor", q.Debtor)}))
		}
		if err := v.Err(); err != nil {
			return fw.Page[TransferOrderDTO]{}, err
		}
		page, err := s.Transfers.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.TrfFieldDate.Asc()))
		if err != nil {
			return fw.Page[TransferOrderDTO]{}, err
		}
		return fw.MapPage(page, transferOrderDTO), nil
	})
}

// transferPublications translates the transfer events into the Published Language.
func transferPublications(r *messaging.Recorder) {
	messaging.On(r, func(_ context.Context, e domain.TransfersExecuted) ([]app.IntegrationEvent, error) {
		out := make([]app.IntegrationEvent, 0, len(e.Transfers))
		for _, t := range e.Transfers {
			out = append(out, contracts.TransferExecutedV1{OrderID: e.AggregateID, EndToEnd: t.EndToEnd, Debtor: e.Debtor, Payee: t.Payee,
				PayableID: t.Payable.String(), IBAN: t.IBAN.String(), Amount: t.Amount.StringFixed(2), ExecutedOn: e.Executed})
		}
		return out, nil
	})
	messaging.On(r, func(_ context.Context, e domain.TransferRejected) ([]app.IntegrationEvent, error) {
		t := e.Transfer
		return []app.IntegrationEvent{contracts.TransferRejectedV1{OrderID: e.AggregateID, EndToEnd: t.EndToEnd, Debtor: e.Debtor, Payee: t.Payee,
			PayableID: t.Payable.String(), Amount: t.Amount.StringFixed(2), RejectedOn: t.Rejected.String(), Reason: t.Reason}}, nil
	})
}
