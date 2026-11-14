package application

// Aprobación del bloqueo y del desbloqueo de proveedores (D-156, paso 3; docs/COMPRAS.md
// «Aprobaciones»). Si la empresa tiene la acción en su lista (la política de Parties), un cambio que
// bloquearía o desbloquearía un perfil no lo hace: deja una solicitud pendiente en pur_approvals, en
// la misma transacción, y el resto del cambio se aplica. Al aprobarse, el bloqueo se aplica en la
// misma transacción que la resolución.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
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
	ActionBlockSupplier   dapproval.Action = "purchases.supplier.block"
	ActionUnblockSupplier dapproval.Action = "purchases.supplier.unblock"
)

// Permisos de aprobar (y rechazar) cada acción: Purchases.Supplier.ApproveBlock y
// Purchases.Supplier.ApproveUnblock.
var (
	PermApproveBlock   = approval.ApprovePermission(ActionBlockSupplier)
	PermApproveUnblock = approval.ApprovePermission(ActionUnblockSupplier)
)

// CodeApprovalPending es el código del 409 cuando ya hay una solicitud pendiente para el mismo
// perfil y la misma acción.
const CodeApprovalPending = "purchases.approval_pending"

// blockPayload es lo pedido: el perfil, si queda bloqueado y el motivo.
type blockPayload struct {
	Supplier string `json:"supplier"`
	Blocked  bool   `json:"blocked"`
	Reason   string `json:"reason"`
}

// Campos de las solicitudes (los nombres son las columnas de pkg/persistence/approvals).
var (
	apvFieldCompany    = spec.Comparable("company", func(r *dapproval.Request) fw.UUID { return r.Company() })
	apvFieldAction     = spec.Comparable("action", func(r *dapproval.Request) string { return string(r.Action()) })
	apvFieldTargetType = spec.Comparable("target_type", func(r *dapproval.Request) string { return r.State().Target.Type })
	apvFieldTargetID   = spec.Comparable("target_id", func(r *dapproval.Request) string { return r.State().Target.ID })
	apvFieldStatus     = spec.Comparable("status", func(r *dapproval.Request) string { return string(r.Status()) })
)

// gate decide si el cambio de bloqueo de st respecto de cur pasa por aprobación. Si la política lo
// exige, crea la solicitud, deja st con el bloqueo (y los motivos) de cur y devuelve el id de la
// solicitud; si no, devuelve "" y st queda como estaba. cur es el estado vacío en un alta.
func (s service) gate(ctx context.Context, id domain.SupplierID, cur domain.SupplierState, st *domain.SupplierState) (string, error) {
	if st.Blocked == cur.Blocked || s.Policy == nil {
		return "", nil
	}
	action, reason := ActionUnblockSupplier, strings.TrimSpace(st.UnblockReason)
	if st.Blocked {
		action, reason = ActionBlockSupplier, strings.TrimSpace(st.BlockReason)
	}
	required, err := s.Policy.Requires(ctx, st.Company.UUID, action)
	if err != nil || !required {
		return "", err
	}
	// El motivo se exige ya al pedir, con los mismos códigos que sin aprobación.
	switch {
	case reason == "" && st.Blocked:
		return "", fw.Violation(domain.CodeBlockReasonRequired, "blocking a supplier needs a reason")
	case reason == "":
		return "", fw.Violation(domain.CodeUnblockReasonRequired, "unblocking a supplier needs a reason")
	}
	dup, err := s.Approvals.Exists(ctx, spec.And(apvFieldTargetType.Eq(domain.SupplierKind), apvFieldTargetID.Eq(id.String()),
		apvFieldAction.Eq(string(action)), apvFieldStatus.Eq(string(dapproval.StatusPending))))
	if err != nil {
		return "", err
	}
	if dup {
		return "", fmt.Errorf("%w: %s: there is already a pending request to %s this supplier", fw.ErrConflict, CodeApprovalPending,
			strings.TrimPrefix(string(action), "purchases.supplier."))
	}
	payload, _ := json.Marshal(blockPayload{Supplier: id.String(), Blocked: st.Blocked, Reason: reason})
	r, err := approval.Service{Repo: s.Approvals}.Request(ctx, approval.RequestInput{Company: st.Company.UUID, Action: action,
		Target: dapproval.Target{Type: domain.SupplierKind, ID: id.String()}, Payload: payload, Reason: reason})
	if err != nil {
		return "", err
	}
	if err := s.auditApproval(ctx, r, app.AuditCreated, ""); err != nil {
		return "", err
	}
	st.Blocked, st.BlockReason, st.UnblockReason = cur.Blocked, cur.BlockReason, cur.UnblockReason
	return r.ID().String(), nil
}

