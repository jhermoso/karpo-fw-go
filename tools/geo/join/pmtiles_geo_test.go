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
	// El front fija maxzoom 8 en su fuente y amplía por encima: con menos pediría teselas que no hay.
	if h[100] != 0 || h[101] != 8 {
		t.Errorf("zoom %d-%d, se espera 0-8", h[100], h[101])
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

// D-b (i): a z0 ya están todos los municipios, como rótulo (capa labels, tesela 0/0/0). Su
// polígono aparece en cuanto tiene área a ese zoom: a z0 una unidad mide unos 10 km.
func TestGenerated_MunicipalitiesAtZ0(t *testing.T) {
	out := os.Getenv("GEO_OUT")
	if out == "" {
		t.Skip("GEO_OUT no apunta a la salida de build.sh")
	}
	data, err := os.ReadFile(filepath.Join(out, "karpo.pmtiles"))
	if err != nil {
		t.Fatal(err)
	}
	u64 := func(at int) uint64 { return binary.LittleEndian.Uint64(data[at : at+8]) }
	// La primera entrada del directorio raíz es la tesela 0/0/0 o un directorio hoja que la contiene.
	run, length, offset := firstEntry(t, gunzip(t, data[u64(8):u64(8)+u64(16)]))
	for run == 0 {
		leaf := u64(40) + offset
		run, length, offset = firstEntry(t, gunzip(t, data[leaf:leaf+length]))
	}
	tile := gunzip(t, data[u64(56)+offset:u64(56)+offset+length])
	local := map[string]int{}
	for _, layer := range fields(t, tile, 3) {
		name := string(fields(t, layer, 1)[0])
		keys, values := fields(t, layer, 3), fields(t, layer, 4)
		for _, feat := range fields(t, layer, 2) {
			tags := packed(t, fields(t, feat, 2))
			for i := 0; i+1 < len(tags); i += 2 {
				if string(keys[tags[i]]) == "level" && string(fields(t, values[tags[i+1]], 1)[0]) == "local" {
					local[name]++
				}
			}
		}
	}
	t.Logf("z0: %d B, municipios por capa %v", length, local)
	if local["labels"] != 8132 || local["boundaries"] == 0 {
		t.Errorf("municipios a z0: %v, se esperan los 8132 rótulos", local)
	}
}

// firstEntry lee la primera entrada de un directorio: columnas de tile_id (delta), run_length,
// length y offset (más uno), con n valores cada una.
func firstEntry(t *testing.T, dir []byte) (run, length, offset uint64) {
	r := bytes.NewReader(dir)
	n := uvarint(t, r)
	if n == 0 || uvarint(t, r) != 0 {
		t.Fatal("la primera entrada del directorio no es la tesela 0/0/0")
	}
	skip := func(k uint64) {
		for i := uint64(0); i < k; i++ {
			uvarint(t, r)
		}
	}
	skip(n - 1)
	run = uvarint(t, r)
	skip(n - 1)
	length = uvarint(t, r)
	skip(n - 1)
	return run, length, uvarint(t, r) - 1
}

func gunzip(t *testing.T, b []byte) []byte {
	z, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func uvarint(t *testing.T, r *bytes.Reader) uint64 {
	v, err := binary.ReadUvarint(r)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// fields devuelve los campos de tipo longitud (protobuf) con ese número; salta los demás.
func fields(t *testing.T, msg []byte, num uint64) [][]byte {
	var out [][]byte
	r := bytes.NewReader(msg)
	for r.Len() > 0 {
		key := uvarint(t, r)
		switch key & 7 {
		case 0:
			uvarint(t, r)
		case 1:
			r.Seek(8, io.SeekCurrent)
		case 5:
			r.Seek(4, io.SeekCurrent)
		case 2:
			l := uvarint(t, r)
			b := make([]byte, l)
			io.ReadFull(r, b)
			if key>>3 == num {
				out = append(out, b)
			}
		default:
			t.Fatalf("tipo protobuf %d", key&7)
		}
	}
	return out
}

func packed(t *testing.T, bs [][]byte) []uint64 {
	var out []uint64
	for _, b := range bs {
		r := bytes.NewReader(b)
		for r.Len() > 0 {
			out = append(out, uvarint(t, r))
		}
	}
	return out
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
