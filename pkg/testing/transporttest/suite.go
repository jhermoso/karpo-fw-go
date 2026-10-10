// Package transporttest is the conformance suite of message transports: what every
// implementation of application.MessageSender and application.MessageSubscriber must prove
// before it carries integration events between bounded contexts. Each transport runs it from its
// own tests with a Harness (the in-process broker included), like repotest for repositories.
//
// The suite drives the real chain on top of the transport: an integration outbox, the relay of
// application/messaging and consumers with their inbox. So it holds for transports that run the
// consumers inside Send (the relay redelivers) and for brokers that store the message and
// redeliver it themselves.
package transporttest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// Transport is the pair of ports under test.
type Transport struct {
	Sender     application.MessageSender
	Subscriber application.MessageSubscriber
}

// MaxDeliveries is how many times the suite lets a message fail before it must be parked. A
// transport that parks messages itself (Harness.DeadLetters) must be configured with this limit
// and with waits between redeliveries short enough for Harness.Timeout.
const MaxDeliveries = 3

// Harness provides fresh transports to the suite and declares what they support.
type Harness struct {
	// New returns an empty transport: nothing sent through an earlier one reaches it.
	New func(t *testing.T) Transport
	// Replicas is true when the transport shares the messages among the replicas of a consumer
	// (each one is delivered to a single replica) instead of delivering to all of them.
	Replicas bool
	// Durable is true when messages sent while a consumer is detached wait for it.
	Durable bool
	// DeadLetters returns the messages the transport parked for a consumer after MaxDeliveries
	// failed deliveries. Nil when the transport does not park: then the relay does, in the
	// outbox of the publisher.
	DeadLetters func(t *testing.T, consumer string) []application.Envelope
	// Timeout bounds the wait for each expected effect (default 15s).
	Timeout time.Duration
}

// Run executes the whole conformance suite.
func Run(t *testing.T, h Harness) {
	if h.Timeout <= 0 {
		h.Timeout = 15 * time.Second
	}
	t.Run("Delivery", func(t *testing.T) { testDelivery(t, h) })
	t.Run("TypeFilter", func(t *testing.T) { testTypeFilter(t, h) })
	t.Run("IndependentConsumers", func(t *testing.T) { testIndependentConsumers(t, h) })
	t.Run("Redelivery", func(t *testing.T) { testRedelivery(t, h) })
	t.Run("Duplicates", func(t *testing.T) { testDuplicates(t, h) })
	t.Run("Replicas", func(t *testing.T) { testReplicas(t, h) })
	t.Run("OrderPerSubject", func(t *testing.T) { testOrder(t, h, 1) })
	t.Run("OrderPerSubjectWithReplicas", func(t *testing.T) {
		if !h.Replicas {
			t.Skip("the transport does not share messages among replicas")
		}
		testOrder(t, h, 2)
	})
	t.Run("Durability", func(t *testing.T) {
		if !h.Durable {
			t.Skip("the transport does not keep messages for detached consumers")
		}
		testDurability(t, h)
	})
	t.Run("PoisonMessage", func(t *testing.T) { testPoison(t, h) })
}

// ---------------------------------------------------------------------------------------------
// Published Language of the suite
// ---------------------------------------------------------------------------------------------

const source = "conformance"

type thingHappened struct {
	Seq  int    `json:"seq"`
	Note string `json:"note,omitempty"`
}

func (thingHappened) IntegrationEventType() string { return "conformance.thing-happened.v1" }

type thingHappenedV2 struct {
	Seq int `json:"seq"`
}

func (thingHappenedV2) IntegrationEventType() string { return "conformance.thing-happened.v2" }

type otherThing struct {
	Seq int `json:"seq"`
}

func (otherThing) IntegrationEventType() string { return "conformance.other-thing.v1" }

// ---------------------------------------------------------------------------------------------
// Fixture: a publishing context (outbox + relay) and consuming services (inbox + handlers)
// ---------------------------------------------------------------------------------------------

type world struct {
	t      *testing.T
	h      Harness
	tr     Transport
	outbox *memory.Outbox
	relay  *outbox.Relay
	seq    int
}

func newWorld(t *testing.T, h Harness) *world {
	t.Helper()
	tr := h.New(t)
	ob := memory.NewOutbox(memory.NewStore(source))
	return &world{t: t, h: h, tr: tr, outbox: ob,
		relay: messaging.NewRelay(source, ob, tr.Sender, outbox.WithMaxAttempts(MaxDeliveries))}
}

