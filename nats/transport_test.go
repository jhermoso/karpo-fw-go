package nats_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	knats "github.com/jhermoso/karpo-fw-go/nats"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/transporttest"
)

// startServer runs a NATS server with JetStream inside the test, storing in dir and listening
// on port (-1: any free port).
func startServer(t *testing.T, dir string, port int) *server.Server {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, JetStream: true, StoreDir: dir, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(20 * time.Second) {
		t.Fatal("the NATS server did not start")
	}
	t.Cleanup(func() { stopServer(srv) })
	return srv
}

func stopServer(srv *server.Server) {
	srv.Shutdown()
	srv.WaitForShutdown()
}

func open(t *testing.T, cfg knats.Config) *knats.Transport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if cfg.Backoff == nil {
		cfg.Backoff = []time.Duration{20 * time.Millisecond}
	}
	tr, err := knats.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

func envelope(id, typ, subject string) application.Envelope {
	return application.Envelope{ID: id, Type: typ, Source: "things", Subject: subject, OccurredAt: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC),
		CorrelationID: "corr-" + id, CausationID: "cause-" + id, Data: []byte(`{"seq":1}`)}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The adapter honours the transport contract, with every optional capability: it shares the
// messages among replicas, keeps them for detached consumers and parks the ones that fail.
func TestTransport_Conformance(t *testing.T) {
	srv := startServer(t, t.TempDir(), -1)
	var mu sync.Mutex
	opened := map[*testing.T]*knats.Transport{}
	transporttest.Run(t, transporttest.Harness{
		New: func(t *testing.T) transporttest.Transport {
			mu.Lock()
			defer mu.Unlock()
			n := len(opened) + 1 // a stream of its own: nothing sent in another case reaches this one
			tr := open(t, knats.Config{URL: srv.ClientURL(), Stream: fmt.Sprintf("CONFORMANCE_%d", n), Prefix: fmt.Sprintf("conformance%d", n),
				MaxDeliveries: transporttest.MaxDeliveries, AckWait: 5 * time.Second})
			opened[t] = tr
			return transporttest.Transport{Sender: tr, Subscriber: tr}
		},
		Replicas: true,
		Durable:  true,
		DeadLetters: func(t *testing.T, consumer string) []application.Envelope {
			mu.Lock()
			tr := opened[t]
			mu.Unlock()
			dead, err := tr.DeadLetters(context.Background(), consumer)
			if err != nil {
				t.Fatal(err)
			}
			out := make([]application.Envelope, len(dead))
			for i, d := range dead {
				out[i] = d.Envelope
			}
			return out
		},
	})
}

func TestSend_RejectsWhatCannotTravel(t *testing.T) {
	srv := startServer(t, t.TempDir(), -1)
	tr := open(t, knats.Config{URL: srv.ClientURL()})
	ctx := context.Background()
	for _, env := range []application.Envelope{
		envelope("", "things.thing-happened.v1", "a"),
		envelope("m-1", "", "a"),
		envelope("m-1", "things.*.v1", "a"),
		envelope("m-1", "things..v1", "a"),
	} {
		if err := tr.Send(ctx, env); !errors.Is(err, domain.ErrValidation) || errors.Is(err, application.ErrTransportUnavailable) {
			t.Errorf("envelope %q of type %q: %v", env.ID, env.Type, err)
		}
	}
	nothing := application.MessageHandlerFunc(func(context.Context, application.Envelope) error { return nil })
	if _, err := tr.Attach(ctx, "bad.name", nothing); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("consumer name: %v", err)
	}
	if _, err := knats.Open(ctx, knats.Config{}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("no URL: %v", err)
	}
}

// What the broker stores: the envelope as JSON under a subject that carries its type, the
// message id the broker deduplicates by, and the correlation in headers.
func TestSend_WireFormat(t *testing.T) {
	srv := startServer(t, t.TempDir(), -1)
	tr := open(t, knats.Config{URL: srv.ClientURL()})
	ctx := context.Background()
	env := envelope("m-1", "things.thing-happened.v1", "thing-7")
	for range 2 { // the second one is a relay sending again: the broker keeps one
		if err := tr.Send(ctx, env); err != nil {
			t.Fatal(err)
		}
	}

	conn, err := natsgo.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	js, _ := jetstream.New(conn)
	stream, err := js.Stream(ctx, "KARPO")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := stream.Info(ctx); err != nil || info.State.Msgs != 1 || info.Config.MaxAge != 7*24*time.Hour {
		t.Fatalf("stream: %+v %v", info, err)
	}
	msg, err := stream.GetLastMsgForSubject(ctx, "karpo.evt.things.thing-happened.v1")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get(jetstream.MsgIDHeader) != "m-1" || msg.Header.Get(knats.HeaderCorrelation) != "corr-m-1" ||
		msg.Header.Get(knats.HeaderCausation) != "cause-m-1" || msg.Header.Get(knats.HeaderSource) != "things" ||
		msg.Header.Get(knats.HeaderSubject) != "thing-7" {
		t.Fatalf("headers: %v", msg.Header)
	}
	want := `{"id":"m-1","type":"things.thing-happened.v1","source":"things","subject":"thing-7","time":"2026-01-01T08:00:00Z","correlationId":"corr-m-1","causationId":"cause-m-1","data":{"seq":1}}`
	if string(msg.Data) != want {
		t.Fatalf("body:\n got %s\nwant %s", msg.Data, want)
	}
}

