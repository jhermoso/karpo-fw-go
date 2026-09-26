package application

import "context"

// Module is a bounded context plugged into a host process (equivalent to the C# IBoundedContext).
// Modules declare their dependencies by name, mirroring the Karpo hierarchy
// (Fw <- ErpKernel <- ErpDetail <- Sectorial...). Optional lifecycle hooks: Starter, Stopper.
// The host implementation lives in application/hosting.
type Module interface {
	Name() string
	Dependencies() []string
}

// Starter is implemented by modules needing initialization (migrations, relays, caches...).
type Starter interface {
	Start(ctx context.Context) error
}

// Stopper is implemented by modules holding resources.
type Stopper interface {
	Stop(ctx context.Context) error
}
