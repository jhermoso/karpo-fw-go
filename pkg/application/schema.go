package application

import (
	"context"
	"errors"
	"time"
)

// ---------------------------------------------------------------------------------------------
// Schema management
// ---------------------------------------------------------------------------------------------

// Migration states reported by a SchemaMigrator.
const (
	MigrationPending  = "pending"  // declared by the code, not applied
	MigrationApplied  = "applied"  // applied with the same checksum
	MigrationDirty    = "dirty"    // started but not finished (engine without transactional DDL)
	MigrationModified = "modified" // applied, but its statements changed afterwards
	MigrationUnknown  = "unknown"  // applied in the database, unknown to this code (newer code ran)
)

// Schema errors.
var (
	// ErrSchemaOutdated: the database schema does not match the migrations of the code.
	ErrSchemaOutdated = errors.New("schema outdated")
	// ErrSchemaDirty: a migration failed half way and must be repaired by hand, then forced.
	ErrSchemaDirty = errors.New("schema dirty")
)

// MigrationStatus is the state of one migration of one bounded context.
type MigrationStatus struct {
	Context   string     `json:"context"`
	Version   int64      `json:"version"`
	Name      string     `json:"name"`
	State     string     `json:"state"`
	AppliedAt *time.Time `json:"appliedAt,omitempty"`
}

// SchemaMigrator manages the versioned schema of the bounded contexts hosted on a database
// (the Go counterpart of IDatabaseSchemaManager, with migrations instead of EnsureCreated).
// Implementation: persistence/sqlrepo.Migrator.
type SchemaMigrator interface {
	// Status reports every migration: declared by the code and recorded in the database.
	Status(ctx context.Context) ([]MigrationStatus, error)
	// Migrate applies the pending migrations in order and returns the ones it applied. It refuses
	// to run on a dirty, modified or unknown history.
	Migrate(ctx context.Context) ([]MigrationStatus, error)
	// Verify fails with ErrSchemaOutdated (or ErrSchemaDirty) unless every migration is applied:
	// run it at startup and before a hot swap so the service never runs on an outdated schema.
	Verify(ctx context.Context) error
}
