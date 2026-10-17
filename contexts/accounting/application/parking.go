package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Permisos de los hechos que esperan a contabilizarse.
var (
	PermParkedRead    = authz.MustPermission("Accounting.Parked.Read")
	PermParkedResolve = authz.MustPermission("Accounting.Parked.Resolve")
)

// companyField es donde cada hecho que contabiliza Accounting nombra la empresa a cuyos libros va. Un
// hecho que no está aquí (payroll.payslip-cancelled.v1 sólo nombra la nómina) sigue al hecho de su
// sujeto.
var companyField = map[string]string{
	"billing.invoice-issued.v1":           "seller",
	"receivables.collection-allocated.v1": "seller",
	"receivables.allocation-reversed.v1":  "seller",
	"treasury.direct-debit-collected.v1":  "creditor",
	"treasury.direct-debit-returned.v1":   "creditor",
	"payroll.payslip-approved.v1":         "employer",
	"payments.payment-allocated.v1":       "company",
	"payments.allocation-reversed.v1":     "company",
	"purchases.invoice-registered.v1":     "company",
	"purchases.invoice-cancelled.v1":      "company",
	"assets.depreciation-charged.v1":      "company",
	"assets.asset-disposed.v1":            "company",
}

func companyOf(env app.Envelope) domain.OrganizationID {
	field, ok := companyField[env.Type]
	if !ok {
		return domain.OrganizationID{}
	}
	var data map[string]json.RawMessage
	var id string
	if json.Unmarshal(env.Data, &data) != nil || json.Unmarshal(data[field], &id) != nil {
		return domain.OrganizationID{}
	}
	u, err := fw.ParseUUID(id)
	if err != nil {
		return domain.OrganizationID{}
	}
	return domain.OrganizationID{UUID: u}
}

// Parking está delante del consumidor de Accounting. Un hecho que una regla impide contabilizar (la
// empresa no tiene contabilidad, al perfil le falta una cuenta, el periodo está cerrado) se guarda
// en vez de rechazarse: rechazarlo haría que su publicador lo enviase una y otra vez, y retendría
// a su publicador para todos los demás oyentes. Los hechos de una empresa se contabilizan en el orden en que
// ocurrieron, así que mientras uno espera, los que vienen después esperan detrás.
type Parking struct {
	inner  Inner
	parked domain.ParkedRepository
	orch   *orchestration.Orchestrator[domain.ParkedID, *domain.ParkedFact]
}

// Inner es el consumidor que contabiliza: toma un mensaje una vez, en su propia unidad de trabajo.
type Inner interface {
	app.MessageHandler
	Name() string
	Types() []string
}

// NewParking pone el aparcamiento delante de un consumidor.
func NewParking(inner Inner, d Deps) *Parking {
	var opts []orchestration.Option
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	return &Parking{inner: inner, parked: d.Parked, orch: orchestration.New[domain.ParkedID, *domain.ParkedFact](d.Parked, d.UoW, opts...)}
}

// Name es el nombre por el que el transporte conoce al consumidor.
func (p *Parking) Name() string { return p.inner.Name() }

func (p *Parking) waiting(ctx context.Context, more spec.Specification[*domain.ParkedFact]) ([]*domain.ParkedFact, error) {
	return p.parked.Find(ctx, spec.And(domain.ParFieldStatus.Eq(string(domain.Parked)), more))
}

func (p *Parking) park(ctx context.Context, env app.Envelope, company domain.OrganizationID, code, reason string) error {
	f, err := domain.Park(domain.NewParkedID(), domain.ParkedState{Company: company, EventType: env.Type, EnvelopeID: env.ID, Source: env.Source,
		Subject: env.Subject, OccurredAt: env.OccurredAt.UTC(), CorrelationID: env.CorrelationID, CausationID: env.CausationID, Data: string(env.Data)},
		code, reason, fw.Now())
	if err != nil {
		return err
	}
	return p.orch.Create(ctx, f)
}

