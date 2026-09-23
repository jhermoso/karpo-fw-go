package archtest_test

import (
	"path/filepath"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

func TestCleanArchitecture_DomainPurity(t *testing.T) {
	root, err := archtest.FindProjectRoot(".")
	if err != nil {
		t.Fatalf("could not locate project root: %v", err)
	}

	domainDir := filepath.Join(root, "pkg", "domain")

	// Domain layer must NOT import application, persistence/database, or infrastructure
	forbiddenInDomain := []string{
		"pkg/application",
		"pkg/persistence",
		"database/sql",
		"net/http",
		"modernc.org",
		"entgo.io",
	}

	archtest.AssertPackageDoesNotImport(t, domainDir, forbiddenInDomain)
}

func TestCleanArchitecture_ApplicationBoundaries(t *testing.T) {
	root, err := archtest.FindProjectRoot(".")
	if err != nil {
		t.Fatalf("could not locate project root: %v", err)
	}

	applicationDir := filepath.Join(root, "pkg", "application")

	// Application layer must NOT import concrete persistence adapters or direct DB drivers
	forbiddenInApplication := []string{
		"pkg/persistence/memory",
		"pkg/persistence/ent",
		"database/sql",
		"modernc.org",
	}

	archtest.AssertPackageDoesNotImport(t, applicationDir, forbiddenInApplication)
}
