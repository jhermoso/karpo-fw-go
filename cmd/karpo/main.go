// Command karpo runs the whole of Karpo in one process on a SQLite file: every bounded context,
// their messages carried in memory, their chores on a timer. It is the reference deployment and
// what a developer starts; a deployment on PostgreSQL, SQL Server, Oracle or MySQL is this same
// program with another driver and another line to open the database.
//
//	karpo migrate   applies the schema of every context
//	karpo serve     verifies the schema and serves (the default)
//
// Configuration comes from the environment:
//
//	KARPO_SQLITE                   path of the database file (required)
//	KARPO_JWT_SECRET               secret that signs the sessions, 32 characters at least (required)
//	KARPO_ADDR                     where to listen (":8080")
//	KARPO_EXPORTS_DIR              where the exported files wait ("exports" beside the database)
//	KARPO_DELIVER_EVERY            how often messages are carried between contexts ("2s")
//	KARPO_CHORES_EVERY             how often the chores run ("1m")
//	KARPO_BOOTSTRAP_ADMIN_USER     first administrator, created only while nobody administers
//	KARPO_BOOTSTRAP_ADMIN_PASSWORD its password, to be changed on the first session
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	expinfra "github.com/jhermoso/karpo-fw-go/contexts/exports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// MinSecret is the least a secret that signs sessions may be.
const MinSecret = 32

type config struct {
	database, secret, addr, exports string
	deliver, chores                 time.Duration
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func every(name, fallback string) (time.Duration, error) {
	d, err := time.ParseDuration(env(name, fallback))
	if err != nil || d < 100*time.Millisecond {
		return 0, fmt.Errorf("%s: a duration of 100ms at least, like %s", name, fallback)
	}
	return d, nil
}

func load() (config, error) {
	c := config{database: os.Getenv("KARPO_SQLITE"), secret: os.Getenv("KARPO_JWT_SECRET"), addr: env("KARPO_ADDR", ":8080")}
	var err1, err2 error
	c.deliver, err1 = every("KARPO_DELIVER_EVERY", "2s")
	c.chores, err2 = every("KARPO_CHORES_EVERY", "1m")
	var missing error
	if c.database == "" {
		missing = errors.Join(missing, errors.New("KARPO_SQLITE: the path of the database file is required"))
	}
	if len(c.secret) < MinSecret {
		missing = errors.Join(missing, fmt.Errorf("KARPO_JWT_SECRET: a secret of %d characters at least is required", MinSecret))
	}
	c.exports = env("KARPO_EXPORTS_DIR", filepath.Join(filepath.Dir(c.database), "exports"))
	return c, errors.Join(missing, err1, err2)
}

func open(path string) (*sql.DB, *sqlrepo.DB, error) {
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, nil, err
	}
	raw.SetMaxOpenConns(1) // SQLite takes one writer; the host is its only client
	return raw, sqlite.Open(raw, sqlrepo.WithName("sqlite")), nil
}

func run(ctx context.Context, command string, logger log.Logger) error {
	cfg, err := load()
	if err != nil {
		return err
	}
	raw, db, err := open(cfg.database)
	if err != nil {
		return err
	}
	defer raw.Close()
	migrator, err := sqlrepo.NewMigrator(db, host.Migrations())
	if err != nil {
		return err
	}
	switch command {
	case "migrate":
		done, err := migrator.Migrate(ctx)
		if err != nil {
			return err
		}
		logger.Info("schema migrated", "contexts", len(host.Migrations()), "migrations", len(done))
		return nil
	case "serve":
	default:
		return fmt.Errorf("unknown command %q: migrate or serve", command)
	}
	// Never on a schema that is behind, or ahead of this program.
	if err := migrator.Verify(ctx); err != nil {
		return fmt.Errorf("the schema is not the one this program expects (run \"karpo migrate\"): %w", err)
	}
	files, err := expinfra.NewDiskFiles(cfg.exports)
	if err != nil {
		return err
	}
	h, err := host.Compose(hotswap.New(db), host.Options{JWTSecret: []byte(cfg.secret), Files: files})
	if err != nil {
		return err
	}
	started, err := h.Start(ctx)
	if err != nil {
		return err
	}
	logger.Info("karpo started", "permissions", started.Permissions, "newFeatures", started.Features, "bootstrap", started.Bootstrap)

	health := distribution.NewHealthRegistry()
	health.RegisterReadinessProbe("database", func(ctx context.Context) error { return raw.PingContext(ctx) })
	mux := http.NewServeMux()
	health.Mount(mux)
	mux.Handle("/", h.Handler())
	server := distribution.NewServer(cfg.addr, mux)
	failed := server.Start()
	logger.Info("listening", "addr", cfg.addr)

	go h.Run(ctx, cfg.deliver, cfg.chores, func(err error) { logger.Error("background work failed; it will be tried again", "error", err.Error()) })

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	logger.Info("stopping")
	stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return server.Shutdown(stop)
}

func main() {
	logger := vanilla.NewJSON(os.Stdout, log.LevelInfo)
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, command, logger); err != nil {
		logger.Error("karpo stopped", "error", err.Error())
		os.Exit(1)
	}
}
