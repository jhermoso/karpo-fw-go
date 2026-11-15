// Package infrastructure guarda el contexto Shipments: mapeos SQL, esquema versionado de los cinco
// motores y factorías intercambiables en caliente.
package infrastructure

import (
	"context"
	"fmt"
	fwapproval "github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/shipments/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/approvals"
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
const Context = "shipments"

// Tablas técnicas del contexto.
const (
	TableOutbox            = "shipments_outbox"
	TableIntegrationOutbox = "shipments_integration_outbox"
	TableAuditLog          = "shipments_audit_log"
	TableInbox             = "shipments_inbox"
	// TableApprovals guarda las solicitudes de aprobación del bloqueo y el desbloqueo (D-156).
	TableApprovals = "shp_approvals"
	// TableCarrierBlockEvents es el historial de bloqueos y desbloqueos de los transportistas.
	TableCarrierBlockEvents = "shp_carrier_block_events"
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

// blockDDL añade los motivos vigentes del bloqueo y su historial (D-156): una fila por bloqueo o
// desbloqueo efectivo, con quién y cuándo. Los motivos miden 800 porque Oracle cuenta bytes.
var blockDDL = []string{
	`ALTER TABLE shp_carriers {add:block_reason} {str:800}{addEnd}`,
	`ALTER TABLE shp_carriers {add:unblock_reason} {str:800}{addEnd}`,
	`CREATE TABLE ` + TableCarrierBlockEvents + ` (carrier_id {uuid} NOT NULL, line_no {int} NOT NULL, blocked {bool} NOT NULL,
	reason {str:800} NOT NULL, changed_at {ts} NOT NULL, changed_by_id {str:64}, changed_by_name {str:200},
	PRIMARY KEY (carrier_id, line_no), FOREIGN KEY (carrier_id) REFERENCES shp_carriers (id))`,
}

// Migrations es el esquema versionado del contexto.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "carriers and shipments", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes, audit log and inbox", Up: technical},
		{Version: 3, Name: "decimal columns", Run: decimalColumnsV3.Run},
		{Version: 4, Name: "carrier block reasons and history", Up: sqlrepo.RenderDDLAll(blockDDL...)},
		{Version: 5, Name: "approval requests", Up: sqlrepo.RenderDDLAll(approvals.DDL(TableApprovals)...)},
		// Una sola solicitud pendiente por destino, sea bloquear o desbloquear, también en la base
		// (motor común, pkg/persistence/approvals): la columna pending_key, rellenada en las filas
		// que ya hubiera, y en la 7 su índice único.
		{Version: 6, Name: "pending key", Up: sqlrepo.RenderDDLAll(approvals.PendingKeyColumnDDL(TableApprovals)),
			Run: approvals.FillPendingKeys(TableApprovals, fwapproval.GroupBlock)},
		{Version: 7, Name: "one pending request per target", Up: sqlrepo.RenderDDLAll(approvals.PendingKeyIndexDDL(TableApprovals))},
	}}
}

// Migrator devuelve el migrador del esquema del contexto.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lista las tablas del contexto, primero las hijas (orden de borrado).
var Tables = []string{"shp_shipment_history", "shp_shipment_lines", "shp_shipments", TableCarrierBlockEvents, "shp_carriers", TableApprovals, TableOutbox, TableIntegrationOutbox,
	TableAuditLog, TableInbox}

// DropAll borra las tablas del contexto y su historial de migraciones (sólo pruebas).
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

