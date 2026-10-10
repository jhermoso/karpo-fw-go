package integration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlconformance"
)

// TestOutbox checks the SQL outbox and its claims (FOR UPDATE SKIP LOCKED, READPAST) on every
// engine: a second relay neither waits for the first nor overtakes it, and publishers are not
// held up by either.
func TestOutbox(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) { sqlconformance.RunOutbox(t, open(t, e)) })
	}
}

// TestRelaysShareAnOutbox drains one outbox with three relays at once (the relays of three
// replicas of a service) while a publisher keeps appending: every message is sent once, and
// the facts about each subject leave in the order they happened, also when a delivery fails.
func TestRelaysShareAnOutbox(t *testing.T) {
	const relays, first, later, subjects = 3, 150, 50, 7
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			box := sqlconformance.ResetOutbox(t, db)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			names := make([]string, subjects)
			for i := range names {
				names[i] = fmt.Sprintf("subject-%d", i)
			}
			msgs := sqlconformance.OutboxMessages(first+later, names...)
			if err := box.Append(ctx, msgs[:first]...); err != nil {
				t.Fatal(err)
			}

			var mu sync.Mutex
			sent := map[string][]int64{} // subject -> versions, in the order they were sent
			times := map[string]int{}    // message id -> successful sends
			failed := map[string]bool{}  // messages that already failed once
			deliver := func(_ context.Context, m application.OutboxMessage) error {
				time.Sleep(time.Millisecond) // a network round trip
				mu.Lock()
				defer mu.Unlock()
				if m.AggregateVersion%17 == 0 && !failed[m.ID] {
					failed[m.ID] = true
					return errors.New("consumer down")
				}
				sent[m.AggregateID] = append(sent[m.AggregateID], m.AggregateVersion)
				times[m.ID]++
				return nil
			}
			total := func() int {
				mu.Lock()
				defer mu.Unlock()
				return len(times)
			}

			var wg sync.WaitGroup
			errs := make(chan error, relays+1)
			for range relays {
				relay := outbox.NewForwarder(box, deliver, outbox.WithClaims(db), outbox.WithBatchSize(10))
				wg.Go(func() {
					for total() < len(msgs) {
						if _, err := relay.RelayOnce(ctx); err != nil {
							errs <- err
							return
						}
					}
				})
			}
			wg.Go(func() { // the publisher
				for _, m := range msgs[first:] {
					if err := box.Append(ctx, m); err != nil {
						errs <- err
						return
					}
				}
			})
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Fatal(err)
			}

			for _, m := range msgs {
				if times[m.ID] != 1 {
					t.Errorf("message %s was sent %d times", m.ID, times[m.ID])
				}
			}
			for subject, versions := range sent {
				if !slices.IsSorted(versions) {
					t.Errorf("%s left out of order: %v", subject, versions)
				}
			}
			if len(failed) == 0 {
				t.Error("the test must exercise failed deliveries")
			}
			if left, err := box.Pending(ctx, 10, 10); err != nil || len(left) != 0 {
				t.Errorf("outbox not drained: %d %v", len(left), err)
			}
		})
	}
}
