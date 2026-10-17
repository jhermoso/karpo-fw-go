// Package cache define contratos genéricos de caché para los servicios de Karpo.
package cache

import (
	"context"
	"time"
)

// Cache es un contrato de caché genérico y seguro entre goroutines para claves de tipo K y valores de tipo V.
type Cache[K comparable, V any] interface {
	// Get recupera un elemento por su clave. Devuelve el valor y true si lo encuentra y no ha caducado.
	Get(ctx context.Context, key K) (V, bool)

	// Set guarda un elemento con un TTL (tiempo de vida) opcional. Si ttl <= 0, el elemento no caduca.
	Set(ctx context.Context, key K, value V, ttl time.Duration) error

	// Delete quita un elemento por su clave.
	Delete(ctx context.Context, key K) error

	// Clear vacía todas las entradas de la caché.
	Clear(ctx context.Context) error
}
