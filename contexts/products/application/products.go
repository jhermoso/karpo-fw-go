package application

import (
	"context"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// BarcodeInput is an identification code.
type BarcodeInput struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// ComponentInput is a component of a kit.
type ComponentInput struct {
	Product  string `json:"product"`
	Quantity string `json:"quantity"`
}

// SupplierInput is how a supplier names and delivers the product.
type SupplierInput struct {
	Supplier  string `json:"supplier"`
	Code      string `json:"code,omitempty"`
	LeadDays  int    `json:"leadDays,omitempty"`
	Preferred bool   `json:"preferred,omitempty"`
}

// DetailsInput are the editable data of a product.
type DetailsInput struct {
	Name            string           `json:"name"`
	Description     string           `json:"description,omitempty"`
	Kind            string           `json:"kind"`
	UoM             string           `json:"uom"`
	Category        string           `json:"category,omitempty"`
	TaxCode         string           `json:"taxCode,omitempty"`
	ExpenseCategory string           `json:"expenseCategory,omitempty"`
	BasePrice       string           `json:"basePrice,omitempty"`
	StandardCost    string           `json:"standardCost,omitempty"`
	ForSale         bool             `json:"forSale,omitempty"`
	ForPurchase     bool             `json:"forPurchase,omitempty"`
	Stocked         bool             `json:"stocked,omitempty"`
	Tracking        string           `json:"tracking,omitempty"`
	BlockedSales    bool             `json:"blockedSales,omitempty"`
	BlockedPurchase bool             `json:"blockedPurchase,omitempty"`
	Barcodes        []BarcodeInput   `json:"barcodes,omitempty"`
	Components      []ComponentInput `json:"components,omitempty"`
	Suppliers       []SupplierInput  `json:"suppliers,omitempty"`
}

// RegisterProduct adds a product to the catalog of a company.
type RegisterProduct struct {
	Company string `json:"company"`
	SKU     string `json:"sku"`
	DetailsInput
}

// ChangeProduct replaces the details of a product.
type ChangeProduct struct {
	ID domain.ProductID `json:"-"`
	DetailsInput
}

// DiscontinueProduct retires a product from a date (today by default).
type DiscontinueProduct struct {
	ID domain.ProductID `json:"-"`
	On vocab.Date       `json:"on,omitzero"`
}

// GetProduct loads a product.
type GetProduct struct{ ID domain.ProductID }

// SearchProducts searches the catalog of the caller's scope.
type SearchProducts struct {
	Company, Text, Kind, Category, Barcode string
	Page, Size                             int
}

// ProductDTO is the transport form of a product.
type ProductDTO struct {
	ID      string `json:"id"`
	Company string `json:"company"`
	SKU     string `json:"sku"`
	DetailsInput
	Discontinued string `json:"discontinued,omitempty"`
	Version      int64  `json:"version"`
}

func productDTO(p *domain.Product) ProductDTO {
	s := p.State()
	d := ProductDTO{ID: p.ID().String(), Company: s.Company.String(), SKU: s.SKU, Discontinued: dateText(s.Discontinued), Version: p.Version(),
		DetailsInput: DetailsInput{Name: s.Name, Description: s.Description, Kind: s.Kind.String(), UoM: s.UoM, TaxCode: s.TaxCode,
			ExpenseCategory: s.ExpenseCategory, BasePrice: s.BasePrice.StringFixed(4), StandardCost: s.StandardCost.StringFixed(4), ForSale: s.ForSale,
			ForPurchase: s.ForPurchase, Stocked: s.Stocked, Tracking: s.Tracking.String(), BlockedSales: s.BlockedSales, BlockedPurchase: s.BlockedPurchase,
			Barcodes: []BarcodeInput{}, Components: []ComponentInput{}, Suppliers: []SupplierInput{}}}
	if !s.Category.IsZero() {
		d.Category = s.Category.String()
	}
	for _, b := range s.Barcodes {
		d.Barcodes = append(d.Barcodes, BarcodeInput{Type: b.Type, Value: b.Value})
	}
	for _, c := range s.Components {
		d.Components = append(d.Components, ComponentInput{Product: c.Product.String(), Quantity: c.Quantity.String()})
	}
	for _, x := range s.Suppliers {
		d.Suppliers = append(d.Suppliers, SupplierInput{Supplier: x.Supplier.String(), Code: x.Code, LeadDays: x.LeadDays, Preferred: x.Preferred})
	}
	return d
}

func details(v *fw.Validation, in DetailsInput) domain.Details {
	kind, ok := domain.ParseKind(in.Kind)
	v.Require(ok, "kind", "enum", "good or service")
	d := domain.Details{Name: in.Name, Description: in.Description, Kind: kind, UoM: strings.TrimSpace(in.UoM), TaxCode: in.TaxCode,
		ExpenseCategory: strings.TrimSpace(in.ExpenseCategory), BasePrice: parseDecimal(v, "basePrice", in.BasePrice),
		StandardCost: parseDecimal(v, "standardCost", in.StandardCost), ForSale: in.ForSale, ForPurchase: in.ForPurchase, Stocked: in.Stocked,
		BlockedSales: in.BlockedSales, BlockedPurchase: in.BlockedPurchase}
	if in.Tracking != "" {
		t, ok := domain.ParseTracking(in.Tracking)
		v.Require(ok, "tracking", "enum", "none, lot or serial")
		d.Tracking = t
	}
	if in.Category != "" {
		d.Category = domain.CategoryID{UUID: parseID(v, "category", in.Category)}
	}
	for _, b := range in.Barcodes {
		d.Barcodes = append(d.Barcodes, domain.Barcode{Type: strings.ToLower(strings.TrimSpace(b.Type)), Value: strings.TrimSpace(b.Value)})
	}
	for _, c := range in.Components {
		d.Components = append(d.Components, domain.Component{Product: domain.ProductID{UUID: parseID(v, "components.product", c.Product)},
			Quantity: parseDecimal(v, "components.quantity", c.Quantity)})
	}
	for _, x := range in.Suppliers {
		d.Suppliers = append(d.Suppliers, domain.SupplierItem{Supplier: domain.PartyID{UUID: parseID(v, "suppliers.supplier", x.Supplier)},
			Code: strings.TrimSpace(x.Code), LeadDays: x.LeadDays, Preferred: x.Preferred})
	}
	return d
}

// references checks what a product points to inside its company: its category, that no other
// product has one of its barcodes, and that its components are goods of the company that do not
// contain it (the C# checked none of this and kept two unrelated barcode stores).
func (s service) references(ctx context.Context, self domain.ProductID, company domain.OrganizationID, d domain.Details) error {
	if !d.Category.IsZero() {
		c, err := s.Categories.Get(ctx, d.Category)
		if err != nil {
			return err
		}
		if c.State().Company != company {
			return fw.NotFound(domain.CategoryKind, d.Category)
		}
	}
	for _, b := range d.Barcodes {
		taken, err := s.Products.Exists(ctx, spec.And(domain.ProdFieldCompany.Eq(company), domain.ProdFieldID.Ne(self),
			domain.ProdFieldBarcodes.Any(domain.BarcodeFieldValue.Eq(b.Value))))
		if err != nil {
			return err
		}
		if taken {
			return fw.Violation("products.duplicate_barcode", "another product has the code "+b.Value)
		}
	}
	// Breadth-first through the components: none of them, at any depth, is the product itself.
	pending := make([]domain.ProductID, 0, len(d.Components))
	for _, c := range d.Components {
		pending = append(pending, c.Product)
	}
	seen := map[domain.ProductID]bool{}
	for depth := 0; len(pending) > 0; depth++ {
		if depth > 10 {
			return fw.Violation("products.kit_depth", "kits nest at most 10 levels")
		}
		ps, err := s.Products.Find(ctx, domain.ProdFieldID.In(pending...))
		if err != nil {
			return err
		}
		if len(ps) != len(pending) {
			return fw.Violation("products.unknown_component", "a component is not in the catalog")
		}
		var next []domain.ProductID
		for _, p := range ps {
			st := p.State()
			if st.Company != company || st.Kind != domain.Good {
				return fw.Violation("products.unknown_component", "a component is a good of the same company")
			}
			seen[p.ID()] = true
			for _, c := range st.Components {
				if c.Product == self {
					return fw.Violation("products.kit_cycle", "a kit cannot contain itself")
				}
				if !seen[c.Product] {
					seen[c.Product] = true
					next = append(next, c.Product)
				}
			}
		}
		pending = next
	}
	return nil
}

func (s service) productUseCases(svc *Service) {
	svc.RegisterProduct = guard(PermProductUpdate, func(ctx context.Context, c RegisterProduct) (ProductDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		d := details(&v, c.DetailsInput)
		if err := v.Err(); err != nil {
			return ProductDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return ProductDTO{}, err
		}
		id := domain.NewProductID()
		p, err := domain.RegisterProduct(id, company, c.SKU, d)
		if err != nil {
			return ProductDTO{}, err
		}
		dup, err := s.Products.Exists(ctx, spec.And(domain.ProdFieldCompany.Eq(company), domain.ProdFieldSKU.Eq(p.State().SKU)))
		if err != nil {
			return ProductDTO{}, err
		}
		if dup {
			return ProductDTO{}, fw.Violation("products.duplicate_sku", "the catalog already has that SKU")
		}
		if err := s.references(ctx, id, company, p.State().Details); err != nil {
			return ProductDTO{}, err
		}
		if err := s.products.Create(ctx, p); err != nil {
			return ProductDTO{}, err
		}
		return productDTO(p), nil
	}, pipeline.Transactional[RegisterProduct, ProductDTO](s.UoW))

	update := func(ctx context.Context, id domain.ProductID, fn func(context.Context, *domain.Product) error) (ProductDTO, error) {
		sc := scopeOf(ctx)
		p, err := s.products.Update(ctx, id, func(ctx context.Context, p *domain.Product) error {
			if err := sc.check(domain.ProductKind, p.ID(), p.State().Company, true); err != nil {
				return err
			}
			return fn(ctx, p)
		})
		if err != nil {
			return ProductDTO{}, err
		}
		return productDTO(p), nil
	}
	svc.ChangeProduct = guard(PermProductUpdate, func(ctx context.Context, c ChangeProduct) (ProductDTO, error) {
		var v fw.Validation
		d := details(&v, c.DetailsInput)
		if err := v.Err(); err != nil {
			return ProductDTO{}, err
		}
		return update(ctx, c.ID, func(ctx context.Context, p *domain.Product) error {
			if err := p.Change(d); err != nil {
				return err
			}
			return s.references(ctx, p.ID(), p.State().Company, p.State().Details)
		})
	}, retry[ChangeProduct, ProductDTO](), pipeline.Transactional[ChangeProduct, ProductDTO](s.UoW))
	svc.DiscontinueProduct = guard(PermProductUpdate, func(ctx context.Context, c DiscontinueProduct) (ProductDTO, error) {
		on := c.On
		if on.IsZero() {
			on = vocab.DateOf(fw.Now())
		}
		return update(ctx, c.ID, func(_ context.Context, p *domain.Product) error { return p.Discontinue(on) })
	}, retry[DiscontinueProduct, ProductDTO]())

	svc.GetProduct = guard(PermProductRead, func(ctx context.Context, q GetProduct) (ProductDTO, error) {
		p, err := s.Products.Get(ctx, q.ID)
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
		parts := []spec.Specification[*domain.Product]{within(scopeOf(ctx), domain.ProdFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.ProdFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if t := strings.TrimSpace(q.Text); t != "" {
			parts = append(parts, spec.Or[*domain.Product](domain.ProdFieldName.ContainsFold(t), domain.ProdFieldSKU.Eq(domain.NormalizeSKU(t))))
		}
		if q.Kind != "" {
			k, ok := domain.ParseKind(q.Kind)
			v.Require(ok, "kind", "enum", "good or service")
			parts = append(parts, domain.ProdFieldKind.Eq(int(k)))
		}
		if q.Category != "" {
			parts = append(parts, domain.ProdFieldCategory.Eq(domain.CategoryID{UUID: parseID(&v, "category", q.Category)}))
		}
		if b := strings.TrimSpace(q.Barcode); b != "" {
			parts = append(parts, domain.ProdFieldBarcodes.Any(domain.BarcodeFieldValue.Eq(b)))
		}
		if err := v.Err(); err != nil {
			return fw.Page[ProductDTO]{}, err
		}
		page, err := s.Products.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.ProdFieldSKU.Asc()))
		if err != nil {
			return fw.Page[ProductDTO]{}, err
		}
		return fw.MapPage(page, productDTO), nil
	})

	svc.CreateCategory = guard(PermProductUpdate, func(ctx context.Context, c CreateCategory) (CategoryDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		var parent domain.CategoryID
		if c.Parent != "" {
			parent = domain.CategoryID{UUID: parseID(&v, "parent", c.Parent)}
		}
		if err := v.Err(); err != nil {
			return CategoryDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return CategoryDTO{}, err
		}
		cat, err := domain.ReconstituteCategory(domain.NewCategoryID(), domain.CategoryState{Company: company, Code: c.Code, Name: c.Name, Parent: parent})
		if err != nil {
			return CategoryDTO{}, err
		}
		if !parent.IsZero() {
			p, err := s.Categories.Get(ctx, parent)
			if err != nil {
				return CategoryDTO{}, err
			}
			if p.State().Company != company {
				return CategoryDTO{}, fw.NotFound(domain.CategoryKind, parent)
			}
		}
		dup, err := s.Categories.Exists(ctx, spec.And(domain.CatFieldCompany.Eq(company), domain.CatFieldCode.Eq(cat.State().Code)))
		if err != nil {
			return CategoryDTO{}, err
		}
		if dup {
			return CategoryDTO{}, fw.Violation("products.duplicate_category", "the company already has that category code")
		}
		if err := s.categories.Create(ctx, cat); err != nil {
			return CategoryDTO{}, err
		}
		return categoryDTO(cat), nil
	}, pipeline.Transactional[CreateCategory, CategoryDTO](s.UoW))
	svc.RenameCategory = guard(PermProductUpdate, func(ctx context.Context, c RenameCategory) (CategoryDTO, error) {
		sc := scopeOf(ctx)
		cat, err := s.categories.Update(ctx, c.ID, func(_ context.Context, cat *domain.Category) error {
			if err := sc.check(domain.CategoryKind, cat.ID(), cat.State().Company, true); err != nil {
				return err
			}
			return cat.Rename(c.Name)
		})
		if err != nil {
			return CategoryDTO{}, err
		}
		return categoryDTO(cat), nil
	}, retry[RenameCategory, CategoryDTO]())
	svc.SearchCategories = guard(PermProductRead, func(ctx context.Context, q SearchCategories) ([]CategoryDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return nil, err
		}
		cs, err := s.Categories.Find(ctx, domain.CatFieldCompany.Eq(company), domain.CatFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []CategoryDTO{}
		for _, c := range cs {
			out = append(out, categoryDTO(c))
		}
		return out, nil
	})
}

// CreateCategory adds a category to the tree of a company.
type CreateCategory struct {
	Company string `json:"company"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	Parent  string `json:"parent,omitempty"`
}

// RenameCategory renames a category.
type RenameCategory struct {
	ID   domain.CategoryID `json:"-"`
	Name string            `json:"name"`
}

// SearchCategories lists the categories of a company.
type SearchCategories struct{ Company string }

// CategoryDTO is the transport form of a category.
type CategoryDTO struct {
	ID      string `json:"id"`
	Company string `json:"company"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	Parent  string `json:"parent,omitempty"`
}

func categoryDTO(c *domain.Category) CategoryDTO {
	s := c.State()
	d := CategoryDTO{ID: c.ID().String(), Company: s.Company.String(), Code: s.Code, Name: s.Name}
	if !s.Parent.IsZero() {
		d.Parent = s.Parent.String()
	}
	return d
}
