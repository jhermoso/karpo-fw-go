package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
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
			"birth_date", "marital_status", "legal_name", "trade_name", "legal_form", "active", "test", "shared"),
		Dehydrate: func(p *domain.Party) (sqlrepo.Values, error) {
			v := sqlrepo.Values{"kind": p.Kind(), "name": p.Name(), "active": p.IsActive(), "test": p.IsTest(),
				"shared": p.IsShared(), "legal_form": nil,
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
				v["legal_form"] = nullable(string(p.Organization().LegalForm))
			}
			return sqlrepo.AuditStampValues(v, p.AuditStamp()), nil
		},
		Hydrate: func(row *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Party, error) {
			s := domain.PartyState{Kind: domain.Kind(row.String("kind")), Active: row.Bool("active"),
				Test: row.Bool("test"), Shared: row.Bool("shared"), Audit: row.AuditStamp()}
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
				form, err := domain.ParseLegalForm(row.String("legal_form"))
				if err != nil {
					return nil, err
				}
				s.Organization = domain.OrganizationDetails{Name: n, LegalForm: form}
			}
			for _, c := range children.Of("identifications") {
				country, err := vocab.NewCountryCode(c.String("country"))
				if err != nil {
					return nil, err
				}
				s.Identities = append(s.Identities, domain.Identification{ID: domain.IdentificationID{UUID: c.UUID("id")},
					Type: domain.DocumentTypeID{UUID: c.UUID("doc_type")}, Country: country, Number: c.String("doc_number"),
					IssuingAuthority: c.String("issuing_authority"), IssuedOn: c.Date("issued_on"), ExpiresOn: c.Date("expires_on"),
					Primary: c.Bool("is_primary")})
			}
			// UUID v7 ids are chronological, but SQL Server sorts UNIQUEIDENTIFIER by byte groups:
			// order in Go so every engine returns the same order.
			slices.SortFunc(s.Identities, func(a, b domain.Identification) int { return bytes.Compare(a.ID.Bytes(), b.ID.Bytes()) })
			for _, c := range children.Of("contacts") {
				contact, err := hydrateContact(c)
				if err != nil {
					return nil, err
				}
				s.Contacts = append(s.Contacts, contact)
			}
			for _, c := range children.Of("affiliations") {
				period, err := vocab.NewValidPeriod(c.Time("valid_from"), c.NullTime("valid_to"))
				if err != nil {
					return nil, err
				}
				s.Affiliations = append(s.Affiliations, domain.Affiliation{Organization: domain.PartyID{UUID: c.UUID("organization")},
					Relationship: domain.RelationshipID{UUID: c.UUID("relationship_id")}, Period: period})
			}
			for _, c := range children.Of("classifications") {
				period, err := vocab.NewValidPeriod(c.Time("valid_from"), c.NullTime("valid_to"))
				if err != nil {
					return nil, err
				}
				s.Classes = append(s.Classes, domain.Classification{ID: domain.ClassificationID{UUID: c.UUID("id")},
					Type: domain.ClassificationTypeID{UUID: c.UUID("class_type")}, Period: period})
			}
			for _, c := range children.Of("roles") {
				period, err := vocab.NewValidPeriod(c.Time("valid_from"), c.NullTime("valid_to"))
				if err != nil {
					return nil, err
				}
				s.Roles = append(s.Roles, domain.PartyRole{ID: domain.PartyRoleID{UUID: c.UUID("id")},
					RoleType: domain.RoleTypeID{UUID: c.UUID("role_type")}, Period: period})
			}
			byStart := func(fa, fb time.Time, ia, ib []byte) int {
				if c := fa.Compare(fb); c != 0 {
					return c
				}
				return bytes.Compare(ia, ib)
			}
			slices.SortFunc(s.Roles, func(a, b domain.PartyRole) int {
				return byStart(a.Period.From(), b.Period.From(), a.ID.Bytes(), b.ID.Bytes())
			})
			slices.SortFunc(s.Contacts, func(a, b domain.Contact) int {
				return byStart(a.Period.From(), b.Period.From(), a.ID.Bytes(), b.ID.Bytes())
			})
			slices.SortFunc(s.Classes, func(a, b domain.Classification) int {
				return byStart(a.Period.From(), b.Period.From(), a.ID.Bytes(), b.ID.Bytes())
			})
			if err := row.Err(); err != nil {
				return nil, err
			}
			return domain.Reconstitute(domain.PartyID{UUID: row.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Party]{{
			Name:       "affiliations",
			Table:      "party_affiliations",
			ForeignKey: "party_id",
			Columns:    []string{"relationship_id", "organization", "valid_from", "valid_to"},
			OrderBy:    []string{"valid_from", "relationship_id"},
			Dehydrate: func(p *domain.Party) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, a := range p.Affiliations() {
					var to any
					if t, ok := a.Period.To(); ok {
						to = t
					}
					out = append(out, sqlrepo.Values{"relationship_id": a.Relationship, "organization": a.Organization,
						"valid_from": a.Period.From(), "valid_to": to})
				}
				return out, nil
			},
		}, {
			Name:       "identifications",
			Table:      "party_identifications",
			ForeignKey: "party_id",
			Columns:    []string{"id", "doc_type", "country", "doc_number", "issuing_authority", "issued_on", "expires_on", "is_primary"},
			Fields:     map[string]string{"number": "doc_number"},
			OrderBy:    []string{"id"},
			Dehydrate: func(p *domain.Party) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, i := range p.Identifications() {
					out = append(out, sqlrepo.Values{"id": i.ID, "doc_type": i.Type, "country": i.Country.String(), "doc_number": i.Number,
						"issuing_authority": nullable(i.IssuingAuthority), "issued_on": optDate(i.IssuedOn),
						"expires_on": optDate(i.ExpiresOn), "is_primary": i.Primary})
				}
				return out, nil
			},
		}, {
			Name:       "contacts",
			Table:      "party_contacts",
			ForeignKey: "party_id",
			Columns: []string{"id", "kind", "contact_value", "street_type", "line1", "line2", "directions", "postal_code",
				"locality", "region", "country", "geo_postal_code", "geo_boundary", "purposes", "non_solicitation", "valid_from", "valid_to"},
			Fields:  map[string]string{"value": "contact_value"},
			OrderBy: []string{"valid_from", "id"},
			Dehydrate: func(p *domain.Party) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, c := range p.Contacts() {
					a := c.Address
					v := sqlrepo.Values{"id": c.ID, "kind": c.Kind, "contact_value": nullable(c.Value), "street_type": nullable(a.StreetType),
						"line1": nullable(a.Line1), "line2": nullable(a.Line2), "directions": nullable(a.Directions),
						"postal_code": nullable(a.PostalCode), "locality": nullable(a.Locality), "region": nullable(a.Region),
						"country": nullable(a.Country.String()), "geo_postal_code": optUUID(a.Geo.PostalCode),
						"geo_boundary": optUUID(a.Geo.Boundary), "purposes": nullable(joinPurposes(c.Purposes)),
						"non_solicitation": c.NonSolicitation, "valid_from": c.Period.From(), "valid_to": nil}
					if t, ok := c.Period.To(); ok {
						v["valid_to"] = t
					}
					out = append(out, v)
				}
				return out, nil
			},
		}, {
			Name:       "classifications",
			Table:      "party_classifications",
			ForeignKey: "party_id",
			Columns:    []string{"id", "class_type", "valid_from", "valid_to"},
			OrderBy:    []string{"valid_from", "id"},
			Dehydrate: func(p *domain.Party) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, c := range p.Classifications() {
					var to any
					if t, ok := c.Period.To(); ok {
						to = t
					}
					out = append(out, sqlrepo.Values{"id": c.ID, "class_type": c.Type, "valid_from": c.Period.From(), "valid_to": to})
				}
				return out, nil
			},
		}, {
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

func hydrateContact(c *sqlrepo.Row) (domain.Contact, error) {
	period, err := vocab.NewValidPeriod(c.Time("valid_from"), c.NullTime("valid_to"))
	if err != nil {
		return domain.Contact{}, err
	}
	out := domain.Contact{ID: domain.ContactID{UUID: c.UUID("id")}, Kind: domain.ContactKind(c.String("kind")),
		Value: c.String("contact_value"), NonSolicitation: c.Bool("non_solicitation"), Period: period}
	if out.Kind == domain.ContactPostal {
		country, err := vocab.NewCountryCode(c.String("country"))
		if err != nil {
			return domain.Contact{}, err
		}
		out.Address = domain.PostalAddress{StreetType: c.String("street_type"), Line1: c.String("line1"), Line2: c.String("line2"),
			Directions: c.String("directions"), PostalCode: c.String("postal_code"), Locality: c.String("locality"),
			Region: c.String("region"), Country: country}
		if !c.IsNull("geo_postal_code") {
			out.Address.Geo.PostalCode = c.UUID("geo_postal_code")
		}
		if !c.IsNull("geo_boundary") {
			out.Address.Geo.Boundary = c.UUID("geo_boundary")
		}
	}
	if p := c.String("purposes"); p != "" {
		for _, x := range strings.Split(p, ",") {
			out.Purposes = append(out.Purposes, domain.Purpose(x))
		}
	}
	return out, c.Err()
}

func joinPurposes(ps []domain.Purpose) string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return strings.Join(out, ",")
}

