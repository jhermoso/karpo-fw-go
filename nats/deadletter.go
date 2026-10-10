package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Header names of a dead letter. Its body is the message as it was sent.
const (
	HeaderDeadConsumer   = "Karpo-Dead-Consumer"
	HeaderDeadError      = "Karpo-Dead-Error"
	HeaderDeadDeliveries = "Karpo-Dead-Deliveries"
	HeaderDeadAt         = "Karpo-Dead-At"
)

// DeadLetter is a message a consumer could not handle after MaxDeliveries. Once the cause is
// fixed, sending Envelope again delivers it (the inboxes of the consumers that did handle it
// discard it). The order of its subject is broken from here on: check what else arrived about
// Envelope.Subject before sending it again.
type DeadLetter struct {
	Envelope   application.Envelope
	Consumer   string
	Error      string
	Deliveries int
	At         time.Time
	// Raw is the body when it could not be read as an envelope.
	Raw []byte
}

func (t *Transport) deadLetter(consumer string, msg jetstream.Msg, env application.Envelope, deliveries uint64, cause error) error {
	text := cause.Error()
	if len(text) > 1000 {
		text = text[:1000]
	}
	dead := &natsgo.Msg{Subject: t.cfg.Prefix + ".dead." + consumer, Data: msg.Data(), Header: natsgo.Header{}}
	for name, values := range msg.Headers() {
		if name != jetstream.MsgIDHeader {
			dead.Header[name] = values
		}
	}
	dead.Header.Set(HeaderDeadConsumer, consumer)
	dead.Header.Set(HeaderDeadError, text)
	dead.Header.Set(HeaderDeadDeliveries, strconv.FormatUint(deliveries, 10))
	dead.Header.Set(HeaderDeadAt, domain.Now().UTC().Format(time.RFC3339Nano))
	if env.ID != "" {
		// Parking twice (the broker did not hear the first time) keeps one dead letter.
		dead.Header.Set(jetstream.MsgIDHeader, consumer+":"+env.ID)
	}
	ctx, cancel := bounded(t.ctx)
	defer cancel()
	_, err := t.js.PublishMsg(ctx, dead)
	return err
}

// DeadLetters returns the messages parked for a consumer, oldest first.
func (t *Transport) DeadLetters(ctx context.Context, consumer string) ([]DeadLetter, error) {
	if !validToken(consumer) {
		return nil, fmt.Errorf("%w: nats: invalid consumer name %q", domain.ErrValidation, consumer)
	}
	unavailable := func(err error) error {
		return fmt.Errorf("%w: nats: dead letters of %s: %w", application.ErrTransportUnavailable, consumer, err)
	}
	stream, err := t.js.Stream(ctx, t.cfg.Stream+"_DEAD")
	if err != nil {
		return nil, unavailable(err)
	}
	// Read straight from the stream, message by message: nothing is consumed or acknowledged.
	subject := jetstream.WithGetMsgSubject(t.cfg.Prefix + ".dead." + consumer)
	var out []DeadLetter
	for seq := uint64(1); ; {
		msg, err := stream.GetMsg(ctx, seq, subject)
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return out, nil
		}
		if err != nil {
			return nil, unavailable(err)
		}
		seq = msg.Sequence + 1
		d := DeadLetter{Consumer: consumer, Error: msg.Header.Get(HeaderDeadError)}
		d.Deliveries, _ = strconv.Atoi(msg.Header.Get(HeaderDeadDeliveries))
		d.At, _ = time.Parse(time.RFC3339Nano, msg.Header.Get(HeaderDeadAt))
		if err := json.Unmarshal(msg.Data, &d.Envelope); err != nil || d.Envelope.ID == "" {
			d.Envelope, d.Raw = application.Envelope{}, msg.Data
		}
		out = append(out, d)
	}
}
