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

// Contract packages: the Go counterpart of the C# *.Contracts assemblies. They hold interfaces
// and pure building blocks (no I/O, no third-party code) and may depend only on the standard
// library and on other contract packages.
var contracts = []string{
	archtest.Std,
	module + "/pkg/domain/...",  // tactical contracts + pure building blocks (Fw.Domain.Contracts)
	module + "/pkg/application", // application contracts and ports (Fw.Application.Contracts)
	module + "/pkg/log",         // logging contract
	module + "/pkg/cache",       // cache contract
	module + "/pkg/time",        // clock contract
}

// Rule 1: the domain contracts are pure: standard library and the domain tree only.
func TestDomainContracts_ArePure(t *testing.T) {
	archtest.AssertOnlyImports(t, root(t, "pkg", "domain"), true, archtest.Std, module+"/pkg/domain/...")
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "domain"), []string{"database/sql", "net/http", "unsafe"})
}

// Rule 2: the application contracts package depends only on contracts, never on its own
// implementations (pipeline, orchestration, outbox, hosting) nor on adapters.
func TestApplicationContracts_DependOnContractsOnly(t *testing.T) {
	archtest.AssertOnlyImports(t, root(t, "pkg", "application"), false, contracts...)
	for _, c := range []string{"log", "cache", "time"} {
		archtest.AssertOnlyImports(t, root(t, "pkg", c), false, contracts...)
	}
}

// Rule 3: application implementations depend on contracts only: never on persistence adapters,
// the transport layer, database packages or third-party code.
func TestApplicationImplementations_DependOnContractsOnly(t *testing.T) {
	for _, pkg := range []string{"pipeline", "orchestration", "outbox", "hosting"} {
		archtest.AssertOnlyImports(t, root(t, "pkg", "application", pkg), true, contracts...)
	}
}

// Rule 4: the distribution layer talks to the application, never to persistence adapters.
func TestDistribution_DoesNotReachPersistence(t *testing.T) {
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "distribution"), []string{"/pkg/persistence", "database/sql"})
}

// Rule 5: persistence adapters are technology agnostic (no drivers, no third-party code) and
// never depend on the transport layer or on application implementations.
func TestPersistence_IsDriverAgnostic(t *testing.T) {
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "persistence"), []string{
		"/pkg/distribution", "net/http",
		"/pkg/application/pipeline", "/pkg/application/orchestration", "/pkg/application/outbox", "/pkg/application/hosting",
	})
	archtest.AssertNoThirdParty(t, root(t, "pkg", "persistence"), module)
}

// Rule 6: event infrastructure implements application contracts and depends on nothing else.
func TestEvents_DependOnContractsOnly(t *testing.T) {
	archtest.AssertOnlyImports(t, root(t, "pkg", "events"), true, append(contracts, module+"/pkg/events/...")...)
}

func TestMatchesAndImports(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{archtest.Std, "net/http", true},
		{archtest.Std, "entgo.io/ent", false},
		{module + "/pkg/domain/...", module + "/pkg/domain", true},
		{module + "/pkg/domain/...", module + "/pkg/domain/spec", true},
		{module + "/pkg/domain/...", module + "/pkg/domainx", false},
		{module + "/pkg/application", module + "/pkg/application/pipeline", false},
	}
	for _, c := range cases {
		if got := archtest.Matches(c.pattern, c.path); got != c.want {
			t.Errorf("Matches(%q, %q) = %v", c.pattern, c.path, got)
		}
	}
	imps, err := archtest.Imports(root(t, "pkg", "testing", "archtest"), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range imps {
		if imp.Path == module+"/pkg/testing/archtest" {
			t.Fatal("test files must be ignored")
		}
	}
}
