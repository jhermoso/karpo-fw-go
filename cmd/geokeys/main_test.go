package main

import (
	"encoding/csv"
	"slices"
	"strings"
	"testing"

	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
)

// La salida es la cabecera y una fila por clave de MapKeys, en su orden.
func TestRun_WritesMapKeys(t *testing.T) {
	var out strings.Builder
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(out.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	s, err := infrastructure.LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := infrastructure.MapKeys(s, gapp.MaxDepth)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(keys)+1 || !slices.Equal(rows[0], infrastructure.MapKeyHeader) {
		t.Fatalf("%d filas para %d claves; cabecera %v", len(rows), len(keys), rows[0])
	}
	for i, k := range keys {
		if !slices.Equal(rows[i+1], k.Row()) {
			t.Fatalf("fila %d: %v; quería %v", i+1, rows[i+1], k.Row())
		}
	}
}
