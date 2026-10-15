// Package application holds the Financial use cases (with permissions and company scope) and the
// translation to the Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/financial/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# asked only for a session, and anyone could delete an account). Opening an
// account, keeping its details, stopping it and closing it are separate.
var (
	PermAccountRead   = authz.MustPermission("Financial.Account.Read")
	PermAccountCreate = authz.MustPermission("Financial.Account.Create")
	PermAccountUpdate = authz.MustPermission("Financial.Account.Update")
	PermAccountBlock  = authz.MustPermission("Financial.Account.Block")
	PermAccountClose  = authz.MustPermission("Financial.Account.Close")
)

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{PermAccountRead, PermAccountCreate, PermAccountUpdate, PermAccountBlock, PermAccountClose,
		PermProductRead, PermProductUpdate, PermAgreementRead, PermAgreementUpdate}
}

// Deps are the ports the use cases need; Recorder and Audit are optional. Without Institutions no
// company is a financial institution, and nothing can be created.
type Deps struct {
	Accounts     domain.AccountRepository
	Products     domain.ProductRepository
	Agreements   domain.AgreementRepository
	Institutions domain.Institutions
	UoW          fw.UnitOfWork
	Recorder     app.EventRecorder
	Audit        app.AuditLog
}

// Commands and queries.
type (
	// OpenAccount opens an account for a customer.
	OpenAccount struct {
		Company   string     `json:"company"`
		Number    string     `json:"number"`
		Virtual   bool       `json:"virtual,omitempty"`
		BIC       string     `json:"bic,omitempty"`
		Currency  string     `json:"currency,omitempty"` // EUR when empty
		Name      string     `json:"name,omitempty"`
		Product   string     `json:"product,omitempty"`
		Agreement string     `json:"agreement,omitempty"`
		Demo      bool       `json:"demo,omitempty"`
		Opened    vocab.Date `json:"opened,omitzero"`
		Holder    string     `json:"holder"`
		Uses      []string   `json:"uses,omitempty"`
	}
	// DescribeAccount replaces the name, the BIC, the product and the agreement of an account.
	DescribeAccount struct {
		ID        domain.AccountID `json:"-"`
		Name      string           `json:"name,omitempty"`
		BIC       string           `json:"bic,omitempty"`
		Product   string           `json:"product,omitempty"`
		Agreement string           `json:"agreement,omitempty"`
	}
	// RelateParty gives a party a role on an account.
	RelateParty struct {
		ID      domain.AccountID `json:"-"`
		Party   string           `json:"party"`
		Role    string           `json:"role"`
		From    vocab.Date       `json:"from,omitzero"`
		Primary bool             `json:"primary,omitempty"`
	}
	// UnrelateParty ends the role of a party on an account.
	UnrelateParty struct {
		ID    domain.AccountID `json:"-"`
		Party string           `json:"party"`
		Role  string           `json:"role"`
		On    vocab.Date       `json:"on,omitzero"`
	}
	// FileUnder files an account under another of its holders.
	FileUnder struct {
		ID    domain.AccountID `json:"-"`
		Party string           `json:"party"`
	}
	// ChangeUse gives an account a use or ends it.
	ChangeUse struct {
		ID  domain.AccountID `json:"-"`
		Use string           `json:"use"`
		On  vocab.Date       `json:"on,omitzero"`
	}
	// ChangeStatus blocks, sets apart, releases or closes an account.
	ChangeStatus struct {
		ID     domain.AccountID `json:"-"`
		Reason string           `json:"reason,omitempty"`
		On     vocab.Date       `json:"on,omitzero"` // of the closing
	}
	// GetAccount reads an account by its identity, or by its number in a company.
	GetAccount struct {
		ID      domain.AccountID
		Company string
		Number  string
	}
	// SearchAccounts searches the accounts of the caller's scope, by number.
	SearchAccounts struct {
		Company, Status, Party, Use, Currency, Name, Demo, Product, Agreement string
		Page, Size                                                            int
	}
	// GetStats counts the accounts of a company.
	GetStats struct{ Company string }
)

