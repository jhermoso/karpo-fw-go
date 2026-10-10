// Package nats carries integration events between processes over NATS JetStream: it implements
// application.MessageSender and application.MessageSubscriber, so a bounded context composed
// for the in-process broker runs unchanged when it is deployed on its own.
//
// It is a module of its own: the framework never depends on the NATS client.
//
//   - Every envelope is stored by the broker, in one stream, before Send returns. Its subject is
//     "{prefix}.evt.{type}", so consumers filter by integration event type.
//   - A logical consumer (usually a bounded context) is a durable consumer shared by its
//     replicas. It handles one message at a time, which keeps the facts about one subject in the
//     order they were sent: replicas take over from each other, they do not add throughput.
//   - A message is acknowledged only after its handler succeeded. A failed one comes back after a
//     growing wait and, after MaxDeliveries, is parked in the dead letter stream
//     ("{prefix}.dead.{consumer}") so that the next ones carry on.
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
)

// Config configures a Transport. The zero value of every field but URL is a working default.
// Every service of a deployment must use the same Stream, Prefix and MaxAge.
type Config struct {
	// URL of the server ("nats://host:4222"; several, comma separated, for a cluster). It comes
	// from the environment of the service, like any credential passed in Options.
	URL string
	// Name identifies the connection on the server (the service name).
	Name string
	// Stream holds the integration events (default "KARPO"); "{Stream}_DEAD" the dead letters.
	Stream string
	// Prefix is the first token of every subject (default "karpo").
	Prefix string
	// MaxAge is how long the broker keeps a message (default 7 days). The inboxes of the
	// consumers must remember a message for longer, or a late redelivery would repeat it.
	MaxAge time.Duration
	// StreamReplicas is the number of servers that store each message (default 1).
	StreamReplicas int
	// MaxDeliveries is how many times a message is handed to a consumer before it is parked
	// (default 8).
	MaxDeliveries int
	// Backoff lists the wait after each failed delivery; the last one repeats (default 1s, 5s,
	// 15s, 30s, 1m, 2m, 5m: about ten minutes for the eight deliveries).
	Backoff []time.Duration
	// AckWait is how long the broker waits for a replica that went silent before handing its
	// message to another one (default 30s). A handler may take longer: the message is kept
	// alive while it runs.
	AckWait time.Duration
	// Options are passed to the NATS client (credentials, TLS...).
	Options []natsgo.Option
	// Logger receives delivery failures and parked messages.
	Logger log.Logger
}

func (c Config) withDefaults() Config {
	if c.Stream == "" {
		c.Stream = "KARPO"
	}
	if c.Prefix == "" {
		c.Prefix = "karpo"
	}
	if c.MaxAge <= 0 {
		c.MaxAge = 7 * 24 * time.Hour
	}
	if c.StreamReplicas <= 0 {
		c.StreamReplicas = 1
	}
	if c.MaxDeliveries <= 0 {
		c.MaxDeliveries = 8
	}
	if len(c.Backoff) == 0 {
		c.Backoff = []time.Duration{time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}
	}
	if c.AckWait <= 0 {
		c.AckWait = 30 * time.Second
	}
	return c
}

// Transport is a connection to NATS JetStream that sends envelopes and attaches consumers.
type Transport struct {
	cfg    Config
	conn   *natsgo.Conn
	js     jetstream.JetStream
	ctx    context.Context // lifetime of the handlers
	cancel context.CancelFunc

	mu   sync.Mutex
	subs map[*subscription]struct{}
}

var (
	_ application.MessageSender     = (*Transport)(nil)
	_ application.MessageSubscriber = (*Transport)(nil)
)

// Header names of a message. The body is the JSON of the application.Envelope.
const (
	HeaderSource      = "Karpo-Source"
	HeaderSubject     = "Karpo-Subject"
	HeaderCorrelation = "Karpo-Correlation-Id"
	HeaderCausation   = "Karpo-Causation-Id"
)

