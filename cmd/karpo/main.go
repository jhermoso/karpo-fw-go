// Command karpo ejecuta Karpo entero en un proceso sobre un fichero SQLite: todos los contextos
// delimitados, sus mensajes llevados en memoria, sus tareas periódicas con un temporizador. Es lo
// que arranca un desarrollador; el despliegue en PostgreSQL (o Supabase) es cmd/karpo-postgres,
// el mismo programa con otro driver.
//
//	karpo migrate   aplica el esquema de todos los contextos
//	karpo serve     comprueba el esquema y sirve (lo predeterminado)
//
// KARPO_SQLITE es la ruta del fichero de la base de datos (obligatoria); el resto de la
// configuración es la del paquete serve.
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
	raw.SetMaxOpenConns(1) // SQLite admite un solo escritor; el anfitrión es su único cliente
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
