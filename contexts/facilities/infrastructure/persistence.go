// Package infrastructure stores the Facilities context: SQL mapping, versioned schema of the five
// engines (with the facility type seed), catalogs, hot-swap factories and the Geography adapter.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	geo "github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

// Context is the name of the bounded context.
const Context = "facilities"

// Technical tables of the context.
const (
	TableOutbox            = "facilities_outbox"
	TableIntegrationOutbox = "facilities_integration_outbox"
	TableAuditLog          = "facilities_audit_log"
)

var schemaDDL = []string{
	`CREATE TABLE fac_facility_types (id {uuid} NOT NULL PRIMARY KEY, name {str:100} NOT NULL, description {str:500},
	requires_contact {bool} NOT NULL, active {bool} NOT NULL)`,
	`CREATE TABLE fac_facilities (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, organization {uuid} NOT NULL,
	facility_type {uuid} NOT NULL, name {str:200} NOT NULL, description {str:1000}, part_of {uuid}, area_m2 {str:30},
	street_type {str:10}, line1 {str:200}, line2 {str:200}, postal_code {str:20}, locality {str:200}, region {str:200},
	country {str:2}, geo_postal_code {uuid}, geo_boundary {uuid}, phone {str:20}, email {str:320}, active {bool} NOT NULL,
	created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200},
	FOREIGN KEY (facility_type) REFERENCES fac_facility_types (id), FOREIGN KEY (part_of) REFERENCES fac_facilities (id))`,
	`CREATE INDEX ix_fac_facilities_org ON fac_facilities (organization)`,
	`CREATE INDEX ix_fac_facilities_part_of ON fac_facilities (part_of)`,
}

func technicalDDL(d string) []string {
	switch d {
	case "sqlite":
		return slices.Concat(sqlite.OutboxDDL(TableOutbox), sqlite.OutboxDDL(TableIntegrationOutbox), sqlite.AuditDDL(TableAuditLog))
	case "postgres":
		return slices.Concat(postgres.OutboxDDL(TableOutbox), postgres.OutboxDDL(TableIntegrationOutbox), postgres.AuditDDL(TableAuditLog))
	case "sqlserver":
		return slices.Concat(sqlserver.OutboxDDL(TableOutbox), sqlserver.OutboxDDL(TableIntegrationOutbox), sqlserver.AuditDDL(TableAuditLog))
	case "oracle":
		return slices.Concat(oracle.OutboxDDL(TableOutbox), oracle.OutboxDDL(TableIntegrationOutbox), oracle.AuditDDL(TableAuditLog))
	case "mysql":
		return slices.Concat(mysql.OutboxDDL(TableOutbox), mysql.OutboxDDL(TableIntegrationOutbox), mysql.AuditDDL(TableAuditLog))
	}
	return nil
}

// Migrations is the versioned schema of the context. The facilities of each client are
// business data (imported), not seed; only the type catalog is seeded.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "facilities", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
		{Version: 3, Name: "facility types", Run: func(ctx context.Context, db *sqlrepo.DB) error {
			var rows [][]any
			for _, t := range domain.WellKnownFacilityTypes() {
				rows = append(rows, []any{t.ID, t.Name, t.Description, t.RequiresContact, t.Active})
			}
			return db.InsertMany(ctx, "fac_facility_types", []string{"id", "name", "description", "requires_contact", "active"}, rows)
		}},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// DropAll removes the tables of the context and its migration history (tests only).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range []string{"fac_facilities", "fac_facility_types", TableOutbox, TableIntegrationOutbox, TableAuditLog} {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+t)
	}
	_, _ = db.ExecContext(ctx, "DELETE FROM "+sqlrepo.DefaultMigrationsTable+" WHERE context = '"+Context+"'")
}

