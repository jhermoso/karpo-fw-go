// Package archtest provides automated architecture tests to enforce Clean Architecture rules.
package archtest

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AssertPackageDoesNotImport asserts that no Go source file in packageDir imports any forbidden package paths.
func AssertPackageDoesNotImport(t *testing.T, packageDir string, forbiddenPrefixes []string) {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, packageDir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)

	if err != nil {
		t.Fatalf("failed to parse package directory %s: %v", packageDir, err)
	}

	for _, pkg := range pkgs {
		for fileName, file := range pkg.Files {
			for _, imp := range file.Imports {
				importPath := strings.Trim(imp.Path.Value, `"`)
				for _, forbidden := range forbiddenPrefixes {
					if strings.Contains(importPath, forbidden) {
						t.Errorf(
							"Architecture Violation in %s:\n  Package '%s' violates layer boundaries by importing forbidden path '%s'",
							filepath.Base(fileName),
							pkg.Name,
							importPath,
						)
					}
				}
			}
		}
	}
}

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
