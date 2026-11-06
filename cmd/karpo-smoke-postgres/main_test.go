//go:build smoke

package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Sin PostgreSQL: lo que se comprueba antes de abrir nada. Que se niega sobre una base con tablas
// lo prueba smoke.CheckEmpty (host/smoke), y el recorrido entero, docs/SMOKE-MANUAL.md.
func TestRun_RefusesBeforeOpeningAnything(t *testing.T) {
	t.Setenv(URLVar, "")
	cases := []struct {
		env  string
		args []string
		says string
	}{
		{"production", []string{"-postgres", "postgres://u@127.0.0.1:1/x"}, "KARPO_ENV"},
		{"", nil, "KARPO_ENV"},
		{"development", nil, URLVar},
		{"development", []string{"-postgres", "::not a connection string::"}, "not a PostgreSQL connection string"},
		// Una base de otra máquina, aunque esté vacía, puede ser la de producción recién creada.
		{"development", []string{"-postgres", "postgres://u@db.example.com:5432/x"}, "not on this machine"},
		{"development", []string{"-postgres", "postgres://u@10.0.0.5:5432/x"}, "not on this machine"},
		{"development", []string{"-postgres", "host=127.0.0.1,db.example.com user=u dbname=x"}, "not on this machine"},
		// Una de esta máquina pasa la comprobación y falla después, al no responder.
		{"development", []string{"-postgres", "postgres://u@127.0.0.1:1/x?connect_timeout=2"}, "does not answer"},
	}
	for _, c := range cases {
		err := run(context.Background(), c.args, c.env, io.Discard, nil)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("KARPO_ENV=%q %v: %v, want it to say %q", c.env, c.args, err, c.says)
		}
	}
}

// Lo que dice al negarse no lleva la contraseña de la cadena.
func TestRun_DoesNotShowThePassword(t *testing.T) {
	t.Setenv(URLVar, "")
	for _, dsn := range []string{"postgres://u:secreto@127.0.0.1:1/x?connect_timeout=2", "postgres://u:secreto@db.example.com:5432/x"} {
		err := run(context.Background(), []string{"-postgres", dsn}, "development", io.Discard, nil)
		if err == nil || strings.Contains(err.Error(), "secreto") {
			t.Errorf("%v", err)
		}
	}
}

// Las máquinas que cuentan como esta.
func TestCheckLocal(t *testing.T) {
	for dsn, local := range map[string]bool{
		"postgres://u@127.0.0.1:5432/x":        true,
		"postgres://u@127.0.0.2:5432/x":        true,
		"postgres://u@[::1]:5432/x":            true,
		"postgres://u@localhost:5432/x":        true,
		"host=/var/run/postgresql user=u":      true,
		"postgres://u@db.example.com:5432/x":   false,
		"postgres://u@192.168.1.10:5432/x":     false,
		"postgres://u@localhost.example.com/x": false,
		"host=localhost,10.0.0.5 user=u":       false,
	} {
		config, err := pgx.ParseConfig(dsn)
		if err != nil {
			t.Fatalf("%s: %v", dsn, err)
		}
		if got := checkLocal(config) == nil; got != local {
			t.Errorf("%s: local %v, want %v", dsn, got, local)
		}
	}
}
