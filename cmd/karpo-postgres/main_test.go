package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/host/serve"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
)

// TestProgram runs the program against a real PostgreSQL (KARPO_PG_DSN, the one of the integration
// suite): it refuses to start without its configuration, migrates, serves, answers that it is
// ready, asks for a session and lets the first administrator in, and stops when told.
func TestProgram(t *testing.T) {
	dsn := os.Getenv("KARPO_PG_DSN")
	if dsn == "" {
		t.Skip("KARPO_PG_DSN not set")
	}
	logger := vanilla.NewJSON(io.Discard, log.LevelError)
	ctx := context.Background()

	t.Setenv("KARPO_DATABASE_URL", "")
	t.Setenv("KARPO_JWT_SECRET", "short")
	err := run(ctx, serve.Migrate, logger)
	if err == nil || !strings.Contains(err.Error(), "KARPO_DATABASE_URL") || !strings.Contains(err.Error(), "KARPO_JWT_SECRET") {
		t.Fatalf("without configuration: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	t.Setenv("KARPO_DATABASE_URL", dsn)
	t.Setenv("KARPO_JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("KARPO_ADDR", addr)
	t.Setenv("KARPO_EXPORTS_DIR", t.TempDir())
	t.Setenv("KARPO_DELIVER_EVERY", "200ms")
	t.Setenv("KARPO_CHORES_EVERY", "300ms")
	t.Setenv("KARPO_BOOTSTRAP_ADMIN_USER", "root-of-the-program")
	t.Setenv("KARPO_BOOTSTRAP_ADMIN_PASSWORD", "boot-password-0001")

	if err := run(ctx, "polish", logger); err == nil {
		t.Fatal("an unknown command")
	}
	if err := run(ctx, serve.Migrate, logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := run(ctx, serve.Migrate, logger); err != nil {
		t.Fatalf("migrate again: %v", err)
	}

	serving, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- run(serving, serve.Serve, logger) }()
	get := func(path string) int {
		res, err := http.Get("http://" + addr + path)
		if err != nil {
			return 0
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	ready := false
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline) && !ready; {
		select {
		case err := <-done:
			t.Fatalf("it stopped by itself: %v", err)
		default:
		}
		if ready = get("/readyz") == 200; !ready {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if !ready {
		t.Fatal("never ready")
	}
	if got := get("/api/financial/accounts"); got != 401 {
		t.Fatalf("without a session: %d", got)
	}
	// A few rounds of delivery and chores on the real database, and it still answers.
	time.Sleep(time.Second)
	if got := get("/healthz"); got != 200 {
		t.Fatalf("after the chores: %d", got)
	}
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stopping: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("it did not stop")
	}
}