var epoch = time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)

// publish records an integration event about subject in the outbox and returns its envelope.
func (w *world) publish(subject string, evt application.IntegrationEvent) application.Envelope {
	w.t.Helper()
	w.seq++
	data, err := json.Marshal(evt)
	if err != nil {
		w.t.Fatal(err)
	}
	m := application.OutboxMessage{
		ID: domain.NewUUID().String(), EventType: evt.IntegrationEventType(),
		AggregateType: "thing", AggregateID: subject, AggregateVersion: int64(w.seq),
		Payload: data, OccurredAt: epoch.Add(time.Duration(w.seq) * time.Millisecond),
		CorrelationID: fmt.Sprintf("corr-%d", w.seq), CausationID: fmt.Sprintf("cause-%d", w.seq),
	}
	if err := w.outbox.Append(context.Background(), m); err != nil {
		w.t.Fatal(err)
	}
	return messaging.Envelope(source, m)
}

// thing publishes a thingHappened numbered seq about subject.
func (w *world) thing(subject string, seq int) application.Envelope {
	w.t.Helper()
	return w.publish(subject, thingHappened{Seq: seq})
}

// eventually runs the relay until cond holds, or fails after the timeout.
func (w *world) eventually(what string, cond func() bool) {
	w.t.Helper()
	deadline := time.Now().Add(w.h.Timeout)
	for {
		_, _ = w.relay.RelayOnce(context.Background())
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// settle publishes a last message and waits until every service handled it. Deliveries to a
// consumer are ordered, so anything the transport still owed them has arrived by then.
func (w *world) settle(services ...*service) application.Envelope {
	w.t.Helper()
	marker := w.publish("marker", thingHappened{Seq: -1, Note: "marker"})
	w.eventually("the marker to reach every consumer", func() bool {
		for _, s := range services {
			if s.count(marker.ID) == 0 {
				return false
			}
		}
		return true
	})
	return marker
}

func (w *world) attach(consumer string, h application.MessageHandler, types ...string) (detach func()) {
	w.t.Helper()
	stop, err := w.tr.Subscriber.Attach(context.Background(), consumer, h, types...)
	if err != nil {
		w.t.Fatalf("attach %s: %v", consumer, err)
	}
	var once sync.Once
	detach = func() {
		once.Do(func() {
			if err := stop(); err != nil {
				w.t.Errorf("detach %s: %v", consumer, err)
			}
		})
	}
	w.t.Cleanup(detach)
	return detach
}

type effect struct {
	id, subject, replica string
	seq                  int
}

// service is a consuming bounded context: one inbox shared by its replicas.
type service struct {
	w     *world
	name  string
	store *memory.Store
	inbox *memory.Inbox

	mu         sync.Mutex
	effects    []effect
	attempts   map[string]int                                    // handler runs per message id
	fail       func(env application.Envelope, attempt int) error // decides whether a run fails
	deliveries atomic.Int64                                      // envelopes handed over by the transport
}

func (w *world) service(name string) *service {
	store := memory.NewStore(name)
	return &service{w: w, name: name, store: store, inbox: memory.NewInbox(store), attempts: map[string]int{}}
}

// replica attaches one more replica of the service and returns its detach function.
func (s *service) replica(label string) (detach func()) {
	s.w.t.Helper()
	c := messaging.NewConsumer(s.name, s.inbox, s.store)
	messaging.Handle(c, func(_ context.Context, e thingHappened, env application.Envelope) error {
		return s.handle(label, e.Seq, env)
	})
	messaging.Handle(c, func(_ context.Context, e thingHappenedV2, env application.Envelope) error {
		return s.handle(label, e.Seq, env)
	})
	counted := application.MessageHandlerFunc(func(ctx context.Context, env application.Envelope) error {
		s.deliveries.Add(1)
		return c.HandleMessage(ctx, env)
	})
	return s.w.attach(s.name, counted, c.Types()...)
}

func (s *service) handle(replica string, seq int, env application.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[env.ID]++
	if s.fail != nil {
		if err := s.fail(env, s.attempts[env.ID]); err != nil {
			return err
		}
	}
	s.effects = append(s.effects, effect{id: env.ID, subject: env.Subject, replica: replica, seq: seq})
	return nil
}

func (s *service) failing(fn func(env application.Envelope, attempt int) error) *service {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = fn
	return s
}

// count returns how many times the message took effect.
func (s *service) count(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.effects {
		if e.id == id {
			n++
		}
	}
	return n
}

func (s *service) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.effects)
}