// Los motores no coinciden en el orden de los hijos: se ordenan aquí.
func byNo(rows []*sqlrepo.Row) []*sqlrepo.Row {
	out := slices.Clone(rows)
	slices.SortFunc(out, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
	return out
}

// ShipmentMapping mapea Shipment a shp_shipments, sus líneas y su historial.
func ShipmentMapping() sqlrepo.Mapping[domain.ShipmentID, *domain.Shipment] {
	return sqlrepo.Mapping[domain.ShipmentID, *domain.Shipment]{
		Table:    "shp_shipments",
		Decimals: decimalColumns.Of("shp_shipments"),
		Columns: sqlrepo.WithAuditColumns("company", "customer", "source_type", "source_id", "source_ref", "method", "carrier", "tracking", "recipient",
			"destination", "packages", "weight_kg", "cost", "instructions", "estimated_ship", "estimated_arrival", "status", "planned_on", "dispatched",
			"closed_on", "received_by", "reason"),
		Dehydrate: func(s *domain.Shipment) (sqlrepo.Values, error) {
			st := s.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": st.Company, "customer": st.Customer, "source_type": opt(st.Source.Type),
				"source_id": opt(st.Source.ID), "source_ref": opt(st.Source.Ref), "method": opt(string(st.Method)), "carrier": optUUID(st.Carrier.UUID),
				"tracking": opt(st.Tracking), "recipient": opt(st.Recipient), "destination": opt(st.Destination), "packages": int64(st.Packages),
				"weight_kg": st.WeightKg.Round(3), "cost": st.Cost.Round(2), "instructions": opt(st.Instructions),
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
			Decimals: decimalColumns.Of("shp_shipment_lines"),
			Columns:  []string{"line_no", "sku", "description", "quantity", "uom"},
			Dehydrate: func(s *domain.Shipment) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range s.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(l.No), "sku": opt(l.SKU), "description": l.Description, "quantity": l.Quantity,
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

// CarrierMapping mapea Carrier a shp_carriers.
func CarrierMapping() sqlrepo.Mapping[domain.CarrierID, *domain.Carrier] {
	return sqlrepo.Mapping[domain.CarrierID, *domain.Carrier]{
		Table:   "shp_carriers",
		Columns: sqlrepo.WithAuditColumns("company", "code", "name", "party", "tracking_url", "blocked", "block_reason", "unblock_reason"),
		Dehydrate: func(c *domain.Carrier) (sqlrepo.Values, error) {
			s := c.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "code": s.Code, "name": s.Name, "party": optUUID(s.Party.UUID),
				"tracking_url": opt(s.TrackingURL), "blocked": s.Blocked, "block_reason": opt(s.BlockReason), "unblock_reason": opt(s.UnblockReason)},
				c.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Carrier, error) {
			s := domain.CarrierState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Code: r.String("code"), Name: r.String("name"),
				Party: domain.PartyID{UUID: r.UUID("party")}, TrackingURL: r.String("tracking_url"), Blocked: r.Bool("blocked"),
				BlockReason: r.String("block_reason"), UnblockReason: r.String("unblock_reason"), Audit: r.AuditStamp()}
			for _, c := range byNo(children.Of("blocks")) {
				e := domain.BlockEvent{Blocked: c.Bool("blocked"), Reason: c.String("reason"), At: c.Time("changed_at"),
					By: vocab.Actor{Name: c.String("changed_by_name")}}
				if id := c.String("changed_by_id"); id != "" {
					e.By.PartyID, _ = fw.ParseUUID(id)
				}
				if err := c.Err(); err != nil {
					return nil, err
				}
				s.BlockEvents = append(s.BlockEvents, e)
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteCarrier(domain.CarrierID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Carrier]{{
			Name: "blocks", Table: TableCarrierBlockEvents, ForeignKey: "carrier_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "blocked", "reason", "changed_at", "changed_by_id", "changed_by_name"},
			Dehydrate: func(c *domain.Carrier) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, e := range c.State().BlockEvents {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "blocked": e.Blocked, "reason": e.Reason, "changed_at": e.At,
						"changed_by_id": opt(optID(e.By.PartyID)), "changed_by_name": opt(e.By.Name)})
				}
				return out, nil
			},
		}},
	}
}

func optID(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

// ApprovalRepositoryFactory construye el repositorio de las solicitudes de aprobación.
func ApprovalRepositoryFactory(b hotswap.Backend) (approval.Repository, error) {
	return repository(b, approvals.Mapping(TableApprovals))
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

// ShipmentRepositoryFactory construye el repositorio de envíos.
func ShipmentRepositoryFactory(b hotswap.Backend) (domain.ShipmentRepository, error) {
	return repository(b, ShipmentMapping())
}

// CarrierRepositoryFactory construye el repositorio de transportistas.
func CarrierRepositoryFactory(b hotswap.Backend) (domain.CarrierRepository, error) {
	return repository(b, CarrierMapping())
}

// OutboxFactory construye el outbox de eventos de dominio.
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

// InboxFactory construye el inbox de los eventos que consume Shipments.
func InboxFactory(b hotswap.Backend) (application.InboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewInbox(db, TableInbox)
	case *memory.Store:
		return memory.NewInbox(db), nil
	}
	return nil, unsupported(b)
}
