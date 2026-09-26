package pipeline_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/internal/apptest"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	rt "github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
)

type renameWidget struct {
	ID   rt.WidgetID
	Name string
	Key  string
}

func (c renameWidget) Validate() error {
	var v domain.Validation
	v.Require(!c.ID.IsZero(), "id", "required", "id is required")
	v.Require(c.Name != "", "name", "required", "name is required")
	return v.Err()
}

func (c renameWidget) IdempotencyKey() string { return c.Key }

func TestMiddleware_ChainOrderValidationTransactionAndIdempotency(t *testing.T) {
	f := apptest.New(t)
	w := f.Create(t, "Alpha")

	var trace []string
	tracer := func(name string) application.Middleware[renameWidget, string] {
		return func(next application.Handler[renameWidget, string]) application.Handler[renameWidget, string] {
			return application.HandlerFunc[renameWidget, string](func(ctx context.Context, c renameWidget) (string, error) {
				trace = append(trace, name+">")
				out, err := next.Handle(ctx, c)
				trace = append(trace, "<"+name)
				return out, err
			})
		}
	}
	calls := 0
	core := application.HandlerFunc[renameWidget, string](func(ctx context.Context, c renameWidget) (string, error) {
		calls++
		updated, err := f.Orch.Update(ctx, c.ID, func(_ context.Context, w *rt.Widget) error { return w.Rename(c.Name) })
		if err != nil {
			return "", err
		}
		return updated.Name(), nil
	})
	h := application.Chain[renameWidget, string](core,
		tracer("outer"),
		pipeline.Idempotent[renameWidget, string](memory.NewIdempotencyStore(), time.Hour),
		pipeline.Validating[renameWidget, string](),
		pipeline.RetryOnConflict[renameWidget, string](3, time.Millisecond),
		pipeline.Transactional[renameWidget, string](f.H.Store),
		tracer("inner"),
	)

	if _, err := h.Handle(context.Background(), renameWidget{}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if calls != 0 {
		t.Fatal("invalid command must not reach the handler")
	}

	out, err := h.Handle(context.Background(), renameWidget{ID: w.ID(), Name: "Beta", Key: "k1"})
	if err != nil || out != "Beta" {
		t.Fatalf("unexpected result %q %v", out, err)
	}
	out, err = h.Handle(context.Background(), renameWidget{ID: w.ID(), Name: "Beta", Key: "k1"})
	if err != nil || out != "Beta" || calls != 1 {
		t.Fatalf("idempotent replay must return the stored result without executing: calls=%d out=%q err=%v", calls, out, err)
	}
	want := []string{"outer>", "<outer", "outer>", "inner>", "<inner", "<outer", "outer>", "<outer"}
	if !slices.Equal(trace, want) {
		t.Fatalf("middleware order\nwant %v\ngot  %v", want, trace)
	}
}

func TestRetryOnConflict(t *testing.T) {
	attempts := 0
	h := application.Chain[int, int](
		application.HandlerFunc[int, int](func(context.Context, int) (int, error) {
			attempts++
			if attempts < 3 {
				return 0, domain.Conflict("x", domain.NewUUID(), 1, "stale version")
			}
			return 7, nil
		}),
		pipeline.RetryOnConflict[int, int](3, 0),
	)
	if v, err := h.Handle(context.Background(), 0); err != nil || v != 7 || attempts != 3 {
		t.Fatalf("v=%d err=%v attempts=%d", v, err, attempts)
	}
}
