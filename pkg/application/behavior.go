package application

import (
	"context"
)

// NextFunc represents the next step in the pipeline (either another behavior or the final handler).
type NextFunc func(ctx context.Context) (any, error)

// PipelineBehavior defines a middleware interceptor for requests dispatched through the Mediator.
// It allows cross-cutting concerns (logging, metrics, validation) to wrap handler execution.
type PipelineBehavior func(ctx context.Context, request any, next NextFunc) (any, error)
