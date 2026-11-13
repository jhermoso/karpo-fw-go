// Package application contiene los casos de uso de Assets (con permisos y ámbito de empresa) y la
// traducción al Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/assets/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/assets/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permisos (los endpoints de activos del C# sólo exigían autenticación). Llevar el registro,
// ejecutar la amortización de un mes y dar de baja un activo van por separado.
var (
	PermAssetRead    = authz.MustPermission("Assets.Asset.Read")
	PermAssetUpdate  = authz.MustPermission("Assets.Asset.Update")
	PermAssetDispose = authz.MustPermission("Assets.Asset.Dispose")
	PermDepreciate   = authz.MustPermission("Assets.Depreciation.Run")
)

// Deps son los puertos que necesitan los casos de uso; Recorder y Audit son opcionales.
type Deps struct {
	Assets   domain.AssetRepository
	UoW      fw.UnitOfWork
	Recorder app.EventRecorder
	Audit    app.AuditLog
}

// Service expone los casos de uso.
type Service struct {
	Register app.CommandHandler[RegisterAsset, AssetDTO]
	Change   app.CommandHandler[ChangeAsset, AssetDTO]
	Dispose  app.CommandHandler[DisposeAsset, AssetDTO]
	// Disposed dice si un activo está dado de baja, a quien puede darlo de baja (el permiso y el
	// ámbito de escritura de Dispose): el DELETE del contrato de mantenimiento lo pregunta.
	Disposed app.QueryHandler[GetAsset, bool]
	// Changeable comprueba que quien llama puede cambiar un activo (el permiso y el ámbito de
	// escritura de Change), aunque el cambio no cambie nada.
	Changeable   app.QueryHandler[GetAsset, bool]
	Depreciate   app.CommandHandler[RunDepreciation, RunDTO]
	GetAsset     app.QueryHandler[GetAsset, AssetDTO]
	SearchAssets app.QueryHandler[SearchAssets, fw.Page[AssetDTO]]
	Book         app.QueryHandler[GetBook, BookDTO]
}

type scope struct {
	global bool
	ac     *authz.Context
	orgs   []domain.OrganizationID
}

func scopeOf(ctx context.Context) scope {
	ac, ok := authz.FromContext(ctx)
	if !ok {
		return scope{}
	}
	s := scope{global: ac.GlobalAdmin, ac: ac}
	for _, u := range ac.EffectiveOrganizations {
		s.orgs = append(s.orgs, domain.OrganizationID{UUID: u})
	}
	return s
}