func (s *service) attemptsOf(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts[id]
}

func (s *service) snapshot() []effect {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.effects)
}

// exactlyOnce fails unless each of the envelopes took effect once in the service.
func (s *service) exactlyOnce(envs ...application.Envelope) {
	s.w.t.Helper()
	for _, env := range envs {
		if n := s.count(env.ID); n != 1 {
			s.w.t.Errorf("%s: message %s (%s) took effect %d times, want 1", s.name, env.ID, env.Subject, n)
		}
	}
}

// probe is a consumer without inbox that keeps every envelope it is handed.
type probe struct {
	mu  sync.Mutex
	got []application.Envelope
}

func (p *probe) HandleMessage(_ context.Context, env application.Envelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, env)
	return nil
}

func (p *probe) envelopes() []application.Envelope {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.got)
}

func (p *probe) has(id string) bool {
	return slices.ContainsFunc(p.envelopes(), func(e application.Envelope) bool { return e.ID == id })
}

// first returns the envelopes without redeliveries, in order of first arrival.
func (p *probe) first() []application.Envelope {
	var out []application.Envelope
	for _, e := range p.envelopes() {
		if !slices.ContainsFunc(out, func(o application.Envelope) bool { return o.ID == e.ID }) {
			out = append(out, e)
		}
	}
	return out
}

func ids(envs []application.Envelope) []string {
	out := make([]string, len(envs))
	for i, e := range envs {
		out[i] = e.ID
	}
	return out
}

// ---------------------------------------------------------------------------------------------
// Cases
// ---------------------------------------------------------------------------------------------

// Everything sent reaches a subscribed consumer with the envelope untouched.
func testDelivery(t *testing.T, h Harness) {
	w := newWorld(t, h)
	p := &probe{}
	w.attach("audit", p)
	sent := []application.Envelope{
		w.publish("thing-1", thingHappened{Seq: 1, Note: "ñandú \"quoted\" <tag>"}),
		w.publish("thing-2", thingHappenedV2{Seq: 2}),
		w.publish("thing-1", otherThing{Seq: 3}),
	}
	w.eventually("the three messages", func() bool { return len(p.first()) == len(sent) })

	for i, got := range p.first() {
		want := sent[i]
		if got.ID != want.ID || got.Type != want.Type || got.Source != want.Source || got.Subject != want.Subject ||
			got.CorrelationID != want.CorrelationID || got.CausationID != want.CausationID || !got.OccurredAt.Equal(want.OccurredAt) {
			t.Errorf("envelope %d changed on the way:\nsent %+v\ngot  %+v", i, want, got)
		}
		var a, b bytes.Buffer
		if json.Compact(&a, want.Data) != nil || json.Compact(&b, got.Data) != nil || !bytes.Equal(a.Bytes(), b.Bytes()) {
			t.Errorf("data of envelope %d changed on the way:\nsent %s\ngot  %s", i, want.Data, got.Data)
		}
	}
}

// A consumer receives the types it asked for and nothing else; v1 and v2 are different types.
// A type a consumer did not ask for has no effect on it and does not hold its next messages back.
func testTypeFilter(t *testing.T, h Harness) {
	w := newWorld(t, h)
	onlyV1, all := &probe{}, &probe{}
	w.attach("only-v1", onlyV1, thingHappened{}.IntegrationEventType())
	w.attach("everything", all)
	svc := w.service("billing")
	svc.replica("r1")

	a1 := w.thing("a", 1)
	other := w.publish("a", otherThing{Seq: 2})
	v2 := w.publish("a", thingHappenedV2{Seq: 3})
	a4 := w.thing("a", 4)
	marker := w.settle(svc)
	w.eventually("the marker on both probes", func() bool { return onlyV1.has(marker.ID) && all.has(marker.ID) })

	if got, want := ids(onlyV1.first()), []string{a1.ID, a4.ID, marker.ID}; !slices.Equal(got, want) {
		t.Errorf("a consumer of v1 must receive only v1: got %v, want %v", got, want)
	}
	if got, want := ids(all.first()), []string{a1.ID, other.ID, v2.ID, a4.ID, marker.ID}; !slices.Equal(got, want) {
		t.Errorf("a consumer of every type must receive all of them in order: got %v, want %v", got, want)
	}
	svc.exactlyOnce(a1, v2, a4)
	if svc.count(other.ID) != 0 {
		t.Errorf("a type without handler must have no effect")
	}
}

