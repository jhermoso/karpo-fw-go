//go:build smoke

package smoke

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	expinfra "github.com/jhermoso/karpo-fw-go/contexts/exports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Database es la base de datos nueva sobre la que sirve un programa de humo, ya abierta.
type Database struct {
	// DB es la base, todavía sin migrar.
	DB *sqlrepo.DB
	// Ping dice si responde, para la sonda de disponibilidad.
	Ping func(context.Context) error
	// Name es cómo la nombra el aviso del arranque. Nunca lleva una contraseña.
	Name string
	// Exports es la carpeta de los ficheros exportados.
	Exports string
}

// Serve es lo que hace un programa de humo (cmd/karpo-smoke, cmd/karpo-smoke-postgres) una vez
// que ha comprobado el entorno y la dirección y ha abierto una base nueva: la migra, compone el
// publicador, siembra si seed, escucha en address y sirve hasta que termina ctx. A listening, si
// no es nil, se le dice la dirección en cuanto escucha.
func Serve(ctx context.Context, env, address string, d Database, seed bool, out io.Writer, listening chan<- string) error {
	migrator, err := sqlrepo.NewMigrator(d.DB, host.Migrations())
	if err != nil {
		return err
	}
	if _, err := migrator.Migrate(ctx); err != nil {
		return err
	}
	if err := migrator.Verify(ctx); err != nil {
		return err
	}
	files, err := expinfra.NewDiskFiles(d.Exports)
	if err != nil {
		return err
	}
	p, err := New(ctx, env, hotswap.New(d.DB), Options{Files: files, Ping: d.Ping})
	if err != nil {
		return err
	}
	if seed {
		_, said, err := p.Seed(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "seed: %s\n", strings.ReplaceAll(said, "\n", "\n  "))
	}
	listener, err := p.Listen(address)
	if err != nil {
		return err
	}
	fmt.Fprint(out, Banner(listener.Addr().String(), d.Name))

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

// PostgresObjects lista lo que alguien ha creado en una base PostgreSQL, en los esquemas que no son
// del propio PostgreSQL (fuera de pg_catalog, information_schema, pg_toast y los demás pg_*):
// tablas, vistas, vistas materializadas, tablas foráneas, secuencias, funciones y tipos. De los
// tipos no cuenta los que PostgreSQL crea solo: el de la fila de cada tabla y los de array. Lee
// pg_catalog, y no information_schema, que calla lo que el usuario no tiene permiso para ver.
func PostgresObjects(ctx context.Context, raw *sql.DB) ([]string, error) {
	rows, err := raw.QueryContext(ctx, `WITH own AS (SELECT oid, nspname FROM pg_catalog.pg_namespace
			WHERE nspname NOT IN ('pg_catalog', 'information_schema') AND nspname NOT LIKE 'pg\_%')
		SELECT own.nspname || '.' || c.relname FROM pg_catalog.pg_class c JOIN own ON own.oid = c.relnamespace
			WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
		UNION ALL
		SELECT own.nspname || '.' || p.proname || '()' FROM pg_catalog.pg_proc p JOIN own ON own.oid = p.pronamespace
		UNION ALL
		SELECT own.nspname || '.' || t.typname FROM pg_catalog.pg_type t JOIN own ON own.oid = t.typnamespace
			LEFT JOIN pg_catalog.pg_class r ON r.oid = t.typrelid
			WHERE t.typcategory <> 'A' AND (t.typrelid = 0 OR r.relkind = 'c')
		ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("smoke: the PostgreSQL database does not answer: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CheckEmpty decide si el publicador puede arrancar sobre una base que ya existe, como una de
// PostgreSQL: sólo si no tiene nada (objects, de PostgresObjects). Una base con algo puede ser la
// de alguien, el publicador dejaría en ella un administrador cuya contraseña nadie sabe, y nunca
// borra nada para hacerse sitio. database es cómo se nombra en el mensaje.
//
// Entre esta comprobación y la migración pasa un instante en el que otro podría crear algo en la
// base; no se cierra, porque la base es de esta máquina (el programa se niega a otra) y desechable.
func CheckEmpty(database string, objects []string) error {
	if len(objects) == 0 {
		return nil
	}
	shown := objects
	if len(shown) > 3 {
		shown = shown[:3]
	}
	noun := "objects"
	if len(objects) == 1 {
		noun = "object"
	}
	return fmt.Errorf("smoke: %s already has %d %s (%s...): the smoke publisher only starts on a new, empty database and never deletes anything (create a new one)",
		database, len(objects), noun, strings.Join(shown, ", "))
}