// Open connects to the server and makes sure the streams exist.
func Open(ctx context.Context, cfg Config) (*Transport, error) {
	cfg = cfg.withDefaults()
	if cfg.URL == "" {
		return nil, fmt.Errorf("%w: nats: no server URL", domain.ErrValidation)
	}
	for what, token := range map[string]string{"stream": cfg.Stream, "prefix": cfg.Prefix} {
		if !validToken(token) {
			return nil, fmt.Errorf("%w: nats: invalid %s %q", domain.ErrValidation, what, token)
		}
	}
	opts := append([]natsgo.Option{natsgo.MaxReconnects(-1)}, cfg.Options...)
	if cfg.Name != "" {
		opts = append(opts, natsgo.Name(cfg.Name))
	}
	conn, err := natsgo.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: nats: %w", application.ErrTransportUnavailable, err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("nats: %w", err)
	}
	t := &Transport{cfg: cfg, conn: conn, js: js, subs: map[*subscription]struct{}{}}
	t.ctx, t.cancel = context.WithCancel(context.Background())
	streams := []jetstream.StreamConfig{
		{Name: cfg.Stream, Subjects: []string{cfg.Prefix + ".evt.>"}, Storage: jetstream.FileStorage,
			Retention: jetstream.LimitsPolicy, MaxAge: cfg.MaxAge, Replicas: cfg.StreamReplicas},
		// Dead letters wait for a person: they do not expire.
		{Name: cfg.Stream + "_DEAD", Subjects: []string{cfg.Prefix + ".dead.>"}, Storage: jetstream.FileStorage,
			Retention: jetstream.LimitsPolicy, Replicas: cfg.StreamReplicas},
	}
	for _, sc := range streams {
		if err := t.ensureStream(ctx, sc); err != nil {
			t.Close()
			return nil, err
		}
	}
	return t, nil
}

// ensureStream creates the stream when it is missing. An existing one is left as it is: its
// limits belong to whoever operates the broker.
func (t *Transport) ensureStream(ctx context.Context, sc jetstream.StreamConfig) error {
	if _, err := t.js.Stream(ctx, sc.Name); err == nil {
		return nil
	} else if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return fmt.Errorf("%w: nats: stream %s: %w", application.ErrTransportUnavailable, sc.Name, err)
	}
	if _, err := t.js.CreateStream(ctx, sc); err != nil && !errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
		return fmt.Errorf("nats: creating stream %s: %w", sc.Name, err)
	}
	return nil
}

// Close detaches every consumer, waits for the handlers in flight and closes the connection.
func (t *Transport) Close() error {
	t.mu.Lock()
	subs := make([]*subscription, 0, len(t.subs))
	for s := range t.subs {
		subs = append(subs, s)
	}
	t.mu.Unlock()
	for _, s := range subs {
		_ = s.detach()
	}
	t.cancel()
	t.conn.Close()
	return nil
}

// Send implements application.MessageSender: it returns once the broker stored every envelope,
// in the order given. A failure to reach the broker is an application.ErrTransportUnavailable.
func (t *Transport) Send(ctx context.Context, envelopes ...application.Envelope) error {
	for _, env := range envelopes {
		if env.ID == "" || !validSubject(env.Type) {
			return fmt.Errorf("%w: nats: envelope %q of type %q cannot be sent", domain.ErrValidation, env.ID, env.Type)
		}
		data, err := json.Marshal(env)
		if err != nil {
			return fmt.Errorf("nats: encoding %s: %w", env.ID, err)
		}
		msg := &natsgo.Msg{Subject: t.cfg.Prefix + ".evt." + env.Type, Data: data, Header: natsgo.Header{}}
		// The broker drops a message it already stored with this id (a relay sending it again).
		msg.Header.Set(jetstream.MsgIDHeader, env.ID)
		for name, value := range map[string]string{HeaderSource: env.Source, HeaderSubject: env.Subject,
			HeaderCorrelation: env.CorrelationID, HeaderCausation: env.CausationID} {
			if value != "" {
				msg.Header.Set(name, value)
			}
		}
		sendCtx, cancel := bounded(ctx)
		_, err = t.js.PublishMsg(sendCtx, msg)
		cancel()
		if err != nil {
			if errors.Is(err, natsgo.ErrMaxPayload) {
				return fmt.Errorf("nats: message %s (%s) of %d bytes exceeds the maximum the server accepts: %w", env.ID, env.Type, len(data), err)
			}
			return fmt.Errorf("%w: nats: sending %s (%s): %w", application.ErrTransportUnavailable, env.ID, env.Type, err)
		}
	}
	return nil
}

// brokerTimeout bounds a call to the broker made with a context without deadline (a relay
// holds its messages while it sends: it must not wait for a broker that is gone).
const brokerTimeout = 10 * time.Second

func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, brokerTimeout)
}

func (t *Transport) warn(msg string, args ...any) {
	if t.cfg.Logger != nil {
		t.cfg.Logger.Warn(msg, args...)
	}
}

// validToken reports whether s can be one token of a subject or the name of a stream or
// durable consumer.
func validToken(s string) bool {
	return s != "" && !strings.ContainsAny(s, ".*> \t\r\n/\\")
}

// validSubject reports whether s (an integration event type) can be the tail of a subject.
func validSubject(s string) bool {
	if s == "" || strings.ContainsAny(s, "*> \t\r\n") {
		return false
	}
	for _, token := range strings.Split(s, ".") {
		if token == "" {
			return false
		}
	}
	return true
}