// DTOs.
type (
	HolderDTO struct {
		Party   string `json:"party"`
		Role    string `json:"role"`
		From    string `json:"from"`
		Thru    string `json:"thru,omitempty"`
		Primary bool   `json:"primary,omitempty"`
	}
	UseDTO struct {
		Use  string `json:"use"`
		From string `json:"from"`
		Thru string `json:"thru,omitempty"`
	}
	AccountDTO struct {
		ID        string      `json:"id"`
		Company   string      `json:"company"`
		Number    string      `json:"number"`
		Virtual   bool        `json:"virtual"`
		BIC       string      `json:"bic,omitempty"`
		Currency  string      `json:"currency"`
		Name      string      `json:"name,omitempty"`
		Product   string      `json:"product,omitempty"`
		Agreement string      `json:"agreement,omitempty"`
		Status    string      `json:"status"`
		Demo      bool        `json:"demo"`
		Opened    string      `json:"opened"`
		Closed    string      `json:"closed,omitempty"`
		Reason    string      `json:"reason,omitempty"`
		Holder    string      `json:"holder,omitempty"` // who it is filed under
		Holders   []HolderDTO `json:"holders"`
		Uses      []UseDTO    `json:"uses"`
		Version   int64       `json:"version"`
	}
	// StatsDTO counts the accounts of a company: all of them, and by status, currency and current
	// use. Demo accounts are counted apart.
	StatsDTO struct {
		Total      int            `json:"total"`
		Demo       int            `json:"demo"`
		ByStatus   map[string]int `json:"byStatus"`
		ByCurrency map[string]int `json:"byCurrency"`
		ByUse      map[string]int `json:"byUse"`
	}
)

