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
//	-seed  carga algo que mirar, si no hay nada
//
// No lee ningún secreto ni escribe ninguno: la clave que firma las sesiones y la contraseña del
// perfil se crean al arrancar y desaparecen al parar.
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
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	expinfra "github.com/jhermoso/karpo-fw-go/contexts/exports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/host/smoke"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
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
	seed := flags.Bool("seed", false, "loads something to look at, if there is nothing")
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
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	migrator, err := sqlrepo.NewMigrator(db, host.Migrations())
	if err != nil {
		return err
	}
	if _, err := migrator.Migrate(ctx); err != nil {
		return err
	}
	if err := migrator.Verify(ctx); err != nil {
		return err
	}
	files, err := expinfra.NewDiskFiles(filepath.Join(filepath.Dir(*path), "exports"))
	if err != nil {
		return err
	}
	p, err := smoke.New(ctx, env, hotswap.New(db), smoke.Options{Files: files, Ping: raw.PingContext})
	if err != nil {
		return err
	}
	if *seed {
		said, err := p.Seed()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "seed: %s\n", said)
	}
	listener, err := p.Listen(address)
	if err != nil {
		return err
	}
	fmt.Fprint(out, smoke.Banner(listener.Addr().String(), *path))

	server := &http.Server{Handler: p, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	failed := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			failed <- err
		}
	}()
	go p.Host.Run(ctx, 2*time.Second, time.Minute, func(err error) { fmt.Fprintf(out, "background work failed; it will be tried again: %v\n", err) })
	if listening != nil {
		listening <- listener.Addr().String()
	}

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return server.Shutdown(stop)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Getenv(smoke.EnvVar), os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, "karpo-smoke stopped:", err)
		os.Exit(1)
	}
}
