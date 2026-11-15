package application

// Bloquear y desbloquear una cuenta (D-156, docs/CUENTAS-CLIENTES.md «El bloqueo»). «Bloqueada» es
// un estado del ciclo de vida de la cuenta (activa, bloqueada, abandonada, cerrada): la regla se
// aplica a las dos transiciones activa → bloqueada (bloquear) y bloqueada → activa (desbloquear, o
// liberar una bloqueada). Las dos exigen motivo y admiten la versión leída; cada una queda en el
// historial (fin_account_block_events). Si la empresa de la cuenta tiene la acción en su lista de
// aprobaciones (la política de Parties), la orden no cambia la cuenta: deja una solicitud pendiente
// en fin_approvals y, al aprobarse, se aplica en la misma transacción que la resolución.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	dapproval "github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// Acciones que pueden exigir aprobación. Se guardan en la configuración de cada empresa: no se
// renombran.
const (
	ActionBlockAccount   dapproval.Action = "financial.account.block"
	ActionUnblockAccount dapproval.Action = "financial.account.unblock"
)

// Permisos de aprobar (y rechazar) cada acción: Financial.Account.ApproveBlock y
// Financial.Account.ApproveUnblock.
var (
	PermApproveBlock   = approval.ApprovePermission(ActionBlockAccount)
	PermApproveUnblock = approval.ApprovePermission(ActionUnblockAccount)
)

// CodeApprovalPending es el código del 409 cuando ya hay una solicitud pendiente para la misma
// cuenta y la misma acción.
const CodeApprovalPending = "financial.approval_pending"

// BlockEventDTO es una fila del historial de bloqueos.
type BlockEventDTO struct {
	Blocked bool      `json:"blocked"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
	By      string    `json:"by,omitempty"`
	ByName  string    `json:"byName,omitempty"`
}

// blockPayload es lo pedido: la cuenta, si queda bloqueada y el motivo.
type blockPayload struct {
	Account string `json:"account"`
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason"`
}

// Campos de las solicitudes (los nombres son las columnas de pkg/persistence/approvals).
var (
	apvFieldCompany    = spec.Comparable("company", func(r *dapproval.Request) fw.UUID { return r.Company() })
	apvFieldAction     = spec.Comparable("action", func(r *dapproval.Request) string { return string(r.Action()) })
	apvFieldTargetType = spec.Comparable("target_type", func(r *dapproval.Request) string { return r.State().Target.Type })
	apvFieldTargetID   = spec.Comparable("target_id", func(r *dapproval.Request) string { return r.State().Target.ID })
	apvFieldStatus     = spec.Comparable("status", func(r *dapproval.Request) string { return string(r.Status()) })
)

// setBlock bloquea (blocked) o desbloquea una cuenta. Abandoned libera sin motivo una cuenta
// abandonada (Release); una bloqueada se desbloquea con su motivo.
func (s service) setBlock(ctx context.Context, c ChangeStatus, blocked, release bool) (AccountDTO, error) {
	a, err := s.Accounts.Get(ctx, c.ID)
	if err != nil {
		return AccountDTO{}, err
	}
	cur := a.State()
	if err := scopeOf(ctx).check(domain.AccountKind, a.ID(), cur.Company, true); err != nil {
		return AccountDTO{}, err
	}
	if c.Version != 0 && a.Version() != c.Version {
		return AccountDTO{}, fw.Conflict(domain.AccountKind, a.ID(), c.Version, "the account changed after it was read: read it again")
	}
	if release && cur.Status != domain.Blocked {
		return s.update(ctx, c.ID, func(a *domain.Account) error { return a.Release(c.Reason, app.ActorFrom(ctx), fw.Now()) })
	}
	if err := a.CheckBlock(blocked, c.Reason); err != nil {
		return AccountDTO{}, err
	}
	pending, err := s.gate(ctx, a.ID(), cur.Company, blocked, strings.TrimSpace(c.Reason))
	if err != nil || pending != "" {
		dto := accountDTO(a)
		dto.PendingApproval = pending
		return dto, err
	}
	return s.update(ctx, c.ID, func(a *domain.Account) error {
		if blocked {
			return a.Block(c.Reason, app.ActorFrom(ctx), fw.Now())
		}
		return a.Unblock(c.Reason, app.ActorFrom(ctx), fw.Now())
	})
}

// blockHistory es el historial de bloqueos de una cuenta del ámbito de quien llama.
func (s service) blockHistory(ctx context.Context, q GetAccount) ([]BlockEventDTO, error) {
	a, err := s.Accounts.Get(ctx, q.ID)
	if err != nil {
		return nil, err
	}
	if err := scopeOf(ctx).check(domain.AccountKind, a.ID(), a.State().Company, false); err != nil {
		return nil, err
	}
	out := []BlockEventDTO{}
	for _, e := range a.State().BlockEvents {
		out = append(out, BlockEventDTO{Blocked: e.Blocked, Reason: e.Reason, At: e.At, By: optID(e.By.PartyID), ByName: e.By.Name})
	}
	return out, nil
}