// Two logical consumers both receive each message; one failing does not repeat the effect in
// the other.
func testIndependentConsumers(t *testing.T, h Harness) {
	w := newWorld(t, h)
	billing, crm := w.service("billing"), w.service("crm")
	crm.failing(func(env application.Envelope, attempt int) error {
		if env.Subject == "a" && attempt == 1 {
			return errors.New("crm is down")
		}
		return nil
	})
	billing.replica("r1")
	crm.replica("r1")

	a, b := w.thing("a", 1), w.thing("b", 1)
	w.eventually("both consumers to handle both messages", func() bool { return billing.total() == 2 && crm.total() == 2 })
	w.settle(billing, crm)

	billing.exactlyOnce(a, b)
	crm.exactlyOnce(a, b)
	if n := crm.attemptsOf(a.ID); n < 2 {
		t.Errorf("the failed message must be delivered again to the consumer that failed: %d runs", n)
	}
}

// A message whose handler fails comes back until it succeeds, and takes effect once.
func testRedelivery(t *testing.T, h Harness) {
	w := newWorld(t, h)
	svc := w.service("billing").failing(func(_ application.Envelope, attempt int) error {
		if attempt < MaxDeliveries {
			return fmt.Errorf("transient failure %d", attempt)
		}
		return nil
	})
	svc.replica("r1")

	a := w.thing("a", 1)
	w.eventually("the message to succeed after failing", func() bool { return svc.count(a.ID) == 1 })
	if n := svc.attemptsOf(a.ID); n != MaxDeliveries {
		t.Errorf("handler runs: %d, want %d", n, MaxDeliveries)
	}
}

