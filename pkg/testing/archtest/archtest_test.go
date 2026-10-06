package archtest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

const module = "github.com/jhermoso/karpo-fw-go"

// decimalLib is the only third-party package allowed in the domain: Go has no decimal type and
// floating point is not acceptable for amounts (decision recorded in docs/LENGUAJE-UBICUO.md).
const decimalLib = "github.com/shopspring/decimal"

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
	module + "/pkg/domain/...",        // tactical contracts + pure building blocks (Fw.Domain.Contracts)
	module + "/pkg/application",       // application contracts and ports (Fw.Application.Contracts)
	module + "/pkg/application/authz", // authorization contracts (Fw.Application.Contracts/Authorization)
	module + "/pkg/log",               // logging contract
	module + "/pkg/cache",             // cache contract
	module + "/pkg/time",              // clock contract
	module + "/pkg/observability",     // telemetry contract (traces, metrics, shared names)
}

// Rule 1: the domain contracts are pure: standard library, the domain tree and the approved
// decimal library only. The core packages (domain, spec) stay standard-library only.
func TestDomainContracts_ArePure(t *testing.T) {
	archtest.AssertOnlyImports(t, root(t, "pkg", "domain"), true, archtest.Std, decimalLib, module+"/pkg/domain/...")
	archtest.AssertOnlyImports(t, root(t, "pkg", "domain"), false, archtest.Std, module+"/pkg/domain/...")
	archtest.AssertOnlyImports(t, root(t, "pkg", "domain", "spec"), false, archtest.Std, module+"/pkg/domain/...")
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "domain"), []string{"database/sql", "net/http", "unsafe"})
}

// Rule 2: the application contracts package depends only on contracts, never on its own
// implementations (pipeline, orchestration, outbox, hosting) nor on adapters.
func TestApplicationContracts_DependOnContractsOnly(t *testing.T) {
	archtest.AssertOnlyImports(t, root(t, "pkg", "application"), false, contracts...)
	archtest.AssertOnlyImports(t, root(t, "pkg", "application", "authz"), false, contracts...)
	for _, c := range []string{"log", "cache", "time", "observability"} {
		archtest.AssertOnlyImports(t, root(t, "pkg", c), false, contracts...)
	}
}

// Rule 3: application implementations depend on contracts only: never on persistence adapters,
// the transport layer, database packages or third-party code.
func TestApplicationImplementations_DependOnContractsOnly(t *testing.T) {
	for _, pkg := range []string{"pipeline", "orchestration", "outbox", "hosting", "authorization"} {
		archtest.AssertOnlyImports(t, root(t, "pkg", "application", pkg), true, contracts...)
	}
	// messaging reuses the relay engine of outbox (a sibling implementation, never an adapter).
	archtest.AssertOnlyImports(t, root(t, "pkg", "application", "messaging"), false, append(contracts, module+"/pkg/application/outbox")...)
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
		"/pkg/application/authorization", "/pkg/application/messaging", "/pkg/messaging",
	})
	archtest.AssertOnlyImports(t, root(t, "pkg", "persistence"), true, archtest.Std, decimalLib, module+"/...")
}

// Rule 6: event infrastructure and message transports implement application contracts and
// depend on nothing else.
func TestEvents_DependOnContractsOnly(t *testing.T) {
	archtest.AssertOnlyImports(t, root(t, "pkg", "events"), true, append(contracts, module+"/pkg/events/...")...)
	archtest.AssertOnlyImports(t, root(t, "pkg", "messaging"), false, append(contracts, module+"/pkg/messaging/...")...)
}

// Rule 8: the telemetry contract is standard library only, and its implementations (one package
// per technology) depend on contracts and on their own tree, never on third-party code: an
// OpenTelemetry adapter belongs in a separate module so the root go.mod does not grow.
func TestObservability_ContractIsPureAndAdaptersStayOut(t *testing.T) {
	archtest.AssertOnlyImports(t, root(t, "pkg", "observability"), false, archtest.Std)
	archtest.AssertOnlyImports(t, root(t, "pkg", "observability"), true, append(contracts, module+"/pkg/observability/...")...)
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "observability"), []string{"/pkg/persistence", "/pkg/distribution", "database/sql"})
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

// Rule 7: bounded contexts talk through contracts. A context may import another one only through
// its contracts package (Published Language and Open Host Service), and never in its domain.
func TestContexts_DependOnEachOtherThroughContracts(t *testing.T) {
	dirs, err := os.ReadDir(root(t, "contexts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		imps, err := archtest.Imports(root(t, "contexts", name), true)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range imps {
			rest, ok := strings.CutPrefix(imp.Path, module+"/contexts/")
			if !ok {
				continue
			}
			other, sub, _ := strings.Cut(rest, "/")
			if other == name {
				continue
			}
			if sub != "contracts" || strings.Contains(filepath.ToSlash(imp.File), "/domain/") {
				t.Errorf("%s imports %s: contexts may only use other contexts' contracts, outside their domain", imp.File, imp.Path)
			}
		}
	}
}
