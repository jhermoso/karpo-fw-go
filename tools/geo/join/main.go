// Command join enlaza las geometrías de Natural Earth (países) y de la BDDAE del IGN (comunidades,
// provincias y municipios) con keys.csv, la tabla de claves del mapa que escribe cmd/geokeys, y
// escribe GeoJSON por líneas con las propiedades de la tesela.
//
//	go run ./tools/geo/join -keys keys.csv -ne ne.geojson -ign "au-*.json" \
//	    -expected tools/geo/expected-unmatched.txt -out boundaries.geojsonl -islands island-municipalities.geojsonl
//
// Lo que la BDDAE trae y no enlaza se compara con la lista cerrada expected-unmatched: cualquier
// diferencia, de más o de menos, es un error. Una clave española sin geometría también lo es; los
// países de la semilla que Natural Earth no trae se listan en la salida de error.
package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Key es una fila de keys.csv.
type Key struct {
	Code, ParentCode, Name, Level, Kind string
	HasChildren                         bool
	LAU, IslandOf                       string
}

// regions traduce el código de comunidad del INE (las cifras 3 y 4 del código nacional de la
// BDDAE) a su clave ISO 3166-2 tal como la da la semilla (Navarra, ES-NA: defecto conocido).
var regions = map[string]string{
	"01": "ES-AN", "02": "ES-AR", "03": "ES-AS", "04": "ES-IB", "05": "ES-CN", "06": "ES-CB", "07": "ES-CL", "08": "ES-CM",
	"09": "ES-CT", "10": "ES-VC", "11": "ES-EX", "12": "ES-GA", "13": "ES-MD", "14": "ES-MC", "15": "ES-NA", "16": "ES-PV",
	"17": "ES-RI", "18": "ES-CE", "19": "ES-ML",
}

// IGNCode devuelve la clave del mapa de un código nacional de la BDDAE: 11 cifras, que son 34
// (España), la comunidad, la provincia y las 5 del municipio, que son su INE. level es el nombre
// del nivel en la BDDAE.
func IGNCode(national, level string) (string, bool) {
	if len(national) != 11 || !strings.HasPrefix(national, "34") {
		return "", false
	}
	switch {
	case strings.HasPrefix(level, "Comunidad") || strings.HasPrefix(level, "Ciudad"):
		code, ok := regions[national[2:4]]
		return code, ok
	case level == "Provincia":
		return national[4:6], true
	case level == "Municipio":
		return national[6:11], true
	}
	return "", false
}

func readKeys(path string) (map[string]Key, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || strings.Join(rows[0], ",") != "code,parentCode,name,level,kind,hasChildren,lauCode,ine,islandOf" {
		return nil, errors.New("keys.csv: unexpected header")
	}
	out := map[string]Key{}
	for _, r := range rows[1:] {
		out[r[0]] = Key{Code: r[0], ParentCode: r[1], Name: r[2], Level: r[3], Kind: r[4], HasChildren: r[5] == "true", LAU: r[6], IslandOf: r[8]}
	}
	return out, nil
}

// ReadExpected lee la lista cerrada de códigos nacionales que no enlazan (código y motivo).
func ReadExpected(r io.Reader) (map[string]bool, error) {
	out := map[string]bool{}
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[strings.Fields(line)[0]] = true
	}
	return out, s.Err()
}

// CompareUnmatched falla si lo que no enlaza no es exactamente lo esperado.
func CompareUnmatched(got, expected map[string]bool) error {
	var extra, missing []string
	for c := range got {
		if !expected[c] {
			extra = append(extra, c)
		}
	}
	for c := range expected {
		if !got[c] {
			missing = append(missing, c)
		}
	}
	slices.Sort(extra)
	slices.Sort(missing)
	if len(extra)+len(missing) > 0 {
		return fmt.Errorf("unmatched BDDAE units differ from expected-unmatched: not listed %v; listed but matched or absent %v", extra, missing)
	}
	return nil
}

type feature struct {
	Properties map[string]any  `json:"properties"`
	Geometry   json.RawMessage `json:"geometry"`
}

// Properties son las propiedades de la tesela de una clave.
func Properties(k Key) map[string]any {
	p := map[string]any{"code": k.Code, "name": k.Name, "level": k.Level, "kind": k.Kind, "hasChildren": k.HasChildren}
	if k.ParentCode != "" {
		p["parentCode"] = k.ParentCode
	}
	if k.LAU != "" {
		p["lauCode"] = k.LAU
	}
	return p
}

