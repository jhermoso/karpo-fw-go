// Command karpo-postgres ejecuta Karpo entero en un proceso sobre PostgreSQL, que es también lo
// que es Supabase. Es el programa de producción: el mismo anfitrión que cmd/karpo, con otro
// driver.
//
//	karpo-postgres migrate   aplica el esquema de todos los contextos
//	karpo-postgres serve     comprueba el esquema y sirve (lo predeterminado)
//
// KARPO_DATABASE_URL es la cadena de conexión (obligatoria), por ejemplo
//
//	postgres://user:password@host:5432/database?sslmode=require
//
// KARPO_DB_MAX_CONNS limita las conexiones que mantiene (10). El resto de la configuración es la
// del paquete serve.
//
// En Supabase se usa la conexión directa o el pooler de sesión (puerto 5432). El pooler de
// transacción (puerto 6543) da a cada sentencia una conexión distinta: el bloqueo que impide que
// dos migraciones se ejecuten a la vez y las transacciones de los casos de uso necesitan una que
// se mantenga.
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

// open abre la base de datos de una cadena de conexión, con como mucho conns conexiones.
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
	if command == serve.Migrate {
		// Una migración puede cambiar el tipo de una columna: sin caché de sentencias, ninguna
		// sentencia preparada antes del cambio se reutiliza con los tipos viejos.
		url = postgres.WithoutStatementCache(url)
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