// HandleMessage implementa application.MessageHandler: contabiliza el hecho, o lo guarda. Debe
// llamarse fuera de toda unidad de trabajo: lo que el consumidor escribió antes de que una regla
// rechazase el hecho se deshace con la suya, y sólo entonces se guarda el hecho.
func (p *Parking) HandleMessage(ctx context.Context, env app.Envelope) error {
	if !slices.Contains(p.inner.Types(), env.Type) {
		return nil // no es un hecho que contabilice Accounting
	}
	// Enviado otra vez después de guardarse: ya está aquí.
	if known, err := p.parked.Exists(ctx, domain.ParFieldEnvelope.Eq(env.ID)); err != nil || known {
		return err
	}
	company := companyOf(env)
	// Detrás de lo que espera: de su empresa, o sobre lo mismo.
	var ahead []*domain.ParkedFact
	var err error
	if !company.IsZero() {
		if ahead, err = p.waiting(ctx, domain.ParFieldCompany.Eq(company)); err != nil {
			return err
		}
	}
	if len(ahead) == 0 && env.Subject != "" {
		if ahead, err = p.waiting(ctx, domain.ParFieldSubject.Eq(env.Subject)); err != nil {
			return err
		}
		if len(ahead) > 0 && company.IsZero() {
			company = ahead[0].State().Company
		}
	}
	if len(ahead) > 0 {
		return p.park(ctx, env, company, domain.WaitingCode, "an earlier fact waits to be posted: this one goes after it")
	}
	err = p.inner.HandleMessage(ctx, env)
	var rule *fw.RuleViolationError
	if errors.As(err, &rule) {
		return p.park(ctx, env, company, rule.Code, rule.Error())
	}
	return err
}

func envelopeOf(s domain.ParkedState) app.Envelope {
	return app.Envelope{ID: s.EnvelopeID, Type: s.EventType, Source: s.Source, Subject: s.Subject, OccurredAt: s.OccurredAt, CorrelationID: s.CorrelationID,
		CausationID: s.CausationID, Data: json.RawMessage(s.Data)}
}

// inOrder ordena los hechos tal como ocurrieron.
func inOrder(fs []*domain.ParkedFact) {
	slices.SortFunc(fs, func(a, b *domain.ParkedFact) int {
		x, y := a.State(), b.State()
		if c := x.OccurredAt.Compare(y.OccurredAt); c != 0 {
			return c
		}
		if c := x.ReceivedAt.Compare(y.ReceivedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID().String(), b.ID().String())
	})
}

// retry vuelve a intentar los hechos que esperan, en el orden en que ocurrieron. El primero de una
// empresa que todavía no puede contabilizarse detiene esa empresa: el resto se queda detrás. Lo que
// detiene un hecho sin ser una regla (una validación, un fallo) detiene también su empresa, y
// ninguna otra: se anota en el hecho y se devuelve al final, cuando todas las empresas han tenido su
// turno. Un hecho rechazado otra vez por el mismo código y motivo no se vuelve a escribir: el
// planificador reintenta cada vuelta, y una empresa que tarda días en abrir sus libros llenaría, si
// no, la pista de auditoría de nada.
func (p *Parking) retry(ctx context.Context, only spec.Specification[*domain.ParkedFact]) (posted, waiting int, err error) {
	all, err := p.waiting(ctx, only)
	if err != nil {
		return 0, 0, err
	}
	inOrder(all)
	stopped := map[domain.OrganizationID]bool{}
	var failed error
	for _, f := range all {
		st := f.State()
		if stopped[st.Company] {
			waiting++
			continue
		}
		herr := p.inner.HandleMessage(ctx, envelopeOf(st))
		if herr == nil {
			if _, err := p.orch.Update(ctx, f.ID(), func(_ context.Context, x *domain.ParkedFact) error { x.MarkPosted(fw.Now()); return nil }); err != nil {
				return posted, waiting, errors.Join(failed, err)
			}
			posted++
			continue
		}
		code, reason := domain.FailedCode, herr.Error()
		var rule *fw.RuleViolationError
		if errors.As(herr, &rule) {
			code, reason = rule.Code, rule.Error()
		} else {
			failed = errors.Join(failed, fmt.Errorf("accounting: %s %s: %w", st.EventType, st.EnvelopeID, herr))
		}
		if !f.SameRefusal(code, reason) {
			if _, err := p.orch.Update(ctx, f.ID(), func(_ context.Context, x *domain.ParkedFact) error { x.Refused(code, reason); return nil }); err != nil {
				return posted, waiting, errors.Join(failed, err)
			}
		}
		stopped[st.Company] = true
		waiting++
	}
	return posted, waiting, failed
}

// Órdenes y consultas de lo que espera.
type (
	// RetryParked vuelve a intentar los hechos que esperan: los de una empresa, o los de todas las
	// empresas del ámbito de quien llama cuando no se da ninguna.
	RetryParked struct {
		Company string `json:"company,omitempty"`
	}
	// DiscardParked renuncia a un hecho que espera, diciendo por qué.
	DiscardParked struct {
		ID   domain.ParkedID `json:"-"`
		Note string          `json:"note"`
	}
	// SearchParked lista los hechos guardados, los más antiguos primero.
	SearchParked struct {
		Company, Status string
		Page, Size      int
	}
)

