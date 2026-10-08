package hotswap_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlconformance"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	rt "github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
)

type widgetRepo = domain.Repository[rt.WidgetID, *rt.Widget]

// factory builds the Widget repository for whatever backend is current: the same domain
// repository value works over SQL (any dialect) and memory.
func factory(b hotswap.Backend) (widgetRepo, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, sqlconformance.WidgetMapping())
	case *memory.Store:
		return memory.NewRepository[rt.WidgetID, *rt.Widget](db), nil
	}
	return nil, fmt.Errorf("unsupported backend %T", b)
}

func sqliteFile(t *testing.T, name string) (string, *sqlrepo.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".db")
	db := openSQLite(t, path, name)
	if err := sqlconformance.Reset(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return path, db
}

func openSQLite(t *testing.T, path, name string) *sqlrepo.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() }) // Windows cannot delete open database files
	return sqlite.Open(raw, sqlrepo.WithName(name))
}

func widget(t *testing.T, name string) *rt.Widget {
	t.Helper()
	w, err := rt.NewWidget(rt.NewWidgetID(), name, 100, true, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestSwap_RoutesNewOperationsAndClosesOldBackend(t *testing.T) {
	ctx := context.Background()
	pathA, dbA := sqliteFile(t, "A")
	_, dbB := sqliteFile(t, "B")

	sw := hotswap.New(dbA)
	repo := hotswap.Repository(sw, factory)

	onA := widget(t, "on-A")
	if err := repo.Save(ctx, onA); err != nil {
		t.Fatal(err)
	}
	if err := sw.Swap(ctx, dbB); err != nil {
		t.Fatalf("swap: %v", err)
	}
	if sw.Name() != "B" {
		t.Fatalf("expected B to be current, got %s", sw.Name())
	}
	if _, err := repo.Get(ctx, onA.ID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("after swap reads must hit B, got %v", err)
	}
	onB := widget(t, "on-B")
	if err := repo.Save(ctx, onB); err != nil {
		t.Fatal(err)
	}
	if err := dbA.Ping(ctx); err == nil {
		t.Fatal("old backend must be closed after the swap drained")
	}

	// The data written before the swap is still in A.
	reopened := openSQLite(t, pathA, "A2")
	defer reopened.Close()
	if _, err := sqlrepo.MustRepository(reopened, sqlconformance.WidgetMapping()).Get(ctx, onA.ID()); err != nil {
		t.Fatalf("A must keep its data: %v", err)
	}
}

func TestSwap_InFlightUnitOfWorkStaysOnItsBackend(t *testing.T) {
	ctx := context.Background()
	pathA, dbA := sqliteFile(t, "A")
	_, dbB := sqliteFile(t, "B")
	sw := hotswap.New(dbA)
	repo := hotswap.Repository(sw, factory)

	inTx := make(chan struct{})
	release := make(chan struct{})
	txWidget := widget(t, "written-in-tx")
	txErr := make(chan error, 1)
	go func() {
		txErr <- sw.Do(ctx, func(ctx context.Context) error {
			if err := repo.Save(ctx, widget(t, "first")); err != nil {
				return err
			}
			close(inTx)
			<-release // the swap happens while this transaction is open
			return repo.Save(ctx, txWidget)
		})
	}()
	<-inTx

	swapDone := make(chan error, 1)
	go func() { swapDone <- sw.Swap(ctx, dbB) }()

	// New operations are routed to B immediately, while the transaction still runs on A.
	deadline := time.Now().Add(5 * time.Second)
	for sw.Name() != "B" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	onB := widget(t, "during-swap")
	if err := repo.Save(ctx, onB); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-swapDone:
		t.Fatalf("swap must wait for the in-flight transaction, returned %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if err := <-txErr; err != nil {
		t.Fatalf("in-flight transaction failed: %v", err)
	}
	if err := <-swapDone; err != nil {
		t.Fatalf("swap: %v", err)
	}

	reopened := openSQLite(t, pathA, "A2")
	defer reopened.Close()
	a := sqlrepo.MustRepository(reopened, sqlconformance.WidgetMapping())
	if _, err := a.Get(ctx, txWidget.ID()); err != nil {
		t.Fatalf("the transaction must have committed on A: %v", err)
	}
	if _, err := a.Get(ctx, onB.ID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("operations issued during the swap must not reach A: %v", err)
	}
	if _, err := repo.Get(ctx, onB.ID()); err != nil {
		t.Fatalf("B must serve new operations: %v", err)
	}
}

func TestSwap_TimeoutKeepsRoutingAndClosesLater(t *testing.T) {
	a, b := memory.NewStore("A"), memory.NewStore("B")
	sw := hotswap.New(a)
	repo := hotswap.Repository(sw, factory)

	hold := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_ = sw.Do(context.Background(), func(ctx context.Context) error {
			close(started)
			<-hold
			return nil
		})
	}()
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := sw.Swap(ctx, b); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if sw.Current() != b {
		t.Fatal("routing must switch even if draining times out")
	}
	if err := repo.Save(context.Background(), widget(t, "x")); err != nil {
		t.Fatalf("B must work: %v", err)
	}
	close(hold)
	deadline := time.Now().Add(2 * time.Second)
	for a.Do(context.Background(), func(context.Context) error { return nil }) == nil {
		if time.Now().After(deadline) {
			t.Fatal("old backend must be closed once drained")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSwap_ConcurrentTrafficNeverFails(t *testing.T) {
	stores := []*memory.Store{memory.NewStore("s0")}
	sw := hotswap.New(stores[0])
	repo := hotswap.Repository(sw, factory)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var ops, failures atomic.Int64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				err := sw.Do(context.Background(), func(ctx context.Context) error {
					w, err := rt.NewWidget(rt.NewWidgetID(), "w", 1, true, nil, time.Now())
					if err != nil {
						return err
					}
					if err := repo.Save(ctx, w); err != nil {
						return err
					}
					_, err = repo.Get(ctx, w.ID())
					return err
				})
				ops.Add(1)
				if err != nil {
					failures.Add(1)
					t.Errorf("operation failed during swaps: %v", err)
					return
				}
			}
		}()
	}
	for i := 1; i <= 20; i++ {
		next := memory.NewStore(fmt.Sprintf("s%d", i))
		if err := sw.Swap(context.Background(), next); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	if failures.Load() > 0 || ops.Load() == 0 {
		t.Fatalf("ops=%d failures=%d", ops.Load(), failures.Load())
	}
}

func TestSwitch_CloseRejectsNewWork(t *testing.T) {
	sw := hotswap.New(memory.NewStore("A"))
	if err := sw.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := sw.Do(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, hotswap.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}