// Service exposes the use cases.
type Service struct {
	Open      app.CommandHandler[OpenAccount, AccountDTO]
	Describe  app.CommandHandler[DescribeAccount, AccountDTO]
	Relate    app.CommandHandler[RelateParty, AccountDTO]
	Unrelate  app.CommandHandler[UnrelateParty, AccountDTO]
	FileUnder app.CommandHandler[FileUnder, AccountDTO]
	Assign    app.CommandHandler[ChangeUse, AccountDTO]
	Withdraw  app.CommandHandler[ChangeUse, AccountDTO]
	Block     app.CommandHandler[ChangeStatus, AccountDTO]
	Abandon   app.CommandHandler[ChangeStatus, AccountDTO]
	Release   app.CommandHandler[ChangeStatus, AccountDTO]
	Close     app.CommandHandler[ChangeStatus, AccountDTO]
	Get       app.QueryHandler[GetAccount, AccountDTO]
	Search    app.QueryHandler[SearchAccounts, fw.Page[AccountDTO]]
	Stats     app.QueryHandler[GetStats, StatsDTO]

	DefineProduct      app.CommandHandler[DefineProduct, ProductDTO]
	ChangeProduct      app.CommandHandler[ChangeProduct, ProductDTO]
	DiscontinueProduct app.CommandHandler[DiscontinueProduct, ProductDTO]
	GetProduct         app.QueryHandler[GetProduct, ProductDTO]
	SearchProducts     app.QueryHandler[SearchProducts, fw.Page[ProductDTO]]

	SignAgreement      app.CommandHandler[SignAgreement, AgreementDTO]
	ChangeAgreement    app.CommandHandler[ChangeAgreement, AgreementDTO]
	TerminateAgreement app.CommandHandler[TerminateAgreement, AgreementDTO]
	GetAgreement       app.QueryHandler[GetAgreement, AgreementDTO]
	SearchAgreements   app.QueryHandler[SearchAgreements, fw.Page[AgreementDTO]]
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

// check returns a uniform 404 outside the company's scope and 403 when it is read-only.
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

func optionalID(v *fw.Validation, field, s string) fw.UUID {
	if s == "" {
		return fw.UUID{}
	}
	return parseID(v, field, s)
}

func optID(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

func optDate(d vocab.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func today(d vocab.Date) vocab.Date {
	if d.IsZero() {
		return vocab.DateOf(fw.Now())
	}
	return d
}

func accountDTO(a *domain.Account) AccountDTO {
	s := a.State()
	d := AccountDTO{ID: a.ID().String(), Company: s.Company.String(), Number: s.Number, Virtual: s.Virtual, BIC: s.BIC, Currency: s.Currency.String(),
		Name: s.Name, Product: optID(s.Product), Agreement: optID(s.Agreement), Status: string(s.Status), Demo: s.Demo, Opened: s.Opened.String(), Closed: optDate(s.Closed), Reason: s.Reason,
		Holder: optID(a.PrimaryHolder().UUID), Holders: []HolderDTO{}, Uses: []UseDTO{}, Version: a.Version()}
	for _, h := range s.Holders {
		d.Holders = append(d.Holders, HolderDTO{Party: h.Party.String(), Role: h.Role, From: h.From.String(), Thru: optDate(h.Thru), Primary: h.Primary})
	}
	for _, u := range s.Uses {
		d.Uses = append(d.Uses, UseDTO{Use: u.Code, From: u.From.String(), Thru: optDate(u.Thru)})
	}
	return d
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func changing[In, Out any](uow fw.UnitOfWork, p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return guard(p, fn, pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond), pipeline.Transactional[In, Out](uow))
}

type service struct {
	Deps
	accounts   *orchestration.Orchestrator[domain.AccountID, *domain.Account]
	products   *orchestration.Orchestrator[domain.ProductID, *domain.Product]
	agreements *orchestration.Orchestrator[domain.AgreementID, *domain.Agreement]
}

func newService(d Deps) service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	return service{Deps: d, accounts: orchestration.New[domain.AccountID, *domain.Account](d.Accounts, d.UoW, opts...),
		products:   orchestration.New[domain.ProductID, *domain.Product](d.Products, d.UoW, opts...),
		agreements: orchestration.New[domain.AgreementID, *domain.Agreement](d.Agreements, d.UoW, opts...)}
}

// update changes an account of a company the caller may write in.
func (s service) update(ctx context.Context, id domain.AccountID, fn func(*domain.Account) error) (AccountDTO, error) {
	sc := scopeOf(ctx)
	a, err := s.accounts.Update(ctx, id, func(_ context.Context, a *domain.Account) error {
		if err := sc.check(domain.AccountKind, a.ID(), a.State().Company, true); err != nil {
			return err
		}
		return fn(a)
	})
	if err != nil {
		return AccountDTO{}, err
	}
	return accountDTO(a), nil
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	s := newService(d)
	svc := &Service{}
	s.catalogUseCases(svc)

	svc.Open = changing(d.UoW, PermAccountCreate, func(ctx context.Context, c OpenAccount) (AccountDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		holder := domain.PartyID{UUID: parseID(&v, "holder", c.Holder)}
		product := optionalID(&v, "product", c.Product)
		agreement := optionalID(&v, "agreement", c.Agreement)
		if c.Currency == "" {
			c.Currency = "EUR"
		}
		currency, err := vocab.NewCurrencyCode(strings.ToUpper(strings.TrimSpace(c.Currency)))
		v.Require(err == nil, "currency", "format", "a currency code")
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return AccountDTO{}, err
		}
		if err := s.institution(ctx, company); err != nil {
			return AccountDTO{}, err
		}
		if err := s.governs(ctx, company, product, agreement, []domain.PartyID{holder}, today(c.Opened), true); err != nil {
			return AccountDTO{}, err
		}
		uses := []string{}
		for _, u := range c.Uses {
			if u = strings.ToLower(strings.TrimSpace(u)); !slices.Contains(uses, u) {
				uses = append(uses, u)
			}
		}
		a, err := domain.Open(domain.NewAccountID(), domain.Opening{Company: company, Number: c.Number, Virtual: c.Virtual, BIC: c.BIC, Currency: currency,
			Name: c.Name, Product: product, Agreement: agreement, Demo: c.Demo, Opened: today(c.Opened), Holder: holder, Uses: uses})
		if err != nil {
			return AccountDTO{}, err
		}
		same, err := d.Accounts.Find(ctx, spec.And(domain.AccFieldCompany.Eq(company), domain.AccFieldNumber.Eq(a.State().Number)))
		if err != nil {
			return AccountDTO{}, err
		}
		if len(same) > 0 {
			return AccountDTO{}, fw.Violation("financial.duplicate_number", "the institution already keeps an account with that number")
		}
		if err := s.accounts.Create(ctx, a); err != nil {
			return AccountDTO{}, err
		}
		return accountDTO(a), nil
	})

	svc.Describe = changing(d.UoW, PermAccountUpdate, func(ctx context.Context, c DescribeAccount) (AccountDTO, error) {
		var v fw.Validation
		product := optionalID(&v, "product", c.Product)
		agreement := optionalID(&v, "agreement", c.Agreement)
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		return s.update(ctx, c.ID, func(a *domain.Account) error {
			st, holders := a.State(), []domain.PartyID{}
			for _, h := range st.Holders {
				if h.Current() && h.Role == domain.RoleHolder {
					holders = append(holders, h.Party)
				}
			}
			if err := s.governs(ctx, st.Company, product, agreement, holders, vocab.DateOf(fw.Now()), false); err != nil {
				return err
			}
			return a.Describe(c.Name, c.BIC, product, agreement)
		})
	})

	svc.Relate = changing(d.UoW, PermAccountUpdate, func(ctx context.Context, c RelateParty) (AccountDTO, error) {
		var v fw.Validation
		party := domain.PartyID{UUID: parseID(&v, "party", c.Party)}
		role := strings.ToLower(strings.TrimSpace(c.Role))
		v.Require(slices.Contains(domain.Roles, role), "role", "enum", "holder, authorized or beneficiary")
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		return s.update(ctx, c.ID, func(a *domain.Account) error { return a.Relate(party, role, today(c.From), c.Primary) })
	})

	svc.Unrelate = changing(d.UoW, PermAccountUpdate, func(ctx context.Context, c UnrelateParty) (AccountDTO, error) {
		var v fw.Validation
		party := domain.PartyID{UUID: parseID(&v, "party", c.Party)}
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		return s.update(ctx, c.ID, func(a *domain.Account) error {
			return a.Unrelate(party, strings.ToLower(strings.TrimSpace(c.Role)), today(c.On))
		})
	})

	svc.FileUnder = changing(d.UoW, PermAccountUpdate, func(ctx context.Context, c FileUnder) (AccountDTO, error) {
		var v fw.Validation
		party := domain.PartyID{UUID: parseID(&v, "party", c.Party)}
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		return s.update(ctx, c.ID, func(a *domain.Account) error { return a.FileUnder(party) })
	})

	use := func(v *fw.Validation, code string) string {
		code = strings.ToLower(strings.TrimSpace(code))
		v.Require(slices.Contains(domain.Uses, code), "use", "enum", "a use of the catalogue")
		return code
	}
	svc.Assign = changing(d.UoW, PermAccountUpdate, func(ctx context.Context, c ChangeUse) (AccountDTO, error) {
		var v fw.Validation
		code := use(&v, c.Use)
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		return s.update(ctx, c.ID, func(a *domain.Account) error { return a.Assign(code, today(c.On)) })
	})
	svc.Withdraw = changing(d.UoW, PermAccountUpdate, func(ctx context.Context, c ChangeUse) (AccountDTO, error) {
		var v fw.Validation
		code := use(&v, c.Use)
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		return s.update(ctx, c.ID, func(a *domain.Account) error { return a.Withdraw(code, today(c.On)) })
	})

	svc.Block = changing(d.UoW, PermAccountBlock, func(ctx context.Context, c ChangeStatus) (AccountDTO, error) {
		return s.update(ctx, c.ID, func(a *domain.Account) error { return a.Block(c.Reason) })
	})
	svc.Abandon = changing(d.UoW, PermAccountBlock, func(ctx context.Context, c ChangeStatus) (AccountDTO, error) {
		return s.update(ctx, c.ID, func(a *domain.Account) error { return a.Abandon(c.Reason) })
	})
	svc.Release = changing(d.UoW, PermAccountBlock, func(ctx context.Context, c ChangeStatus) (AccountDTO, error) {
		return s.update(ctx, c.ID, func(a *domain.Account) error { return a.Release() })
	})
	svc.Close = changing(d.UoW, PermAccountClose, func(ctx context.Context, c ChangeStatus) (AccountDTO, error) {
		return s.update(ctx, c.ID, func(a *domain.Account) error { return a.Close(today(c.On), c.Reason) })
	})

	svc.Get = guard(PermAccountRead, func(ctx context.Context, q GetAccount) (AccountDTO, error) {
		var a *domain.Account
		if q.ID.IsZero() {
			var v fw.Validation
			company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
			if err := v.Err(); err != nil {
				return AccountDTO{}, err
			}
			found, err := d.Accounts.Find(ctx, spec.And(domain.AccFieldCompany.Eq(company), domain.AccFieldNumber.Eq(domain.NormalizeNumber(q.Number))))
			if err != nil {
				return AccountDTO{}, err
			}
			if len(found) == 0 {
				return AccountDTO{}, fw.NotFound(domain.AccountKind, fw.UUID{})
			}
			a = found[0]
		} else {
			var err error
			if a, err = d.Accounts.Get(ctx, q.ID); err != nil {
				return AccountDTO{}, err
			}
		}
		if err := scopeOf(ctx).check(domain.AccountKind, a.ID(), a.State().Company, false); err != nil {
			return AccountDTO{}, err
		}
		return accountDTO(a), nil
	})

	svc.Search = guard(PermAccountRead, func(ctx context.Context, q SearchAccounts) (fw.Page[AccountDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Account]{within(scopeOf(ctx), domain.AccFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.AccFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Status != "" {
			v.Require(slices.Contains(domain.Statuses, domain.Status(q.Status)), "status", "enum", "a status")
			parts = append(parts, domain.AccFieldStatus.Eq(q.Status))
		}
		if q.Party != "" { // whoever has a role on it now
			party := domain.PartyID{UUID: parseID(&v, "party", q.Party)}
			parts = append(parts, domain.AccFieldHolders.Any(spec.And(domain.HolFieldParty.Eq(party), domain.HolFieldCurrent.Eq(true))))
		}
		if q.Use != "" {
			v.Require(slices.Contains(domain.Uses, q.Use), "use", "enum", "a use of the catalogue")
			parts = append(parts, domain.AccFieldUses.Any(spec.And(domain.UseFieldCode.Eq(q.Use), domain.UseFieldCurrent.Eq(true))))
		}
		if q.Currency != "" {
			parts = append(parts, domain.AccFieldCurrency.Eq(strings.ToUpper(q.Currency)))
		}
		if q.Name != "" {
			parts = append(parts, domain.AccFieldName.ContainsFold(q.Name))
		}
		if q.Product != "" {
			parts = append(parts, domain.AccFieldProduct.Eq(parseID(&v, "product", q.Product)))
		}
		if q.Agreement != "" {
			parts = append(parts, domain.AccFieldAgreement.Eq(parseID(&v, "agreement", q.Agreement)))
		}
		if q.Demo != "" {
			v.Require(q.Demo == "true" || q.Demo == "false", "demo", "enum", "true or false")
			parts = append(parts, domain.AccFieldDemo.Eq(q.Demo == "true"))
		}
		if err := v.Err(); err != nil {
			return fw.Page[AccountDTO]{}, err
		}
		page, err := d.Accounts.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.AccFieldNumber.Asc()))
		if err != nil {
			return fw.Page[AccountDTO]{}, err
		}
		return fw.MapPage(page, accountDTO), nil
	})

	// The C# statistics ignored the scope of the caller and were cached for everybody.
	svc.Stats = guard(PermAccountRead, func(ctx context.Context, q GetStats) (StatsDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return StatsDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return StatsDTO{}, err
		}
		all, err := d.Accounts.Find(ctx, domain.AccFieldCompany.Eq(company))
		if err != nil {
			return StatsDTO{}, err
		}
		out := StatsDTO{ByStatus: map[string]int{}, ByCurrency: map[string]int{}, ByUse: map[string]int{}}
		for _, a := range all {
			st := a.State()
			if st.Demo {
				out.Demo++
				continue
			}
			out.Total++
			out.ByStatus[string(st.Status)]++
			out.ByCurrency[st.Currency.String()]++
			for _, u := range st.Uses {
				if u.Thru.IsZero() {
					out.ByUse[u.Code]++
				}
			}
		}
		return out, nil
	})
	return svc
}