func optDate(d vocab.Date) any {
	if d.IsZero() {
		return nil
	}
	return d
}

func optUUID(u fw.UUID) any {
	if u.IsZero() {
		return nil
	}
	return u
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
	rows, err := c.db.Select(ctx, "relationship_types", []string{"id", "name", "description", "from_role", "to_role", "hierarchical"}, "name")
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
			FromRole: domain.RoleTypeID{UUID: r.UUID("from_role")}, ToRole: domain.RoleTypeID{UUID: r.UUID("to_role")},
			Hierarchical: r.Bool("hierarchical")}
		if err := r.Err(); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// DocumentTypes implements domain.Catalogs.
func (c SQLCatalogs) DocumentTypes(ctx context.Context) ([]domain.DocumentType, error) {
	rows, err := c.db.Select(ctx, "document_types", []string{"id", "code", "name", "default_pattern", "requires_expiry",
		"requires_authority", "active"}, "code")
	if err != nil {
		return nil, err
	}
	out := make([]domain.DocumentType, 0, len(rows))
	for _, r := range rows {
		name, err := vocab.NewName(r.String("name"))
		if err != nil {
			return nil, err
		}
		out = append(out, domain.DocumentType{ID: domain.DocumentTypeID{UUID: r.UUID("id")}, Code: r.String("code"), Name: name,
			DefaultPattern: r.String("default_pattern"), RequiresExpiry: r.Bool("requires_expiry"),
			RequiresIssuingAuthority: r.Bool("requires_authority"), Active: r.Bool("active")})
		if err := r.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// CountryDocumentRules implements domain.Catalogs.
func (c SQLCatalogs) CountryDocumentRules(ctx context.Context) ([]domain.CountryDocumentRule, error) {
	rows, err := c.db.Select(ctx, "country_document_rules", []string{"id", "country", "doc_type", "available", "is_default",
		"display_order", "pattern", "min_length", "max_length", "check_digit", "requires_expiry", "requires_authority", "active"}, "id")
	if err != nil {
		return nil, err
	}
	optBool := func(r *sqlrepo.Row, col string) *bool {
		if r.IsNull(col) {
			return nil
		}
		b := r.Bool(col)
		return &b
	}
	out := make([]domain.CountryDocumentRule, 0, len(rows))
	for _, r := range rows {
		country, err := vocab.NewCountryCode(r.String("country"))
		if err != nil {
			return nil, err
		}
		out = append(out, domain.CountryDocumentRule{ID: r.String("id"), Country: country,
			DocumentType: domain.DocumentTypeID{UUID: r.UUID("doc_type")}, Available: r.Bool("available"), Default: r.Bool("is_default"),
			DisplayOrder: int(r.Int64("display_order")), Pattern: r.String("pattern"), MinLength: int(r.Int64("min_length")),
			MaxLength: int(r.Int64("max_length")), CheckDigit: r.String("check_digit"), RequiresExpiry: optBool(r, "requires_expiry"),
			RequiresIssuingAuthority: optBool(r, "requires_authority"), Active: r.Bool("active")})
		if err := r.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ClassificationTypes implements domain.Catalogs. Families are returned before their leaves.
func (c SQLCatalogs) ClassificationTypes(ctx context.Context) ([]domain.ClassificationType, error) {
	rows, err := c.db.Select(ctx, "classification_types", []string{"id", "name", "description", "family_id", "active",
		"applies_to", "exclusive_family"}, "id")
	if err != nil {
		return nil, err
	}
	var families, leaves []domain.ClassificationType
	for _, r := range rows {
		name, err := vocab.NewName(r.String("name"))
		if err != nil {
			return nil, err
		}
		t := domain.ClassificationType{ID: domain.ClassificationTypeID{UUID: r.UUID("id")}, Name: name,
			Description: r.String("description"), Active: r.Bool("active"),
			AppliesTo: domain.Applicability(r.String("applies_to")), Exclusive: r.Bool("exclusive_family")}
		if !r.IsNull("family_id") {
			f := domain.ClassificationTypeID{UUID: r.UUID("family_id")}
			t.Family = &f
			leaves = append(leaves, t)
		} else {
			families = append(families, t)
		}
		if err := r.Err(); err != nil {
			return nil, err
		}
	}
	return append(families, leaves...), nil
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

// DocumentTypes implements domain.Catalogs.
func (WellKnownCatalogs) DocumentTypes(context.Context) ([]domain.DocumentType, error) {
	return domain.WellKnownDocumentTypes(), nil
}

// CountryDocumentRules implements domain.Catalogs.
func (WellKnownCatalogs) CountryDocumentRules(context.Context) ([]domain.CountryDocumentRule, error) {
	return domain.WellKnownCountryDocumentRules(), nil
}

// ClassificationTypes implements domain.Catalogs.
func (WellKnownCatalogs) ClassificationTypes(context.Context) ([]domain.ClassificationType, error) {
	return domain.WellKnownClassificationTypes(), nil
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

func (c swappableCatalogs) DocumentTypes(ctx context.Context) (out []domain.DocumentType, err error) {
	err = c.b.With(ctx, func(ctx context.Context, x domain.Catalogs) error { out, err = x.DocumentTypes(ctx); return err })
	return out, err
}

func (c swappableCatalogs) CountryDocumentRules(ctx context.Context) (out []domain.CountryDocumentRule, err error) {
	err = c.b.With(ctx, func(ctx context.Context, x domain.Catalogs) error { out, err = x.CountryDocumentRules(ctx); return err })
	return out, err
}

func (c swappableCatalogs) ClassificationTypes(ctx context.Context) (out []domain.ClassificationType, err error) {
	err = c.b.With(ctx, func(ctx context.Context, x domain.Catalogs) error { out, err = x.ClassificationTypes(ctx); return err })
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
