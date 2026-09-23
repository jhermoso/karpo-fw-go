package memory_test

import (
	"context"
	"sync"
	"testing"
	stdtime "time"

	"github.com/jhermoso/karpo-fw-go/pkg/cache/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

func TestMemoryCache_BasicOperations(t *testing.T) {
	ctx := context.Background()
	c := memory.New[string, int]()

	// Initial Get
	if _, found := c.Get(ctx, "count"); found {
		t.Fatalf("expected key 'count' not found")
	}

	// Set and Get
	if err := c.Set(ctx, "count", 42, 0); err != nil {
		t.Fatalf("failed to set: %v", err)
	}

	val, found := c.Get(ctx, "count")
	if !found || val != 42 {
		t.Fatalf("expected 42, got %v (found=%v)", val, found)
	}

	// Delete
	if err := c.Delete(ctx, "count"); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}
	if _, found := c.Get(ctx, "count"); found {
		t.Fatalf("expected key to be deleted")
	}

	// Clear
	_ = c.Set(ctx, "a", 1, 0)
	_ = c.Set(ctx, "b", 2, 0)
	if err := c.Clear(ctx); err != nil {
		t.Fatalf("failed to clear: %v", err)
	}
	if _, found := c.Get(ctx, "a"); found {
		t.Fatalf("expected cache empty after clear")
	}
}

func TestMemoryCache_TTL_WithFakeClock(t *testing.T) {
	ctx := context.Background()
	startTime := stdtime.Date(2026, 9, 23, 10, 0, 0, 0, stdtime.UTC)
	mockClock := fake.New(startTime)
	c := memory.NewWithClock[string, string](mockClock)

	// Set with 10 second TTL
	_ = c.Set(ctx, "token", "secret123", 10*stdtime.Second)

	// Still valid after 5 seconds
	mockClock.Advance(5 * stdtime.Second)
	val, found := c.Get(ctx, "token")
	if !found || val != "secret123" {
		t.Fatalf("expected token to still be valid after 5s")
	}

	// Expired after another 6 seconds (total 11s)
	mockClock.Advance(6 * stdtime.Second)
	if _, found := c.Get(ctx, "token"); found {
		t.Fatalf("expected token to be expired after 11s")
	}
}

func TestMemoryCache_Concurrent(t *testing.T) {
	ctx := context.Background()
	c := memory.New[int, int]()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = c.Set(ctx, n, n*10, 0)
			val, found := c.Get(ctx, n)
			if found && val != n*10 {
				t.Errorf("unexpected value: %v", val)
			}
		}(i)
	}

	wg.Wait()
}