func write(w *bufio.Writer, props map[string]any, geom json.RawMessage) error {
	b, err := json.Marshal(map[string]any{"type": "Feature", "properties": props, "geometry": geom})
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func readFeatures(path string) ([]feature, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var fc struct {
		Features []feature `json:"features"`
	}
	if err := json.NewDecoder(bufio.NewReader(f)).Decode(&fc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return fc.Features, nil
}

func levelName(p map[string]any) string {
	if m, ok := p["nationalLevelName"].(map[string]any); ok {
		s, _ := m["LocalisedCharacterString"].(string)
		return s
	}
	s, _ := p["nationalLevelName"].(string)
	return s
}

func run(args []string, stderr io.Writer) error {
	fl := flag.NewFlagSet("join", flag.ContinueOnError)
	keysPath := fl.String("keys", "keys.csv", "tabla de claves (cmd/geokeys)")
	nePath := fl.String("ne", "", "países de Natural Earth en GeoJSON")
	ignGlob := fl.String("ign", "", "páginas GeoJSON de la BDDAE (patrón)")
	expectedPath := fl.String("expected", "tools/geo/expected-unmatched.txt", "lista cerrada de lo que no enlaza")
	outPath := fl.String("out", "boundaries.geojsonl", "salida: entidades con geometría propia")
	islandsPath := fl.String("islands", "island-municipalities.geojsonl", "salida: municipios insulares, para disolver por isla")
	if err := fl.Parse(args); err != nil {
		return err
	}

	keys, err := readKeys(*keysPath)
	if err != nil {
		return err
	}
	ef, err := os.Open(*expectedPath)
	if err != nil {
		return err
	}
	expected, err := ReadExpected(ef)
	ef.Close()
	if err != nil {
		return err
	}
	out, err := os.Create(*outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	islands, err := os.Create(*islandsPath)
	if err != nil {
		return err
	}
	defer islands.Close()
	ow, iw := bufio.NewWriter(out), bufio.NewWriter(islands)

	done := map[string]bool{}
	pages, err := filepath.Glob(*ignGlob)
	if err != nil || len(pages) == 0 {
		return fmt.Errorf("no BDDAE pages match %q", *ignGlob)
	}
	unmatched := map[string]bool{}
	for _, page := range pages {
		fs, err := readFeatures(page)
		if err != nil {
			return err
		}
		for _, f := range fs {
			national, _ := f.Properties["nationalCode"].(string)
			code, ok := IGNCode(national, levelName(f.Properties))
			k, known := keys[code]
			if !ok || !known || k.Kind == "country" || done[code] {
				unmatched[national] = true
				continue
			}
			done[code] = true
			if err := write(ow, Properties(k), f.Geometry); err != nil {
				return err
			}
			if k.IslandOf != "" {
				// Con las propiedades de su isla: la tubería disuelve agrupando por code.
				if err := write(iw, Properties(keys[k.IslandOf]), f.Geometry); err != nil {
					return err
				}
			}
		}
	}
	// Los países, al final: GDAL deduce las columnas de las primeras entidades, y los países no
	// llevan parentCode ni lauCode.
	countries, err := readFeatures(*nePath)
	if err != nil {
		return err
	}
	for _, f := range countries {
		code, _ := f.Properties["ISO_A2_EH"].(string)
		k, ok := keys[code]
		if !ok || k.Kind != "country" || done[code] {
			continue
		}
		done[code] = true
		if err := write(ow, Properties(k), f.Geometry); err != nil {
			return err
		}
	}
	if err := errors.Join(ow.Flush(), iw.Flush()); err != nil {
		return err
	}
	var missingCountries, missingSpain []string
	for code, k := range keys {
		switch {
		case done[code] || k.Kind == "island":
		case k.Kind == "country":
			missingCountries = append(missingCountries, code)
		default:
			missingSpain = append(missingSpain, code)
		}
	}
	slices.Sort(missingCountries)
	slices.Sort(missingSpain)
	fmt.Fprintf(stderr, "join: %d entities; countries without Natural Earth geometry (%d): %s\n", len(done), len(missingCountries),
		strings.Join(missingCountries, " "))
	if len(missingSpain) > 0 {
		return fmt.Errorf("keys without BDDAE geometry: %v", missingSpain)
	}
	return CompareUnmatched(unmatched, expected)
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "join:", err)
		os.Exit(1)
	}
}
