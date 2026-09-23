package application

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

type handlerWrapper func(ctx context.Context, request any) (any, error)

// Mediator dispatches commands and queries to their respective handlers through a pipeline of behaviors.
type Mediator struct {
	mu        sync.RWMutex
	handlers  map[reflect.Type]handlerWrapper
	behaviors []PipelineBehavior
}

// NewMediator creates an empty Mediator.
func NewMediator() *Mediator {
	return &Mediator{
		handlers:  make(map[reflect.Type]handlerWrapper),
		behaviors: make([]PipelineBehavior, 0),
	}
}

// Use adds a pipeline behavior (middleware) to the mediator.
func (m *Mediator) Use(behavior PipelineBehavior) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.behaviors = append(m.behaviors, behavior)
}

// RegisterCommandHandler registers a handler for TCommand.
func RegisterCommandHandler[TCommand any, TResult any](m *Mediator, handler CommandHandler[TCommand, TResult]) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmdType := reflect.TypeOf((*TCommand)(nil)).Elem()
	m.handlers[cmdType] = func(ctx context.Context, req any) (any, error) {
		cmd, ok := req.(TCommand)
		if !ok {
			return nil, fmt.Errorf("unexpected command type: %T", req)
		}
		res := handler.Handle(ctx, cmd)
		return res, nil
	}
}

// RegisterQueryHandler registers a handler for TQuery.
func RegisterQueryHandler[TQuery any, TResult any](m *Mediator, handler QueryHandler[TQuery, TResult]) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queryType := reflect.TypeOf((*TQuery)(nil)).Elem()
	m.handlers[queryType] = func(ctx context.Context, req any) (any, error) {
		q, ok := req.(TQuery)
		if !ok {
			return nil, fmt.Errorf("unexpected query type: %T", req)
		}
		res := handler.Handle(ctx, q)
		return res, nil
	}
}

// Send dispatches a command or query through the pipeline to its registered handler.
func Send[TResult any](ctx context.Context, m *Mediator, request any) result.Result[TResult] {
	if request == nil {
		return result.FailMsg[TResult]("cannot dispatch nil request")
	}

	reqType := reflect.TypeOf(request)

	m.mu.RLock()
	handler, found := m.handlers[reqType]
	behaviors := append([]PipelineBehavior(nil), m.behaviors...)
	m.mu.RUnlock()

	if !found {
		return result.FailMsg[TResult]("no handler registered for request type %v", reqType)
	}

	// Build the execution chain: behaviors wrapping the final handler
	var pipeline NextFunc = func(c context.Context) (any, error) {
		return handler(c, request)
	}

	// Wrap in reverse order so first registered behavior executes first
	for i := len(behaviors) - 1; i >= 0; i-- {
		b := behaviors[i]
		next := pipeline
		pipeline = func(c context.Context) (any, error) {
			return b(c, request, next)
		}
	}

	rawRes, err := pipeline(ctx)
	if err != nil {
		return result.Fail[TResult](err)
	}

	typedRes, ok := rawRes.(result.Result[TResult])
	if !ok {
		return result.FailMsg[TResult]("handler returned unexpected result type: %T", rawRes)
	}

	return typedRes
}