// Directory answers other contexts which accounts a party has (contracts.Accounts).
type Directory struct{ accounts domain.AccountRepository }

// NewDirectory builds the directory over the accounts.
func NewDirectory(accounts domain.AccountRepository) Directory { return Directory{accounts: accounts} }

// OfParty implements contracts.Accounts.
func (d Directory) OfParty(ctx context.Context, company, party string) ([]contracts.AccountRef, error) {
	c, err1 := fw.ParseUUID(company)
	p, err2 := fw.ParseUUID(party)
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("%w: company and party are ids", fw.ErrValidation)
	}
	pid := domain.PartyID{UUID: p}
	found, err := d.accounts.Find(ctx, spec.And(domain.AccFieldCompany.Eq(domain.OrganizationID{UUID: c}),
		domain.AccFieldHolders.Any(spec.And(domain.HolFieldParty.Eq(pid), domain.HolFieldCurrent.Eq(true)))))
	if err != nil {
		return nil, err
	}
	out := []contracts.AccountRef{}
	for _, a := range found {
		st := a.State()
		for _, h := range st.Holders {
			if h.Current() && h.Party == pid {
				out = append(out, contracts.AccountRef{ID: a.ID().String(), Number: st.Number, Currency: st.Currency.String(), Status: string(st.Status),
					Role: h.Role, Operable: st.Status == domain.Active})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number+out[i].Role < out[j].Number+out[j].Role })
	return out, nil
}

var _ contracts.Accounts = Directory{}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.AccountOpened) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.AccountOpenedV1{AccountID: e.AggregateID, Company: e.Company, Number: e.Number, Currency: e.Currency,
			Holder: e.Holder, Virtual: e.Virtual, Demo: e.Demo, Opened: e.Opened.String()}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.AgreementSigned) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.AgreementSignedV1{AgreementID: e.AggregateID, Company: e.Company, Customer: e.Customer, Number: e.Number,
			Family: e.Family, Product: e.Product, Signed: e.Signed.String(), From: e.From.String(), Thru: optDate(e.Thru)}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.AgreementTerminated) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.AgreementTerminatedV1{AgreementID: e.AggregateID, Company: e.Company, Customer: e.Customer, Number: e.Number,
			On: e.On.String(), Reason: e.Reason}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.AccountStatusChanged) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.AccountStatusChangedV1{AccountID: e.AggregateID, Company: e.Company, Number: e.Number, Holder: e.Holder,
			From: e.From, To: e.To, Reason: e.Reason}}, nil
	})
	return r
}
