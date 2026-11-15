package application

// Bloquear y desbloquear el crédito de un cliente (D-156 ampliada; docs/COBROS.md «El bloqueo»):
// son órdenes propias con motivo obligatorio y la versión leída; cada cambio efectivo queda en el
// historial (rec_credit_block_events). Si el vendedor tiene la acción en su lista de aprobaciones
// (la política de Parties), la orden no cambia el perfil: deja una solicitud pendiente en
// rec_approvals y, al aprobarse, el bloqueo se aplica en la misma transacción que la resolución.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
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
	ActionBlockCredit   dapproval.Action = "receivables.credit.block"
	ActionUnblockCredit dapproval.Action = "receivables.credit.unblock"
)

// Permisos de aprobar (y rechazar) cada acción: Receivables.Credit.ApproveBlock y
// Receivables.Credit.ApproveUnblock.
var (
	PermApproveBlock   = approval.ApprovePermission(ActionBlockCredit)
	PermApproveUnblock = approval.ApprovePermission(ActionUnblockCredit)
)

// CodeApprovalPending es el código del 409 cuando ya hay una solicitud pendiente para el mismo
// perfil y la misma acción.
const CodeApprovalPending = "receivables.approval_pending"

// SetCreditBlock bloquea o desbloquea (según el manejador) el crédito de un cliente con su motivo.
type SetCreditBlock struct {
	ID     domain.CreditProfileID `json:"-"`
	Reason string                 `json:"reason"`
	// Version, si no es cero, es la que se leyó: si el perfil ya está en otra, es un conflicto.
	Version int64 `json:"version,omitempty"`
}

// GetCredit identifica un perfil de crédito.
type GetCredit struct {
	ID domain.CreditProfileID
}

// BlockEventDTO es una fila del historial de bloqueos.
type BlockEventDTO struct {
	Blocked  bool      `json:"blocked"`
	Reason   string    `json:"reason"`
	At       time.Time `json:"at"`
	By       string    `json:"by,omitempty"`
	ByName   string    `json:"byName,omitempty"`
	Approval string    `json:"approval,omitempty"`
}

// blockPayload es lo pedido: el perfil, si queda bloqueado y el motivo.
type blockPayload struct {
	Profile string `json:"profile"`
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason"`
}

// Campos de las solicitudes (los nombres son las columnas de pkg/persistence/approvals).
var (
	apvFieldCompany    = spec.Comparable("company", func(r *dapproval.Request) fw.UUID { return r.Company() })
	apvFieldTargetType = spec.Comparable("target_type", func(r *dapproval.Request) string { return r.State().Target.Type })
	apvFieldTargetID   = spec.Comparable("target_id", func(r *dapproval.Request) string { return r.State().Target.ID })
	apvFieldStatus     = spec.Comparable("status", func(r *dapproval.Request) string { return string(r.Status()) })
)

