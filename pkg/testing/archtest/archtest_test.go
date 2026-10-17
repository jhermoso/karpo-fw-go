package archtest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

const module = "github.com/jhermoso/karpo-fw-go"

// decimalLib es el único paquete de terceros permitido en el dominio: Go no tiene tipo decimal y
// la coma flotante no es aceptable para importes (decisión anotada en docs/LENGUAJE-UBICUO.md).
const decimalLib = "github.com/shopspring/decimal"

func root(t *testing.T, parts ...string) string {
	t.Helper()
	r, err := archtest.FindProjectRoot(".")
	if err != nil {
		t.Fatalf("could not locate project root: %v", err)
	}
	return filepath.Join(append([]string{r}, parts...)...)
}

// Paquetes de contratos: el equivalente en Go de los ensamblados *.Contracts de C#. Contienen
// interfaces y piezas puras (sin E/S, sin código de terceros) y sólo pueden depender de la
// biblioteca estándar y de otros paquetes de contratos.
var contracts = []string{
	archtest.Std,
	module + "/pkg/domain/...",        // contratos tácticos + piezas puras (Fw.Domain.Contracts)
	module + "/pkg/application",       // contratos y puertos de aplicación (Fw.Application.Contracts)
	module + "/pkg/application/authz", // contratos de autorización (Fw.Application.Contracts/Authorization)
	module + "/pkg/log",               // contrato de registro
	module + "/pkg/cache",             // contrato de caché
	module + "/pkg/time",              // contrato de reloj
	module + "/pkg/trace",             // contrato de trazas
	module + "/pkg/metrics",           // contrato de métricas
}

// Regla 1: los contratos de dominio son puros: sólo biblioteca estándar, el árbol del dominio y
// la biblioteca decimal aprobada. Los paquetes núcleo (domain, spec) se quedan en sólo
// biblioteca estándar.
func TestDomainContracts_ArePure(t *testing.T) {
	archtest.AssertOnlyImports(t, root(t, "pkg", "domain"), true, archtest.Std, decimalLib, module+"/pkg/domain/...")
	archtest.AssertOnlyImports(t, root(t, "pkg", "domain"), false, archtest.Std, module+"/pkg/domain/...")
	archtest.AssertOnlyImports(t, root(t, "pkg", "domain", "spec"), false, archtest.Std, module+"/pkg/domain/...")
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "domain"), []string{"database/sql", "net/http", "unsafe"})
}

// Regla 2: el paquete de contratos de aplicación sólo depende de contratos, nunca de sus propias
// implementaciones (pipeline, orchestration, outbox, hosting) ni de adaptadores.
func TestApplicationContracts_DependOnContractsOnly(t *testing.T) {
	archtest.AssertOnlyImports(t, root(t, "pkg", "application"), false, contracts...)
	archtest.AssertOnlyImports(t, root(t, "pkg", "application", "authz"), false, contracts...)
	for _, c := range []string{"log", "cache", "time", "trace", "metrics"} {
		archtest.AssertOnlyImports(t, root(t, "pkg", c), false, contracts...)
	}
}

// Regla 3: las implementaciones de aplicación sólo dependen de contratos: nunca de adaptadores
// de persistencia, de la capa de transporte, de paquetes de base de datos ni de código de terceros.
func TestApplicationImplementations_DependOnContractsOnly(t *testing.T) {
	for _, pkg := range []string{"pipeline", "orchestration", "outbox", "hosting", "authorization"} {
		archtest.AssertOnlyImports(t, root(t, "pkg", "application", pkg), true, contracts...)
	}
	// messaging reutiliza el motor de reenvío de outbox (una implementación hermana, nunca un adaptador).
	archtest.AssertOnlyImports(t, root(t, "pkg", "application", "messaging"), false, append(contracts, module+"/pkg/application/outbox")...)
}

// Regla 4: la capa de distribución habla con la aplicación, nunca con adaptadores de persistencia.
func TestDistribution_DoesNotReachPersistence(t *testing.T) {
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "distribution"), []string{"/pkg/persistence", "database/sql"})
}

// Regla 5: los adaptadores de persistencia son agnósticos de la tecnología (sin drivers, sin
// código de terceros) y nunca dependen de la capa de transporte ni de implementaciones de
// aplicación.
func TestPersistence_IsDriverAgnostic(t *testing.T) {
	archtest.AssertTreeDoesNotImport(t, root(t, "pkg", "persistence"), []string{
		"/pkg/distribution", "net/http",
		"/pkg/application/pipeline", "/pkg/application/orchestration", "/pkg/application/outbox", "/pkg/application/hosting",
		"/pkg/application/authorization", "/pkg/application/messaging", "/pkg/messaging",
	})
	archtest.AssertOnlyImports(t, root(t, "pkg", "persistence"), true, archtest.Std, decimalLib, module+"/...")
}

// Regla 6: la infraestructura de eventos y los transportes de mensajes implementan contratos de
// aplicación y no dependen de nada más.
func TestEvents_DependOnContractsOnly(t *testing.T) {
	archtest.AssertOnlyImports(t, root(t, "pkg", "events"), true, append(contracts, module+"/pkg/events/...")...)
	archtest.AssertOnlyImports(t, root(t, "pkg", "messaging"), false, append(contracts, module+"/pkg/messaging/...")...)
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

// Regla 7: los contextos delimitados hablan a través de contratos. Un contexto sólo puede
// importar otro a través de su paquete de contratos (Published Language y Open Host Service), y
// nunca en su dominio.
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

// Regla 8: los árboles de observabilidad (los contratos y sus implementaciones sin dependencias)
// sólo usan la biblioteca estándar y este módulo. La regla 2 cubre los paquetes de contratos;
// ésta es recursiva, así que log/vanilla, trace/vanilla y metrics/vanilla no pueden ganar una
// importación de terceros.
func TestObservability_IsStandardLibraryOnly(t *testing.T) {
	for _, c := range []string{"log", "trace", "metrics"} {
		archtest.AssertOnlyImports(t, root(t, "pkg", c), true, archtest.Std, module+"/...")
	}
	// Los contratos se mantienen libres del transporte y de la persistencia.
	for _, c := range []string{"trace", "metrics"} {
		archtest.AssertTreeDoesNotImport(t, root(t, "pkg", c), []string{"/pkg/distribution", "/pkg/persistence", "database/sql"})
	}
}

// Regla 9: OpenTelemetry nunca entra en el framework ni en los contextos delimitados. Su
// adaptador implementa trace.Tracer y metrics.Meter en un módulo aparte con su propio go.mod.
func TestOpenTelemetry_StaysOutOfTheFramework(t *testing.T) {
	for _, tree := range []string{"pkg", "contexts", "examples"} {
		archtest.AssertTreeDoesNotImport(t, root(t, tree), []string{"go.opentelemetry.io"})
	}
}
