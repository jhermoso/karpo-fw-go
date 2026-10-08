package memory

import (
	"context"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
)

type inboxKey struct{ consumer, id string }

// Inbox is an in-memory application.InboxStore bound to a Store's unit of work: a claim made
// inside a unit of work that rolls back is forgotten.
type Inbox struct {
	store *Store
	mu    sync.Mutex
	seen  map[inboxKey]struct{}
}

var _ application.InboxStore = (*Inbox)(nil)

// NewInbox creates an inbox on store.
func NewInbox(store *Store) *Inbox {
	return &Inbox{store: store, seen: map[inboxKey]struct{}{}}
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
		i.seen[k] = struct{}{}
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