// check devuelve un 404 uniforme fuera del ámbito de la empresa y un 403 cuando es de sólo lectura.
func (s scope) check(kind string, id fmt.Stringer, org domain.OrganizationID, write bool) error {
	if !s.global && !slices.Contains(s.orgs, org) {
		return fw.NotFound(kind, id)
	}
	if write && !s.global && (s.ac == nil || !s.ac.CanWrite(org.UUID)) {
		return fmt.Errorf("%w: read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

func within[T any](s scope, field spec.Field[T, domain.OrganizationID]) spec.Spec[T] {
	switch {
	case s.global:
		return spec.All[T]()
	case len(s.orgs) == 0:
		return spec.None[T]()
	}
	return field.In(s.orgs...)
}

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

func parseDecimal(v *fw.Validation, field, s string) vocab.Decimal {
	if s == "" {
		return vocab.DecimalFromInt(0)
	}
	d, err := vocab.ParseDecimal(s)
	v.Require(err == nil, field, "format", field+" must be a decimal number")
	return d
}

func money(d vocab.Decimal) string { return d.StringFixed(2) }

func optID(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

func dateText(d vocab.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func changing[In, Out any](uow fw.UnitOfWork, p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return guard(p, fn, pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond), pipeline.Transactional[In, Out](uow))
}

// versioned es changing para un cambio que puede traer la versión leída: con ella, un conflicto es
// definitivo y no se reintenta (y dentro de una transacción ajena, tampoco se debe).
func versioned[In, Out any](uow fw.UnitOfWork, p authz.Permission, version func(In) int64, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	retry := pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond)
	unlessVersioned := func(next app.Handler[In, Out]) app.Handler[In, Out] {
		wrapped := retry(next)
		return app.HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			if version(in) != 0 {
				return next.Handle(ctx, in)
			}
			return wrapped.Handle(ctx, in)
		})
	}
	return guard(p, fn, unlessVersioned, pipeline.Transactional[In, Out](uow))
}

// RegisterAsset da de alta un activo en el registro de una empresa.
type RegisterAsset struct {
	Company    string     `json:"company"`
	Code       string     `json:"code"`
	Name       string     `json:"name"`
	Class      string     `json:"class"`
	Serial     string     `json:"serial,omitempty"`
	Location   string     `json:"location,omitempty"`
	Supplier   string     `json:"supplier,omitempty"`
	Document   string     `json:"document,omitempty"`
	Acquired   vocab.Date `json:"acquired"`
	InService  vocab.Date `json:"inService,omitzero"` // la fecha de adquisición, por omisión
	Cost       string     `json:"cost"`
	Residual   string     `json:"residual,omitempty"`
	LifeMonths int        `json:"lifeMonths,omitempty"`
}

// ChangeAsset sustituye los datos descriptivos de un activo.
type ChangeAsset struct {
	ID       domain.AssetID `json:"-"`
	Name     string         `json:"name"`
	Serial   string         `json:"serial,omitempty"`
	Location string         `json:"location,omitempty"`
	// Version, si no es cero, es la que se leyó: si el activo ya está en otra, el cambio es un conflicto.
	Version int64 `json:"-"`
}

// DisposeAsset vende o desecha un activo en una fecha.
type DisposeAsset struct {
	ID       domain.AssetID `json:"-"`
	Date     vocab.Date     `json:"date"`
	Kind     string         `json:"kind"`
	Proceeds string         `json:"proceeds,omitempty"`
	// Version, si no es cero, es la que se leyó: si el activo ya está en otra, la baja es un conflicto.
	Version int64 `json:"-"`
}

// RunDepreciation carga la amortización de todos los activos de una empresa hasta un mes.
type RunDepreciation struct {
	Company string `json:"company"`
	Year    int    `json:"year"`
	Month   int    `json:"month"`
}

// GetAsset carga un activo con sus cargos.
type GetAsset struct{ ID domain.AssetID }

// SearchAssets busca en el registro del ámbito de quien llama.
type SearchAssets struct {
	Company, Class string
	InService      bool   // no dados de baja
	Text           string // en el nombre, sin distinguir mayúsculas
	Sort           string // code (por omisión) o name; los empates, por la identidad
	Desc           bool
	Page, Size     int
}

// GetBook devuelve el registro de una empresa con sus totales.
type GetBook struct{ Company string }

// ChargeDTO es la forma de transporte de un cargo.
type ChargeDTO struct {
	Period string `json:"period"`
	Amount string `json:"amount"`
}

// AssetDTO es la forma de transporte de un activo.
type AssetDTO struct {
	ID           string      `json:"id"`
	Company      string      `json:"company"`
	Code         string      `json:"code"`
	Name         string      `json:"name"`
	Class        string      `json:"class"`
	Serial       string      `json:"serial,omitempty"`
	Location     string      `json:"location,omitempty"`
	Supplier     string      `json:"supplier,omitempty"`
	Document     string      `json:"document,omitempty"`
	Acquired     string      `json:"acquired"`
	InService    string      `json:"inService"`
	Cost         string      `json:"cost"`
	Residual     string      `json:"residual"`
	LifeMonths   int         `json:"lifeMonths"`
	Monthly      string      `json:"monthly"`
	Accumulated  string      `json:"accumulated"`
	NetBookValue string      `json:"netBookValue"`
	Status       string      `json:"status"`
	Disposed     string      `json:"disposed,omitempty"`
	Disposal     string      `json:"disposal,omitempty"`
	Proceeds     string      `json:"proceeds,omitempty"`
	Result       string      `json:"result,omitempty"`
	Charges      []ChargeDTO `json:"charges,omitempty"`
	Version      int64       `json:"version"`
}

func assetDTO(a *domain.Asset, charges bool) AssetDTO {
	s := a.State()
	d := AssetDTO{ID: a.ID().String(), Company: s.Company.String(), Code: s.Code, Name: s.Name, Class: string(s.Class), Serial: s.Serial,
		Location: optID(s.Location.UUID), Supplier: optID(s.Supplier.UUID), Document: s.Document, Acquired: s.Acquired.String(),
		InService: s.InService.String(), Cost: money(s.Cost), Residual: money(s.Residual), LifeMonths: s.LifeMonths, Monthly: money(a.Monthly()),
		Accumulated: money(a.Accumulated()), NetBookValue: money(a.NetBookValue()), Status: a.Status(), Disposed: dateText(s.Disposed),
		Disposal: s.Disposal, Version: a.Version()}
	if !s.Disposed.IsZero() {
		d.Proceeds, d.Result = money(s.Proceeds), money(a.Result())
	}
	if charges {
		for _, c := range s.Charges {
			d.Charges = append(d.Charges, ChargeDTO{Period: c.Period.String(), Amount: money(c.Amount)})
		}
	}
	return d
}

// RunDTO es el resultado de una ejecución de la amortización.
type RunDTO struct {
	Period  string `json:"period"`
	Assets  int    `json:"assets"`  // activos cargados
	Charges int    `json:"charges"` // meses cargados
	Total   string `json:"total"`
}

// BookDTO es el registro de una empresa con sus totales.
type BookDTO struct {
	Assets       []AssetDTO `json:"assets"`
	Cost         string     `json:"cost"`
	Accumulated  string     `json:"accumulated"`
	NetBookValue string     `json:"netBookValue"`
}

// NewService conecta los casos de uso.
func NewService(d Deps) *Service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	assets := orchestration.New[domain.AssetID, *domain.Asset](d.Assets, d.UoW, opts...)
	svc := &Service{}

	svc.Register = changing(d.UoW, PermAssetUpdate, func(ctx context.Context, c RegisterAsset) (AssetDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		st := domain.AssetState{Company: company, Code: c.Code, Class: domain.Class(strings.TrimSpace(c.Class)), Details: domain.Details{Name: c.Name, Serial: c.Serial},
			Document: c.Document, Acquired: c.Acquired, InService: c.InService, Cost: parseDecimal(&v, "cost", c.Cost),
			Residual: parseDecimal(&v, "residual", c.Residual), LifeMonths: c.LifeMonths}
		if st.InService.IsZero() {
			st.InService = st.Acquired
		}
		if c.Location != "" {
			st.Location = domain.FacilityID{UUID: parseID(&v, "location", c.Location)}
		}
		if c.Supplier != "" {
			st.Supplier = domain.PartyID{UUID: parseID(&v, "supplier", c.Supplier)}
		}
		if err := v.Err(); err != nil {
			return AssetDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return AssetDTO{}, err
		}
		a, err := domain.RegisterAsset(domain.NewAssetID(), st)
		if err != nil {
			return AssetDTO{}, err
		}
		dup, err := d.Assets.Exists(ctx, spec.And(domain.AstFieldCompany.Eq(company), domain.AstFieldCode.Eq(a.State().Code)))
		if err != nil {
			return AssetDTO{}, err
		}
		if dup {
			return AssetDTO{}, fw.Violation("assets.duplicate_code", "the register already has that code")
		}
		if err := assets.Create(ctx, a); err != nil {
			return AssetDTO{}, err
		}
		return assetDTO(a, true), nil
	})

	update := func(ctx context.Context, id domain.AssetID, version int64, fn func(*domain.Asset) error) (AssetDTO, error) {
		sc := scopeOf(ctx)
		a, err := assets.Update(ctx, id, func(_ context.Context, a *domain.Asset) error {
			if err := sc.check(domain.AssetKind, a.ID(), a.State().Company, true); err != nil {
				return err
			}
			if version != 0 && a.Version() != version {
				return fw.Conflict(domain.AssetKind, a.ID(), version, "the asset changed after it was read: read it again")
			}
			return fn(a)
		})
		if err != nil {
			return AssetDTO{}, err
		}
		return assetDTO(a, true), nil
	}
	svc.Change = versioned(d.UoW, PermAssetUpdate, func(c ChangeAsset) int64 { return c.Version }, func(ctx context.Context, c ChangeAsset) (AssetDTO, error) {
		var v fw.Validation
		det := domain.Details{Name: c.Name, Serial: c.Serial}
		if c.Location != "" {
			det.Location = domain.FacilityID{UUID: parseID(&v, "location", c.Location)}
		}
		if err := v.Err(); err != nil {
			return AssetDTO{}, err
		}
		return update(ctx, c.ID, c.Version, func(a *domain.Asset) error { return a.Change(det) })
	})
	svc.Dispose = versioned(d.UoW, PermAssetDispose, func(c DisposeAsset) int64 { return c.Version }, func(ctx context.Context, c DisposeAsset) (AssetDTO, error) {
		var v fw.Validation
		proceeds := parseDecimal(&v, "proceeds", c.Proceeds)
		if err := v.Err(); err != nil {
			return AssetDTO{}, err
		}
		return update(ctx, c.ID, c.Version, func(a *domain.Asset) error { return a.Dispose(c.Date, strings.TrimSpace(c.Kind), proceeds) })
	})
	svc.Changeable = guard(PermAssetUpdate, func(ctx context.Context, q GetAsset) (bool, error) {
		a, err := d.Assets.Get(ctx, q.ID)
		if err != nil {
			return false, err
		}
		if err := scopeOf(ctx).check(domain.AssetKind, a.ID(), a.State().Company, true); err != nil {
			return false, err
		}
		return true, nil
	})
	svc.Disposed = guard(PermAssetDispose, func(ctx context.Context, q GetAsset) (bool, error) {
		a, err := d.Assets.Get(ctx, q.ID)
		if err != nil {
			return false, err
		}
		if err := scopeOf(ctx).check(domain.AssetKind, a.ID(), a.State().Company, true); err != nil {
			return false, err
		}
		return a.Status() == "disposed", nil
	})

	// La ejecución carga cada activo en su propio paso de la misma unidad de trabajo: todos los meses
	// que lleva de retraso, hasta el pedido. Ejecutarla otra vez no carga nada.
	svc.Depreciate = changing(d.UoW, PermDepreciate, func(ctx context.Context, c RunDepreciation) (RunDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		period := domain.Period{Year: c.Year, Month: c.Month}
		v.Require(period.Valid(), "month", "range", "a month of a year is required")
		if err := v.Err(); err != nil {
			return RunDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return RunDTO{}, err
		}
		as, err := d.Assets.Find(ctx, spec.And(domain.AstFieldCompany.Eq(company), domain.AstFieldDisposed.Eq(false)), domain.AstFieldCode.Asc())
		if err != nil {
			return RunDTO{}, err
		}
		out, total := RunDTO{Period: period.String()}, vocab.DecimalFromInt(0)
		for _, x := range as {
			if x.FullyDepreciated() {
				continue
			}
			var charged []domain.Charge
			if _, err := assets.Update(ctx, x.ID(), func(_ context.Context, a *domain.Asset) error {
				var err error
				charged, err = a.DepreciateThrough(period)
				return err
			}); err != nil {
				return RunDTO{}, err
			}
			if len(charged) == 0 {
				continue
			}
			out.Assets++
			out.Charges += len(charged)
			for _, ch := range charged {
				total = total.Add(ch.Amount)
			}
		}
		out.Total = money(total)
		return out, nil
	})

	svc.GetAsset = guard(PermAssetRead, func(ctx context.Context, q GetAsset) (AssetDTO, error) {
		a, err := d.Assets.Get(ctx, q.ID)
		if err != nil {
			return AssetDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.AssetKind, a.ID(), a.State().Company, false); err != nil {
			return AssetDTO{}, err
		}
		return assetDTO(a, true), nil
	})

	svc.SearchAssets = guard(PermAssetRead, func(ctx context.Context, q SearchAssets) (fw.Page[AssetDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Asset]{within(scopeOf(ctx), domain.AstFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.AstFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Class != "" {
			v.Require(slices.Contains(domain.Classes, domain.Class(q.Class)), "class", "enum", "an asset class")
			parts = append(parts, domain.AstFieldClass.Eq(q.Class))
		}
		if q.InService {
			parts = append(parts, domain.AstFieldDisposed.Eq(false))
		}
		if t := strings.TrimSpace(q.Text); t != "" {
			parts = append(parts, domain.AstFieldName.ContainsFold(t))
		}
		order := domain.AstFieldCode.Asc()
		switch {
		case q.Sort == "name" && q.Desc:
			order = domain.AstFieldName.Desc()
		case q.Sort == "name":
			order = domain.AstFieldName.Asc()
		case q.Sort == "" || q.Sort == "code":
			if q.Desc {
				order = domain.AstFieldCode.Desc()
			}
		default:
			v.Add("sort", "enum", "code or name")
		}
		if err := v.Err(); err != nil {
			return fw.Page[AssetDTO]{}, err
		}
		page, err := d.Assets.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, order, domain.AstFieldIDOrder.Asc()))
		if err != nil {
			return fw.Page[AssetDTO]{}, err
		}
		return fw.MapPage(page, func(a *domain.Asset) AssetDTO { return assetDTO(a, false) }), nil
	})

	svc.Book = guard(PermAssetRead, func(ctx context.Context, q GetBook) (BookDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return BookDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return BookDTO{}, err
		}
		as, err := d.Assets.Find(ctx, spec.And(domain.AstFieldCompany.Eq(company), domain.AstFieldDisposed.Eq(false)), domain.AstFieldCode.Asc())
		if err != nil {
			return BookDTO{}, err
		}
		out := BookDTO{Assets: []AssetDTO{}}
		cost, acc := vocab.DecimalFromInt(0), vocab.DecimalFromInt(0)
		for _, a := range as {
			out.Assets = append(out.Assets, assetDTO(a, false))
			cost, acc = cost.Add(a.State().Cost), acc.Add(a.Accumulated())
		}
		out.Cost, out.Accumulated, out.NetBookValue = money(cost), money(acc), money(cost.Sub(acc))
		return out, nil
	})
	return svc
}

// Publications traduce los eventos de dominio al Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.DepreciationCharged) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.DepreciationChargedV1{AssetID: e.AggregateID, Company: e.Company, Code: e.Code, Class: e.Class,
			Period: e.Period, Date: e.Date, Amount: e.Amount, Accumulated: e.Accumulated}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.AssetDisposed) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.AssetDisposedV1{AssetID: e.AggregateID, Company: e.Company, Code: e.Code, Class: e.Class, Kind: e.Kind,
			Date: e.Date, Cost: e.Cost, Accumulated: e.Accumulated, Proceeds: e.Proceeds, Result: e.Result}}, nil
	})
	return r
}