// The same envelope sent twice takes effect once.
func testDuplicates(t *testing.T, h Harness) {
	w := newWorld(t, h)
	svc := w.service("billing")
	svc.replica("r1")

	m := application.OutboxMessage{ID: domain.NewUUID().String(), EventType: thingHappened{}.IntegrationEventType(),
		AggregateType: "thing", AggregateID: "a", Payload: []byte(`{"seq":1}`), OccurredAt: epoch}
	env := messaging.Envelope(source, m)
	for range 2 {
		deadline := time.Now().Add(h.Timeout)
		for w.tr.Sender.Send(context.Background(), env) != nil {
			if time.Now().After(deadline) {
				t.Fatal("timed out sending")
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	w.settle(svc)
	svc.exactlyOnce(env)
}

// Replicas of one consumer: every message takes effect once among them, and when one leaves
// the others carry on.
func testReplicas(t *testing.T, h Harness) {
	w := newWorld(t, h)
	svc := w.service("billing")
	stopFirst := svc.replica("r1")
	svc.replica("r2")

	var sent []application.Envelope
	for i := range 20 {
		sent = append(sent, w.thing(fmt.Sprintf("subject-%d", i%5), i))
	}
	w.eventually("the twenty messages", func() bool { return svc.total() >= len(sent) })
	marker := w.settle(svc)
	svc.exactlyOnce(append(sent, marker)...)
	if n := svc.deliveries.Load(); h.Replicas && n != int64(len(sent)+1) {
		t.Errorf("each message must be delivered to one replica: %d deliveries of %d messages", n, len(sent)+1)
	}

	stopFirst()
	later := []application.Envelope{w.thing("subject-0", 100), w.thing("subject-1", 101), w.thing("subject-2", 102)}
	w.eventually("the remaining replica to take over", func() bool { return svc.total() >= len(sent)+1+len(later) })
	svc.exactlyOnce(later...)
	for _, e := range svc.snapshot() {
		if e.seq >= 100 && e.replica != "r2" {
			t.Errorf("message %d was handled by the detached replica %s", e.seq, e.replica)
		}
	}
}

// The facts about one subject take effect in the order they happened, also when one of them
// fails and is delivered again. Nothing is promised between different subjects.
func testOrder(t *testing.T, h Harness, replicas int) {
	w := newWorld(t, h)
	svc := w.service("ledger").failing(func(env application.Envelope, attempt int) error {
		var e thingHappened
		_ = json.Unmarshal(env.Data, &e)
		if env.Subject == "a" && e.Seq == 2 && attempt < MaxDeliveries {
			return errors.New("the second fact about a fails at first")
		}
		return nil
	})
	for i := range replicas {
		svc.replica(fmt.Sprintf("r%d", i+1))
	}

	want := map[string][]int{}
	for _, f := range []struct {
		subject string
		seq     int
	}{{"a", 1}, {"b", 1}, {"a", 2}, {"a", 3}, {"b", 2}, {"a", 4}, {"c", 1}, {"b", 3}, {"a", 5}} {
		w.thing(f.subject, f.seq)
		want[f.subject] = append(want[f.subject], f.seq)
	}
	w.eventually("the nine facts", func() bool { return svc.total() >= 9 })
	w.settle(svc)

	got := map[string][]int{}
	for _, e := range svc.snapshot() {
		if e.subject != "marker" {
			got[e.subject] = append(got[e.subject], e.seq)
		}
	}
	for subject, seqs := range want {
		if !slices.Equal(got[subject], seqs) {
			t.Errorf("subject %s took effect as %v, want %v", subject, got[subject], seqs)
		}
	}
}

// What is sent while a consumer is detached reaches it when it attaches again, in order.
func testDurability(t *testing.T, h Harness) {
	w := newWorld(t, h)
	svc := w.service("billing")
	stop := svc.replica("r1")
	first := w.thing("a", 1)
	w.eventually("the consumer to exist and handle a message", func() bool { return svc.count(first.ID) == 1 })
	stop()

	missed := []application.Envelope{w.thing("a", 2), w.thing("b", 1), w.thing("a", 3)}
	w.eventually("the transport to accept the messages", func() bool {
		pending, err := w.outbox.Pending(context.Background(), 10, MaxDeliveries)
		return err == nil && len(pending) == 0
	})
	if svc.total() != 1 {
		t.Fatalf("a detached consumer must not receive anything: %d effects", svc.total())
	}

	svc.replica("r2")
	w.eventually("the missed messages", func() bool { return svc.total() >= 1+len(missed) })
	svc.exactlyOnce(missed...)
	var a []int
	for _, e := range svc.snapshot() {
		if e.subject == "a" {
			a = append(a, e.seq)
		}
	}
	if !slices.Equal(a, []int{1, 2, 3}) {
		t.Errorf("missed messages must arrive in order: %v", a)
	}
}

// A message that always fails is parked after MaxDeliveries and the next ones carry on.
func testPoison(t *testing.T, h Harness) {
	w := newWorld(t, h)
	svc := w.service("ledger").failing(func(env application.Envelope, _ int) error {
		var e thingHappened
		_ = json.Unmarshal(env.Data, &e)
		if e.Note == "poison" {
			return errors.New("this message can never be handled")
		}
		return nil
	})
	svc.replica("r1")

	poison := w.publish("a", thingHappened{Seq: 1, Note: "poison"})
	b, a2 := w.thing("b", 1), w.thing("a", 2)
	w.eventually("the messages after the poisoned one", func() bool { return svc.count(b.ID) == 1 && svc.count(a2.ID) == 1 })
	w.settle(svc)

	if svc.count(poison.ID) != 0 {
		t.Errorf("the poisoned message must not take effect")
	}
	if n := svc.attemptsOf(poison.ID); n != MaxDeliveries {
		t.Errorf("the poisoned message must be tried %d times, was tried %d", MaxDeliveries, n)
	}
	if h.DeadLetters != nil {
		w.eventually("the poisoned message among the dead letters", func() bool {
			return slices.ContainsFunc(h.DeadLetters(t, svc.name), func(e application.Envelope) bool { return e.ID == poison.ID })
		})
		return
	}
	// The transport does not park: the relay did, in the outbox of the publisher.
	all, err := w.outbox.Pending(context.Background(), 100, math.MaxInt)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != poison.ID || all[0].Attempts != MaxDeliveries || all[0].LastError == "" {
		t.Errorf("the poisoned message must stay parked in the outbox with its error: %+v", all)
	}
}
