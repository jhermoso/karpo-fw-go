package fake_test

import (
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

func TestFakeClock(t *testing.T) {
	start := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	clk := fake.New(start)

	if !clk.Now().Equal(start) {
		t.Fatalf("expected initial time %v, got %v", start, clk.Now())
	}

	// Advance 5 minutes
	clk.Advance(5 * time.Minute)
	expected := start.Add(5 * time.Minute)
	if !clk.Now().Equal(expected) {
		t.Fatalf("expected time %v after advance, got %v", expected, clk.Now())
	}

	// Since
	elapsed := clk.Since(start)
	if elapsed != 5*time.Minute {
		t.Fatalf("expected elapsed 5m, got %v", elapsed)
	}

	// Sleep
	clk.Sleep(1 * time.Hour)
	if clk.Since(expected) != 1*time.Hour {
		t.Fatalf("expected sleep to advance clock by 1h")
	}

	// Set
	newDate := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	clk.Set(newDate)
	if !clk.Now().Equal(newDate) {
		t.Fatalf("expected set to change time to %v", newDate)
	}
}
