// Package infrastructure adapts the Parties domain to storage: one SQL mapping valid for every
// dialect, the DDL per engine, and the hot-swap factories that build the right adapter for
// whatever backend is current.
package infrastructure

import (
	"context"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/examples/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

// PartyMapping maps the Party aggregate to the parties / party_contacts tables.
func PartyMapping() sqlrepo.Mapping[domain.PartyID, *domain.Party] {
	return sqlrepo.Mapping[domain.PartyID, *domain.Party]{
		Table:   "parties",
		Columns: []string{"party_type", "legal_name", "tax_id", "active", "registered_at"},
		Fields:  map[string]string{"type": "party_type"},
		Dehydrate: func(p *domain.Party) (sqlrepo.Values, error) {
			return sqlrepo.Values{
				"party_type":    p.Type(),
				"legal_name":    p.LegalName(),
				"tax_id":        p.TaxID().String(),
				"active":        p.Active(),
				"registered_at": p.RegisteredAt(),
			}, nil
		},
		Hydrate: func(row *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Party, error) {
			taxID, err := domain.NewTaxID(row.String("tax_id"))
			if err != nil {
				return nil, err
			}
			var contacts []domain.Contact
			for _, c := range children.Of("contacts") {
				contacts = append(contacts, domain.Contact{
					Kind: domain.ContactKind(c.String("kind")), Value: c.String("value"), Primary: c.Bool("is_primary"),
				})
			}
			return domain.Reconstitute(domain.PartyID{UUID: row.UUID("id")}, domain.PartyType(row.String("party_type")),
				row.String("legal_name"), taxID, row.Bool("active"), row.Time("registered_at"), contacts)
		},
		Children: []sqlrepo.Child[*domain.Party]{{
			Name:       "contacts",
			Table:      "party_contacts",
			ForeignKey: "party_id",
			Columns:    []string{"pos", "kind", "value", "is_primary"},
			OrderBy:    []string{"pos"},
			Dehydrate: func(p *domain.Party) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, c := range p.Contacts() {
					out = append(out, sqlrepo.Values{"pos": int64(i), "kind": c.Kind, "value": c.Value, "is_primary": c.Primary})
				}
				return out, nil
			},
		}},
		Custom: map[string]sqlrepo.CustomSQL{
			// parties.min_contacts: a correlated COUNT sub-query, identical on every dialect.
			domain.MinContactsName: func(b *sqlrepo.Builder, args []any) (string, error) {
				id, err := b.Column("id")
				if err != nil {
					return "", err
				}
				n, err := b.Arg(args[0])
				if err != nil {
					return "", err
				}
				q := b.Dialect().Quote
				return fmt.Sprintf("(SELECT COUNT(*) FROM %s pc WHERE pc.%s = %s) >= %s",
					q("party_contacts"), q("party_id"), id, n), nil
			},
		},
	}
}

