package memory

import (
	"context"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

type inboxKey struct{ consumer, id string }

// Inbox is an in-memory application.InboxStore bound to a Store's unit of work: a claim made
// inside a unit of work that rolls back is forgotten.
type Inbox struct {
	store *Store
	mu    sync.Mutex
	seen  map[inboxKey]time.Time // when each message was claimed
}

var (
	_ application.InboxStore  = (*Inbox)(nil)
	_ application.InboxPurger = (*Inbox)(nil)
)

// NewInbox creates an inbox on store.
func NewInbox(store *Store) *Inbox {
	return &Inbox{store: store, seen: map[inboxKey]time.Time{}}
}

// Claim implements application.InboxStore.
func (i *Inbox) Claim(ctx context.Context, consumer, messageID string) (bool, error) {
	if i.store.closed.Load() {
		return false, ErrClosed
	}
	k := inboxKey{consumer, messageID}
	i.mu.Lock()
	_, dup := i.seen[k]
	if !dup {
		i.seen[k] = domain.Now()
	}
	i.mu.Unlock()
	if dup {
		return false, nil
	}
	i.store.onRollback(ctx, func() {
		i.mu.Lock()
		delete(i.seen, k)
		i.mu.Unlock()
	})
	return true, nil
}

// Purge implements application.InboxPurger.
func (i *Inbox) Purge(_ context.Context, olderThan time.Time) (int64, error) {
	if i.store.closed.Load() {
		return 0, ErrClosed
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	var n int64
	for k, at := range i.seen {
		if at.Before(olderThan) {
			delete(i.seen, k)
			n++
		}
	}
	return n, nil
}
