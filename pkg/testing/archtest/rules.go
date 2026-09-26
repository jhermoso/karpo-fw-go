// Package archtest provides automated architecture tests that enforce the layering rules
// (Distribution -> Application -> Domain, adapters at the edges) by parsing Go imports.
// Bounded contexts built on the framework can reuse it on their own packages.
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

// Import is one import found in a non-test Go file.
type Import struct {
	File string
	Path string
}

// Imports returns the imports of the package in dir (recursive includes sub-packages).
// Test files and directories starting with "_" or "." or named testdata are skipped.
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

// IsStdlib reports whether an import path belongs to the standard library
// (its first element has no dot).
func IsStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// AssertPackageDoesNotImport fails when any file of the package in packageDir imports a path
// containing one of the forbidden fragments.
func AssertPackageDoesNotImport(t *testing.T, packageDir string, forbidden []string) {
	t.Helper()
	assertForbidden(t, packageDir, false, forbidden)
}

// AssertTreeDoesNotImport is AssertPackageDoesNotImport applied to packageDir and all its
// sub-packages.
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

// AssertTreeOnlyImports fails when packageDir (recursively) imports anything other than the
// standard library (if allowStdlib) or paths starting with one of the allowed prefixes.
// It is the allow-list form, the strongest guarantee for pure layers such as the domain.
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

// AssertNoThirdParty fails when packageDir (recursively) imports any non-standard, non-module path.
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

// FindProjectRoot walks up from startDir to the directory containing go.mod.
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
