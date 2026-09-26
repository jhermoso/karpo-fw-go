// Package sqlconformance wires the repository conformance suite (pkg/testing/repotest) to any
// SQL dialect: it provides the Widget mapping, the DDL for every supported engine and a harness
// that resets the schema before each test. Each dialect's tests (and the integration module for
// servers) call Run with an opened *sqlrepo.DB.
package sqlconformance

import (
	"context"
	"fmt"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
)

// WidgetMapping maps repotest.Widget: a root table, a child table and a custom specification.
// The same mapping serves every dialect.
func WidgetMapping() sqlrepo.Mapping[repotest.WidgetID, *repotest.Widget] {
	return sqlrepo.Mapping[repotest.WidgetID, *repotest.Widget]{
		Table:   "widgets",
		Columns: []string{"name", "price", "active", "color", "created_at"},
		Dehydrate: func(w *repotest.Widget) (sqlrepo.Values, error) {
			return sqlrepo.Values{
				"name":       w.Name(),
				"price":      w.Price(),
				"active":     w.Active(),
				"color":      w.Color(),
				"created_at": w.CreatedAt(),
			}, nil
		},
		Hydrate: func(row *sqlrepo.Row, children sqlrepo.ChildRows) (*repotest.Widget, error) {
			var parts []repotest.Part
			for _, p := range children.Of("parts") {
				parts = append(parts, repotest.Part{Name: p.String("name"), Qty: p.Int64("qty")})
			}
			return repotest.Reconstitute(
				repotest.WidgetID{UUID: row.UUID("id")},
				row.String("name"), row.Int64("price"), row.Bool("active"),
				row.NullString("color"), row.Time("created_at"), parts)
		},
		Children: []sqlrepo.Child[*repotest.Widget]{{
			Name:       "parts",
			Table:      "widget_parts",
			ForeignKey: "widget_id",
			Columns:    []string{"pos", "name", "qty"},
			OrderBy:    []string{"pos"},
			Dehydrate: func(w *repotest.Widget) ([]sqlrepo.Values, error) {
				parts := w.Parts()
				out := make([]sqlrepo.Values, len(parts))
				for i, p := range parts {
					out[i] = sqlrepo.Values{"pos": int64(i), "name": p.Name, "qty": p.Qty}
				}
				return out, nil
			},
		}},
		Custom: map[string]sqlrepo.CustomSQL{
			repotest.PremiumName: func(b *sqlrepo.Builder, args []any) (string, error) {
				active, err := b.Column("active")
				if err != nil {
					return "", err
				}
				price, err := b.Column("price")
				if err != nil {
					return "", err
				}
				pTrue, err := b.Arg(true)
				if err != nil {
					return "", err
				}
				pMin, err := b.Arg(args[0])
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%s = %s AND %s >= %s", active, pTrue, price, pMin), nil
			},
		},
	}
}

// Schema returns the DROP and CREATE statements of the conformance tables for a dialect name.
func Schema(dialect string) (drop, create []string) {
	drop = []string{"DROP TABLE widget_parts", "DROP TABLE widgets"}
	switch dialect {
	case "sqlite":
		create = []string{
			`CREATE TABLE widgets (id TEXT PRIMARY KEY, version INTEGER NOT NULL, name TEXT NOT NULL,
				price INTEGER NOT NULL, active INTEGER NOT NULL, color TEXT, created_at TEXT NOT NULL)`,
			`CREATE TABLE widget_parts (widget_id TEXT NOT NULL REFERENCES widgets(id), pos INTEGER NOT NULL,
				name TEXT NOT NULL, qty INTEGER NOT NULL, PRIMARY KEY (widget_id, pos))`,
		}
	case "postgres":
		create = []string{
			`CREATE TABLE widgets (id UUID PRIMARY KEY, version BIGINT NOT NULL, name VARCHAR(200) NOT NULL,
				price BIGINT NOT NULL, active BOOLEAN NOT NULL, color VARCHAR(50), created_at TIMESTAMPTZ NOT NULL)`,
			`CREATE TABLE widget_parts (widget_id UUID NOT NULL REFERENCES widgets(id), pos INTEGER NOT NULL,
				name VARCHAR(100) NOT NULL, qty BIGINT NOT NULL, PRIMARY KEY (widget_id, pos))`,
		}
	case "sqlserver":
		create = []string{
			`CREATE TABLE widgets (id UNIQUEIDENTIFIER PRIMARY KEY, version BIGINT NOT NULL, name NVARCHAR(200) NOT NULL,
				price BIGINT NOT NULL, active BIT NOT NULL, color NVARCHAR(50) NULL, created_at DATETIME2(7) NOT NULL)`,
			`CREATE TABLE widget_parts (widget_id UNIQUEIDENTIFIER NOT NULL REFERENCES widgets(id), pos INT NOT NULL,
				name NVARCHAR(100) NOT NULL, qty BIGINT NOT NULL, PRIMARY KEY (widget_id, pos))`,
		}
	case "oracle":
		create = []string{
			`CREATE TABLE widgets (id RAW(16) PRIMARY KEY, version NUMBER(19) NOT NULL, name VARCHAR2(200) NOT NULL,
				price NUMBER(19) NOT NULL, active NUMBER(1) NOT NULL, color VARCHAR2(50), created_at TIMESTAMP(6) WITH TIME ZONE NOT NULL)`,
			`CREATE TABLE widget_parts (widget_id RAW(16) NOT NULL REFERENCES widgets(id), pos NUMBER(10) NOT NULL,
				name VARCHAR2(100) NOT NULL, qty NUMBER(19) NOT NULL, PRIMARY KEY (widget_id, pos))`,
		}
	case "mysql":
		create = []string{
			`CREATE TABLE widgets (id CHAR(36) PRIMARY KEY, version BIGINT NOT NULL, name VARCHAR(200) NOT NULL,
				price BIGINT NOT NULL, active BOOLEAN NOT NULL, color VARCHAR(50), created_at DATETIME(6) NOT NULL)`,
			`CREATE TABLE widget_parts (widget_id CHAR(36) NOT NULL, pos INT NOT NULL,
				name VARCHAR(100) NOT NULL, qty BIGINT NOT NULL, PRIMARY KEY (widget_id, pos),
				FOREIGN KEY (widget_id) REFERENCES widgets(id))`,
		}
	}
	return drop, create
}

// Reset drops (ignoring errors) and recreates the conformance tables.
func Reset(ctx context.Context, db *sqlrepo.DB) error {
	drop, create := Schema(db.Dialect().Name())
	if create == nil {
		return fmt.Errorf("sqlconformance: no schema for dialect %q", db.Dialect().Name())
	}
	for _, s := range drop {
		_, _ = db.ExecContext(ctx, s)
	}
	for _, s := range create {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("sqlconformance: %w\n%s", err, s)
		}
	}
	return nil
}

// Run executes the repository conformance suite against db.
func Run(t *testing.T, db *sqlrepo.DB) {
	repotest.Run(t, repotest.Harness{
		New: func(t *testing.T) (domain.Repository[repotest.WidgetID, *repotest.Widget], domain.UnitOfWork) {
			t.Helper()
			if err := Reset(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			repo, err := sqlrepo.NewRepository(db, WidgetMapping())
			if err != nil {
				t.Fatal(err)
			}
			return repo, db
		},
	})
}
