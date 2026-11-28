// Command geokeys escribe en la salida estándar keys.csv: la tabla de claves del mapa (ISO 3166 e
// INE; la isla, su NUTS) que la tubería tools/geo usa para enlazar las geometrías con la semilla.
// La regla es la de la API de geografía (domain.MapCode), así que tesela y API casan.
//
//	go run ./cmd/geokeys > keys.csv
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"

	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "geokeys:", err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	s, err := infrastructure.LoadSeed()
	if err != nil {
		return err
	}
	keys, err := infrastructure.MapKeys(s, gapp.MaxDepth)
	if err != nil {
		return err
	}
	w := csv.NewWriter(out)
	_ = w.Write(infrastructure.MapKeyHeader)
	for _, k := range keys {
		_ = w.Write(k.Row())
	}
	w.Flush()
	return w.Error()
}
