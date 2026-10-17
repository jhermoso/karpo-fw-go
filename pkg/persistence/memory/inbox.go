package memory

import (
	"context"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
)

type inboxKey struct{ consumer, id string }

// Inbox es un application.InboxStore en memoria ligado a la unidad de trabajo de un Store: una
// reclamación hecha dentro de una unidad de trabajo que hace rollback se olvida.
type Inbox struct {
	store *Store
	mu    sync.Mutex
	seen  map[inboxKey]struct{}
}

var _ application.InboxStore = (*Inbox)(nil)

// NewInbox crea un inbox sobre store.
func NewInbox(store *Store) *Inbox {
	return &Inbox{store: store, seen: map[inboxKey]struct{}{}}
}

// Claim implementa application.InboxStore.
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
