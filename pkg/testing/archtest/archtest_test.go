package archtest_test

import (
	"path/filepath"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

const module = "github.com/jhermoso/karpo-fw-go"

func root(t *testing.T, parts ...string) string {
	t.Helper()
	r, err := archtest.FindProjectRoot(".")
	if err != nil {
		t.Fatalf("could not locate project root: %v", err)
	}
	return filepath.Join(append([]string{r}, parts...)...)
}

// Rule 1: the domain is pure. Allow-list: only the standard library and the domain tree itself
// (identifiers, entities, aggregates, events, specifications, repository contracts).
func TestDomain_IsPure(t *testing.T) {
	archtest.AssertTreeOnlyImports(t, root(t, "pkg", "domain"), true, []string{module + "/pkg/domain"})
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "domain"), []string{"database/sql", "net/http", "unsafe"})
}

// Rule 2: the application layer depends on the domain and on ports only: never on the transport
// layer, persistence adapters or database packages.
func TestApplication_DependsOnPortsOnly(t *testing.T) {
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "application"), []string{
		"/pkg/distribution", "/pkg/persistence", "database/sql", "net/http",
	})
	archtest.AssertNoThirdParty(t, root(t, "pkg", "application"), module)
}

// Rule 3: the distribution layer talks to the application, never to persistence adapters.
func TestDistribution_DoesNotReachPersistence(t *testing.T) {
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "distribution"), []string{"/pkg/persistence", "database/sql"})
}

// Rule 4: persistence adapters are technology agnostic: the generic SQL repository and the
// dialects never import a database driver (the composition root chooses it), and no adapter
// depends on the transport layer.
func TestPersistence_IsDriverAgnostic(t *testing.T) {
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "persistence"), []string{
		"/pkg/distribution", "net/http",
	})
	archtest.AssertNoThirdParty(t, root(t, "pkg", "persistence"), module)
}

// Rule 5: event infrastructure does not depend on application or persistence.
func TestEvents_AreIndependent(t *testing.T) {
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "events"), []string{
		"/pkg/application", "/pkg/persistence", "/pkg/distribution",
	})
}

func TestImports_SkipsTestFiles(t *testing.T) {
	imps, err := archtest.Imports(root(t, "pkg", "testing", "archtest"), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range imps {
		if imp.Path == module+"/pkg/testing/archtest" {
			t.Fatal("test files must be ignored")
		}
	}
	if !archtest.IsStdlib("net/http") || archtest.IsStdlib("entgo.io/ent") {
		t.Fatal("stdlib detection")
	}
}
