// Package inprocess es un transporte de mensajes en memoria: un application.MessageSender que
// reparte cada sobre entre los consumidores suscritos del mismo proceso. Sirve al monolito modular
// (contextos delimitados desplegados juntos pero acoplados sólo mediante eventos de integración) y
// a las pruebas; un adaptador de broker (NATS, RabbitMQ, Kafka...) lo sustituye sin tocar el código
// que publica ni el que consume.
package inprocess

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
)

// Broker entrega de forma síncrona a cada suscripción cuyos tipos incluyen el tipo del sobre.
// Send falla cuando falla cualquier consumidor, para que el relay reintente el mensaje; los
// consumidores que ya lo procesaron descartan el duplicado mediante su inbox.
type Broker struct {
	mu   sync.RWMutex
	subs []subscription
}

type subscription struct {
	consumer string
	types    []string // vacío: todos los tipos
	handler  application.MessageHandler
}

var _ application.MessageSender = (*Broker)(nil)

// NewBroker crea un broker vacío.
func NewBroker() *Broker { return &Broker{} }

// Subscribe registra un consumidor para los tipos dados (todos si no se da ninguno) y devuelve una
// función para darse de baja.
func (b *Broker) Subscribe(consumer string, h application.MessageHandler, types ...string) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, subscription{consumer: consumer, types: slices.Clone(types), handler: h})
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.subs = slices.DeleteFunc(b.subs, func(s subscription) bool { return s.consumer == consumer })
	}
}

// Send implementa application.MessageSender.
func (b *Broker) Send(ctx context.Context, envelopes ...application.Envelope) error {
	b.mu.RLock()
	subs := slices.Clone(b.subs)
	b.mu.RUnlock()
	var errs []error
	for _, env := range envelopes {
		for _, s := range subs {
			if len(s.types) > 0 && !slices.Contains(s.types, env.Type) {
				continue
			}
			if err := s.handler.HandleMessage(ctx, env); err != nil {
				errs = append(errs, fmt.Errorf("consumer %s, message %s (%s): %w", s.consumer, env.ID, env.Type, err))
			}
		}
	}
	return errors.Join(errs...)
}