func idText(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

func (s service) blockUseCases(svc *Service) {
	set := func(blocked bool) app.Handler[SetCreditBlock, CreditDTO] {
		return guard(PermCreditUpdate, func(ctx context.Context, c SetCreditBlock) (CreditDTO, error) {
			p, err := s.Credit.Get(ctx, c.ID)
			if err != nil {
				return CreditDTO{}, err
			}
			cur := p.State()
			if err := scopeOf(ctx).check(domain.CreditProfileKind, p.ID(), cur.Seller, true); err != nil {
				return CreditDTO{}, err
			}
			if c.Version != 0 && p.Version() != c.Version {
				return CreditDTO{}, fw.Conflict(domain.CreditProfileKind, p.ID(), c.Version, "the credit profile changed after it was read: read it again")
			}
			// El motivo se exige siempre, también si ya está así o si pasa por aprobación.
			if err := domain.CheckBlockReason(blocked, c.Reason); err != nil || cur.Blocked == blocked {
				return creditDTO(p), err
			}
			pending, err := s.gate(ctx, p.ID(), cur.Seller, blocked, strings.TrimSpace(c.Reason))
			if err != nil || pending != "" {
				dto := creditDTO(p)
				dto.PendingApproval = pending
				return dto, err
			}
			p, err = s.setBlocked(ctx, c.ID, blocked, c.Reason, c.Version, fw.UUID{})
			if err != nil {
				return CreditDTO{}, err
			}
			return creditDTO(p), nil
		}, pipeline.Transactional[SetCreditBlock, CreditDTO](s.UoW))
	}
	svc.BlockCredit = set(true)
	svc.UnblockCredit = set(false)
	svc.CreditBlockHistory = guard(PermCreditRead, func(ctx context.Context, q GetCredit) ([]BlockEventDTO, error) {
		p, err := s.Credit.Get(ctx, q.ID)
		if err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check(domain.CreditProfileKind, p.ID(), p.State().Seller, false); err != nil {
			return nil, err
		}
		out := []BlockEventDTO{}
		for _, e := range p.State().BlockEvents {
			out = append(out, BlockEventDTO{Blocked: e.Blocked, Reason: e.Reason, At: e.At, By: idText(e.By.PartyID), ByName: e.By.Name,
				Approval: idText(e.Approval)})
		}
		return out, nil
	})
	s.approvalUseCases(svc)
}

// setBlocked bloquea o desbloquea el perfil con su entrada en el historial, en la transacción de
// ctx. version, si no es cero, es la que se leyó: se comprueba dentro del mutador, así un reintento
// no bloquea sobre una versión que el usuario no leyó. approval es la solicitud aprobada, si la
// hubo. El usuario es el del contexto de autorización (el mismo que usa el motor de aprobaciones);
// sin él, el actor de la petición.
func (s service) setBlocked(ctx context.Context, id domain.CreditProfileID, blocked bool, reason string, version int64, approval fw.UUID) (*domain.CreditProfile, error) {
	actor := app.ActorFrom(ctx)
	if ac, ok := authz.FromContext(ctx); ok {
		actor = ac.Actor()
	}
	return s.credit.Update(ctx, id, func(_ context.Context, p *domain.CreditProfile) error {
		if version != 0 && p.Version() != version {
			return fw.Conflict(domain.CreditProfileKind, p.ID(), version, "the credit profile changed after it was read: read it again")
		}
		_, err := p.SetBlocked(blocked, reason, actor, fw.Now(), approval)
		return err
	})
}

// gate pregunta a la política si el cambio pasa por aprobación. Si la exige, crea la solicitud y
// devuelve su id; si no, "". Sólo puede haber una solicitud pendiente por perfil y acción: lo
// comprueba aquí y, en SQL, el índice único de pending_key.
func (s service) gate(ctx context.Context, id domain.CreditProfileID, seller domain.OrganizationID, blocked bool, reason string) (string, error) {
	if s.Policy == nil {
		return "", nil
	}
	action := ActionUnblockCredit
	if blocked {
		action = ActionBlockCredit
	}
	required, err := s.Policy.Requires(ctx, seller.UUID, action)
	if err != nil || !required {
		return "", err
	}
	dup, err := s.Approvals.Exists(ctx, spec.And(apvFieldTargetType.Eq(domain.CreditProfileKind), apvFieldTargetID.Eq(id.String()),
		apvFieldStatus.Eq(string(dapproval.StatusPending)))) // una sola pendiente por destino, bloquear o desbloquear
	if err != nil {
		return "", err
	}
	if dup {
		return "", fmt.Errorf("%w: %s: there is already a pending request to %s this credit", fw.ErrConflict, CodeApprovalPending,
			strings.TrimPrefix(string(action), "receivables.credit."))
	}
	payload, _ := json.Marshal(blockPayload{Profile: id.String(), Blocked: blocked, Reason: reason})
	r, err := approval.Service{Repo: s.Approvals}.Request(ctx, approval.RequestInput{Company: seller.UUID, Action: action,
		Target: dapproval.Target{Type: domain.CreditProfileKind, ID: id.String()}, Payload: payload, Reason: reason})
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

// ApprovalDTO es una solicitud de aprobación de Cobros.
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

// SearchApprovals lista las solicitudes de Cobros del ámbito de quien llama.
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
	// load comprueba que la solicitud es de Cobros y del ámbito de escritura de quien llama (404
	// fuera de él).
	load := func(ctx context.Context, id dapproval.RequestID) error {
		if _, ok := authz.FromContext(ctx); !ok {
			return fmt.Errorf("%w: no authorization context", fw.ErrUnauthorized)
		}
		r, err := s.Approvals.Get(ctx, id)
		if err != nil {
			return err
		}
		if r.State().Target.Type != domain.CreditProfileKind {
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

	svc.SearchApprovals = guard(PermCreditRead, func(ctx context.Context, q SearchApprovals) ([]ApprovalDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*dapproval.Request]{apvFieldTargetType.Eq(domain.CreditProfileKind)}
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

// applyBlock aplica una solicitud aprobada, con su entrada en el historial (quien aprueba y la
// solicitud). Es idempotente: si el perfil ya está en el estado pedido, no se toca y la solicitud
// queda aprobada igualmente.
func (s service) applyBlock(ctx context.Context, r *dapproval.Request) error {
	var p blockPayload
	if err := json.Unmarshal(r.State().Payload, &p); err != nil {
		return fmt.Errorf("receivables: approval %s has an invalid payload: %w", r.ID(), err)
	}
	id, err := domain.ParseCreditProfileID(r.State().Target.ID)
	if err != nil {
		return err
	}
	cur, err := s.Credit.Get(ctx, id)
	if err != nil {
		return err
	}
	if cur.State().Blocked == p.Blocked {
		return nil
	}
	_, err = s.setBlocked(ctx, id, p.Blocked, p.Reason, 0, r.ID().UUID)
	return err
}