// auditApproval deja la solicitud en la auditoría del contexto, como el resto de agregados. Sus
// eventos no salen al outbox: la acción se aplica en la misma transacción que la resolución.
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

// ApprovalDTO es una solicitud de aprobación de Compras.
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

// SearchApprovals lista las solicitudes de Compras del ámbito de quien llama.
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
	// load carga una solicitud de Compras del ámbito de escritura de quien llama (404 fuera de él).
	load := func(ctx context.Context, id dapproval.RequestID) (*dapproval.Request, error) {
		if _, ok := authz.FromContext(ctx); !ok {
			return nil, fmt.Errorf("%w: no authorization context", fw.ErrUnauthorized)
		}
		r, err := s.Approvals.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if r.State().Target.Type != domain.SupplierKind {
			return nil, fw.NotFound(dapproval.AggregateType, id)
		}
		org := domain.OrganizationID{UUID: r.Company()}
		if err := scopeOf(ctx).check(dapproval.AggregateType, id, org, true); err != nil {
			return nil, err
		}
		return r, nil
	}
	resolve := func(fn func(context.Context, dapproval.RequestID, string) (*dapproval.Request, error), apply bool) app.Handler[ResolveApproval, ApprovalDTO] {
		return app.Chain[ResolveApproval, ApprovalDTO](app.HandlerFunc[ResolveApproval, ApprovalDTO](func(ctx context.Context, c ResolveApproval) (ApprovalDTO, error) {
			if _, err := load(ctx, c.ID); err != nil {
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
	// El permiso de aprobar lo comprueba el motor (approval.Service); cancelar sólo lo puede el
	// solicitante.
	svc.ApproveApproval = resolve(apv.Approve, true)
	svc.RejectApproval = resolve(apv.Reject, false)
	svc.CancelApproval = resolve(apv.Cancel, false)

	svc.SearchApprovals = guard(PermSupplierRead, func(ctx context.Context, q SearchApprovals) ([]ApprovalDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*dapproval.Request]{apvFieldTargetType.Eq(domain.SupplierKind)}
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

// applyBlock aplica una solicitud aprobada al perfil. Es idempotente: si el perfil ya está en el
// estado pedido (otro lo cambió entretanto), no lo toca y la solicitud queda aprobada igualmente.
// Sólo cambia el bloqueo y su motivo: lo demás que haya cambiado entretanto se respeta.
func (s service) applyBlock(ctx context.Context, r *dapproval.Request) error {
	var p blockPayload
	if err := json.Unmarshal(r.State().Payload, &p); err != nil {
		return fmt.Errorf("purchases: approval %s has an invalid payload: %w", r.ID(), err)
	}
	id, err := domain.ParseSupplierID(r.State().Target.ID)
	if err != nil {
		return err
	}
	cur, err := s.Suppliers.Get(ctx, id)
	if err != nil {
		return err
	}
	if cur.State().Blocked == p.Blocked {
		return nil
	}
	_, err = s.suppliers.Update(ctx, id, func(_ context.Context, sp *domain.SupplierProfile) error {
		st := sp.State()
		st.Blocked = p.Blocked
		if p.Blocked {
			st.BlockReason = p.Reason
		} else {
			st.UnblockReason = p.Reason
		}
		return sp.Change(st)
	})
	return err
}
