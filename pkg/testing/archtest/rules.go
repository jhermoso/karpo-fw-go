// Package archtest ofrece pruebas de arquitectura automáticas que hacen cumplir las reglas de
// capas (Distribution -> Application -> Domain, adaptadores en los bordes) analizando las
// importaciones de Go. Los contextos delimitados construidos sobre el framework pueden
// reutilizarlo en sus propios paquetes.
package archtest

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Import es una importación encontrada en un fichero Go que no es de prueba.
type Import struct {
	File string
	Path string
}

// Imports devuelve las importaciones del paquete de dir (recursive incluye los subpaquetes).
// Se saltan los ficheros de prueba y los directorios que empiezan por "_" o "." o se llaman
// testdata.
func Imports(dir string, recursive bool) ([]Import, error) {
	var out []Import
	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != dir && (!recursive || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			out = append(out, Import{File: path, Path: p})
		}
		return nil
	}
	return out, filepath.WalkDir(dir, walk)
}

// IsStdlib dice si una ruta de importación pertenece a la biblioteca estándar
// (su primer elemento no tiene punto).
func IsStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// AssertPackageDoesNotImport falla cuando algún fichero del paquete de packageDir importa una
// ruta que contiene uno de los fragmentos prohibidos.
func AssertPackageDoesNotImport(t *testing.T, packageDir string, forbidden []string) {
	t.Helper()
	assertForbidden(t, packageDir, false, forbidden)
}

// AssertTreeDoesNotImport es AssertPackageDoesNotImport aplicado a packageDir y a todos sus
// subpaquetes.
func AssertTreeDoesNotImport(t *testing.T, packageDir string, forbidden []string) {
	t.Helper()
	assertForbidden(t, packageDir, true, forbidden)
}

func assertForbidden(t *testing.T, dir string, recursive bool, forbidden []string) {
	t.Helper()
	imps, err := Imports(dir, recursive)
	if err != nil {
		t.Fatalf("archtest: %v", err)
	}
	for _, imp := range imps {
		for _, f := range forbidden {
			if strings.Contains(imp.Path, f) {
				t.Errorf("architecture violation: %s imports %q (forbidden: %q)", rel(dir, imp.File), imp.Path, f)
			}
		}
	}
}

// AssertTreeOnlyImports falla cuando packageDir (recursivamente) importa algo que no sea la
// biblioteca estándar (si allowStdlib) o rutas que empiezan por uno de los prefijos permitidos.
// Es la forma de lista de permitidos, la garantía más fuerte para capas puras como el dominio.
func AssertTreeOnlyImports(t *testing.T, packageDir string, allowStdlib bool, allowed []string) {
	t.Helper()
	imps, err := Imports(packageDir, true)
	if err != nil {
		t.Fatalf("archtest: %v", err)
	}
	for _, imp := range imps {
		if allowStdlib && IsStdlib(imp.Path) {
			continue
		}
		ok := false
		for _, a := range allowed {
			if imp.Path == a || strings.HasPrefix(imp.Path, a+"/") {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("architecture violation: %s imports %q, only the standard library and %v are allowed",
				rel(packageDir, imp.File), imp.Path, allowed)
		}
	}
}

// Std es el patrón que casa con cualquier importación de la biblioteca estándar en
// AssertOnlyImports.
const Std = "std"

// Matches dice si una ruta de importación casa con un patrón: "std" (biblioteca estándar),
// "path/..." (path y todos sus subpaquetes) o una ruta de paquete exacta.
func Matches(pattern, path string) bool {
	switch {
	case pattern == Std:
		return IsStdlib(path)
	case strings.HasSuffix(pattern, "/..."):
		root := strings.TrimSuffix(pattern, "/...")
		return path == root || strings.HasPrefix(path, root+"/")
	default:
		return path == pattern
	}
}

// AssertOnlyImports falla cuando el paquete de dir (y sus subpaquetes si recursive) importa algo
// que no casa con ninguno de los patrones (ver Matches). Es la regla de lista de permitidos que
// mantiene los paquetes de contratos libres de implementaciones.
func AssertOnlyImports(t *testing.T, dir string, recursive bool, patterns ...string) {
	t.Helper()
	imps, err := Imports(dir, recursive)
	if err != nil {
		t.Fatalf("archtest: %v", err)
	}
	for _, imp := range imps {
		ok := false
		for _, p := range patterns {
			if Matches(p, imp.Path) {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("architecture violation: %s imports %q, allowed: %v", rel(dir, imp.File), imp.Path, patterns)
		}
	}
}

// AssertNoThirdParty falla cuando packageDir (recursivamente) importa una ruta que no es de la
// biblioteca estándar ni del módulo.
func AssertNoThirdParty(t *testing.T, packageDir, modulePath string) {
	t.Helper()
	AssertTreeOnlyImports(t, packageDir, true, []string{modulePath})
}

func rel(base, file string) string {
	if r, err := filepath.Rel(base, file); err == nil {
		return r
	}
	return file
}

// FindProjectRoot sube desde startDir hasta el directorio que contiene go.mod.
func FindProjectRoot(startDir string) (string, error) {
	current, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("go.mod not found in hierarchy of %s", startDir)
		}
		current = parent
	}
}
