// Package application defines strategic application layer abstractions.
// It provides CQRS primitives (Commands, Queries, Handlers) and an in-process Mediator with Pipeline Behaviors.
package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

// Command represents an action intended to mutate domain state.
type Command[TResult any] interface {
	isCommand()
}

// BaseCommand is a convenience struct to embed in concrete command structs.
type BaseCommand[TResult any] struct{}

func (BaseCommand[TResult]) isCommand() {}

// CommandHandler processes a specific command type and returns a typed Result.
type CommandHandler[TCommand any, TResult any] interface {
	Handle(ctx context.Context, cmd TCommand) result.Result[TResult]
}

// CommandHandlerFunc allows using a function as a CommandHandler.
type CommandHandlerFunc[TCommand any, TResult any] func(ctx context.Context, cmd TCommand) result.Result[TResult]

func (fn CommandHandlerFunc[TCommand, TResult]) Handle(ctx context.Context, cmd TCommand) result.Result[TResult] {
	return fn(ctx, cmd)
}

// Query represents a request to retrieve data without side effects.
type Query[TResult any] interface {
	isQuery()
}

// BaseQuery is a convenience struct to embed in concrete query structs.
type BaseQuery[TResult any] struct{}

func (BaseQuery[TResult]) isQuery() {}

// QueryHandler processes a specific query type and returns a typed Result.
type QueryHandler[TQuery any, TResult any] interface {
	Handle(ctx context.Context, query TQuery) result.Result[TResult]
}

// QueryHandlerFunc allows using a function as a QueryHandler.
type QueryHandlerFunc[TQuery any, TResult any] func(ctx context.Context, query TQuery) result.Result[TResult]

func (fn QueryHandlerFunc[TQuery, TResult]) Handle(ctx context.Context, query TQuery) result.Result[TResult] {
	return fn(ctx, query)
}
