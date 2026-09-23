// Package persistence provides persistence infrastructure adapters and re-exports domain repository contracts.
package persistence

import (
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Repository aliases domain.Repository for infrastructure components.
type Repository[ID comparable, T any] = domain.Repository[ID, T]

// ReadRepository aliases domain.ReadRepository.
type ReadRepository[ID comparable, T any] = domain.ReadRepository[ID, T]

// WriteRepository aliases domain.WriteRepository.
type WriteRepository[ID comparable, T any] = domain.WriteRepository[ID, T]

// UnitOfWork aliases domain.UnitOfWork.
type UnitOfWork = domain.UnitOfWork
