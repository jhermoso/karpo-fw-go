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

// importsSmoke is how a violation of rule 10 ends.
const importsSmoke = "imports the smoke publisher"

// behindSmoke says whether a build constraint is false whenever the tag smoke is not set: the
// tag itself, an && with a side that is, or an || with both sides that are.
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

// readSmoke parses the head of a Go file and says whether its build constraint (the //go:build
// line before the package clause, as the compiler reads it) keeps it behind the tag smoke, and
// whether it imports the smoke publisher.
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

// smokeViolations walks every Go file under root, tests included and every program of cmd, and
// says what breaks the rules: a file of the smoke publisher that is not behind the tag, a file
// outside the publisher that imports it (with the tag or without: a real program built with the
// tag would have the door), or a file without the tag that names its routes.
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

// Rule 10: nothing but the smoke publisher imports the smoke publisher: no program of cmd, no
// package, no test, with the build tag or without it.
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
	// The real programs are where the rule looks for them.
	for _, program := range []string{"karpo", "karpo-postgres"} {
		if files, _ := filepath.Glob(filepath.Join(root(t, "cmd", program), "*.go")); len(files) == 0 {
			t.Errorf("cmd/%s has no Go files: the rule is looking in the wrong place", program)
		}
	}
}

// Rule 11: the smoke publisher stays behind its build tag, and nothing without the tag names its
// routes.
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
	imported := "\n\nimport _ \"" + module + smokePackage + "\"\n"
	named := "\n\nconst door = \"" + smokeRoute + "\"\n"
	write("host/smoke/smoke.go", smokeTag+"\n\npackage smoke\n")
	write("host/smoke/more.go", "// Something said before.\n\n//go:build smoke && !race\n\npackage smoke\n") // behind the tag, and something else
	write("cmd/karpo-smoke/main.go", smokeTag+"\n\npackage main"+imported)
	write("cmd/karpo-smoke/main_test.go", smokeTag+"\n\npackage main"+imported)
	write("integration/door_test.go", smokeTag+"\n\npackage integration"+named) // with the tag, the door may be named
	write("host/host.go", "package host\n")
	if got, err := smokeViolations(dir); err != nil || len(got) != 0 {
		t.Fatalf("what is right is taken for wrong: %v %v", got, err)
	}

	wrong := map[string]string{
		"host/smoke/extra.go":            "package smoke\n",                                 // a file of the publisher without the tag
		"cmd/karpo-smoke/other.go":       "//go:build !production\n\npackage main\n",        // another tag is not the tag
		"host/smoke/either.go":           "//go:build smoke || linux\n\npackage smoke\n",    // on linux it needs no tag
		"host/smoke/never.go":            "//go:build !smoke\n\npackage smoke\n",            // the tag, the other way round
		"cmd/karpo/main.go":              "package main" + imported,                         // the real program imports it
		"cmd/karpo/door.go":              smokeTag + "\n\npackage main" + imported,          // with the tag too: built with it, it has the door
		"cmd/another/main_test.go":       "package main" + imported,                         // a test, of a program nobody listed
		"integration/enter_test.go":      smokeTag + "\n\npackage integration" + imported,   // a test with the tag
		"host/serve/serve.go":            "package serve" + named,                           // the real program names the door
		"contexts/security/door_test.go": "package security" + named,                        // a test without the tag
		"pkg/late.go":                    "package pkg\n\n" + smokeTag + named,              // the tag after the package clause is not a tag
		"pkg/block.go":                   "/*\n" + smokeTag + "\n*/\n\npackage pkg" + named, // nor is it inside a block comment
		"pkg/old.go":                     "// +build smoke\n\npackage pkg" + named,          // nor in the old form, which is not looked at
	}
	for rel, text := range wrong {
		write(rel, text)
	}
	write(".hidden/skipped.go", "package hidden"+named) // not looked at
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
