// Package application implements the application layer: use-case handlers (CQRS), cross-cutting
// middleware (transactions, validation, logging, idempotency, retries), the aggregate
// Orchestrator, the transactional outbox and bounded-context modules.
//
// Design choice versus the C# framework: there is no reflection-based mediator. Handlers are
// plain typed values injected where they are used and decorated with generic middleware
// (the same approach as the C# IdempotencyCommandHandlerDecorator), so every
// command -> result pairing is checked by the compiler.
package application

import "context"

// Handler executes a use case: In is the command or query, Out its result.
type Handler[In, Out any] interface {
	Handle(ctx context.Context, in In) (Out, error)
}

// HandlerFunc adapts a function into a Handler.
type HandlerFunc[In, Out any] func(ctx context.Context, in In) (Out, error)

// Handle calls fn.
func (fn HandlerFunc[In, Out]) Handle(ctx context.Context, in In) (Out, error) { return fn(ctx, in) }

// CommandHandler handles a command (state change). Use Transactional middleware on it.
type CommandHandler[C, R any] = Handler[C, R]

// QueryHandler handles a query (no side effects).
type QueryHandler[Q, R any] = Handler[Q, R]

// Middleware decorates a handler with a cross-cutting concern (the Go equivalent of a
// MediatR pipeline behavior or a C# handler decorator), preserving static types.
type Middleware[In, Out any] func(next Handler[In, Out]) Handler[In, Out]

// Chain wraps h with middlewares; the first middleware is the outermost one.
func Chain[In, Out any](h Handler[In, Out], middlewares ...Middleware[In, Out]) Handler[In, Out] {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
