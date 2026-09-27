// Package integration runs the repository conformance suite and the Parties bounded context
// against real database servers. It is a separate module so the framework itself never depends
// on database drivers.
//
// Start the servers with `docker compose up -d` (see docker-compose.yml) and export the DSNs:
//
//	KARPO_PG_DSN      postgres://karpo:karpo@localhost:55433/karpo?sslmode=disable
//	KARPO_MSSQL_DSN   sqlserver://sa:Karpo_2026!@localhost:51433?database=master
//	KARPO_ORACLE_DSN  oracle://karpo:karpo@localhost:51521/FREEPDB1
//	KARPO_MYSQL_DSN   karpo:karpo@tcp(localhost:53306)/karpo?parseTime=true&loc=UTC
//
// Engines without a DSN are skipped.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	_ "github.com/sijms/go-ora/v2"

	parties "github.com/jhermoso/karpo-fw-go/examples/parties/domain"
	"github.com/jhermoso/karpo-fw-go/examples/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlconformance"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

type engine struct {
	name    string
	env     string
	driver  string
	dialect sqlrepo.Dialect
}

var engines = []engine{
	{"postgres", "KARPO_PG_DSN", "pgx", postgres.New()},
	{"sqlserver", "KARPO_MSSQL_DSN", "sqlserver", sqlserver.New()},
	{"oracle", "KARPO_ORACLE_DSN", "oracle", oracle.New()},
	// RAW(16) in .NET Guid byte order, to share tables with the C# Karpo services.
	{"oracle-dotnet-guids", "KARPO_ORACLE_DSN", "oracle", oracle.New(oracle.WithDotNetGUIDs())},
	{"mysql", "KARPO_MYSQL_DSN", "mysql", mysql.New()},
}

func open(t *testing.T, e engine) *sqlrepo.DB {
	t.Helper()
	dsn := os.Getenv(e.env)
	if dsn == "" {
		t.Skipf("%s not set", e.env)
	}
	raw, err := sql.Open(e.driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := raw.PingContext(ctx); err != nil {
		t.Fatalf("%s unreachable: %v", e.name, err)
	}
	return sqlrepo.New(raw, e.dialect)
}

// TestConformance proves that every engine honours the domain.Repository contract, including
// the equivalence between each specification executed in SQL and its in-memory evaluation.
func TestConformance(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			sqlconformance.Run(t, open(t, e))
		})
	}
}

// TestAuditLog checks the SQL audit log on every engine.
func TestAuditLog(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) { sqlconformance.RunAuditLog(t, open(t, e)) })
	}
}

// TestMigrations checks the schema migrator on every engine.
func TestMigrations(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) { sqlconformance.RunMigrations(t, open(t, e)) })
	}
}

// TestInbox checks the SQL inbox on every engine.
func TestInbox(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) { sqlconformance.RunInbox(t, open(t, e)) })
	}
}

// TestParties runs the Parties mapping (child table, custom COUNT specification) on every engine.
func TestParties(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			resetParties(t, db)
			repo := sqlrepo.MustRepository(db, infrastructure.PartyMapping())

			tax, _ := parties.NewTaxID("B12345678")
			p, err := parties.Register(parties.NewPartyID(), parties.Organization, "Acme Corporation", tax)
			if err != nil {
				t.Fatal(err)
			}
			_ = p.AddContact(parties.Contact{Kind: parties.Email, Value: "info@acme.test", Primary: true})
			_ = p.AddContact(parties.Contact{Kind: parties.Phone, Value: "555"})
			ana, _ := vocab.NewActor(fw.NewUUID(), "Ana")
			created := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
			traits.Stamp(p, ana, created)
			if err := repo.Save(ctx, p); err != nil {
				t.Fatal(err)
			}
			traits.Stamp(p, vocab.SystemActor, created.Add(time.Hour))
			if err := repo.Save(ctx, p); err != nil {
				t.Fatal(err)
			}
			reloaded, err := repo.Get(ctx, p.ID())
			if err != nil {
				t.Fatal(err)
			}
			if got := reloaded.AuditStamp(); got.CreatedBy != ana || !got.CreatedAt.Equal(created) ||
				got.ModifiedBy != vocab.SystemActor || !got.ModifiedAt.Equal(created.Add(time.Hour)) {
				t.Fatalf("audit stamp round trip: %+v", got)
			}
			dup, _ := parties.Register(parties.NewPartyID(), parties.Organization, "Copy", tax)
			if err := repo.Save(ctx, dup); err == nil {
				t.Fatal("unique tax id must be enforced by the database")
			}

			found, err := repo.Find(ctx, parties.MinContacts(2).And(parties.ReachableBy(parties.Email), parties.NameContains("CORP")))
			if err != nil || len(found) != 1 || len(found[0].Contacts()) != 2 {
				t.Fatalf("find: %v %d", err, len(found))
			}
			page, err := repo.FindPage(ctx, parties.Active(), fw.NewPageRequest(1, 10, parties.FieldLegalName.Asc()))
			if err != nil || page.Total != 1 {
				t.Fatalf("page: %+v %v", page, err)
			}
		})
	}
}

