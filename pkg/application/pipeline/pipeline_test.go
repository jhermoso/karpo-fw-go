package pipeline_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
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

type openWidget struct {
	Name string
	Key  string
}

func (c openWidget) IdempotencyKey() string { return c.Key }

func callerContext(t *testing.T, name string) context.Context {
	t.Helper()
	return authz.WithContext(context.Background(), &authz.Context{TenantID: "t1", Subject: domain.NewUUID(), SubjectName: name})
}

func TestIdempotent_KeyBelongsToTheCallerAndToTheRequest(t *testing.T) {
	calls := 0
	h := application.Chain[openWidget, string](
		application.HandlerFunc[openWidget, string](func(ctx context.Context, c openWidget) (string, error) {
			calls++
			caller, _ := authz.FromContext(ctx)
			return caller.SubjectName + " opened " + c.Name, nil
		}),
		pipeline.Idempotent[openWidget, string](memory.NewIdempotencyStore(), time.Hour),
	)
	ana, luis := callerContext(t, "ana"), callerContext(t, "luis")

	first, err := h.Handle(ana, openWidget{Name: "Alpha", Key: "k1"})
	if err != nil || first != "ana opened Alpha" {
		t.Fatalf("first call: %q %v", first, err)
	}
	if again, err := h.Handle(ana, openWidget{Name: "Alpha", Key: "k1"}); err != nil || again != first || calls != 1 {
		t.Fatalf("the same caller repeating the request must get the stored result: %q %v calls=%d", again, err, calls)
	}

	// Another caller with the same key is another request: it runs, and gets its own result.
	other, err := h.Handle(luis, openWidget{Name: "Alpha", Key: "k1"})
	if err != nil || other != "luis opened Alpha" || calls != 2 {
		t.Fatalf("another caller must not receive the first caller's result: %q %v calls=%d", other, err, calls)
	}

	// The same caller reusing the key for a different request is told so, and nothing runs.
	_, err = h.Handle(ana, openWidget{Name: "Beta", Key: "k1"})
	var rv *domain.RuleViolationError
	if !errors.As(err, &rv) || rv.Code != pipeline.CodeIdempotencyKeyReused || calls != 2 {
		t.Fatalf("a key reused with a different request must be rejected: %v calls=%d", err, calls)
	}
}
