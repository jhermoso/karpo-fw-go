package messaging_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

type accountOpened struct {
	domain.EventMeta
	Owner string `json:"owner"`
	Limit int    `json:"limit"`
}

func (accountOpened) EventType() string { return "accounts.account_opened" }

type accountNoted struct{ domain.EventMeta }

func (accountNoted) EventType() string { return "accounts.account_noted" }

type accountOpenedV1 struct {
	AccountID string `json:"accountId"`
	Owner     string `json:"owner"`
}

func (accountOpenedV1) IntegrationEventType() string { return "accounts.account-opened.v1" }

type badType struct{}

func (badType) IntegrationEventType() string { return "AccountOpened" }

func TestValidType(t *testing.T) {
	for typ, want := range map[string]bool{
		"parties.party-registered.v1": true, "a.b.c.v12": true,
		"parties.party-registered": false, "PartyRegistered": false, "parties.v1": false,
		"parties.party_registered.v1": false, "parties.party-registered.v0": false,
	} {
		if messaging.ValidType(typ) != want {
			t.Errorf("%q: want %v", typ, want)
		}
	}
}

func opened(owner string) accountOpened {
	return accountOpened{EventMeta: domain.EventMeta{EventID: domain.NewUUID().String(), AggregateType: "account",
		AggregateID: "acc-1", AggregateVersion: 1, OccurredAt: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}, Owner: owner}
}

func TestRecorder_TranslatesInTheUnitOfWork(t *testing.T) {
	ctx := application.WithCorrelationID(context.Background(), "corr-1")
	store := memory.NewStore("s")
	outbox := memory.NewOutbox(store)
	rec := messaging.NewRecorder("accounts", outbox)
	messaging.On(rec, func(_ context.Context, e accountOpened) ([]application.IntegrationEvent, error) {
		return []application.IntegrationEvent{accountOpenedV1{AccountID: e.AggregateID, Owner: e.Owner}}, nil
	})

	evt := opened("ana")
	if err := store.Do(ctx, func(ctx context.Context) error {
		return rec.Record(ctx, []domain.Event{evt, accountNoted{}})
	}); err != nil {
		t.Fatal(err)
	}
	msgs, _ := outbox.Pending(ctx, 10, 10)
	if len(msgs) != 1 {
		t.Fatalf("only translated events are published: %d", len(msgs))
	}
	env := messaging.Envelope("accounts", msgs[0])
	if env.Type != "accounts.account-opened.v1" || env.Source != "accounts" || env.Subject != "acc-1" ||
		env.CausationID != evt.EventID || env.CorrelationID != "corr-1" || !env.OccurredAt.Equal(evt.OccurredAt) ||
		string(env.Data) != `{"accountId":"acc-1","owner":"ana"}` || env.ID == evt.EventID {
		t.Fatalf("envelope: %+v data=%s", env, env.Data)
	}

	// A translation error or an invalid type name fails the unit of work: nothing is recorded.
	boom := errors.New("boom")
	messaging.On(rec, func(context.Context, accountNoted) ([]application.IntegrationEvent, error) { return nil, boom })
	if err := store.Do(ctx, func(ctx context.Context) error {
		return rec.Record(ctx, []domain.Event{opened("luis"), accountNoted{}})
	}); !errors.Is(err, boom) {
		t.Fatalf("translator error: %v", err)
	}
	bad := messaging.NewRecorder("accounts", outbox).Use(application.TranslatorFunc(func(context.Context, domain.Event) ([]application.IntegrationEvent, error) {
		return []application.IntegrationEvent{badType{}}, nil
	}))
	if err := store.Do(ctx, func(ctx context.Context) error {
		return bad.Record(ctx, []domain.Event{accountNoted{}})
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("invalid type: %v", err)
	}
	if msgs, _ := outbox.Pending(ctx, 10, 10); len(msgs) != 1 {
		t.Fatalf("failed units of work must not leave messages: %d", len(msgs))
	}
}

func TestConsumer_InboxMakesRedeliveryHarmless(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore("consumer")
	c := messaging.NewConsumer("crm", memory.NewInbox(store), store)
	var seen []string
	fail := true
	messaging.Handle(c, func(ctx context.Context, e accountOpenedV1, env application.Envelope) error {
		if application.CausationID(ctx) != env.ID || application.CorrelationID(ctx) != "corr-9" {
			t.Errorf("context must carry the message ids")
		}
		if fail {
			fail = false
			return errors.New("transient")
		}
		seen = append(seen, e.Owner)
		return nil
	})
	env := application.Envelope{ID: "m-1", Type: "accounts.account-opened.v1", CorrelationID: "corr-9",
		Data: []byte(`{"accountId":"acc-1","owner":"ana","extra":"ignored"}`)}

	if err := c.HandleMessage(ctx, env); err == nil {
		t.Fatal("first delivery fails")
	}
	for range 3 { // redeliveries
		if err := c.HandleMessage(ctx, env); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 1 || seen[0] != "ana" {
		t.Fatalf("a failed attempt must release the claim and duplicates must be skipped: %v", seen)
	}
	if err := c.HandleMessage(ctx, application.Envelope{ID: "m-2", Type: "other.thing.v1"}); err != nil {
		t.Fatal("unknown types are acknowledged")
	}
	if err := c.HandleMessage(ctx, application.Envelope{Type: "accounts.account-opened.v1"}); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("an envelope without id cannot be deduplicated")
	}
	if got := c.Types(); len(got) != 1 || got[0] != "accounts.account-opened.v1" {
		t.Fatalf("types: %v", got)
	}
}

func TestRelay_ThroughTheInProcessBroker(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore("s")
	outbox := memory.NewOutbox(store)
	rec := messaging.NewRecorder("accounts", outbox)
	messaging.On(rec, func(_ context.Context, e accountOpened) ([]application.IntegrationEvent, error) {
		return []application.IntegrationEvent{accountOpenedV1{AccountID: e.AggregateID, Owner: e.Owner}}, nil
	})
	if err := rec.Record(ctx, []domain.Event{opened("ana")}); err != nil {
		t.Fatal(err)
	}

	broker := inprocess.NewBroker()
	var a, b, other int
	unsubscribe := broker.Subscribe("a", application.MessageHandlerFunc(func(context.Context, application.Envelope) error { a++; return nil }))
	broker.Subscribe("b", application.MessageHandlerFunc(func(context.Context, application.Envelope) error {
		b++
		if b == 1 {
			return errors.New("down")
		}
		return nil
	}), "accounts.account-opened.v1")
	broker.Subscribe("c", application.MessageHandlerFunc(func(context.Context, application.Envelope) error { other++; return nil }), "x.y.v1")

	relay := messaging.NewRelay("accounts", outbox, broker)
	if n, err := relay.RelayOnce(ctx); n != 0 || err != nil {
		t.Fatalf("a consumer failure keeps the message pending: %d %v", n, err)
	}
	if n, err := relay.RelayOnce(ctx); n != 1 || err != nil {
		t.Fatalf("retry: %d %v", n, err)
	}
	if a != 2 || b != 2 || other != 0 {
		t.Fatalf("fan-out: a=%d b=%d other=%d (at-least-once: a sees the retry too)", a, b, other)
	}
	unsubscribe()
	if err := broker.Send(ctx, application.Envelope{ID: "z", Type: "accounts.account-opened.v1"}); err != nil || a != 2 {
		t.Fatal("unsubscribed consumers receive nothing")
	}
}
