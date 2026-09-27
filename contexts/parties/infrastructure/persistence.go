package infrastructure

import (
	"context"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// PartyMapping maps Party to parties + party_roles.
func PartyMapping() sqlrepo.Mapping[domain.PartyID, *domain.Party] {
	return sqlrepo.Mapping[domain.PartyID, *domain.Party]{
		Table: "parties",
		Columns: sqlrepo.WithAuditColumns("kind", "name", "given_name", "first_surname", "second_surname", "gender",
			"birth_date", "marital_status", "legal_name", "trade_name", "active", "test"),
		Dehydrate: func(p *domain.Party) (sqlrepo.Values, error) {
			v := sqlrepo.Values{"kind": p.Kind(), "name": p.Name(), "active": p.IsActive(), "test": p.IsTest(),
				"given_name": nil, "first_surname": nil, "second_surname": nil, "gender": nil, "birth_date": nil,
				"marital_status": nil, "legal_name": nil, "trade_name": nil}
			if p.Kind() == domain.KindPerson {
				d := p.Person()
				v["given_name"], v["first_surname"], v["second_surname"] = d.Name.Given(), d.Name.FirstSurname(), nullable(d.Name.SecondSurname())
				v["gender"], v["marital_status"] = nullable(string(d.Gender)), nullable(string(d.MaritalStatus))
				if !d.BirthDate.IsZero() {
					v["birth_date"] = d.BirthDate
				}
			} else {
				n := p.Organization().Name
				v["legal_name"], v["trade_name"] = n.Legal(), nullable(n.Trade())
			}
			return sqlrepo.AuditStampValues(v, p.AuditStamp()), nil
		},
		Hydrate: func(row *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Party, error) {
			s := domain.PartyState{Kind: domain.Kind(row.String("kind")), Active: row.Bool("active"),
				Test: row.Bool("test"), Audit: row.AuditStamp()}
			switch s.Kind {
			case domain.KindPerson:
				n, err := domain.NewPersonalName(row.String("given_name"), row.String("first_surname"), row.String("second_surname"))
				if err != nil {
					return nil, err
				}
				g, err := domain.ParseGender(row.String("gender"))
				if err != nil {
					return nil, err
				}
				m, err := domain.ParseMaritalStatus(row.String("marital_status"))
				if err != nil {
					return nil, err
				}
				s.Person = domain.PersonDetails{Name: n, Gender: g, BirthDate: row.Date("birth_date"), MaritalStatus: m}
			case domain.KindOrganization:
				n, err := domain.NewOrganizationName(row.String("legal_name"), row.String("trade_name"))
				if err != nil {
					return nil, err
				}
				s.Organization = domain.OrganizationDetails{Name: n}
			}
			for _, c := range children.Of("roles") {
				period, err := vocab.NewValidPeriod(c.Time("valid_from"), c.NullTime("valid_to"))
				if err != nil {
					return nil, err
				}
				s.Roles = append(s.Roles, domain.PartyRole{ID: domain.PartyRoleID{UUID: c.UUID("id")},
					RoleType: domain.RoleTypeID{UUID: c.UUID("role_type")}, Period: period})
			}
			if err := row.Err(); err != nil {
				return nil, err
			}
			return domain.Reconstitute(domain.PartyID{UUID: row.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Party]{{
			Name:       "roles",
			Table:      "party_roles",
			ForeignKey: "party_id",
			Columns:    []string{"id", "role_type", "valid_from", "valid_to"},
			OrderBy:    []string{"valid_from", "id"},
			Dehydrate: func(p *domain.Party) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, r := range p.Roles() {
					var to any
					if t, ok := r.Period.To(); ok {
						to = t
					}
					out = append(out, sqlrepo.Values{"id": r.ID, "role_type": r.RoleType, "valid_from": r.Period.From(), "valid_to": to})
				}
				return out, nil
			},
		}},
	}
}

// RelationshipMapping maps Relationship to party_relationships.
func RelationshipMapping() sqlrepo.Mapping[domain.RelationshipID, *domain.Relationship] {
	return sqlrepo.Mapping[domain.RelationshipID, *domain.Relationship]{
		Table: "party_relationships",
		Columns: sqlrepo.WithAuditColumns("rel_type", "from_party", "to_party", "from_role", "to_role",
			"valid_from", "valid_to", "remark"),
		Fields: map[string]string{"type": "rel_type"},
		Dehydrate: func(r *domain.Relationship) (sqlrepo.Values, error) {
			return sqlrepo.AuditStampValues(sqlrepo.Values{"rel_type": r.Type(), "from_party": r.From(), "to_party": r.To(),
				"from_role": r.FromRole(), "to_role": r.ToRole(), "valid_from": r.Since(), "valid_to": r.Until(),
				"remark": nullable(r.Remark())}, r.AuditStamp()), nil
		},
		Hydrate: func(row *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Relationship, error) {
			period, err := vocab.NewValidPeriod(row.Time("valid_from"), row.NullTime("valid_to"))
			if err != nil {
				return nil, err
			}
			s := domain.RelationshipState{
				Type: domain.RelationshipTypeID{UUID: row.UUID("rel_type")},
				From: domain.PartyID{UUID: row.UUID("from_party")}, To: domain.PartyID{UUID: row.UUID("to_party")},
				FromRole: domain.RoleTypeID{UUID: row.UUID("from_role")}, ToRole: domain.RoleTypeID{UUID: row.UUID("to_role")},
				Period: period, Remark: row.String("remark"), Audit: row.AuditStamp(),
			}
			if err := row.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteRelationship(domain.RelationshipID{UUID: row.UUID("id")}, s)
		},
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---------------------------------------------------------------------------------------------
// Catalogs
// ---------------------------------------------------------------------------------------------

// SQLCatalogs reads the role and relationship type catalogs from the database.
type SQLCatalogs struct{ db *sqlrepo.DB }

var _ domain.Catalogs = SQLCatalogs{}

// RoleTypes implements domain.Catalogs.
func (c SQLCatalogs) RoleTypes(ctx context.Context) ([]domain.RoleType, error) {
	rows, err := c.db.Select(ctx, "role_types", []string{"id", "name", "description", "parent_id", "category"}, "name")
	if err != nil {
		return nil, err
	}
	out := make([]domain.RoleType, 0, len(rows))
	for _, r := range rows {
		name, err := vocab.NewName(r.String("name"))
		if err != nil {
			return nil, err
		}
		t := domain.RoleType{ID: domain.RoleTypeID{UUID: r.UUID("id")}, Name: name,
			Description: r.String("description"), Category: r.Bool("category")}
		if !r.IsNull("parent_id") {
			p := domain.RoleTypeID{UUID: r.UUID("parent_id")}
			t.Parent = &p
		}
		if err := r.Err(); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// RelationshipTypes implements domain.Catalogs.
func (c SQLCatalogs) RelationshipTypes(ctx context.Context) ([]domain.RelationshipType, error) {
	rows, err := c.db.Select(ctx, "relationship_types", []string{"id", "name", "description", "from_role", "to_role"}, "name")
	if err != nil {
		return nil, err
	}
	out := make([]domain.RelationshipType, 0, len(rows))
	for _, r := range rows {
		name, err := vocab.NewName(r.String("name"))
		if err != nil {
			return nil, err
		}
		t := domain.RelationshipType{ID: domain.RelationshipTypeID{UUID: r.UUID("id")}, Name: name, Description: r.String("description"),
			FromRole: domain.RoleTypeID{UUID: r.UUID("from_role")}, ToRole: domain.RoleTypeID{UUID: r.UUID("to_role")}}
		if err := r.Err(); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// WellKnownCatalogs serves the seed catalogs from memory (in-memory backend and tests).
type WellKnownCatalogs struct{}

// RoleTypes implements domain.Catalogs.
func (WellKnownCatalogs) RoleTypes(context.Context) ([]domain.RoleType, error) {
	return domain.WellKnownRoleTypes(), nil
}

// RelationshipTypes implements domain.Catalogs.
func (WellKnownCatalogs) RelationshipTypes(context.Context) ([]domain.RelationshipType, error) {
	return domain.WellKnownRelationshipTypes(), nil
}

// ---------------------------------------------------------------------------------------------
// Hot-swap factories: every port of the context for any backend
// ---------------------------------------------------------------------------------------------

func unsupported(b hotswap.Backend) error { return fmt.Errorf("parties: unsupported backend %T", b) }

// PartyRepositoryFactory builds the Party repository.
func PartyRepositoryFactory(b hotswap.Backend) (domain.PartyRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, PartyMapping())
	case *memory.Store:
		return memory.NewRepository[domain.PartyID, *domain.Party](db), nil
	}
	return nil, unsupported(b)
}

// RelationshipRepositoryFactory builds the Relationship repository.
func RelationshipRepositoryFactory(b hotswap.Backend) (domain.RelationshipRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, RelationshipMapping())
	case *memory.Store:
		return memory.NewRepository[domain.RelationshipID, *domain.Relationship](db), nil
	}
	return nil, unsupported(b)
}

// CatalogsFor returns the catalog reader of a backend.
func CatalogsFor(b hotswap.Backend) (domain.Catalogs, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return SQLCatalogs{db: db}, nil
	case *memory.Store:
		return WellKnownCatalogs{}, nil
	}
	return nil, unsupported(b)
}

// SwappableCatalogs returns catalogs that follow the backend of the Switch.
func SwappableCatalogs(sw *hotswap.Switch) domain.Catalogs {
	return swappableCatalogs{b: hotswap.Bind(sw, CatalogsFor)}
}

type swappableCatalogs struct {
	b *hotswap.Binding[domain.Catalogs]
}

func (c swappableCatalogs) RoleTypes(ctx context.Context) (out []domain.RoleType, err error) {
	err = c.b.With(ctx, func(ctx context.Context, x domain.Catalogs) error { out, err = x.RoleTypes(ctx); return err })
	return out, err
}

func (c swappableCatalogs) RelationshipTypes(ctx context.Context) (out []domain.RelationshipType, err error) {
	err = c.b.With(ctx, func(ctx context.Context, x domain.Catalogs) error { out, err = x.RelationshipTypes(ctx); return err })
	return out, err
}

// OutboxFactory builds the domain event outbox.
func OutboxFactory(b hotswap.Backend) (application.OutboxStore, error) {
	return outboxFor(b, TablePartiesOutbox)
}

// IntegrationOutboxFactory builds the integration outbox (Published Language).
func IntegrationOutboxFactory(b hotswap.Backend) (application.OutboxStore, error) {
	return outboxFor(b, TableIntegrationOutbox)
}

func outboxFor(b hotswap.Backend, table string) (application.OutboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewOutbox(db, table)
	case *memory.Store:
		return memory.NewOutbox(db), nil
	}
	return nil, unsupported(b)
}

// AuditLogFactory builds the audit log.
func AuditLogFactory(b hotswap.Backend) (application.AuditLog, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewAuditLog(db, TableAuditLog)
	case *memory.Store:
		return memory.NewAuditLog(db), nil
	}
	return nil, unsupported(b)
}
