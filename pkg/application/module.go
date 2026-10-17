package application

import "context"

// Module es un contexto delimitado enchufado a un proceso anfitrión (equivale a IBoundedContext de C#).
// Los módulos declaran sus dependencias por nombre, reflejando la jerarquía de Karpo
// (Fw <- ErpKernel <- ErpDetail <- Sectorial...). Ganchos opcionales de ciclo de vida: Starter, Stopper.
// La implementación del anfitrión está en application/hosting.
type Module interface {
	Name() string
	Dependencies() []string
}

// Starter lo implementan los módulos que necesitan inicialización (migraciones, relays, cachés...).
type Starter interface {
	Start(ctx context.Context) error
}

// Stopper lo implementan los módulos que retienen recursos.
type Stopper interface {
	Stop(ctx context.Context) error
}
