// Command karpo runs the whole of Karpo in one process on a SQLite file: every bounded context,
// their messages carried in memory, their chores on a timer. It is what a developer starts; the
// deployment on PostgreSQL (or Supabase) is cmd/karpo-postgres, the same program with another
// driver.
//
//	karpo migrate   applies the schema of every context
//	karpo serve     verifies the schema and serves (the default)
//
// KARPO_SQLITE is the path of the database file (required); the rest of the configuration is that
// of package serve.
package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/host/serve"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

func run(ctx context.Context, command string, logger log.Logger) error {
	path := os.Getenv("KARPO_SQLITE")
	cfg, err := serve.Load(filepath.Join(filepath.Dir(path), "exports"))
	if path == "" {
		err = errors.Join(errors.New("KARPO_SQLITE: the path of the database file is required"), err)
	}
	if err != nil {
		return err
	}
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return err
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1) // SQLite takes one writer; the host is its only client
	return serve.Run(ctx, command, sqlite.Open(raw, sqlrepo.WithName("sqlite")), raw.PingContext, cfg, logger)
}

func main() {
	logger := vanilla.NewJSON(os.Stdout, log.LevelInfo)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, serve.Command(os.Args), logger); err != nil {
		logger.Error("karpo stopped", "error", err.Error())
		os.Exit(1)
	}
}
