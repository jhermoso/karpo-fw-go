//go:build smoke

// Command karpo-smoke es la publicadora de humo de Karpo: lo que sirve cmd/karpo, sobre un
// fichero SQLite nuevo, con una puerta que deja entrar a una persona SIN CREDENCIALES (paquete
// host/smoke). Es para una máquina de desarrollo y nada más: sólo compila con -tags smoke, sólo
// arranca con KARPO_ENV=development y sólo escucha en 127.0.0.1.
//
//	KARPO_ENV=development go run -tags smoke ./cmd/karpo-smoke [-port 8092] [-db <file>] [-seed]
//
//	-port  el puerto de 127.0.0.1 en el que escucha (8092)
//	-db    el fichero SQLite, que todavía no debe existir: el publicador sólo trabaja sobre una
//	       base de datos que crea él mismo (por defecto: un fichero nuevo en un directorio
//	       temporal nuevo)
//	-seed  carga, si no hay nada, lo que pide el recorrido de docs/SMOKE-MANUAL.md: una empresa,
//	       su cliente, un producto con existencias, los tipos de IVA, una serie, condiciones de
//	       pago, un plan de cuentas y los usuarios de los perfiles reader y clerk
//
// No lee ningún secreto ni escribe ninguno: la clave que firma las sesiones y las contraseñas de
// los perfiles se crean al arrancar y desaparecen al parar. Lo mismo sobre PostgreSQL es
// cmd/karpo-smoke-postgres.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/host/smoke"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// run arranca el publicador y sirve hasta que termina ctx. env es lo que dice KARPO_ENV. A
// listening, si no es nil, se le dice la dirección en cuanto escucha.
func run(ctx context.Context, args []string, env string, out io.Writer, listening chan<- string) error {
	flags := flag.NewFlagSet("karpo-smoke", flag.ContinueOnError)
	flags.SetOutput(out)
	port := flags.Int("port", 8092, "the port of 127.0.0.1 it listens on")
	path := flags.String("db", "", "the SQLite file, which must not exist yet (default: a new one in a new temporary directory)")
	seed := flags.Bool("seed", false, "loads, if there is nothing, what the walk of docs/SMOKE-MANUAL.md needs")
	if err := flags.Parse(args); err != nil {
		return err
	}
	// Antes de abrir o crear nada.
	if err := smoke.CheckEnv(env); err != nil {
		return err
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(*port))
	if err := smoke.CheckAddress(address); err != nil {
		return err
	}
	if *path == "" {
		dir, err := os.MkdirTemp("", "karpo-smoke-")
		if err != nil {
			return err
		}
		*path = filepath.Join(dir, "karpo.db")
	} else if _, err := os.Stat(*path); err == nil {
		// Antes de abrirlo: migrarlo y arrancar el anfitrión sobre él ya lo cambiaría.
		return fmt.Errorf("karpo-smoke: the file %s already exists: the smoke publisher only starts on a new database, one it creates itself (give -db a file that does not exist)", *path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("karpo-smoke: -db: %w", err)
	}

	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(*path)+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return err
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1) // SQLite admite un solo escritor; el anfitrión es su único cliente
	return smoke.Serve(ctx, env, address, smoke.Database{DB: sqlite.Open(raw, sqlrepo.WithName("sqlite")), Ping: raw.PingContext,
		Name: *path, Exports: filepath.Join(filepath.Dir(*path), "exports")}, *seed, out, listening)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Getenv(smoke.EnvVar), os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, "karpo-smoke stopped:", err)
		os.Exit(1)
	}
}