func opt(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optUUID(u fw.UUID) any {
	if u.IsZero() {
		return nil
	}
	return u
}

// FacilityMapping maps Facility to fac_facilities.
func FacilityMapping() sqlrepo.Mapping[domain.FacilityID, *domain.Facility] {
	return sqlrepo.Mapping[domain.FacilityID, *domain.Facility]{
		Table: "fac_facilities",
		Columns: sqlrepo.WithAuditColumns("organization", "facility_type", "name", "description", "part_of", "area_m2", "street_type",
			"line1", "line2", "postal_code", "locality", "region", "country", "geo_postal_code", "geo_boundary", "phone", "email", "active"),
		Fields: map[string]string{"type": "facility_type"},
		Dehydrate: func(f *domain.Facility) (sqlrepo.Values, error) {
			l := f.Location()
			a := l.Address
			var partOf, area any
			if p := f.PartOf(); p != nil {
				partOf = *p
			}
			if !f.Area().IsZero() {
				area = f.Area().String()
			}
			return sqlrepo.AuditStampValues(sqlrepo.Values{"organization": f.Owner(), "facility_type": f.Type(), "name": f.Name(),
				"description": opt(f.Description()), "part_of": partOf, "area_m2": area, "street_type": opt(a.StreetType),
				"line1": opt(a.Line1), "line2": opt(a.Line2), "postal_code": opt(a.PostalCode), "locality": opt(a.Locality),
				"region": opt(a.Region), "country": opt(a.Country.String()), "geo_postal_code": optUUID(a.Geo.PostalCode),
				"geo_boundary": optUUID(a.Geo.Boundary), "phone": opt(l.Phone.String()), "email": opt(l.Email.String()),
				"active": f.IsActive()}, f.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Facility, error) {
			var v fw.Validation
			s := domain.FacilityState{Owner: domain.OrganizationID{UUID: r.UUID("organization")},
				Type: domain.FacilityTypeID{UUID: r.UUID("facility_type")}, Name: r.String("name"), Description: r.String("description"),
				Active: r.Bool("active"), Audit: r.AuditStamp()}
			if !r.IsNull("part_of") {
				p := domain.FacilityID{UUID: r.UUID("part_of")}
				s.PartOf = &p
			}
			if a := r.String("area_m2"); a != "" {
				d, err := vocab.ParseDecimal(a)
				v.Merge("area_m2", err)
				s.Area = d
			}
			addr := domain.Address{StreetType: r.String("street_type"), Line1: r.String("line1"), Line2: r.String("line2"),
				PostalCode: r.String("postal_code"), Locality: r.String("locality"), Region: r.String("region")}
			if c := r.String("country"); c != "" {
				cc, err := vocab.NewCountryCode(c)
				v.Merge("country", err)
				addr.Country = cc
			}
			if !r.IsNull("geo_postal_code") {
				addr.Geo.PostalCode = r.UUID("geo_postal_code")
			}
			if !r.IsNull("geo_boundary") {
				addr.Geo.Boundary = r.UUID("geo_boundary")
			}
			s.Location.Address = addr
			if p := r.String("phone"); p != "" {
				ph, err := vocab.NewPhone(p)
				v.Merge("phone", err)
				s.Location.Phone = ph
			}
			if e := r.String("email"); e != "" {
				em, err := vocab.NewEmail(e)
				v.Merge("email", err)
				s.Location.Email = em
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			if err := v.Err(); err != nil {
				return nil, err
			}
			return domain.Reconstitute(domain.FacilityID{UUID: r.UUID("id")}, s)
		},
	}
}

// SQLCatalogs reads the facility types from the database.
type SQLCatalogs struct{ db *sqlrepo.DB }

// FacilityTypes implements domain.Catalogs.
func (c SQLCatalogs) FacilityTypes(ctx context.Context) ([]domain.FacilityType, error) {
	rows, err := c.db.Select(ctx, "fac_facility_types", []string{"id", "name", "description", "requires_contact", "active"}, "name")
	if err != nil {
		return nil, err
	}
	out := make([]domain.FacilityType, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.FacilityType{ID: domain.FacilityTypeID{UUID: r.UUID("id")}, Name: r.String("name"),
			Description: r.String("description"), RequiresContact: r.Bool("requires_contact"), Active: r.Bool("active")})
		if err := r.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// WellKnownCatalogs serves the seed (in-memory backend).
type WellKnownCatalogs struct{}

// FacilityTypes implements domain.Catalogs.
func (WellKnownCatalogs) FacilityTypes(context.Context) ([]domain.FacilityType, error) {
	return domain.WellKnownFacilityTypes(), nil
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("facilities: unsupported backend %T", b) }

// RepositoryFactory builds the facility repository.
func RepositoryFactory(b hotswap.Backend) (domain.Repository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, FacilityMapping())
	case *memory.Store:
		return memory.NewRepository[domain.FacilityID, *domain.Facility](db), nil
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

// OutboxFactory builds the domain event outbox; IntegrationOutboxFactory, the Published Language one.
func OutboxFactory(b hotswap.Backend) (application.OutboxStore, error) { return outbox(b, TableOutbox) }

// IntegrationOutboxFactory builds the integration outbox.
func IntegrationOutboxFactory(b hotswap.Backend) (application.OutboxStore, error) {
	return outbox(b, TableIntegrationOutbox)
}

func outbox(b hotswap.Backend, table string) (application.OutboxStore, error) {
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

// GeographyAddresses adapts the Geography AddressChecker to the Facilities port (ACL).
type GeographyAddresses struct{ Checker geo.AddressChecker }

// CheckAddress validates the postal code for the country and its municipality, completing the
// Geography references.
func (g GeographyAddresses) CheckAddress(ctx context.Context, a domain.Address) (domain.Address, error) {
	q := geo.PostalAddressQuery{Country: a.Country.String(), PostalCode: a.PostalCode}
	if !a.Geo.Boundary.IsZero() {
		q.Boundary = a.Geo.Boundary.String()
	}
	res, err := g.Checker.CheckPostalAddress(ctx, q)
	if err != nil {
		return domain.Address{}, err
	}
	a.PostalCode = res.PostalCode
	if res.PostalCodeID != "" {
		a.Geo.PostalCode = fw.MustParseUUID(res.PostalCodeID)
	}
	if a.Geo.Boundary.IsZero() && res.Boundary != "" {
		a.Geo.Boundary = fw.MustParseUUID(res.Boundary)
	}
	return a, nil
}
