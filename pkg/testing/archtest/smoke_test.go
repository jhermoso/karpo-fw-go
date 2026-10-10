package archtest_test

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

// The smoke publisher (host/smoke and cmd/karpo-smoke) lets anybody in without credentials. It
// exists only with the build tag smoke, and this file, which has no tag and so always runs, is
// what the pipeline checks it with.

// smokeTag is the line every file of the smoke publisher starts with.
const smokeTag = "//go:build smoke"

// smokePackage and smokeRoute are what nothing without the tag may import or name. The route is
// written in two pieces so that this file does not name it.
const (
	smokePackage = "/host/smoke"
	smokeRoute   = "/api/" + "smoke"
)

// smokeTrees are the directories where everything is of the smoke publisher.
var smokeTrees = []string{"host/smoke", "cmd/karpo-smoke"}

// hasSmokeTag says whether a Go file carries the build tag smoke, alone, before its package clause.
func hasSmokeTag(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	lines := bufio.NewScanner(f)
	lines.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == smokeTag {
			return true, nil
		}
		if strings.HasPrefix(line, "package ") {
			break
		}
	}
	return false, lines.Err()
}

// smokeViolations walks every Go file under root, tests included, and says what breaks the rule:
// a file of the smoke publisher without the tag, or a file without the tag that imports the
// smoke publisher or names its routes.
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
		tagged, err := hasSmokeTag(path)
		if err != nil {
			return err
		}
		if tagged {
			return nil
		}
		if slices.ContainsFunc(smokeTrees, func(tree string) bool { return strings.HasPrefix(rel, tree+"/") }) {
			out = append(out, rel+": a file of the smoke publisher without the line "+smokeTag)
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(text), smokeRoute) {
			out = append(out, rel+": names "+smokeRoute+" without the build tag smoke")
		}
		if strings.Contains(string(text), `"`+module+smokePackage) {
			out = append(out, rel+": imports the smoke publisher without the build tag smoke")
		}
		return nil
	})
	return out, err
}

// Rule 10: what is served for real never imports the smoke publisher.
func TestSmoke_IsNotImportedByThePrograms(t *testing.T) {
	for _, tree := range [][]string{{"cmd", "karpo"}, {"cmd", "karpo-postgres"}, {"host"}, {"pkg"}, {"contexts"}, {"examples"}, {"integration"}} {
		archtest.AssertTreeDoesNotImport(t, root(t, tree...), []string{smokePackage})
	}
}

// Rule 11: the smoke publisher stays behind its build tag, and nothing without the tag imports it
// or names its routes.
func TestSmoke_StaysBehindItsTag(t *testing.T) {
	violations, err := smokeViolations(root(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Error(v)
	}
	// The publisher is where the rule looks for it: if it moves, the rule moves with it.
	for _, tree := range smokeTrees {
		if files, _ := filepath.Glob(filepath.Join(root(t, filepath.FromSlash(tree)), "*.go")); len(files) == 0 {
			t.Errorf("%s has no Go files: the rule is looking in the wrong place", tree)
		}
	}
}

// The rule bites: each of the ways out of the tag is seen.
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
	write("host/smoke/smoke.go", smokeTag+"\n\npackage smoke\n")
	write("cmd/karpo-smoke/main.go", smokeTag+"\n\npackage main\n\nimport _ \""+module+smokePackage+"\"\n")
	write("host/host.go", "package host\n")
	if got, err := smokeViolations(dir); err != nil || len(got) != 0 {
		t.Fatalf("what is right is taken for wrong: %v %v", got, err)
	}

	write("host/smoke/extra.go", "package smoke\n")                                                  // a file of the publisher without the tag
	write("cmd/karpo-smoke/other.go", "//go:build !production\n\npackage main\n")                    // another tag is not the tag
	write("cmd/karpo/main.go", "package main\n\nimport _ \""+module+smokePackage+"\"\n")             // the real program imports it
	write("host/serve/serve.go", "package serve\n\nconst door = \""+smokeRoute+"/token\"\n")         // the real program names the door
	write("contexts/security/door_test.go", "package security\n\nconst door = \""+smokeRoute+"\"\n") // a test without the tag
	write("pkg/late.go", "package pkg\n\n"+smokeTag+"\n\nconst door = \""+smokeRoute+"\"\n")         // the tag after the package clause is not a tag
	write(".hidden/skipped.go", "package hidden\n\nconst door = \""+smokeRoute+"\"\n")               // not looked at
	got, err := smokeViolations(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"host/smoke/extra.go", "cmd/karpo-smoke/other.go", "cmd/karpo/main.go", "host/serve/serve.go", "contexts/security/door_test.go", "pkg/late.go"} {
		if !slices.ContainsFunc(got, func(v string) bool { return strings.HasPrefix(v, file+":") }) {
			t.Errorf("%s is not seen: %v", file, got)
		}
	}
	if len(got) != 6 {
		t.Errorf("%d violations, want 6: %v", len(got), got)
	}
}
