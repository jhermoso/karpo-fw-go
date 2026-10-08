package application

import (
	"context"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions of the catalog of products and of the agreements.
var (
	PermProductRead     = authz.MustPermission("Financial.Product.Read")
	PermProductUpdate   = authz.MustPermission("Financial.Product.Update")
	PermAgreementRead   = authz.MustPermission("Financial.Agreement.Read")
	PermAgreementUpdate = authz.MustPermission("Financial.Agreement.Update")
)

// Commands and queries of products and agreements.
type (
	// DefineProduct adds a product to what an institution offers.
	DefineProduct struct {
		Company        string `json:"company"`
		Code           string `json:"code"`
		Name           string `json:"name"`
		Description    string `json:"description,omitempty"`
		Family         string `json:"family"`
		RegulatoryCode string `json:"regulatoryCode,omitempty"`
	}
	// ChangeProduct replaces what describes a product.
	ChangeProduct struct {
		ID             domain.ProductID `json:"-"`
		Name           string           `json:"name"`
		Description    string           `json:"description,omitempty"`
		Family         string           `json:"family"`
		RegulatoryCode string           `json:"regulatoryCode,omitempty"`
	}
	// DiscontinueProduct stops offering a product from a day (today when empty); Reinstate offers
	// it again.
	DiscontinueProduct struct {
		ID        domain.ProductID `json:"-"`
		On        vocab.Date       `json:"on,omitzero"`
		Reinstate bool             `json:"-"`
	}
	// GetProduct reads a product.
	GetProduct struct{ ID domain.ProductID }
	// SearchProducts lists the products of the caller's scope, by code.
	SearchProducts struct {
		Company, Family, Offered string
		Page, Size               int
	}
	// SignAgreement records an agreement signed with a customer.
	SignAgreement struct {
		Company     string     `json:"company"`
		Customer    string     `json:"customer"`
		Number      string     `json:"number"`
		Name        string     `json:"name"`
		Description string     `json:"description,omitempty"`
		Family      string     `json:"family,omitempty"` // that of the product when empty
		Product     string     `json:"product,omitempty"`
		Signed      vocab.Date `json:"signed,omitzero"`
		From        vocab.Date `json:"from,omitzero"`
		Thru        vocab.Date `json:"thru,omitzero"`
	}
	// ChangeAgreement replaces the name, the description and the last day of an agreement.
	ChangeAgreement struct {
		ID          domain.AgreementID `json:"-"`
		Name        string             `json:"name"`
		Description string             `json:"description,omitempty"`
		Thru        vocab.Date         `json:"thru,omitzero"`
	}
	// TerminateAgreement ends an agreement on a day (today when empty), saying why.
	TerminateAgreement struct {
		ID     domain.AgreementID `json:"-"`
		On     vocab.Date         `json:"on,omitzero"`
		Reason string             `json:"reason,omitempty"`
	}
	// GetAgreement reads an agreement.
	GetAgreement struct{ ID domain.AgreementID }
	// SearchAgreements lists the agreements of the caller's scope, by number.
	SearchAgreements struct {
		Company, Customer, Product, Status string
		Page, Size                         int
	}
)

// DTOs.
type (
	ProductDTO struct {
		ID             string `json:"id"`
		Company        string `json:"company"`
		Code           string `json:"code"`
		Name           string `json:"name"`
		Description    string `json:"description,omitempty"`
		Family         string `json:"family"`
		RegulatoryCode string `json:"regulatoryCode,omitempty"`
		Discontinued   string `json:"discontinued,omitempty"`
		Offered        bool   `json:"offered"` // today
		Version        int64  `json:"version"`
	}
	AgreementDTO struct {
		ID          string `json:"id"`
		Company     string `json:"company"`
		Customer    string `json:"customer"`
		Number      string `json:"number"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Family      string `json:"family"`
		Product     string `json:"product,omitempty"`
		Signed      string `json:"signed"`
		From        string `json:"from"`
		Thru        string `json:"thru,omitempty"`
		Status      string `json:"status"`
		Reason      string `json:"reason,omitempty"`
		Version     int64  `json:"version"`
	}
)

func productDTO(p *domain.Product) ProductDTO {
	s := p.State()
	return ProductDTO{ID: p.ID().String(), Company: s.Company.String(), Code: s.Code, Name: s.Name, Description: s.Description, Family: s.Family,
		RegulatoryCode: s.RegulatoryCode, Discontinued: optDate(s.Discontinued), Offered: p.Offered(vocab.DateOf(fw.Now())), Version: p.Version()}
}

func agreementDTO(a *domain.Agreement) AgreementDTO {
	s := a.State()
	return AgreementDTO{ID: a.ID().String(), Company: s.Company.String(), Customer: s.Customer.String(), Number: s.Number, Name: s.Name,
		Description: s.Description, Family: s.Family, Product: optID(s.Product.UUID), Signed: s.Signed.String(), From: s.From.String(),
		Thru: optDate(s.Thru), Status: string(s.Status), Reason: s.Reason, Version: a.Version()}
}

// institution refuses a company that is not a financial institution: this context is not for it.
func (s service) institution(ctx context.Context, company domain.OrganizationID) error {
	ok := false
	if s.Institutions != nil {
		var err error
		if ok, err = s.Institutions.IsFinancialInstitution(ctx, company); err != nil {
			return err
		}
	}
	if !ok {
		return fw.Violation("financial.not_an_institution", "the company is not a financial institution: it has no financial products, agreements or accounts")
	}
	return nil
}

// productOf returns a product of a company, or a violation.
func (s service) productOf(ctx context.Context, company domain.OrganizationID, id fw.UUID) (*domain.Product, error) {
	p, err := s.Products.Get(ctx, domain.ProductID{UUID: id})
	if err != nil || p.State().Company != company {
		return nil, fw.Violation("financial.unknown_product", "the institution does not have that product")
	}
	return p, nil
}

// agreementOf returns an agreement of a company, or a violation.
func (s service) agreementOf(ctx context.Context, company domain.OrganizationID, id fw.UUID) (*domain.Agreement, error) {
	a, err := s.Agreements.Get(ctx, domain.AgreementID{UUID: id})
	if err != nil || a.State().Company != company {
		return nil, fw.Violation("financial.unknown_agreement", "the institution does not have that agreement")
	}
	return a, nil
}

// governs checks what an account is opened under or moved to: a product of the institution and an
// agreement of the institution in force, signed with one of its holders.
func (s service) governs(ctx context.Context, company domain.OrganizationID, product, agreement fw.UUID, holders []domain.PartyID, on vocab.Date,
	opening bool) error {
	if !product.IsZero() {
		p, err := s.productOf(ctx, company, product)
		if err != nil {
			return err
		}
		if opening && !p.Offered(on) {
			return fw.Violation("financial.discontinued", "the product is no longer offered")
		}
	}
	if !agreement.IsZero() {
		a, err := s.agreementOf(ctx, company, agreement)
		if err != nil {
			return err
		}
		if !a.InForce(on) {
			return fw.Violation("financial.agreement_not_in_force", "the agreement is not in force on "+on.String())
		}
		if !slices.Contains(holders, a.State().Customer) {
			return fw.Violation("financial.agreement_of_another", "the agreement was signed with someone who does not hold the account")
		}
	}
	return nil
}

func family(v *fw.Validation, s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	v.Require(slices.Contains(domain.Families, s), "family", "enum", "payment, deposit, loan, investment, leasing or other")
	return s
}

func (s service) catalogUseCases(svc *Service) {
	d := s.Deps
	svc.DefineProduct = changing(d.UoW, PermProductUpdate, func(ctx context.Context, c DefineProduct) (ProductDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		fam := family(&v, c.Family)
		if err := v.Err(); err != nil {
			return ProductDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return ProductDTO{}, err
		}
		if err := s.institution(ctx, company); err != nil {
			return ProductDTO{}, err
		}
		p, err := domain.ReconstituteProduct(domain.NewProductID(), domain.ProductState{Company: company, Code: c.Code, Name: c.Name,
			Description: c.Description, Family: fam, RegulatoryCode: c.RegulatoryCode})
		if err != nil {
			return ProductDTO{}, err
		}
		same, err := d.Products.Find(ctx, spec.And(domain.ProFieldCompany.Eq(company), domain.ProFieldCode.Eq(p.State().Code)))
		if err != nil {
			return ProductDTO{}, err
		}
		if len(same) > 0 {
			return ProductDTO{}, fw.Violation("financial.duplicate_code", "the institution already has a product with that code")
		}
		if err := s.products.Create(ctx, p); err != nil {
			return ProductDTO{}, err
		}
		return productDTO(p), nil
	})

	product := func(ctx context.Context, id domain.ProductID, fn func(*domain.Product) error) (ProductDTO, error) {
		sc := scopeOf(ctx)
		p, err := s.products.Update(ctx, id, func(_ context.Context, p *domain.Product) error {
			if err := sc.check(domain.ProductKind, p.ID(), p.State().Company, true); err != nil {
				return err
			}
			return fn(p)
		})
		if err != nil {
			return ProductDTO{}, err
		}
		return productDTO(p), nil
	}
	svc.ChangeProduct = changing(d.UoW, PermProductUpdate, func(ctx context.Context, c ChangeProduct) (ProductDTO, error) {
		return product(ctx, c.ID, func(p *domain.Product) error { return p.Change(c.Name, c.Description, c.Family, c.RegulatoryCode) })
	})
	svc.DiscontinueProduct = changing(d.UoW, PermProductUpdate, func(ctx context.Context, c DiscontinueProduct) (ProductDTO, error) {
		return product(ctx, c.ID, func(p *domain.Product) error {
			if c.Reinstate {
				p.Reinstate()
				return nil
			}
			return p.Discontinue(today(c.On))
		})
	})
	svc.GetProduct = guard(PermProductRead, func(ctx context.Context, q GetProduct) (ProductDTO, error) {
		p, err := d.Products.Get(ctx, q.ID)
		if err != nil {
			return ProductDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.ProductKind, p.ID(), p.State().Company, false); err != nil {
			return ProductDTO{}, err
		}
		return productDTO(p), nil
	})
	svc.SearchProducts = guard(PermProductRead, func(ctx context.Context, q SearchProducts) (fw.Page[ProductDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Product]{within(scopeOf(ctx), domain.ProFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.ProFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Family != "" {
			parts = append(parts, domain.ProFieldFamily.Eq(family(&v, q.Family)))
		}
		v.Require(q.Offered == "" || q.Offered == "true" || q.Offered == "false", "offered", "enum", "true or false")
		if err := v.Err(); err != nil {
			return fw.Page[ProductDTO]{}, err
		}
		if q.Offered == "" {
			page, err := d.Products.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.ProFieldCode.Asc()))
			if err != nil {
				return fw.Page[ProductDTO]{}, err
			}
			return fw.MapPage(page, productDTO), nil
		}
		// Whether it is offered depends on the day: a catalog is short enough to tell here.
		all, err := d.Products.Find(ctx, spec.And(parts...))
		if err != nil {
			return fw.Page[ProductDTO]{}, err
		}
		day, items := vocab.DateOf(fw.Now()), []ProductDTO{}
		slices.SortFunc(all, func(a, b *domain.Product) int { return strings.Compare(a.State().Code, b.State().Code) })
		for _, p := range all {
			if p.Offered(day) == (q.Offered == "true") {
				items = append(items, productDTO(p))
			}
		}
		req := fw.NewPageRequest[*domain.Product](q.Page, q.Size)
		from := min((req.Number-1)*req.Size, len(items))
		return fw.NewPage(items[from:min(from+req.Size, len(items))], int64(len(items)), req.Number, req.Size), nil
	})

	svc.SignAgreement = changing(d.UoW, PermAgreementUpdate, func(ctx context.Context, c SignAgreement) (AgreementDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		customer := domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}
		prod := optionalID(&v, "product", c.Product)
		if err := v.Err(); err != nil {
			return AgreementDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return AgreementDTO{}, err
		}
		if err := s.institution(ctx, company); err != nil {
			return AgreementDTO{}, err
		}
		st := domain.AgreementState{Company: company, Customer: customer, Number: c.Number, Name: c.Name, Description: c.Description, Family: c.Family,
			Product: domain.ProductID{UUID: prod}, Signed: today(c.Signed), From: c.From, Thru: c.Thru}
		if !prod.IsZero() {
			p, err := s.productOf(ctx, company, prod)
			if err != nil {
				return AgreementDTO{}, err
			}
			if !p.Offered(st.Signed) {
				return AgreementDTO{}, fw.Violation("financial.discontinued", "the product is no longer offered")
			}
			if strings.TrimSpace(st.Family) == "" {
				st.Family = p.State().Family
			}
		}
		a, err := domain.Sign(domain.NewAgreementID(), st)
		if err != nil {
			return AgreementDTO{}, err
		}
		same, err := d.Agreements.Find(ctx, spec.And(domain.AgrFieldCompany.Eq(company), domain.AgrFieldNumber.Eq(a.State().Number)))
		if err != nil {
			return AgreementDTO{}, err
		}
		if len(same) > 0 {
			return AgreementDTO{}, fw.Violation("financial.duplicate_number", "the institution already has an agreement with that number")
		}
		if err := s.agreements.Create(ctx, a); err != nil {
			return AgreementDTO{}, err
		}
		return agreementDTO(a), nil
	})

	agreement := func(ctx context.Context, id domain.AgreementID, fn func(*domain.Agreement) error) (AgreementDTO, error) {
		sc := scopeOf(ctx)
		a, err := s.agreements.Update(ctx, id, func(_ context.Context, a *domain.Agreement) error {
			if err := sc.check(domain.AgreementKind, a.ID(), a.State().Company, true); err != nil {
				return err
			}
			return fn(a)
		})
		if err != nil {
			return AgreementDTO{}, err
		}
		return agreementDTO(a), nil
	}
	svc.ChangeAgreement = changing(d.UoW, PermAgreementUpdate, func(ctx context.Context, c ChangeAgreement) (AgreementDTO, error) {
		return agreement(ctx, c.ID, func(a *domain.Agreement) error { return a.Change(c.Name, c.Description, c.Thru) })
	})
	svc.TerminateAgreement = changing(d.UoW, PermAgreementUpdate, func(ctx context.Context, c TerminateAgreement) (AgreementDTO, error) {
		return agreement(ctx, c.ID, func(a *domain.Agreement) error { return a.Terminate(today(c.On), c.Reason) })
	})
	svc.GetAgreement = guard(PermAgreementRead, func(ctx context.Context, q GetAgreement) (AgreementDTO, error) {
		a, err := d.Agreements.Get(ctx, q.ID)
		if err != nil {
			return AgreementDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.AgreementKind, a.ID(), a.State().Company, false); err != nil {
			return AgreementDTO{}, err
		}
		return agreementDTO(a), nil
	})
	svc.SearchAgreements = guard(PermAgreementRead, func(ctx context.Context, q SearchAgreements) (fw.Page[AgreementDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Agreement]{within(scopeOf(ctx), domain.AgrFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.AgrFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Customer != "" {
			parts = append(parts, domain.AgrFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", q.Customer)}))
		}
		if q.Product != "" {
			parts = append(parts, domain.AgrFieldProduct.Eq(domain.ProductID{UUID: parseID(&v, "product", q.Product)}))
		}
		if q.Status != "" {
			v.Require(q.Status == string(domain.StatusInForce) || q.Status == string(domain.StatusTerminated), "status", "enum", "in-force or terminated")
			parts = append(parts, domain.AgrFieldStatus.Eq(q.Status))
		}
		if err := v.Err(); err != nil {
			return fw.Page[AgreementDTO]{}, err
		}
		page, err := d.Agreements.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.AgrFieldNumber.Asc()))
		if err != nil {
			return fw.Page[AgreementDTO]{}, err
		}
		return fw.MapPage(page, agreementDTO), nil
	})
}
