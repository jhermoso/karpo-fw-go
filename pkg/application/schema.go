package application

import (
	"context"
	"errors"
	"time"
)

// ---------------------------------------------------------------------------------------------
// Gestión del esquema
// ---------------------------------------------------------------------------------------------

// Estados de migración de los que informa un SchemaMigrator.
const (
	MigrationPending  = "pending"  // declarada por el código, no aplicada
	MigrationApplied  = "applied"  // aplicada con el mismo checksum
	MigrationDirty    = "dirty"    // empezada pero no terminada (motor sin DDL transaccional)
	MigrationModified = "modified" // aplicada, pero sus sentencias cambiaron después
	MigrationUnknown  = "unknown"  // aplicada en la base de datos, desconocida para este código (se ejecutó código más nuevo)
)

// Errores de esquema.
var (
	// ErrSchemaOutdated: el esquema de la base de datos no coincide con las migraciones del código.
	ErrSchemaOutdated = errors.New("schema outdated")
	// ErrSchemaDirty: una migración falló a medias y hay que repararla a mano y después forzarla.
	ErrSchemaDirty = errors.New("schema dirty")
)

// MigrationStatus es el estado de una migración de un contexto delimitado.
type MigrationStatus struct {
	Context   string     `json:"context"`
	Version   int64      `json:"version"`
	Name      string     `json:"name"`
	State     string     `json:"state"`
	AppliedAt *time.Time `json:"appliedAt,omitempty"`
}

// SchemaMigrator gestiona el esquema versionado de los contextos delimitados alojados en una base
// de datos (la contrapartida en Go de IDatabaseSchemaManager, con migraciones en lugar de EnsureCreated).
// Implementación: persistence/sqlrepo.Migrator.
type SchemaMigrator interface {
	// Status informa de todas las migraciones: las declaradas por el código y las registradas en la base de datos.
	Status(ctx context.Context) ([]MigrationStatus, error)
	// Migrate aplica en orden las migraciones pendientes y devuelve las que ha aplicado. Se niega
	// a ejecutarse sobre un historial sucio, modificado o desconocido.
	Migrate(ctx context.Context) ([]MigrationStatus, error)
	// Verify falla con ErrSchemaOutdated (o ErrSchemaDirty) salvo que estén aplicadas todas las migraciones:
	// debe ejecutarse al arrancar y antes de un cambio en caliente para que el servicio nunca funcione con un esquema desfasado.
	Verify(ctx context.Context) error
}