// DTOs.
type (
	ParkedDTO struct {
		ID         string          `json:"id"`
		Company    string          `json:"company,omitempty"`
		EventType  string          `json:"eventType"`
		Source     string          `json:"source,omitempty"`
		Subject    string          `json:"subject,omitempty"`
		OccurredAt string          `json:"occurredAt,omitempty"`
		ReceivedAt string          `json:"receivedAt"`
		Status     string          `json:"status"`
		Code       string          `json:"code,omitempty"`
		Reason     string          `json:"reason,omitempty"`
		Attempts   int             `json:"attempts"`
		ResolvedAt string          `json:"resolvedAt,omitempty"`
		Note       string          `json:"note,omitempty"`
		Data       json.RawMessage `json:"data"`
		Version    int64           `json:"version"`
	}
	RetriedDTO struct {
		Posted  int `json:"posted"`
		Waiting int `json:"waiting"`
	}
)

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parkedDTO(f *domain.ParkedFact) ParkedDTO {
	s := f.State()
	company := ""
	if !s.Company.IsZero() {
		company = s.Company.String()
	}
	return ParkedDTO{ID: f.ID().String(), Company: company, EventType: s.EventType, Source: s.Source, Subject: s.Subject, OccurredAt: rfc3339(s.OccurredAt),
		ReceivedAt: rfc3339(s.ReceivedAt), Status: string(s.Status), Code: s.Code, Reason: s.Reason, Attempts: s.Attempts, ResolvedAt: rfc3339(s.ResolvedAt),
		Note: s.Note, Data: json.RawMessage(s.Data), Version: f.Version()}
}

// companies restringe los hechos al ámbito de quien llama. Un hecho que no nombra empresa es sólo
// para un administrador global.
func companies(sc scope) spec.Specification[*domain.ParkedFact] {
	switch {
	case sc.global:
		return spec.All[*domain.ParkedFact]()
	case len(sc.orgs) == 0:
		return spec.None[*domain.ParkedFact]()
	}
	return domain.ParFieldCompany.In(sc.orgs...)
}

func (s service) parkedUseCases(svc *Service) {
	d := s.Deps
	svc.RetryParked = guard(PermParkedResolve, func(ctx context.Context, c RetryParked) (RetriedDTO, error) {
		if d.Parking == nil {
			return RetriedDTO{}, nil
		}
		sc := scopeOf(ctx)
		only := companies(sc)
		if c.Company != "" {
			var v fw.Validation
			company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
			if err := v.Err(); err != nil {
				return RetriedDTO{}, err
			}
			if err := sc.check("parties.party", company, company, true); err != nil {
				return RetriedDTO{}, err
			}
			only = domain.ParFieldCompany.Eq(company)
		}
		posted, waiting, err := d.Parking.retry(ctx, only)
		return RetriedDTO{Posted: posted, Waiting: waiting}, err
	})

	svc.DiscardParked = guard(PermParkedResolve, func(ctx context.Context, c DiscardParked) (ParkedDTO, error) {
		if d.Parking == nil {
			return ParkedDTO{}, fw.NotFound(domain.ParkedKind, c.ID)
		}
		sc := scopeOf(ctx)
		f, err := d.Parking.orch.Update(ctx, c.ID, func(_ context.Context, f *domain.ParkedFact) error {
			st := f.State()
			if st.Company.IsZero() && !sc.global {
				return fw.NotFound(domain.ParkedKind, f.ID())
			}
			if !st.Company.IsZero() {
				if err := sc.check(domain.ParkedKind, f.ID(), st.Company, true); err != nil {
					return err
				}
			}
			return f.Discard(c.Note, fw.Now())
		})
		if err != nil {
			return ParkedDTO{}, err
		}
		return parkedDTO(f), nil
	})

	svc.SearchParked = guard(PermParkedRead, func(ctx context.Context, q SearchParked) (fw.Page[ParkedDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.ParkedFact]{companies(scopeOf(ctx))}
		if q.Company != "" {
			parts = append(parts, domain.ParFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Status != "" {
			v.Require(slices.Contains(domain.ParkedStatuses, domain.ParkedStatus(q.Status)), "status", "enum", "parked, posted or discarded")
			parts = append(parts, domain.ParFieldStatus.Eq(q.Status))
		}
		if err := v.Err(); err != nil {
			return fw.Page[ParkedDTO]{}, err
		}
		page, err := d.Parked.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.ParFieldReceived.Asc()))
		if err != nil {
			return fw.Page[ParkedDTO]{}, err
		}
		return fw.MapPage(page, parkedDTO), nil
	})
}