// Schema returns the DDL of the Parties tables (plus the outbox) for a dialect.
func Schema(dialect string) []string {
	var ddl, outbox []string
	switch dialect {
	case "sqlite":
		ddl = []string{
			`CREATE TABLE IF NOT EXISTS parties (id TEXT PRIMARY KEY, version INTEGER NOT NULL, party_type TEXT NOT NULL,
				legal_name TEXT NOT NULL, tax_id TEXT NOT NULL UNIQUE, active INTEGER NOT NULL, registered_at TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS party_contacts (party_id TEXT NOT NULL REFERENCES parties(id), pos INTEGER NOT NULL,
				kind TEXT NOT NULL, value TEXT NOT NULL, is_primary INTEGER NOT NULL, PRIMARY KEY (party_id, pos))`,
		}
		outbox = sqlite.OutboxDDL("")
	case "postgres":
		ddl = []string{
			`CREATE TABLE IF NOT EXISTS parties (id UUID PRIMARY KEY, version BIGINT NOT NULL, party_type VARCHAR(20) NOT NULL,
				legal_name VARCHAR(300) NOT NULL, tax_id VARCHAR(20) NOT NULL UNIQUE, active BOOLEAN NOT NULL, registered_at TIMESTAMPTZ NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS party_contacts (party_id UUID NOT NULL REFERENCES parties(id), pos INTEGER NOT NULL,
				kind VARCHAR(20) NOT NULL, value VARCHAR(300) NOT NULL, is_primary BOOLEAN NOT NULL, PRIMARY KEY (party_id, pos))`,
		}
		outbox = postgres.OutboxDDL("")
	case "sqlserver":
		ddl = []string{
			`CREATE TABLE parties (id UNIQUEIDENTIFIER PRIMARY KEY, version BIGINT NOT NULL, party_type NVARCHAR(20) NOT NULL,
				legal_name NVARCHAR(300) NOT NULL, tax_id NVARCHAR(20) NOT NULL UNIQUE, active BIT NOT NULL, registered_at DATETIME2(7) NOT NULL)`,
			`CREATE TABLE party_contacts (party_id UNIQUEIDENTIFIER NOT NULL REFERENCES parties(id), pos INT NOT NULL,
				kind NVARCHAR(20) NOT NULL, value NVARCHAR(300) NOT NULL, is_primary BIT NOT NULL, PRIMARY KEY (party_id, pos))`,
		}
		outbox = sqlserver.OutboxDDL("")
	case "oracle":
		ddl = []string{
			`CREATE TABLE parties (id RAW(16) PRIMARY KEY, version NUMBER(19) NOT NULL, party_type VARCHAR2(20) NOT NULL,
				legal_name VARCHAR2(300) NOT NULL, tax_id VARCHAR2(20) NOT NULL UNIQUE, active NUMBER(1) NOT NULL, registered_at TIMESTAMP(6) WITH TIME ZONE NOT NULL)`,
			`CREATE TABLE party_contacts (party_id RAW(16) NOT NULL REFERENCES parties(id), pos NUMBER(10) NOT NULL,
				kind VARCHAR2(20) NOT NULL, value VARCHAR2(300) NOT NULL, is_primary NUMBER(1) NOT NULL, PRIMARY KEY (party_id, pos))`,
		}
		outbox = oracle.OutboxDDL("")
	case "mysql":
		ddl = []string{
			`CREATE TABLE IF NOT EXISTS parties (id CHAR(36) PRIMARY KEY, version BIGINT NOT NULL, party_type VARCHAR(20) NOT NULL,
				legal_name VARCHAR(300) NOT NULL, tax_id VARCHAR(20) NOT NULL UNIQUE, active BOOLEAN NOT NULL, registered_at DATETIME(6) NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS party_contacts (party_id CHAR(36) NOT NULL, pos INT NOT NULL, kind VARCHAR(20) NOT NULL,
				value VARCHAR(300) NOT NULL, is_primary BOOLEAN NOT NULL, PRIMARY KEY (party_id, pos), FOREIGN KEY (party_id) REFERENCES parties(id))`,
		}
		outbox = mysql.OutboxDDL("")
	}
	return append(ddl, outbox...)
}

// Migrate creates the schema on db.
func Migrate(ctx context.Context, db *sqlrepo.DB) error {
	stmts := Schema(db.Dialect().Name())
	if len(stmts) == 0 {
		return fmt.Errorf("parties: no schema for dialect %q", db.Dialect().Name())
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("parties: migrate: %w", err)
		}
	}
	return nil
}

// RepositoryFactory builds the Party repository for any backend (hot-swap factory).
func RepositoryFactory(b hotswap.Backend) (domain.Repository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, PartyMapping())
	case *memory.Store:
		return memory.NewRepository[domain.PartyID, *domain.Party](db), nil
	}
	return nil, fmt.Errorf("parties: unsupported backend %T", b)
}

// OutboxFactory builds the outbox store for any backend (hot-swap factory).
func OutboxFactory(b hotswap.Backend) (application.OutboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewOutbox(db, "")
	case *memory.Store:
		return memory.NewOutbox(db), nil
	}
	return nil, fmt.Errorf("parties: unsupported backend %T", b)
}
