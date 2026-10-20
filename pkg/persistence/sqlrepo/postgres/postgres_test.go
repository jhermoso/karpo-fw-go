package postgres_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
)

func TestWithoutStatementCache(t *testing.T) {
	cases := map[string]string{
		"postgres://u:p@h:5432/db":                     "postgres://u:p@h:5432/db?default_query_exec_mode=describe_exec",
		"postgres://u:p@h/db?sslmode=disable":          "postgres://u:p@h/db?sslmode=disable&default_query_exec_mode=describe_exec",
		"host=h user=u dbname=db":                      "host=h user=u dbname=db default_query_exec_mode=describe_exec",
		"postgres://h/db?default_query_exec_mode=exec": "postgres://h/db?default_query_exec_mode=exec",
	}
	for in, want := range cases {
		if got := postgres.WithoutStatementCache(in); got != want {
			t.Errorf("%s:\nwant %s\ngot  %s", in, want, got)
		}
	}
}
