// Package application reúne los CONTRATOS de la capa de aplicación (la contrapartida en Go de
// Paranoia.Karpo.Fw.Application.Contracts): manejadores de casos de uso (CQRS) y su tipo de middleware,
// los puertos de los que depende la capa (publicación y registro de eventos, almacén del outbox,
// almacén de idempotencia, decodificación de eventos), los módulos de contexto delimitado y las
// claves del contexto de la petición.
//
// No contiene ninguna implementación con comportamiento propio; las pruebas de arquitectura hacen
// cumplir que sólo importa la biblioteca estándar y otros paquetes de contratos. Las implementaciones están en:
//
//	application/pipeline       Validating, Transactional, RetryOnConflict, Idempotent, Logging
//	application/orchestration  Orchestrator, Execute
//	application/outbox         Recorder (EventRecorder sobre un OutboxStore), Relay
//	application/hosting        Host (ciclo de vida de los módulos)
//
// Decisión de diseño frente al framework de C#: no hay mediador basado en reflexión. Los
// manejadores son valores tipados simples que se inyectan donde se usan y se decoran con middleware
// genérico (el mismo enfoque que el IdempotencyCommandHandlerDecorator de C#), de modo que el
// compilador comprueba cada emparejamiento orden -> resultado.
package application

import "context"

// Handler ejecuta un caso de uso: In es la orden o la consulta, Out su resultado.
type Handler[In, Out any] interface {
	Handle(ctx context.Context, in In) (Out, error)
}

// HandlerFunc adapta una función a Handler.
type HandlerFunc[In, Out any] func(ctx context.Context, in In) (Out, error)

// Handle llama a fn.
func (fn HandlerFunc[In, Out]) Handle(ctx context.Context, in In) (Out, error) { return fn(ctx, in) }

// CommandHandler maneja una orden (cambio de estado). Debe envolverse con el middleware Transactional.
type CommandHandler[C, R any] = Handler[C, R]

// QueryHandler maneja una consulta (sin efectos laterales).
type QueryHandler[Q, R any] = Handler[Q, R]

// Middleware decora un manejador con un aspecto transversal (el equivalente en Go de un
// pipeline behavior de MediatR o de un decorador de manejador de C#), conservando los tipos estáticos.
type Middleware[In, Out any] func(next Handler[In, Out]) Handler[In, Out]

// Chain envuelve h con los middlewares; el primero es el más exterior.
func Chain[In, Out any](h Handler[In, Out], middlewares ...Middleware[In, Out]) Handler[In, Out] {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
