// Package infrastructure stores the Inventory context: SQL mappings, versioned schema of the five
// engines, hot-swap factories and the adapter to the Products catalog.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	products "github.com/jhermoso/karpo-fw-go/contexts/products/contracts"
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
const Context = "inventory"

// Technical tables of the context.
const (
	TableOutbox            = "inventory_outbox"
	TableIntegrationOutbox = "inventory_integration_outbox"
	TableAuditLog          = "inventory_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE inv_warehouses (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, code {str:10} NOT NULL,
	name {str:100} NOT NULL, facility {uuid}, active {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_inv_warehouses_code ON inv_warehouses (company, code)`,
	`CREATE TABLE inv_levels (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, warehouse_id {uuid} NOT NULL,
	product {uuid} NOT NULL, on_hand {str:30} NOT NULL, reserved {str:30} NOT NULL, average_cost {str:30} NOT NULL, reorder_point {str:30} NOT NULL,
	moves {bigint} NOT NULL, is_empty {bool} NOT NULL, FOREIGN KEY (warehouse_id) REFERENCES inv_warehouses (id))`,
	`CREATE UNIQUE INDEX ux_inv_levels ON inv_levels (warehouse_id, product)`,
	`CREATE INDEX ix_inv_levels_product ON inv_levels (company, product)`,
	`CREATE TABLE inv_movements (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, warehouse_id {uuid} NOT NULL,
	product {uuid} NOT NULL, seq {bigint} NOT NULL, kind {int} NOT NULL, moved_on {date} NOT NULL, quantity {str:30} NOT NULL, unit_cost {str:30} NOT NULL,
	move_value {str:30} NOT NULL, balance {str:30} NOT NULL, lot {str:40}, note {str:200}, source_type {str:80}, source_id {str:80}, ` + audit + `,
	FOREIGN KEY (warehouse_id) REFERENCES inv_warehouses (id))`,
	`CREATE UNIQUE INDEX ux_inv_movements_seq ON inv_movements (warehouse_id, product, seq)`,
	`CREATE INDEX ix_inv_movements_date ON inv_movements (company, moved_on)`,
	`CREATE INDEX ix_inv_movements_source ON inv_movements (source_type, source_id)`,
	`CREATE TABLE inv_reservations (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, warehouse_id {uuid} NOT NULL,
	product {uuid} NOT NULL, source_type {str:80} NOT NULL, source_id {str:80} NOT NULL, quantity {str:30} NOT NULL, open_quantity {str:30} NOT NULL,
	closed {bool} NOT NULL, ` + audit + `, FOREIGN KEY (warehouse_id) REFERENCES inv_warehouses (id))`,
	`CREATE INDEX ix_inv_reservations_source ON inv_reservations (source_type, source_id)`,
	`CREATE INDEX ix_inv_reservations_level ON inv_reservations (warehouse_id, product, closed)`,
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

// Migrations is the versioned schema of the context.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "warehouses, stock levels, ledger and reservations", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"inv_reservations", "inv_movements", "inv_levels", "inv_warehouses", TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

// WarehouseMapping maps Warehouse to inv_warehouses.
func WarehouseMapping() sqlrepo.Mapping[domain.WarehouseID, *domain.Warehouse] {
	return sqlrepo.Mapping[domain.WarehouseID, *domain.Warehouse]{
		Table:   "inv_warehouses",
		Columns: sqlrepo.WithAuditColumns("company", "code", "name", "facility", "active"),
		Dehydrate: func(w *domain.Warehouse) (sqlrepo.Values, error) {
			s := w.State()
			var facility any
			if !s.Facility.IsZero() {
				facility = s.Facility
			}
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "code": s.Code, "name": s.Name, "facility": facility, "active": s.Active},
				w.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Warehouse, error) {
			s := domain.WarehouseState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Code: r.String("code"), Name: r.String("name"),
				Facility: domain.FacilityID{UUID: r.UUID("facility")}, Active: r.Bool("active"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteWarehouse(domain.WarehouseID{UUID: r.UUID("id")}, s)
		},
	}
}

// LevelMapping maps Level to inv_levels.
func LevelMapping() sqlrepo.Mapping[domain.LevelID, *domain.Level] {
	return sqlrepo.Mapping[domain.LevelID, *domain.Level]{
		Table:   "inv_levels",
		Columns: []string{"company", "warehouse_id", "product", "on_hand", "reserved", "average_cost", "reorder_point", "moves", "is_empty"},
		Dehydrate: func(l *domain.Level) (sqlrepo.Values, error) {
			s := l.State()
			return sqlrepo.Values{"company": s.Company, "warehouse_id": s.Warehouse, "product": s.Product, "on_hand": s.OnHand.String(),
				"reserved": s.Reserved.String(), "average_cost": s.AverageCost.StringFixed(4), "reorder_point": s.ReorderPoint.String(), "moves": s.Moves,
				"is_empty": s.OnHand.IsZero()}, nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Level, error) {
			s := domain.LevelState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Warehouse: domain.WarehouseID{UUID: r.UUID("warehouse_id")},
				Product: domain.ProductID{UUID: r.UUID("product")}, OnHand: r.Decimal("on_hand"), Reserved: r.Decimal("reserved"),
				AverageCost: r.Decimal("average_cost"), ReorderPoint: r.Decimal("reorder_point"), Moves: r.Int64("moves")}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteLevel(domain.LevelID{UUID: r.UUID("id")}, s)
		},
	}
}

// MovementMapping maps Movement to inv_movements.
func MovementMapping() sqlrepo.Mapping[domain.MovementID, *domain.Movement] {
	return sqlrepo.Mapping[domain.MovementID, *domain.Movement]{
		Table: "inv_movements",
		Columns: sqlrepo.WithAuditColumns("company", "warehouse_id", "product", "seq", "kind", "moved_on", "quantity", "unit_cost", "move_value", "balance",
			"lot", "note", "source_type", "source_id"),
		Dehydrate: func(m *domain.Movement) (sqlrepo.Values, error) {
			s := m.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "warehouse_id": s.Warehouse, "product": s.Product, "seq": s.Seq,
				"kind": int64(s.Kind), "moved_on": s.Date, "quantity": s.Quantity.String(), "unit_cost": s.UnitCost.StringFixed(4),
				"move_value": s.Value.StringFixed(2), "balance": s.Balance.String(), "lot": opt(s.Lot), "note": opt(s.Note), "source_type": opt(s.Source.Type),
				"source_id": opt(s.Source.ID)}, m.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Movement, error) {
			s := domain.MovementState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Warehouse: domain.WarehouseID{UUID: r.UUID("warehouse_id")},
				Product: domain.ProductID{UUID: r.UUID("product")}, Seq: r.Int64("seq"), Kind: domain.MoveKind(r.Int64("kind")), Date: r.Date("moved_on"),
				Quantity: r.Decimal("quantity"), UnitCost: r.Decimal("unit_cost"), Value: r.Decimal("move_value"), Balance: r.Decimal("balance"),
				Lot: r.String("lot"), Note: r.String("note"), Source: domain.Source{Type: r.String("source_type"), ID: r.String("source_id")},
				Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteMovement(domain.MovementID{UUID: r.UUID("id")}, s)
		},
	}
}

// ReservationMapping maps Reservation to inv_reservations.
func ReservationMapping() sqlrepo.Mapping[domain.ReservationID, *domain.Reservation] {
	return sqlrepo.Mapping[domain.ReservationID, *domain.Reservation]{
		Table:   "inv_reservations",
		Columns: sqlrepo.WithAuditColumns("company", "warehouse_id", "product", "source_type", "source_id", "quantity", "open_quantity", "closed"),
		Dehydrate: func(x *domain.Reservation) (sqlrepo.Values, error) {
			s := x.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "warehouse_id": s.Warehouse, "product": s.Product,
				"source_type": s.Source.Type, "source_id": s.Source.ID, "quantity": s.Quantity.String(), "open_quantity": s.Open.String(),
				"closed": s.Open.IsZero()}, x.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Reservation, error) {
			s := domain.ReservationState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Warehouse: domain.WarehouseID{UUID: r.UUID("warehouse_id")},
				Product: domain.ProductID{UUID: r.UUID("product")}, Source: domain.Source{Type: r.String("source_type"), ID: r.String("source_id")},
				Quantity: r.Decimal("quantity"), Open: r.Decimal("open_quantity"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteReservation(domain.ReservationID{UUID: r.UUID("id")}, s)
		},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("inventory: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// Repository factories.
func WarehouseRepositoryFactory(b hotswap.Backend) (domain.WarehouseRepository, error) {
	return repository(b, WarehouseMapping())
}

func LevelRepositoryFactory(b hotswap.Backend) (domain.LevelRepository, error) {
	return repository(b, LevelMapping())
}

func MovementRepositoryFactory(b hotswap.Backend) (domain.MovementRepository, error) {
	return repository(b, MovementMapping())
}

func ReservationRepositoryFactory(b hotswap.Backend) (domain.ReservationRepository, error) {
	return repository(b, ReservationMapping())
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

// ProductsCatalog adapts the Products Catalog contract to the Inventory port (ACL).
type ProductsCatalog struct{ Catalog products.Catalog }

var _ domain.Catalog = ProductsCatalog{}

// Items implements domain.Catalog.
func (p ProductsCatalog) Items(ctx context.Context, ids []domain.ProductID) (map[domain.ProductID]domain.Item, error) {
	req := make([]string, 0, len(ids))
	for _, id := range ids {
		req = append(req, id.String())
	}
	out := map[domain.ProductID]domain.Item{}
	for len(req) > 0 {
		n := min(len(req), products.MaxBatch)
		refs, err := p.Catalog.Products(ctx, req[:n])
		if err != nil {
			return nil, err
		}
		for id, r := range refs {
			pid, err1 := fw.ParseUUID(id)
			company, err2 := fw.ParseUUID(r.Company)
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("inventory: invalid product from Products: %+v", r)
			}
			it := domain.Item{Company: domain.OrganizationID{UUID: company}, SKU: r.SKU, Name: r.Name, Stocked: r.Stocked, Tracked: r.Tracking != "none"}
			if r.Discontinued != "" {
				d, err := vocab.ParseDate(r.Discontinued)
				if err != nil {
					return nil, fmt.Errorf("inventory: invalid product from Products: %+v", r)
				}
				it.Discontinued = d
			}
			out[domain.ProductID{UUID: pid}] = it
		}
		req = req[n:]
	}
	return out, nil
}