// gate pregunta a la política si el cambio pasa por aprobación. Si la exige, crea la solicitud y
// devuelve su id; si no, "".
func (s service) gate(ctx context.Context, id domain.AccountID, company domain.OrganizationID, blocked bool, reason string) (string, error) {
	if s.Policy == nil {
		return "", nil
	}
	action := ActionUnblockAccount
	if blocked {
		action = ActionBlockAccount
	}
	required, err := s.Policy.Requires(ctx, company.UUID, action)
	if err != nil || !required {
		return "", err
	}
	dup, err := s.Approvals.Exists(ctx, spec.And(apvFieldTargetType.Eq(domain.AccountKind), apvFieldTargetID.Eq(id.String()),
		apvFieldAction.Eq(string(action)), apvFieldStatus.Eq(string(dapproval.StatusPending))))
	if err != nil {
		return "", err
	}
	if dup {
		return "", fmt.Errorf("%w: %s: there is already a pending request to %s this account", fw.ErrConflict, CodeApprovalPending,
			strings.TrimPrefix(string(action), "financial.account."))
	}
	payload, _ := json.Marshal(blockPayload{Account: id.String(), Blocked: blocked, Reason: reason})
	r, err := approval.Service{Repo: s.Approvals}.Request(ctx, approval.RequestInput{Company: company.UUID, Action: action,
		Target: dapproval.Target{Type: domain.AccountKind, ID: id.String()}, Payload: payload, Reason: reason})
	if err != nil {
		return "", err
	}
	if err := s.auditApproval(ctx, r, app.AuditCreated, ""); err != nil {
		return "", err
	}
	return r.ID().String(), nil
}

// auditApproval deja la solicitud en la auditoría del contexto. Sus eventos no salen al outbox: la
// acción se aplica en la misma transacción que la resolución.
func (s service) auditApproval(ctx context.Context, r *dapproval.Request, op string, before dapproval.Status) error {
	evts := r.PendingEvents()
	r.ClearEvents()
	if s.Audit == nil {
		return nil
	}
	st := r.State()
	after := map[string]any{"action": string(st.Action), "status": string(st.Status)}
	if st.Comment != "" {
		after["comment"] = st.Comment
	}
	var prev map[string]any
	if before != "" {
		prev = map[string]any{"action": string(st.Action), "status": string(before)}
	}
	rec := app.AuditRecord{ID: fw.NewUUID().String(), AggregateType: dapproval.AggregateType, AggregateID: r.ID().String(),
		AggregateVersion: r.Version(), Operation: op, Actor: app.ActorFrom(ctx), Channel: app.Channel(ctx),
		CorrelationID: app.CorrelationID(ctx), At: fw.Now(), Changes: traits.Diff(prev, after)}
	for _, e := range evts {
		rec.Events = append(rec.Events, e.EventType())
	}
	return s.Audit.Append(ctx, rec)
}