// TestHotSwapAcrossEngines keeps one domain repository alive while the backend is swapped
// PostgreSQL -> SQL Server -> Oracle -> MySQL, with a unit of work in flight on each switch.
func TestHotSwapAcrossEngines(t *testing.T) {
	ctx := context.Background()
	var dbs, checks []*sqlrepo.DB // switch backends (closed when retired) and inspection pools
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		db := open(t, e)
		resetParties(t, db)
		dbs = append(dbs, db)
		checks = append(checks, open(t, e))
	}

	sw := hotswap.New(dbs[0])
	repo := hotswap.Repository(sw, infrastructure.RepositoryFactory)

	for i, db := range dbs {
		name := db.Dialect().Name()
		tax, _ := parties.NewTaxID(fmt.Sprintf("SWAP%05d", i))
		p, _ := parties.Register(parties.NewPartyID(), parties.Organization, "Party on "+name, tax)

		if i+1 < len(dbs) {
			// Open a unit of work on the current engine and swap while it is still running.
			started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			go func() {
				done <- sw.Do(ctx, func(ctx context.Context) error {
					close(started)
					<-release
					return repo.Save(ctx, p)
				})
			}()
			<-started
			swapped := make(chan error, 1)
			go func() { swapped <- sw.Swap(ctx, dbs[i+1]) }()
			for sw.Name() == name {
				time.Sleep(time.Millisecond)
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatalf("in-flight unit of work on %s: %v", name, err)
			}
			if err := <-swapped; err != nil {
				t.Fatal(err)
			}
		} else if err := repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}

		// The aggregate landed on the engine that was current when its unit of work began.
		direct := sqlrepo.MustRepository(checks[i], infrastructure.PartyMapping())
		got, err := direct.Get(ctx, p.ID())
		if err != nil || got.LegalName() != "Party on "+name {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if n, err := repo.Count(ctx, nil); err != nil || n != 1 {
		t.Fatalf("the last engine must hold only its own party: %d %v", n, err)
	}
}

func resetParties(t *testing.T, db *sqlrepo.DB) {
	t.Helper()
	ctx := context.Background()
	dropPartiesTables(ctx, db)
	m, err := infrastructure.Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx); !errors.Is(err, application.ErrSchemaOutdated) {
		t.Fatalf("an empty database must be outdated: %v", err)
	}
	if err := infrastructure.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
}

// dropPartiesTables removes the tables of both the framework example and the Parties context
// (they share names on the integration databases), children first.
func dropPartiesTables(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range []string{"party_relationships", "party_roles", "party_contacts", "party_identifications", "party_affiliations", "party_facility_roles", "facility_role_types",
		"party_classifications", "parties", "relationship_types", "role_types", "country_document_rules", "document_types",
		"classification_types", "outbox_messages", "parties_integration_outbox", "parties_outbox", "audit_log", "parties_audit_log",
		"schema_migrations", "schema_migrations_lock"} {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+t)
	}
}
