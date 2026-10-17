package archtest_test

import (
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// La publicadora de humo (host/smoke y cmd/karpo-smoke) deja entrar a cualquiera sin
// credenciales. Sólo existe con la etiqueta de compilación smoke, y este fichero, que no tiene
// etiqueta y por eso se ejecuta siempre, es con lo que el pipeline lo comprueba.

// smokeTag es la línea con la que empieza cada fichero de la publicadora de humo.
const smokeTag = "//go:build smoke"

// smokePackage y smokeRoute son lo que nada sin la etiqueta puede importar ni nombrar. La ruta
// se escribe en dos trozos para que este fichero no la nombre.
const (
	smokePackage = "/host/smoke"
	smokeRoute   = "/api/" + "smoke"
)

// smokeTrees son los directorios en los que todo es de la publicadora de humo.
var smokeTrees = []string{"host/smoke", "cmd/karpo-smoke"}

// importsSmoke es cómo termina una infracción de la regla 10.
const importsSmoke = "imports the smoke publisher"

// behindSmoke dice si una restricción de compilación es falsa siempre que no está puesta la
// etiqueta smoke: la etiqueta misma, un && con un lado que lo es, o un || con los dos lados que
// lo son.
func behindSmoke(x constraint.Expr) bool {
	switch x := x.(type) {
	case *constraint.TagExpr:
		return x.Tag == "smoke"
	case *constraint.AndExpr:
		return behindSmoke(x.X) || behindSmoke(x.Y)
	case *constraint.OrExpr:
		return behindSmoke(x.X) && behindSmoke(x.Y)
	}
	return false
}

// readSmoke analiza la cabecera de un fichero Go y dice si su restricción de compilación (la
// línea //go:build antes de la cláusula package, tal como la lee el compilador) lo deja detrás
// de la etiqueta smoke, y si importa la publicadora de humo.
func readSmoke(path string) (tagged, imports bool, err error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return false, false, err
	}
	for _, group := range f.Comments {
		for _, c := range group.List {
			if c.Pos() > f.Package || !constraint.IsGoBuild(c.Text) {
				continue
			}
			if x, err := constraint.Parse(c.Text); err == nil && behindSmoke(x) {
				tagged = true
			}
		}
	}
	for _, i := range f.Imports {
		if p, _ := strconv.Unquote(i.Path.Value); p == module+smokePackage || strings.HasPrefix(p, module+smokePackage+"/") {
			imports = true
		}
	}
	return tagged, imports, nil
}

// smokeViolations recorre todos los ficheros Go bajo root, pruebas incluidas y todos los
// programas de cmd, y dice qué incumple las reglas: un fichero de la publicadora de humo que no
// está detrás de la etiqueta, un fichero fuera del publicador que lo importa (con la etiqueta o
// sin ella: un programa real compilado con la etiqueta tendría la puerta), o un fichero sin la
// etiqueta que nombra sus rutas.
func smokeViolations(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		tagged, imports, err := readSmoke(path)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(smokeTrees, func(tree string) bool { return strings.HasPrefix(rel, tree+"/") }) {
			if !tagged {
				out = append(out, rel+": a file of the smoke publisher that is not behind the build tag smoke")
			}
			return nil
		}
		if imports {
			out = append(out, rel+": "+importsSmoke)
		}
		if tagged {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(text), smokeRoute) {
			out = append(out, rel+": names "+smokeRoute+" without the build tag smoke")
		}
		return nil
	})
	return out, err
}