// With the broker down, Send fails as a transport outage (the relay does not count attempts);
// when it is back, sending and consuming carry on by themselves.
func TestOutage(t *testing.T) {
	dir := t.TempDir()
	srv := startServer(t, dir, -1)
	port := srv.Addr().(*net.TCPAddr).Port
	// A message handed over while the connection was coming back can be lost on the way: the
	// broker delivers it again after AckWait, so the wait is kept short here.
	tr := open(t, knats.Config{URL: srv.ClientURL(), AckWait: time.Second, Options: []natsgo.Option{natsgo.ReconnectWait(50 * time.Millisecond)}})
	var handled atomic.Int64
	if _, err := tr.Attach(context.Background(), "ledger", application.MessageHandlerFunc(func(context.Context, application.Envelope) error {
		handled.Add(1)
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	send := func(id string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		return tr.Send(ctx, envelope(id, "things.thing-happened.v1", "a"))
	}
	if err := send("m-1"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the first message", func() bool { return handled.Load() == 1 })

	stopServer(srv)
	if err := send("m-2"); !errors.Is(err, application.ErrTransportUnavailable) {
		t.Fatalf("sending with the broker down: %v", err)
	}

	startServer(t, dir, port)
	eventually(t, "the connection to come back", func() bool { return send("m-2") == nil })
	eventually(t, "the consumer to carry on", func() bool { return handled.Load() == 2 })
}

// A message that fails every time is parked with its cause, and a body that is not an envelope
// is parked at once; neither stops the consumer.
func TestDeadLetters(t *testing.T) {
	srv := startServer(t, t.TempDir(), -1)
	tr := open(t, knats.Config{URL: srv.ClientURL(), MaxDeliveries: 2})
	ctx := context.Background()
	var handled atomic.Int64
	if _, err := tr.Attach(ctx, "ledger", application.MessageHandlerFunc(func(_ context.Context, env application.Envelope) error {
		if env.Subject == "poison" {
			return errors.New("the ledger of this company does not exist")
		}
		handled.Add(1)
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	conn, err := natsgo.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	js, _ := jetstream.New(conn)
	if _, err := js.Publish(ctx, "karpo.evt.things.thing-happened.v1", []byte("not json")); err != nil { // stored before the next ones
		t.Fatal(err)
	}
	if err := tr.Send(ctx, envelope("m-1", "things.thing-happened.v1", "poison"), envelope("m-2", "things.thing-happened.v1", "fine")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the message after the two that cannot be handled", func() bool { return handled.Load() == 1 })

	dead, err := tr.DeadLetters(ctx, "ledger")
	if err != nil || len(dead) != 2 {
		t.Fatalf("dead letters: %+v %v", dead, err)
	}
	if d := dead[0]; string(d.Raw) != "not json" || d.Deliveries != 1 || d.Consumer != "ledger" || d.Error == "" {
		t.Errorf("a body that is not an envelope is parked as it came: %+v", d)
	}
	if d := dead[1]; d.Envelope.ID != "m-1" || d.Envelope.Subject != "poison" || d.Deliveries != 2 || d.Raw != nil ||
		d.Error != "the ledger of this company does not exist" || d.At.IsZero() || string(d.Envelope.Data) != `{"seq":1}` {
		t.Errorf("dead letter: %+v", d)
	}
	if other, err := tr.DeadLetters(ctx, "billing"); err != nil || len(other) != 0 {
		t.Errorf("dead letters are kept per consumer: %+v %v", other, err)
	}
}

// A handler that takes longer than AckWait keeps its message: no other replica gets it.
func TestSlowHandlerKeepsItsMessage(t *testing.T) {
	srv := startServer(t, t.TempDir(), -1)
	tr := open(t, knats.Config{URL: srv.ClientURL(), AckWait: 300 * time.Millisecond})
	ctx := context.Background()
	var deliveries, done atomic.Int64
	slow := application.MessageHandlerFunc(func(context.Context, application.Envelope) error {
		deliveries.Add(1)
		time.Sleep(1200 * time.Millisecond)
		done.Add(1)
		return nil
	})
	for range 2 {
		if _, err := tr.Attach(ctx, "ledger", slow); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.Send(ctx, envelope("m-1", "things.thing-happened.v1", "a")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the slow handler", func() bool { return done.Load() == 1 })
	time.Sleep(600 * time.Millisecond) // two more AckWait: a redelivery would have shown up
	if n := deliveries.Load(); n != 1 {
		t.Fatalf("the message was delivered %d times while it was being handled", n)
	}
}
