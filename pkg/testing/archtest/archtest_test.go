package archtest_test

import (
	"path/filepath"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

// Regla 1: El Dominio no puede importar Aplicación, Distribución, ni Infraestructura
func TestCleanArchitecture_DomainPurity(t *testing.T) {
	root, err := archtest.FindProjectRoot(".")
	if err != nil {
		t.Fatalf("could not locate project root: %v", err)
	}

	domainDir := filepath.Join(root, "pkg", "domain")

	forbiddenInDomain := []string{
		"pkg/application",
		"pkg/distribution",
		"pkg/persistence",
		"database/sql",
		"net/http",
		"modernc.org",
		"entgo.io",
	}

	archtest.AssertPackageDoesNotImport(t, domainDir, forbiddenInDomain)
}

// Regla 2: La Aplicación no puede importar Distribución ni adaptadores de persistencia
func TestCleanArchitecture_ApplicationBoundaries(t *testing.T) {
	root, err := archtest.FindProjectRoot(".")
	if err != nil {
		t.Fatalf("could not locate project root: %v", err)
	}

	applicationDir := filepath.Join(root, "pkg", "application")

	forbiddenInApplication := []string{
		"pkg/distribution",
		"pkg/persistence/memory",
		"pkg/persistence/ent",
		"database/sql",
		"modernc.org",
		"net/http",
	}

	archtest.AssertPackageDoesNotImport(t, applicationDir, forbiddenInApplication)
}

// Regla 3: La Distribución no puede importar adaptadores concretos de base de datos
func TestCleanArchitecture_DistributionBoundaries(t *testing.T) {
	root, err := archtest.FindProjectRoot(".")
	if err != nil {
		t.Fatalf("could not locate project root: %v", err)
	}

	distributionDir := filepath.Join(root, "pkg", "distribution")

	forbiddenInDistribution := []string{
		"pkg/persistence/ent",
		"pkg/persistence/memory",
		"modernc.org",
	}

	archtest.AssertPackageDoesNotImport(t, distributionDir, forbiddenInDistribution)
}
