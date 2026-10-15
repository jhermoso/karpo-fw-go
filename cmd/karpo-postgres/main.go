// Command karpo-postgres runs the whole of Karpo in one process on PostgreSQL, which is also what
// Supabase is. It is the production program: the same host as cmd/karpo, with another driver.
//
//	karpo-postgres migrate   applies the schema of every context
//	karpo-postgres serve     verifies the schema and serves (the default)
//
// KARPO_DATABASE_URL is the connection string (required), for example
//
//	postgres://user:password@host:5432/database?sslmode=require
//
// KARPO_DB_MAX_CONNS bounds the connections it keeps (10). The rest of the configuration is that
// of package serve.
//
// On Supabase use the direct connection or the session pooler (port 5432). The transaction pooler
// (port 6543) gives each statement a different connection: the lock that keeps two migrations
// from running at once and the transactions of the use cases need one that stays.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jhermoso/karpo-fw-go/host/serve"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
)

// open opens the database of a connection string, with at most conns connections.
func open(ctx context.Context, url string, conns int) (*sql.DB, error) {
	raw, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(conns)
	raw.SetMaxIdleConns(conns)
	raw.SetConnMaxIdleTime(5 * time.Minute)
	ping, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := raw.PingContext(ping); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("the database does not answer: %w", err)
	}
	return raw, nil
}

func run(ctx context.Context, command string, logger log.Logger) error {
	url := os.Getenv("KARPO_DATABASE_URL")
	cfg, err := serve.Load("exports")
	if url == "" {
		err = errors.Join(errors.New("KARPO_DATABASE_URL: the connection string of the database is required"), err)
	}
	conns, cerr := strconv.Atoi(serve.Env("KARPO_DB_MAX_CONNS", "10"))
	if cerr != nil || conns < 2 {
		err = errors.Join(err, errors.New("KARPO_DB_MAX_CONNS: two connections at least"))
	}
	if err != nil {
		return err
	}
	raw, err := open(ctx, url, conns)
	if err != nil {
		return err
	}
	defer raw.Close()
	return serve.Run(ctx, command, postgres.Open(raw, sqlrepo.WithName("postgres")), raw.PingContext, cfg, logger)
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
