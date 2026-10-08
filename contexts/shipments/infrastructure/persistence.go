// Package infrastructure stores the Shipments context: SQL mappings, versioned schema of the five
// engines and hot-swap factories.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/shipments/domain"
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
const Context = "shipments"

// Technical tables of the context.
const (
	TableOutbox            = "shipments_outbox"
	TableIntegrationOutbox = "shipments_integration_outbox"
	TableAuditLog          = "shipments_audit_log"
	TableInbox             = "shipments_inbox"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE shp_carriers (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, code {str:20} NOT NULL,
	name {str:120} NOT NULL, party {uuid}, tracking_url {str:300}, blocked {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_shp_carriers_code ON shp_carriers (company, code)`,
	`CREATE TABLE shp_shipments (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, customer {uuid} NOT NULL,
	source_type {str:40}, source_id {str:64}, source_ref {str:60}, method {str:20}, carrier {uuid}, tracking {str:60}, recipient {str:200},
	destination {str:500}, packages {int} NOT NULL, weight_kg {str:30} NOT NULL, cost {str:30} NOT NULL, instructions {str:500},
	estimated_ship {date}, estimated_arrival {date}, status {str:20} NOT NULL, planned_on {date} NOT NULL, dispatched {date}, closed_on {date},
	received_by {str:200}, reason {str:200}, ` + audit + `)`,
	`CREATE INDEX ix_shp_shipments_source ON shp_shipments (company, source_type, source_id)`,
	`CREATE INDEX ix_shp_shipments_status ON shp_shipments (company, status)`,
	`CREATE TABLE shp_shipment_lines (shipment_id {uuid} NOT NULL, line_no {int} NOT NULL, sku {str:40}, description {str:200} NOT NULL,
	quantity {str:30} NOT NULL, uom {str:10}, PRIMARY KEY (shipment_id, line_no), FOREIGN KEY (shipment_id) REFERENCES shp_shipments (id))`,
	`CREATE TABLE shp_shipment_history (shipment_id {uuid} NOT NULL, line_no {int} NOT NULL, status {str:20} NOT NULL, changed_on {date} NOT NULL,
	PRIMARY KEY (shipment_id, line_no), FOREIGN KEY (shipment_id) REFERENCES shp_shipments (id))`,
}

func technicalDDL(d string) []string {
	switch d {
	case "sqlite":
		return slices.Concat(sqlite.OutboxDDL(TableOutbox), sqlite.OutboxDDL(TableIntegrationOutbox), sqlite.AuditDDL(TableAuditLog), sqlite.InboxDDL(TableInbox))
	case "postgres":
		return slices.Concat(postgres.OutboxDDL(TableOutbox), postgres.OutboxDDL(TableIntegrationOutbox), postgres.AuditDDL(TableAuditLog), postgres.InboxDDL(TableInbox))
	case "sqlserver":
		return slices.Concat(sqlserver.OutboxDDL(TableOutbox), sqlserver.OutboxDDL(TableIntegrationOutbox), sqlserver.AuditDDL(TableAuditLog), sqlserver.InboxDDL(TableInbox))
	case "oracle":
		return slices.Concat(oracle.OutboxDDL(TableOutbox), oracle.OutboxDDL(TableIntegrationOutbox), oracle.AuditDDL(TableAuditLog), oracle.InboxDDL(TableInbox))
	case "mysql":
		return slices.Concat(mysql.OutboxDDL(TableOutbox), mysql.OutboxDDL(TableIntegrationOutbox), mysql.AuditDDL(TableAuditLog), mysql.InboxDDL(TableInbox))
	}
	return nil
}

// Migrations is the versioned schema of the context.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "carriers and shipments", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes, audit log and inbox", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"shp_shipment_history", "shp_shipment_lines", "shp_shipments", "shp_carriers", TableOutbox, TableIntegrationOutbox,
	TableAuditLog, TableInbox}

// DropAll removes the tables of the context and its migration history (tests only).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range Tables {
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

func optDate(d vocab.Date) any {
	if d.IsZero() {
		return nil
	}
	return d
}

// Engines do not agree on the order of the children: they are sorted here.
func byNo(rows []*sqlrepo.Row) []*sqlrepo.Row {
	out := slices.Clone(rows)
	slices.SortFunc(out, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
	return out
}

// ShipmentMapping maps Shipment to shp_shipments, its lines and its history.
func ShipmentMapping() sqlrepo.Mapping[domain.ShipmentID, *domain.Shipment] {
	return sqlrepo.Mapping[domain.ShipmentID, *domain.Shipment]{
		Table: "shp_shipments",
		Columns: sqlrepo.WithAuditColumns("company", "customer", "source_type", "source_id", "source_ref", "method", "carrier", "tracking", "recipient",
			"destination", "packages", "weight_kg", "cost", "instructions", "estimated_ship", "estimated_arrival", "status", "planned_on", "dispatched",
			"closed_on", "received_by", "reason"),
		Dehydrate: func(s *domain.Shipment) (sqlrepo.Values, error) {
			st := s.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": st.Company, "customer": st.Customer, "source_type": opt(st.Source.Type),
				"source_id": opt(st.Source.ID), "source_ref": opt(st.Source.Ref), "method": opt(string(st.Method)), "carrier": optUUID(st.Carrier.UUID),
				"tracking": opt(st.Tracking), "recipient": opt(st.Recipient), "destination": opt(st.Destination), "packages": int64(st.Packages),
				"weight_kg": st.WeightKg.StringFixed(3), "cost": st.Cost.StringFixed(2), "instructions": opt(st.Instructions),
				"estimated_ship": optDate(st.EstimatedShip), "estimated_arrival": optDate(st.EstimatedArrival), "status": string(st.Status),
				"planned_on": st.History[0].On, "dispatched": optDate(st.Dispatched), "closed_on": optDate(st.Closed), "received_by": opt(st.ReceivedBy),
				"reason": opt(st.Reason)}, s.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Shipment, error) {
			st := domain.ShipmentState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				Source: domain.Source{Type: r.String("source_type"), ID: r.String("source_id"), Ref: r.String("source_ref")},
				Plan: domain.Plan{Method: domain.Method(r.String("method")), Carrier: domain.CarrierID{UUID: r.UUID("carrier")}, Tracking: r.String("tracking"),
					Recipient: r.String("recipient"), Destination: r.String("destination"), Packages: int(r.Int64("packages")), WeightKg: r.Decimal("weight_kg"),
					Cost: r.Decimal("cost"), Instructions: r.String("instructions"), EstimatedShip: r.Date("estimated_ship"),
					EstimatedArrival: r.Date("estimated_arrival")},
				Status: domain.Status(r.String("status")), Dispatched: r.Date("dispatched"), Closed: r.Date("closed_on"), ReceivedBy: r.String("received_by"),
				Reason: r.String("reason"), Audit: r.AuditStamp()}
			for _, c := range byNo(children.Of("lines")) {
				st.Lines = append(st.Lines, domain.Line{No: int(c.Int64("line_no")), SKU: c.String("sku"), Description: c.String("description"),
					Quantity: c.Decimal("quantity"), UoM: c.String("uom")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range byNo(children.Of("history")) {
				st.History = append(st.History, domain.Step{Status: domain.Status(c.String("status")), On: c.Date("changed_on")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteShipment(domain.ShipmentID{UUID: r.UUID("id")}, st)
		},
		Children: []sqlrepo.Child[*domain.Shipment]{{
			Name: "lines", Table: "shp_shipment_lines", ForeignKey: "shipment_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "sku", "description", "quantity", "uom"},
			Dehydrate: func(s *domain.Shipment) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range s.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(l.No), "sku": opt(l.SKU), "description": l.Description, "quantity": l.Quantity.String(),
						"uom": opt(l.UoM)})
				}
				return out, nil
			},
		}, {
			Name: "history", Table: "shp_shipment_history", ForeignKey: "shipment_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "status", "changed_on"},
			Dehydrate: func(s *domain.Shipment) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, h := range s.State().History {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "status": string(h.Status), "changed_on": h.On})
				}
				return out, nil
			},
		}},
	}
}

// CarrierMapping maps Carrier to shp_carriers.
func CarrierMapping() sqlrepo.Mapping[domain.CarrierID, *domain.Carrier] {
	return sqlrepo.Mapping[domain.CarrierID, *domain.Carrier]{
		Table:   "shp_carriers",
		Columns: sqlrepo.WithAuditColumns("company", "code", "name", "party", "tracking_url", "blocked"),
		Dehydrate: func(c *domain.Carrier) (sqlrepo.Values, error) {
			s := c.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "code": s.Code, "name": s.Name, "party": optUUID(s.Party.UUID),
				"tracking_url": opt(s.TrackingURL), "blocked": s.Blocked}, c.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Carrier, error) {
			s := domain.CarrierState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Code: r.String("code"), Name: r.String("name"),
				Party: domain.PartyID{UUID: r.UUID("party")}, TrackingURL: r.String("tracking_url"), Blocked: r.Bool("blocked"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteCarrier(domain.CarrierID{UUID: r.UUID("id")}, s)
		},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("shipments: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// ShipmentRepositoryFactory builds the shipment repository.
func ShipmentRepositoryFactory(b hotswap.Backend) (domain.ShipmentRepository, error) {
	return repository(b, ShipmentMapping())
}

// CarrierRepositoryFactory builds the carrier repository.
func CarrierRepositoryFactory(b hotswap.Backend) (domain.CarrierRepository, error) {
	return repository(b, CarrierMapping())
}

// OutboxFactory builds the domain event outbox.
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

// InboxFactory builds the inbox of the events Shipments consumes.
func InboxFactory(b hotswap.Backend) (application.InboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewInbox(db, TableInbox)
	case *memory.Store:
		return memory.NewInbox(db), nil
	}
	return nil, unsupported(b)
}
