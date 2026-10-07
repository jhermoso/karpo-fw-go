// Package infrastructure stores the Exchange context: SQL mappings, versioned schema of the five
// engines and hot-swap factories.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
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
const Context = "exchange"

// Technical tables of the context.
const (
	TableOutbox            = "exchange_outbox"
	TableIntegrationOutbox = "exchange_integration_outbox"
	TableAuditLog          = "exchange_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE exg_currencies (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, code {str:10} NOT NULL,
	name {str:120} NOT NULL, rate {str:30} NOT NULL, rate_on {date}, facial {str:30} NOT NULL, crypto {bool} NOT NULL, blocked {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_exg_currencies_code ON exg_currencies (company, code)`,
	`CREATE TABLE exg_margins (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, currency {str:10} NOT NULL,
	segment_code {str:20} NOT NULL, kind {str:20} NOT NULL, level1 {str:30} NOT NULL, level2 {str:30} NOT NULL, level3 {str:30} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_exg_margins_segment ON exg_margins (company, currency, segment_code)`,
	`CREATE TABLE exg_settings (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, price_level {int} NOT NULL,
	promotion_mode {str:20} NOT NULL, validate_promotion {bool} NOT NULL, expiry_hours {int} NOT NULL, crypto_enabled {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_exg_settings_company ON exg_settings (company)`,
	`CREATE TABLE exg_reservations (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, customer {uuid} NOT NULL,
	reference {str:40} NOT NULL, channel {str:20} NOT NULL, segment_code {str:20} NOT NULL, pickup_at {ts} NOT NULL, pickup_on {date} NOT NULL,
	time_slot {str:50}, expires_at {ts} NOT NULL, facility {uuid}, promotion_code {str:15}, collaborator {uuid}, status {str:20} NOT NULL,
	reason {str:200}, created_on {date} NOT NULL, total {str:30} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_exg_reservations_reference ON exg_reservations (reference)`,
	`CREATE INDEX ix_exg_reservations_status ON exg_reservations (company, status)`,
	`CREATE INDEX ix_exg_reservations_created ON exg_reservations (company, created_on)`,
	`CREATE TABLE exg_reservation_lines (reservation_id {uuid} NOT NULL, line_no {int} NOT NULL, currency {str:10} NOT NULL, requested {str:30} NOT NULL,
	delivered {str:30} NOT NULL, base_rate {str:30} NOT NULL, offered_rate {str:30} NOT NULL, margin_kind {str:20} NOT NULL, margin_value {str:30} NOT NULL,
	eur {str:30} NOT NULL, collector {bool} NOT NULL, collector_note {str:500}, PRIMARY KEY (reservation_id, line_no),
	FOREIGN KEY (reservation_id) REFERENCES exg_reservations (id))`,
	`CREATE TABLE exg_reservation_history (reservation_id {uuid} NOT NULL, line_no {int} NOT NULL, status {str:20} NOT NULL, changed_at {ts} NOT NULL,
	PRIMARY KEY (reservation_id, line_no), FOREIGN KEY (reservation_id) REFERENCES exg_reservations (id))`,
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
		{Version: 1, Name: "currencies, margins, settings and reservations", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"exg_reservation_history", "exg_reservation_lines", "exg_reservations", "exg_settings", "exg_margins", "exg_currencies",
	TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

// CurrencyMapping maps Currency to exg_currencies.
func CurrencyMapping() sqlrepo.Mapping[domain.CurrencyID, *domain.Currency] {
	return sqlrepo.Mapping[domain.CurrencyID, *domain.Currency]{
		Table:   "exg_currencies",
		Columns: sqlrepo.WithAuditColumns("company", "code", "name", "rate", "rate_on", "facial", "crypto", "blocked"),
		Dehydrate: func(c *domain.Currency) (sqlrepo.Values, error) {
			s := c.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "code": s.Code, "name": s.Name, "rate": s.Rate.StringFixed(6),
				"rate_on": optDate(s.RateOn), "facial": s.Facial.StringFixed(2), "crypto": s.Crypto, "blocked": s.Blocked}, c.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Currency, error) {
			s := domain.CurrencyState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Code: r.String("code"), Name: r.String("name"),
				Rate: r.Decimal("rate"), RateOn: r.Date("rate_on"), Facial: r.Decimal("facial"), Crypto: r.Bool("crypto"), Blocked: r.Bool("blocked"),
				Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteCurrency(domain.CurrencyID{UUID: r.UUID("id")}, s)
		},
	}
}

// MarginMapping maps Margin to exg_margins.
func MarginMapping() sqlrepo.Mapping[domain.MarginID, *domain.Margin] {
	return sqlrepo.Mapping[domain.MarginID, *domain.Margin]{
		Table:   "exg_margins",
		Columns: sqlrepo.WithAuditColumns("company", "currency", "segment_code", "kind", "level1", "level2", "level3"),
		Dehydrate: func(m *domain.Margin) (sqlrepo.Values, error) {
			s := m.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "currency": s.Currency, "segment_code": s.Segment, "kind": string(s.Kind),
				"level1": s.Values[0].String(), "level2": s.Values[1].String(), "level3": s.Values[2].String()}, m.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Margin, error) {
			s := domain.MarginState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Currency: r.String("currency"), Segment: r.String("segment_code"),
				Kind: domain.MarginKind(r.String("kind")), Values: [domain.MaxLevel]vocab.Decimal{r.Decimal("level1"), r.Decimal("level2"), r.Decimal("level3")},
				Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteMargin(domain.MarginID{UUID: r.UUID("id")}, s)
		},
	}
}

// SettingsMapping maps Settings to exg_settings.
func SettingsMapping() sqlrepo.Mapping[domain.SettingsID, *domain.Settings] {
	return sqlrepo.Mapping[domain.SettingsID, *domain.Settings]{
		Table:   "exg_settings",
		Columns: sqlrepo.WithAuditColumns("company", "price_level", "promotion_mode", "validate_promotion", "expiry_hours", "crypto_enabled"),
		Dehydrate: func(x *domain.Settings) (sqlrepo.Values, error) {
			s := x.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "price_level": int64(s.Level), "promotion_mode": s.PromotionMode,
				"validate_promotion": s.ValidatePromotion, "expiry_hours": int64(s.ExpiryHours), "crypto_enabled": s.CryptoEnabled}, x.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Settings, error) {
			s := domain.SettingsState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Level: int(r.Int64("price_level")),
				PromotionMode: r.String("promotion_mode"), ValidatePromotion: r.Bool("validate_promotion"), ExpiryHours: int(r.Int64("expiry_hours")),
				CryptoEnabled: r.Bool("crypto_enabled"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteSettings(domain.SettingsID{UUID: r.UUID("id")}, s)
		},
	}
}

// ReservationMapping maps Reservation to exg_reservations, its lines and its history.
func ReservationMapping() sqlrepo.Mapping[domain.ReservationID, *domain.Reservation] {
	return sqlrepo.Mapping[domain.ReservationID, *domain.Reservation]{
		Table: "exg_reservations",
		Columns: sqlrepo.WithAuditColumns("company", "customer", "reference", "channel", "segment_code", "pickup_at", "pickup_on", "time_slot", "expires_at",
			"facility", "promotion_code", "collaborator", "status", "reason", "created_on", "total"),
		Dehydrate: func(x *domain.Reservation) (sqlrepo.Values, error) {
			s := x.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "customer": s.Customer, "reference": s.Reference, "channel": s.Channel,
				"segment_code": s.Segment, "pickup_at": s.Pickup, "pickup_on": vocab.DateOf(s.Pickup), "time_slot": opt(s.TimeSlot), "expires_at": s.ExpiresAt,
				"facility": optUUID(s.Facility), "promotion_code": opt(s.PromotionCode), "collaborator": optUUID(s.Collaborator.UUID),
				"status": string(s.Status), "reason": opt(s.Reason), "created_on": vocab.DateOf(s.History[0].At), "total": x.Total().StringFixed(2)},
				x.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Reservation, error) {
			s := domain.ReservationState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				Reference: r.String("reference"), Channel: r.String("channel"), Segment: r.String("segment_code"), Pickup: r.Time("pickup_at").UTC(),
				TimeSlot: r.String("time_slot"), ExpiresAt: r.Time("expires_at").UTC(), Facility: r.UUID("facility"), PromotionCode: r.String("promotion_code"),
				Collaborator: domain.PartyID{UUID: r.UUID("collaborator")}, Status: domain.Status(r.String("status")), Reason: r.String("reason"),
				Audit: r.AuditStamp()}
			for _, c := range byNo(children.Of("lines")) {
				s.Lines = append(s.Lines, domain.Line{No: int(c.Int64("line_no")), Currency: c.String("currency"), Requested: c.Decimal("requested"),
					Delivered: c.Decimal("delivered"), BaseRate: c.Decimal("base_rate"), OfferedRate: c.Decimal("offered_rate"),
					MarginKind: domain.MarginKind(c.String("margin_kind")), MarginValue: c.Decimal("margin_value"), Eur: c.Decimal("eur"),
					Collector: c.Bool("collector"), CollectorNote: c.String("collector_note")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range byNo(children.Of("history")) {
				s.History = append(s.History, domain.Step{Status: domain.Status(c.String("status")), At: c.Time("changed_at").UTC()})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteReservation(domain.ReservationID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Reservation]{{
			Name: "lines", Table: "exg_reservation_lines", ForeignKey: "reservation_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "currency", "requested", "delivered", "base_rate", "offered_rate", "margin_kind", "margin_value", "eur", "collector",
				"collector_note"},
			Dehydrate: func(x *domain.Reservation) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range x.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(l.No), "currency": l.Currency, "requested": l.Requested.StringFixed(2),
						"delivered": l.Delivered.StringFixed(2), "base_rate": l.BaseRate.StringFixed(6), "offered_rate": l.OfferedRate.StringFixed(6),
						"margin_kind": string(l.MarginKind), "margin_value": l.MarginValue.String(), "eur": l.Eur.StringFixed(2), "collector": l.Collector,
						"collector_note": opt(l.CollectorNote)})
				}
				return out, nil
			},
		}, {
			Name: "history", Table: "exg_reservation_history", ForeignKey: "reservation_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "status", "changed_at"},
			Dehydrate: func(x *domain.Reservation) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, h := range x.State().History {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "status": string(h.Status), "changed_at": h.At})
				}
				return out, nil
			},
		}},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("exchange: unsupported backend %T", b) }

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
func CurrencyRepositoryFactory(b hotswap.Backend) (domain.CurrencyRepository, error) {
	return repository(b, CurrencyMapping())
}

// MarginRepositoryFactory builds the margin repository.
func MarginRepositoryFactory(b hotswap.Backend) (domain.MarginRepository, error) {
	return repository(b, MarginMapping())
}

// SettingsRepositoryFactory builds the settings repository.
func SettingsRepositoryFactory(b hotswap.Backend) (domain.SettingsRepository, error) {
	return repository(b, SettingsMapping())
}

// ReservationRepositoryFactory builds the reservation repository.
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
