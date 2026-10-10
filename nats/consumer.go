package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// subscription is one replica of a logical consumer.
type subscription struct {
	t        *Transport
	consumer string
	handler  application.MessageHandler
	consume  jetstream.ConsumeContext

	mu      sync.Mutex // held while a message is handled
	stopped bool
}

// Attach implements application.MessageSubscriber. The logical consumer is a durable JetStream
// consumer named after it: it is created on the first attachment, keeps its position while no
// replica is attached, and starts from the oldest message the broker still has.
func (t *Transport) Attach(ctx context.Context, consumer string, h application.MessageHandler, types ...string) (func() error, error) {
	if !validToken(consumer) {
		return nil, fmt.Errorf("%w: nats: invalid consumer name %q", domain.ErrValidation, consumer)
	}
	filters := []string{t.cfg.Prefix + ".evt.>"}
	if len(types) > 0 {
		filters = filters[:0]
		for _, typ := range types {
			if !validSubject(typ) {
				return nil, fmt.Errorf("%w: nats: invalid message type %q", domain.ErrValidation, typ)
			}
			filters = append(filters, t.cfg.Prefix+".evt."+typ)
		}
	}
	cons, err := t.js.CreateOrUpdateConsumer(ctx, t.cfg.Stream, jetstream.ConsumerConfig{
		Durable:        consumer,
		FilterSubjects: filters,
		DeliverPolicy:  jetstream.DeliverAllPolicy,
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        t.cfg.AckWait,
		// One message in flight for the whole consumer: the next one is not handed to any
		// replica until this one is settled. That is what keeps the order of a subject.
		MaxAckPending: 1,
		// Unlimited for the broker: the limit is applied here, where the message can be
		// parked before it is given up.
		MaxDeliver: -1,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: nats: consumer %s: %w", application.ErrTransportUnavailable, consumer, err)
	}
	s := &subscription{t: t, consumer: consumer, handler: h}
	s.consume, err = cons.Consume(s.onMessage, jetstream.PullMaxMessages(1),
		// A request for the next message that the broker lost (it was restarting) is noticed by
		// the missing heartbeats and made again: with these values, within a few seconds
		// instead of the half minute of the defaults.
		jetstream.PullExpiry(10*time.Second), jetstream.PullHeartbeat(2*time.Second),
		jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			t.warn("nats: consumer", "consumer", consumer, "error", err)
		}))
	if err != nil {
		return nil, fmt.Errorf("%w: nats: consumer %s: %w", application.ErrTransportUnavailable, consumer, err)
	}
	t.mu.Lock()
	t.subs[s] = struct{}{}
	t.mu.Unlock()
	return s.detach, nil
}

// detach stops the replica and waits for the message it is handling, if any.
func (s *subscription) detach() error {
	s.t.mu.Lock()
	delete(s.t.subs, s)
	s.t.mu.Unlock()

	s.consume.Stop()
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	return nil
}

func (s *subscription) onMessage(msg jetstream.Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		_ = msg.Nak() // another replica takes it at once
		return
	}
	deliveries := uint64(1)
	if meta, err := msg.Metadata(); err == nil {
		deliveries = meta.NumDelivered
	}

	var env application.Envelope
	if err := json.Unmarshal(msg.Data(), &env); err != nil || env.ID == "" {
		// Not an envelope: no delivery will ever succeed.
		s.park(msg, env, deliveries, fmt.Errorf("not an envelope: %w", errors.Join(err, domain.ErrValidation)))
		return
	}

	// A handler may outlast AckWait: tell the broker the message is still being worked on, or
	// it would hand it to another replica as well.
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(s.t.cfg.AckWait / 3)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				_ = msg.InProgress()
			}
		}
	}()
	err := s.handler.HandleMessage(s.t.ctx, env)
	close(done)

	switch {
	case err == nil:
		ackCtx, cancel := bounded(s.t.ctx)
		ackErr := msg.DoubleAck(ackCtx)
		cancel()
		if ackErr != nil {
			// The broker will deliver it again and the inbox of the consumer will discard it.
			s.t.warn("nats: acknowledgement not confirmed", "consumer", s.consumer, "message_id", env.ID, "error", ackErr)
		}
	case deliveries >= uint64(s.t.cfg.MaxDeliveries):
		s.park(msg, env, deliveries, err)
	default:
		wait := s.t.cfg.Backoff[min(int(deliveries)-1, len(s.t.cfg.Backoff)-1)]
		s.t.warn("nats: delivery failed", "consumer", s.consumer, "message_id", env.ID, "type", env.Type,
			"correlation_id", env.CorrelationID, "delivery", deliveries, "retry_in", wait, "error", err)
		_ = msg.NakWithDelay(wait)
	}
}

// park moves a message that cannot be handled to the dead letters and settles it, so that the
// consumer carries on. If the dead letter cannot be stored the message stays with the consumer.
func (s *subscription) park(msg jetstream.Msg, env application.Envelope, deliveries uint64, cause error) {
	if err := s.t.deadLetter(s.consumer, msg, env, deliveries, cause); err != nil {
		s.t.warn("nats: message could not be parked", "consumer", s.consumer, "message_id", env.ID, "error", err)
		_ = msg.NakWithDelay(s.t.cfg.Backoff[len(s.t.cfg.Backoff)-1])
		return
	}
	if s.t.cfg.Logger != nil {
		s.t.cfg.Logger.Error("nats: message parked as dead letter", "consumer", s.consumer, "message_id", env.ID, "type", env.Type,
			"subject", env.Subject, "correlation_id", env.CorrelationID, "deliveries", deliveries, "error", cause)
	}
	_ = msg.TermWithReason("parked as dead letter")
}
