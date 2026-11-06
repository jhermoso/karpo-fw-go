//go:build smoke

// Command karpo-smoke-postgres es la publicadora de humo de Karpo sobre PostgreSQL, el destino de
// producción: lo mismo que cmd/karpo-smoke, sobre una base PostgreSQL vacía en vez de un fichero
// SQLite. Está en un módulo aparte, como cmd/karpo-postgres, para que el driver no entre en el
// módulo del framework. Sólo compila con -tags smoke, sólo arranca con KARPO_ENV=development y
// sólo escucha en 127.0.0.1.
//
//	KARPO_ENV=development KARPO_SMOKE_POSTGRES_URL=<dsn> go run -tags smoke . [-port 8092] [-seed]
//
//	-port      el puerto de 127.0.0.1 en el que escucha (8092)
//	-postgres  la cadena de conexión (por defecto, la de KARPO_SMOKE_POSTGRES_URL, que no deja la
//	           contraseña en la línea de órdenes)
//	-seed      carga lo que pide el recorrido de docs/SMOKE-MANUAL.md, y los perfiles reader y clerk
//
// Sólo arranca sobre una base de esta máquina (127.0.0.1, ::1, localhost o un socket unix) que
// no tiene nada (smoke.PostgresObjects), y nunca borra nada: una base de otra máquina, o con algo
// dentro, puede ser la de alguien; también una de producción recién creada y vacía. La base es para una vez; la siguiente, otra nueva. La cadena de conexión no se
// muestra: el aviso del arranque dice usuario, máquina, puerto y base.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jhermoso/karpo-fw-go/host/smoke"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
)

// URLVar es la variable de entorno de la cadena de conexión.
const URLVar = "KARPO_SMOKE_POSTGRES_URL"

// run arranca el publicador y sirve hasta que termina ctx. env es lo que dice KARPO_ENV. A
// listening, si no es nil, se le dice la dirección en cuanto escucha.
func run(ctx context.Context, args []string, env string, out io.Writer, listening chan<- string) error {
	flags := flag.NewFlagSet("karpo-smoke-postgres", flag.ContinueOnError)
	flags.SetOutput(out)
	port := flags.Int("port", 8092, "the port of 127.0.0.1 it listens on")
	dsn := flags.String("postgres", "", "the connection string of a PostgreSQL database without tables (default: "+URLVar+")")
	seed := flags.Bool("seed", false, "loads, if there is nothing, what the walk of docs/SMOKE-MANUAL.md needs")
	if err := flags.Parse(args); err != nil {
		return err
	}
	// Antes de abrir nada.
	if err := smoke.CheckEnv(env); err != nil {
		return err
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(*port))
	if err := smoke.CheckAddress(address); err != nil {
		return err
	}
	if *dsn == "" {
		*dsn = os.Getenv(URLVar)
	}
	if *dsn == "" {
		return errors.New("karpo-smoke-postgres: the connection string of a new, empty PostgreSQL database is required: -postgres or " + URLVar)
	}
	config, err := pgx.ParseConfig(*dsn)
	if err != nil {
		return fmt.Errorf("karpo-smoke-postgres: that is not a PostgreSQL connection string: %w", err)
	}
	name := fmt.Sprintf("PostgreSQL %s@%s:%d/%s", config.User, config.Host, config.Port, config.Database)
	if err := checkLocal(config); err != nil {
		return err
	}

	// Sin caché de sentencias: el mismo proceso migra y sirve, y una migración puede cambiar el tipo
	// de una columna (cmd/karpo-postgres hace lo mismo al migrar).
	raw, err := sql.Open("pgx", postgres.WithoutStatementCache(*dsn))
	if err != nil {
		return err
	}
	defer raw.Close()
	raw.SetMaxOpenConns(10)
	look, cancel := context.WithTimeout(ctx, 15*time.Second)
	objects, err := smoke.PostgresObjects(look, raw)
	cancel()
	if err != nil {
		return err
	}
	// Antes de migrar: con algo dentro, la base no es nueva, y no se toca.
	if err := smoke.CheckEmpty("the database "+name, objects); err != nil {
		return err
	}
	// Las exportaciones son de este arranque, como la base: se borran al parar.
	exports, err := os.MkdirTemp("", "karpo-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(exports)
	return smoke.Serve(ctx, env, address, smoke.Database{DB: postgres.Open(raw, sqlrepo.WithName("postgres")), Ping: raw.PingContext,
		Name: name, Exports: filepath.Join(exports, "exports")}, *seed, out, listening)
}

// checkLocal dice si la base de una cadena de conexión está en esta máquina: todas sus máquinas
// (la primera y las alternativas) son una IP de loopback, localhost o un socket unix (una ruta).
// Una cadena que apunta por error a otra base, aunque esté vacía, no se migra.
func checkLocal(config *pgx.ConnConfig) error {
	hosts := []string{config.Host}
	for _, f := range config.Fallbacks {
		hosts = append(hosts, f.Host)
	}
	for _, h := range hosts {
		ip := net.ParseIP(h)
		if !strings.HasPrefix(h, "/") && !strings.EqualFold(h, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("karpo-smoke-postgres: the database is on %q, not on this machine: the smoke publisher only uses a disposable database of this machine (127.0.0.1, ::1, localhost or a unix socket)", h)
		}
	}
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Getenv(smoke.EnvVar), os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, "karpo-smoke-postgres stopped:", err)
		os.Exit(1)
	}
}
