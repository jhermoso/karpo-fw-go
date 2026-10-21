// Package infrastructure persiste el contexto Facilities: mapeo SQL, esquema versionado de los
// cinco motores (con la semilla de tipos de instalación), catálogos, factorías intercambiables en caliente y el
// adaptador de Geography.
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

// Context es el nombre del contexto delimitado.
const Context = "facilities"

// Tablas técnicas del contexto.
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

// Migrations es el esquema versionado del contexto. Las instalaciones de cada cliente son datos de
// negocio (importados), no semilla; sólo se siembra el catálogo de tipos.
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
		// En qué punto de la calle está la dirección, pieza a pieza (vocab.Premises), como en
		// Parties.
		{Version: 4, Name: "address: number, block, portal, staircase, floor and door", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE fac_facilities {add:addr_number} {str:10}{addEnd}`,
			`ALTER TABLE fac_facilities {add:addr_qualifier} {str:10}{addEnd}`,
			`ALTER TABLE fac_facilities {add:addr_block} {str:10}{addEnd}`,
			`ALTER TABLE fac_facilities {add:addr_portal} {str:10}{addEnd}`,
			`ALTER TABLE fac_facilities {add:addr_staircase} {str:10}{addEnd}`,
			`ALTER TABLE fac_facilities {add:addr_floor} {str:10}{addEnd}`,
			`ALTER TABLE fac_facilities {add:addr_door} {str:10}{addEnd}`)},
		{Version: 5, Name: "decimal columns", Run: decimalColumnsV5.Run},
	}}
}

// Migrator devuelve el migrador del esquema del contexto.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// DropAll quita las tablas del contexto y su historial de migraciones (sólo pruebas).
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

// FacilityMapping mapea Facility a fac_facilities.
func FacilityMapping() sqlrepo.Mapping[domain.FacilityID, *domain.Facility] {
	return sqlrepo.Mapping[domain.FacilityID, *domain.Facility]{
		Table:    "fac_facilities",
		Decimals: decimalColumns.Of("fac_facilities"),
		Columns: sqlrepo.WithAuditColumns("organization", "facility_type", "name", "description", "part_of", "area_m2", "street_type",
			"line1", "line2", "postal_code", "locality", "region", "country", "geo_postal_code", "geo_boundary", "phone", "email", "active",
			"addr_number", "addr_qualifier", "addr_block", "addr_portal", "addr_staircase", "addr_floor", "addr_door"),
		Fields: map[string]string{"type": "facility_type"},
		Dehydrate: func(f *domain.Facility) (sqlrepo.Values, error) {
			l := f.Location()
			a := l.Address
			var partOf, area any
			if p := f.PartOf(); p != nil {
				partOf = *p
			}
			if !f.Area().IsZero() {
				area = f.Area()
			}
			return sqlrepo.AuditStampValues(sqlrepo.Values{"organization": f.Owner(), "facility_type": f.Type(), "name": f.Name(),
				"description": opt(f.Description()), "part_of": partOf, "area_m2": area, "street_type": opt(a.StreetType),
				"line1": opt(a.Line1), "line2": opt(a.Line2), "postal_code": opt(a.PostalCode), "locality": opt(a.Locality),
				"region": opt(a.Region), "country": opt(a.Country.String()), "geo_postal_code": optUUID(a.Geo.PostalCode),
				"geo_boundary": optUUID(a.Geo.Boundary), "phone": opt(l.Phone.String()), "email": opt(l.Email.String()),
				"active": f.IsActive(), "addr_number": opt(a.Premises.Number), "addr_qualifier": opt(a.Premises.Qualifier),
				"addr_block": opt(a.Premises.Block), "addr_portal": opt(a.Premises.Portal), "addr_staircase": opt(a.Premises.Staircase),
				"addr_floor": opt(a.Premises.Floor), "addr_door": opt(a.Premises.Door)}, f.AuditStamp()), nil
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
			if !r.IsNull("area_m2") {
				s.Area = r.Decimal("area_m2")
			}
			addr := domain.Address{StreetType: r.String("street_type"), Line1: r.String("line1"), Line2: r.String("line2"),
				PostalCode: r.String("postal_code"), Locality: r.String("locality"), Region: r.String("region"),
				Premises: vocab.Premises{Number: r.String("addr_number"), Qualifier: r.String("addr_qualifier"), Block: r.String("addr_block"),
					Portal: r.String("addr_portal"), Staircase: r.String("addr_staircase"), Floor: r.String("addr_floor"), Door: r.String("addr_door")}}
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

// SQLCatalogs lee los tipos de instalación de la base de datos.
type SQLCatalogs struct{ db *sqlrepo.DB }

// FacilityTypes implementa domain.Catalogs.
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

// WellKnownCatalogs sirve la semilla (backend en memoria).
type WellKnownCatalogs struct{}

// FacilityTypes implementa domain.Catalogs.
func (WellKnownCatalogs) FacilityTypes(context.Context) ([]domain.FacilityType, error) {
	return domain.WellKnownFacilityTypes(), nil
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("facilities: unsupported backend %T", b) }

// RepositoryFactory construye el repositorio de instalaciones.
func RepositoryFactory(b hotswap.Backend) (domain.Repository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, FacilityMapping())
	case *memory.Store:
		return memory.NewRepository[domain.FacilityID, *domain.Facility](db), nil
	}
	return nil, unsupported(b)
}

// CatalogsFor devuelve el lector de catálogos de un backend.
func CatalogsFor(b hotswap.Backend) (domain.Catalogs, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return SQLCatalogs{db: db}, nil
	case *memory.Store:
		return WellKnownCatalogs{}, nil
	}
	return nil, unsupported(b)
}

// OutboxFactory construye el outbox de eventos de dominio; IntegrationOutboxFactory, el del
// Published Language.
func OutboxFactory(b hotswap.Backend) (application.OutboxStore, error) { return outbox(b, TableOutbox) }

// IntegrationOutboxFactory construye el outbox de integración.
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

// AuditLogFactory construye el registro de auditoría.
func AuditLogFactory(b hotswap.Backend) (application.AuditLog, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewAuditLog(db, TableAuditLog)
	case *memory.Store:
		return memory.NewAuditLog(db), nil
	}
	return nil, unsupported(b)
}

// GeographyAddresses adapta el AddressChecker de Geography al puerto de Facilities (ACL).
type GeographyAddresses struct{ Checker geo.AddressChecker }

// CheckAddress valida el código postal para el país y su municipio, y completa las referencias a
// Geography.
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
