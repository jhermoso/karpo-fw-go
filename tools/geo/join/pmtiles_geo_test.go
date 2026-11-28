//go:build geo

// Comprobación del fichero generado por tools/geo/build.sh; fuera de la batería normal:
//
//	GEO_OUT=../geo-downloads/out go test -tags geo ./tools/geo/join
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerated_HeaderAndAttribution(t *testing.T) {
	out := os.Getenv("GEO_OUT")
	if out == "" {
		t.Skip("GEO_OUT no apunta a la salida de build.sh")
	}
	f, err := os.Open(filepath.Join(out, "karpo.pmtiles"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := make([]byte, 127)
	if _, err := io.ReadFull(f, h); err != nil {
		t.Fatal(err)
	}
	if string(h[:7]) != "PMTiles" || h[7] != 3 {
		t.Fatalf("cabecera: %q", h[:8])
	}
	offset, length := binary.LittleEndian.Uint64(h[24:32]), binary.LittleEndian.Uint64(h[32:40])
	raw := make([]byte, length)
	if _, err := f.ReadAt(raw, int64(offset)); err != nil {
		t.Fatal(err)
	}
	if h[97] == 2 { // gzip
		z, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if raw, err = io.ReadAll(z); err != nil {
			t.Fatal(err)
		}
	}
	var meta struct {
		Attribution  string `json:"attribution"`
		VectorLayers []struct {
			ID string `json:"id"`
		} `json:"vector_layers"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta.Attribution, "Natural Earth") || !strings.Contains(meta.Attribution, "CC-BY 4.0 ign.es") {
		t.Errorf("atribución: %q", meta.Attribution)
	}
	layers := map[string]bool{}
	for _, l := range meta.VectorLayers {
		layers[l.ID] = true
	}
	if !layers["boundaries"] || !layers["labels"] {
		t.Errorf("capas: %v", meta.VectorLayers)
	}
}

// Cada entidad de la tesela tiene un rótulo: los rótulos dicen qué entró en ella.
func TestGenerated_EveryKeyHasItsEntity(t *testing.T) {
	out := os.Getenv("GEO_OUT")
	if out == "" {
		t.Skip("GEO_OUT no apunta a la salida de build.sh")
	}
	keys, err := readKeys(filepath.Join(out, "keys.csv"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(out, "labels.geojsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	seen := map[string]bool{}
	s := bufio.NewScanner(f)
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		var feat struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(s.Bytes(), &feat); err != nil {
			t.Fatal(err)
		}
		code, _ := feat.Properties["code"].(string)
		k, ok := keys[code]
		if !ok || seen[code] {
			t.Fatalf("clave desconocida o repetida: %q", code)
		}
		seen[code] = true
		if feat.Properties["level"] != k.Level || feat.Properties["kind"] != k.Kind || feat.Properties["hasChildren"] == nil {
			t.Errorf("%s: %v", code, feat.Properties)
		}
	}
	missing := map[string]int{}
	for code, k := range keys {
		if !seen[code] {
			missing[k.Kind]++
		}
	}
	// Sólo faltan los 11 países que Natural Earth 10m no trae por separado (SOURCES.md).
	if len(missing) != 1 || missing["country"] != 11 {
		t.Errorf("faltan: %v", missing)
	}
}
