package infrastructure

import (
	"github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// The catalog of products, the agreements, and what ties an account to an agreement.
var catalogDDL = []string{
	`CREATE TABLE fin_products (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, product_code {str:30} NOT NULL,
	product_name {str:120} NOT NULL, description {str:500}, family {str:20} NOT NULL, regulatory_code {str:50}, discontinued_on {date}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_fin_products_code ON fin_products (company, product_code)`,
	`CREATE TABLE fin_agreements (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, customer {uuid} NOT NULL,
	agreement_number {str:40} NOT NULL, agreement_name {str:200} NOT NULL, description {str:500}, family {str:20} NOT NULL, product {uuid},
	signed_on {date} NOT NULL, from_day {date} NOT NULL, thru_day {date}, status {str:20} NOT NULL, reason {str:200}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_fin_agreements_number ON fin_agreements (company, agreement_number)`,
	`CREATE INDEX ix_fin_agreements_customer ON fin_agreements (company, customer, status)`,
	`ALTER TABLE fin_accounts {add:agreement} {uuid}{addEnd}`,
}

// ProductMapping maps Product to fin_products.
func ProductMapping() sqlrepo.Mapping[domain.ProductID, *domain.Product] {
	return sqlrepo.Mapping[domain.ProductID, *domain.Product]{
		Table:   "fin_products",
		Columns: sqlrepo.WithAuditColumns("company", "product_code", "product_name", "description", "family", "regulatory_code", "discontinued_on"),
		Dehydrate: func(p *domain.Product) (sqlrepo.Values, error) {
			s := p.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "product_code": s.Code, "product_name": s.Name, "description": opt(s.Description),
				"family": s.Family, "regulatory_code": opt(s.RegulatoryCode), "discontinued_on": optDate(s.Discontinued)}, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Product, error) {
			s := domain.ProductState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Code: r.String("product_code"), Name: r.String("product_name"),
				Description: r.String("description"), Family: r.String("family"), RegulatoryCode: r.String("regulatory_code"),
				Discontinued: r.Date("discontinued_on"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteProduct(domain.ProductID{UUID: r.UUID("id")}, s)
		},
	}
}

// AgreementMapping maps Agreement to fin_agreements.
func AgreementMapping() sqlrepo.Mapping[domain.AgreementID, *domain.Agreement] {
	return sqlrepo.Mapping[domain.AgreementID, *domain.Agreement]{
		Table: "fin_agreements",
		Columns: sqlrepo.WithAuditColumns("company", "customer", "agreement_number", "agreement_name", "description", "family", "product", "signed_on",
			"from_day", "thru_day", "status", "reason"),
		Dehydrate: func(a *domain.Agreement) (sqlrepo.Values, error) {
			s := a.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "customer": s.Customer, "agreement_number": s.Number, "agreement_name": s.Name,
				"description": opt(s.Description), "family": s.Family, "product": optUUID(s.Product.UUID), "signed_on": s.Signed, "from_day": s.From,
				"thru_day": optDate(s.Thru), "status": string(s.Status), "reason": opt(s.Reason)}, a.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Agreement, error) {
			s := domain.AgreementState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				Number: r.String("agreement_number"), Name: r.String("agreement_name"), Description: r.String("description"), Family: r.String("family"),
				Product: domain.ProductID{UUID: r.UUID("product")}, Signed: r.Date("signed_on"), From: r.Date("from_day"), Thru: r.Date("thru_day"),
				Status: domain.AgreementStatus(r.String("status")), Reason: r.String("reason"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteAgreement(domain.AgreementID{UUID: r.UUID("id")}, s)
		},
	}
}

// ProductRepositoryFactory builds the product repository.
func ProductRepositoryFactory(b hotswap.Backend) (domain.ProductRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, ProductMapping())
	case *memory.Store:
		return memory.NewRepository[domain.ProductID, *domain.Product](db), nil
	}
	return nil, unsupported(b)
}

// AgreementRepositoryFactory builds the agreement repository.
func AgreementRepositoryFactory(b hotswap.Backend) (domain.AgreementRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, AgreementMapping())
	case *memory.Store:
		return memory.NewRepository[domain.AgreementID, *domain.Agreement](db), nil
	}
	return nil, unsupported(b)
}
