package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKeys = `code,parentCode,name,level,kind,hasChildren,lauCode,ine,islandOf
ES,,España,0,country,true,,,
PT,,Portugal,0,country,false,,,
FR,,Francia,0,country,false,,,
ES-IB,ES,Illes Balears,1,region,true,,,
07,ES-IB,Illes Balears,2,province,true,,,
ES531,07,Mallorca,3,island,true,,,
07040,07,Palma,3,municipality,false,07040,07040,ES531
`

const sq = `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`

// Una página con la comunidad (nivel como objeto), la provincia y el municipio insular (nivel
// como texto), el municipio repetido, una unidad esperada sin enlazar y una que no enlaza.
func testPage(extra string) string {
	f := func(code, level string) string {
		return `{"type":"Feature","properties":{"nationalCode":"` + code + `","nationalLevelName":` + level + `},"geometry":` + sq + `}`
	}
	fs := []string{
		f("34040000000", `{"LocalisedCharacterString":"Comunidad autónoma"}`),
		f("34040700000", `"Provincia"`),
		f("34040707040", `"Municipio"`),
		f("34040707040", `"Municipio"`),
		f("34000000000", `"País"`),
	}
	if extra != "" {
		fs = append(fs, f(extra, `"Municipio"`))
	}
	return `{"type":"FeatureCollection","features":[` + strings.Join(fs, ",") + `]}`
}

const testNE = `{"type":"FeatureCollection","features":[
{"type":"Feature","properties":{"ISO_A2_EH":"ES"},"geometry":` + sq + `},
{"type":"Feature","properties":{"ISO_A2_EH":"PT"},"geometry":` + sq + `},
{"type":"Feature","properties":{"ISO_A2_EH":"PT"},"geometry":` + sq + `},
{"type":"Feature","properties":{"ISO_A2_EH":"07"},"geometry":` + sq + `},
{"type":"Feature","properties":{"ISO_A2_EH":"-99"},"geometry":` + sq + `}]}`

type fixture struct{ dir, keys, page, expected string }

func newFixture() fixture {
	return fixture{keys: testKeys, page: testPage(""),
		expected: "# duplicado y país\n34040707040 repetido\n34000000000 país\n"}
}

func (fx fixture) run(t *testing.T) (error, string, []map[string]any, []map[string]any) {
	t.Helper()
	d := t.TempDir()
	for name, body := range map[string]string{"keys.csv": fx.keys, "au-1.json": fx.page, "ne.geojson": testNE, "expected.txt": fx.expected} {
		if err := os.WriteFile(filepath.Join(d, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var stderr strings.Builder
	p := func(n string) string { return filepath.Join(d, n) }
	err := run([]string{"-keys", p("keys.csv"), "-ne", p("ne.geojson"), "-ign", p("au-*.json"),
		"-expected", p("expected.txt"), "-out", p("out.geojsonl"), "-islands", p("islands.geojsonl")}, &stderr)
	return err, stderr.String(), readLines(t, p("out.geojsonl")), readLines(t, p("islands.geojsonl"))
}

func readLines(t *testing.T, path string) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []map[string]any
	s := bufio.NewScanner(f)
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		var v struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(s.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v.Properties)
	}
	return out
}

func TestRun_JoinsAndOrders(t *testing.T) {
	err, stderr, out, islands := newFixture().run(t)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, p := range out {
		codes = append(codes, p["code"].(string))
	}
	// Una vez cada unidad; los países al final, sólo los de clase country y sin repetir.
	if got := strings.Join(codes, " "); got != "ES-IB 07 07040 ES PT" {
		t.Fatalf("salida: %s", got)
	}
	if out[2]["parentCode"] != "07" || out[2]["lauCode"] != "07040" || out[4]["parentCode"] != nil {
		t.Fatalf("propiedades: %v %v", out[2], out[4])
	}
	// El municipio insular, otra vez, con las propiedades de su isla.
	if len(islands) != 1 || islands[0]["code"] != "ES531" || islands[0]["kind"] != "island" {
		t.Fatalf("islas: %v", islands)
	}
	if !strings.Contains(stderr, "join: 5 entities") || !strings.Contains(stderr, "(1): FR") {
		t.Fatalf("stderr: %s", stderr)
	}
}

func TestRun_Fails(t *testing.T) {
	cases := map[string]func(*fixture){
		"unidad inesperada":      func(fx *fixture) { fx.page = testPage("34040799999") },
		"esperada que no está":   func(fx *fixture) { fx.expected += "34111111111 sobra\n" },
		"clave sin geometría":    func(fx *fixture) { fx.keys += "07001,07,Alaró,3,municipality,false,07001,07001,\n" },
		"cabecera de keys.csv":   func(fx *fixture) { fx.keys = "code,name\n" },
		"página que no es JSON":  func(fx *fixture) { fx.page = "{" },
		"sin lista de esperados": func(fx *fixture) { fx.expected = "\x00" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newFixture()
			change(&fx)
			if err, _, _, _ := fx.run(t); err == nil {
				t.Fatal("no falla")
			}
		})
	}
}

func TestRun_MissingInputs(t *testing.T) {
	d := t.TempDir()
	for _, args := range [][]string{
		{"-bad"},
		{"-keys", filepath.Join(d, "none.csv")},
	} {
		if run(args, io.Discard) == nil {
			t.Errorf("%v no falla", args)
		}
	}
	os.WriteFile(filepath.Join(d, "k.csv"), []byte(testKeys), 0o644)
	os.WriteFile(filepath.Join(d, "e.txt"), nil, 0o644)
	base := []string{"-keys", filepath.Join(d, "k.csv"), "-expected", filepath.Join(d, "e.txt"),
		"-out", filepath.Join(d, "o"), "-islands", filepath.Join(d, "i")}
	for _, extra := range [][]string{
		{"-expected", filepath.Join(d, "none.txt")},
		{"-ign", filepath.Join(d, "nada-*.json")},
		{"-out", filepath.Join(d, "no", "dir", "o")},
		{"-islands", filepath.Join(d, "no", "dir", "i")},
	} {
		if run(append(append([]string{}, base...), extra...), io.Discard) == nil {
			t.Errorf("%v no falla", extra)
		}
	}
	// Sin Natural Earth.
	os.WriteFile(filepath.Join(d, "au-1.json"), []byte(testPage("")), 0o644)
	if run(append(base, "-ign", filepath.Join(d, "au-*.json"), "-ne", filepath.Join(d, "none.geojson")), io.Discard) == nil {
		t.Error("sin Natural Earth no falla")
	}
}