// Regla 10: nada salvo la publicadora de humo importa la publicadora de humo: ningún programa de
// cmd, ningún paquete, ninguna prueba, con la etiqueta de compilación o sin ella.
func TestSmoke_IsNotImportedByThePrograms(t *testing.T) {
	violations, err := smokeViolations(root(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		if strings.HasSuffix(v, importsSmoke) {
			t.Error(v)
		}
	}
	// Los programas reales están donde la regla los busca.
	for _, program := range []string{"karpo", "karpo-postgres"} {
		if files, _ := filepath.Glob(filepath.Join(root(t, "cmd", program), "*.go")); len(files) == 0 {
			t.Errorf("cmd/%s has no Go files: the rule is looking in the wrong place", program)
		}
	}
}

// Regla 11: la publicadora de humo se queda detrás de su etiqueta de compilación, y nada sin la
// etiqueta nombra sus rutas.
func TestSmoke_StaysBehindItsTag(t *testing.T) {
	violations, err := smokeViolations(root(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		if !strings.HasSuffix(v, importsSmoke) {
			t.Error(v)
		}
	}
	// El publicador está donde la regla lo busca: si se mueve, la regla se mueve con él.
	for _, tree := range smokeTrees {
		if files, _ := filepath.Glob(filepath.Join(root(t, filepath.FromSlash(tree)), "*.go")); len(files) == 0 {
			t.Errorf("%s has no Go files: the rule is looking in the wrong place", tree)
		}
	}
}

// La regla muerde: se ve cada una de las formas de salirse de la etiqueta.
func TestSmoke_TheRuleBites(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	imported := "\n\nimport _ \"" + module + smokePackage + "\"\n"
	named := "\n\nconst door = \"" + smokeRoute + "\"\n"
	write("host/smoke/smoke.go", smokeTag+"\n\npackage smoke\n")
	write("host/smoke/more.go", "// Something said before.\n\n//go:build smoke && !race\n\npackage smoke\n") // detrás de la etiqueta, y algo más
	write("cmd/karpo-smoke/main.go", smokeTag+"\n\npackage main"+imported)
	write("cmd/karpo-smoke/main_test.go", smokeTag+"\n\npackage main"+imported)
	write("integration/door_test.go", smokeTag+"\n\npackage integration"+named) // con la etiqueta, la puerta se puede nombrar
	write("host/host.go", "package host\n")
	if got, err := smokeViolations(dir); err != nil || len(got) != 0 {
		t.Fatalf("what is right is taken for wrong: %v %v", got, err)
	}

	wrong := map[string]string{
		"host/smoke/extra.go":            "package smoke\n",                                 // un fichero del publicador sin la etiqueta
		"cmd/karpo-smoke/other.go":       "//go:build !production\n\npackage main\n",        // otra etiqueta no es la etiqueta
		"host/smoke/either.go":           "//go:build smoke || linux\n\npackage smoke\n",    // en linux no necesita etiqueta
		"host/smoke/never.go":            "//go:build !smoke\n\npackage smoke\n",            // la etiqueta, al revés
		"cmd/karpo/main.go":              "package main" + imported,                         // el programa real lo importa
		"cmd/karpo/door.go":              smokeTag + "\n\npackage main" + imported,          // también con la etiqueta: compilado con ella, tiene la puerta
		"cmd/another/main_test.go":       "package main" + imported,                         // una prueba, de un programa que nadie ha listado
		"integration/enter_test.go":      smokeTag + "\n\npackage integration" + imported,   // una prueba con la etiqueta
		"host/serve/serve.go":            "package serve" + named,                           // el programa real nombra la puerta
		"contexts/security/door_test.go": "package security" + named,                        // una prueba sin la etiqueta
		"pkg/late.go":                    "package pkg\n\n" + smokeTag + named,              // la etiqueta después de la cláusula package no es una etiqueta
		"pkg/block.go":                   "/*\n" + smokeTag + "\n*/\n\npackage pkg" + named, // ni lo es dentro de un comentario de bloque
		"pkg/old.go":                     "// +build smoke\n\npackage pkg" + named,          // ni en la forma antigua, que no se mira
	}
	for rel, text := range wrong {
		write(rel, text)
	}
	write(".hidden/skipped.go", "package hidden"+named) // no se mira
	got, err := smokeViolations(dir)
	if err != nil {
		t.Fatal(err)
	}
	for file := range wrong {
		if !slices.ContainsFunc(got, func(v string) bool { return strings.HasPrefix(v, file+":") }) {
			t.Errorf("%s is not seen: %v", file, got)
		}
	}
	if len(got) != len(wrong) {
		t.Errorf("%d violations, want %d: %v", len(got), len(wrong), got)
	}
}