// ApprovalDTO es una solicitud de aprobación de Cuentas.
type ApprovalDTO struct {
	ID          string          `json:"id"`
	Company     string          `json:"company"`
	Action      string          `json:"action"`
	TargetType  string          `json:"targetType"`
	TargetID    string          `json:"targetId"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	RequestedBy string          `json:"requestedBy"`
	RequestedAt time.Time       `json:"requestedAt"`
	Status      string          `json:"status"`
	ResolvedBy  string          `json:"resolvedBy,omitempty"`
	ResolvedAt  *time.Time      `json:"resolvedAt,omitempty"`
	Comment     string          `json:"comment,omitempty"`
	Version     int64           `json:"version"`
}

func approvalDTO(r *dapproval.Request) ApprovalDTO {
	s := r.State()
	out := ApprovalDTO{ID: r.ID().String(), Company: s.Company.String(), Action: string(s.Action), TargetType: s.Target.Type, TargetID: s.Target.ID,
		Payload: s.Payload, Reason: s.Reason, RequestedBy: s.RequestedBy.String(), RequestedAt: s.RequestedAt, Status: string(s.Status),
		Comment: s.Comment, Version: r.Version()}
	if !s.ResolvedBy.IsZero() {
		out.ResolvedBy = s.ResolvedBy.String()
	}
	if !s.ResolvedAt.IsZero() {
		t := s.ResolvedAt
		out.ResolvedAt = &t
	}
	return out
}

// SearchApprovals lista las solicitudes de Cuentas del ámbito de quien llama.
type SearchApprovals struct {
	Company string // opcional: estrecha el ámbito
	Status  string // pending (por omisión), approved, rejected, cancelled o all
}

// ResolveApproval aprueba, rechaza o cancela una solicitud; el comentario es obligatorio al rechazar.
type ResolveApproval struct {
	ID      dapproval.RequestID `json:"-"`
	Comment string              `json:"comment,omitempty"`
}

func (s service) approvalUseCases(svc *Service) {
	apv := approval.Service{Repo: s.Approvals}
	// load carga una solicitud de Cuentas del ámbito de escritura de quien llama (404 fuera de él).
	load := func(ctx context.Context, id dapproval.RequestID) error {
		if _, ok := authz.FromContext(ctx); !ok {
			return fmt.Errorf("%w: no authorization context", fw.ErrUnauthorized)
		}
		r, err := s.Approvals.Get(ctx, id)
		if err != nil {
			return err
		}
		if r.State().Target.Type != domain.AccountKind {
			return fw.NotFound(dapproval.AggregateType, id)
		}
		return scopeOf(ctx).check(dapproval.AggregateType, id, domain.OrganizationID{UUID: r.Company()}, true)
	}
	resolve := func(fn func(context.Context, dapproval.RequestID, string) (*dapproval.Request, error), apply bool) app.Handler[ResolveApproval, ApprovalDTO] {
		return app.Chain[ResolveApproval, ApprovalDTO](app.HandlerFunc[ResolveApproval, ApprovalDTO](func(ctx context.Context, c ResolveApproval) (ApprovalDTO, error) {
			if err := load(ctx, c.ID); err != nil {
				return ApprovalDTO{}, err
			}
			r, err := fn(ctx, c.ID, c.Comment)
			if err != nil {
				return ApprovalDTO{}, err
			}
			if err := s.auditApproval(ctx, r, app.AuditUpdated, dapproval.StatusPending); err != nil {
				return ApprovalDTO{}, err
			}
			if apply {
				if err := s.applyBlock(ctx, r); err != nil {
					return ApprovalDTO{}, err
				}
			}
			return approvalDTO(r), nil
		}), pipeline.Transactional[ResolveApproval, ApprovalDTO](s.UoW))
	}
	// El permiso de aprobar lo comprueba el motor; cancelar sólo lo puede el solicitante.
	svc.ApproveApproval = resolve(apv.Approve, true)
	svc.RejectApproval = resolve(apv.Reject, false)
	svc.CancelApproval = resolve(apv.Cancel, false)

	svc.SearchApprovals = guard(PermAccountRead, func(ctx context.Context, q SearchApprovals) ([]ApprovalDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*dapproval.Request]{apvFieldTargetType.Eq(domain.AccountKind)}
		sc := scopeOf(ctx)
		switch {
		case sc.global:
		case len(sc.orgs) == 0:
			return []ApprovalDTO{}, nil
		default:
			orgs := make([]fw.UUID, 0, len(sc.orgs))
			for _, o := range sc.orgs {
				orgs = append(orgs, o.UUID)
			}
			parts = append(parts, apvFieldCompany.In(orgs...))
		}
		if q.Company != "" {
			parts = append(parts, apvFieldCompany.Eq(parseID(&v, "company", q.Company)))
		}
		switch st := dapproval.Status(q.Status); st {
		case "":
			parts = append(parts, apvFieldStatus.Eq(string(dapproval.StatusPending)))
		case "all":
		case dapproval.StatusPending, dapproval.StatusApproved, dapproval.StatusRejected, dapproval.StatusCancelled:
			parts = append(parts, apvFieldStatus.Eq(string(st)))
		default:
			v.Add("status", "enum", "pending, approved, rejected, cancelled or all")
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		rs, err := s.Approvals.Find(ctx, spec.And(parts...))
		if err != nil {
			return nil, err
		}
		out := make([]ApprovalDTO, 0, len(rs))
		for _, r := range rs {
			out = append(out, approvalDTO(r))
		}
		return out, nil
	})
}

// applyBlock aplica una solicitud aprobada. Es idempotente: si la cuenta ya está en el estado pedido,
// no se toca y la solicitud queda aprobada igualmente. En el historial queda quien aprueba.
func (s service) applyBlock(ctx context.Context, r *dapproval.Request) error {
	var p blockPayload
	if err := json.Unmarshal(r.State().Payload, &p); err != nil {
		return fmt.Errorf("financial: approval %s has an invalid payload: %w", r.ID(), err)
	}
	id, err := domain.ParseAccountID(r.State().Target.ID)
	if err != nil {
		return err
	}
	cur, err := s.Accounts.Get(ctx, id)
	if err != nil {
		return err
	}
	switch st := cur.State().Status; {
	case p.Blocked && st == domain.Blocked, !p.Blocked && st == domain.Active:
		return nil // ya está como se pidió
	case p.Blocked && st != domain.Active, !p.Blocked && st != domain.Blocked:
		// La transacción se deshace y la solicitud sigue pendiente: hay que rechazarla o cancelarla.
		return fw.Violation(domain.CodeApprovalNotApplicable, "the account is "+string(st)+": the request no longer applies; reject or cancel it")
	}
	_, err = s.accounts.Update(ctx, id, func(ctx context.Context, a *domain.Account) error {
		if p.Blocked {
			return a.Block(p.Reason, app.ActorFrom(ctx), fw.Now())
		}
		return a.Unblock(p.Reason, app.ActorFrom(ctx), fw.Now())
	})
	return err
}
