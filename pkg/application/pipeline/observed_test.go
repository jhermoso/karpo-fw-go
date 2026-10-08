package pipeline_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	logvanilla "github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	metricsvanilla "github.com/jhermoso/karpo-fw-go/pkg/metrics/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
	tracevanilla "github.com/jhermoso/karpo-fw-go/pkg/trace/vanilla"
)

type renameParty struct {
	Name string
	fail error
}

const renamePartyType = "pipeline_test.renameParty"

func TestObserved_OutcomeFollowsTheTaxonomy(t *testing.T) {
	cases := []struct {
		err     error
		outcome string
	}{
		{nil, "ok"},
		{&domain.ValidationError{Errors: []domain.FieldError{{Field: "name", Code: "required"}}}, "validation"},
		{fmt.Errorf("%w: bad id", domain.ErrInvalidIdentity), "validation"},
		{&domain.RuleViolationError{Code: "parties.closed", Message: "closed"}, "rule"},
		{fmt.Errorf("party %w", domain.ErrNotFound), "not_found"},
		{fmt.Errorf("version: %w", domain.ErrConflict), "conflict"},
		{domain.ErrUnauthorized, "unauthorized"},
		{domain.ErrForbidden, "forbidden"},
		{authz.ErrIndeterminate, "error"},
		{errors.New("sqlrepo: connection refused"), "error"},
	}
	for _, c := range cases {
		t.Run(c.outcome, func(t *testing.T) {
			var logs bytes.Buffer
			rec := tracevanilla.NewRecorder(0)
			reg := metricsvanilla.NewRegistry()
			h := application.Chain[renameParty, string](
				application.HandlerFunc[renameParty, string](func(_ context.Context, in renameParty) (string, error) {
					return in.Name, in.fail
				}),
				pipeline.Observed[renameParty, string](logvanilla.NewJSON(&logs, log.LevelInfo), tracevanilla.New(tracevanilla.WithExporter(rec)), reg),
			)

			ctx := application.WithCorrelationID(context.Background(), "flow-7")
			_, err := h.Handle(ctx, renameParty{Name: "Ada Lovelace", fail: c.err})
			if !errors.Is(err, c.err) {
				t.Fatalf("the error must pass through untouched: %v", err)
			}

			if n := reg.HistogramCount(metrics.UseCaseDuration, "request", renamePartyType, "outcome", c.outcome); n != 1 {
				t.Fatalf("expected one observation with outcome %q: %v", c.outcome, reg.SeriesLabels(metrics.UseCaseDuration))
			}
			spans := rec.Spans()
			if len(spans) != 1 || spans[0].Name != renamePartyType || spans[0].Attr("outcome") != c.outcome {
				t.Fatalf("unexpected span: %+v", spans)
			}
			if failed := spans[0].Err != nil; failed != (c.outcome == "error") {
				t.Fatalf("only unexpected failures mark the span as failed: %v", spans[0].Err)
			}
			if strings.Contains(logs.String(), "Ada") || strings.Contains(fmt.Sprint(spans[0].Attributes), "Ada") {
				t.Fatal("the request body must never be recorded")
			}

			if c.outcome != "error" {
				if logs.Len() != 0 {
					t.Fatalf("only unexpected failures are logged at Info and above: %s", logs.String())
				}
				return
			}
			var l map[string]any
			if err := json.Unmarshal(logs.Bytes(), &l); err != nil {
				t.Fatalf("expected one JSON line: %v\n%s", err, logs.String())
			}
			if l["level"] != "ERROR" || l["request"] != renamePartyType || l["error"] != c.err.Error() ||
				l["correlation_id"] != "flow-7" || l["trace_id"] != spans[0].Context.TraceIDString() {
				t.Fatalf("unexpected line: %v", l)
			}
		})
	}
}

func TestObserved_SpanIsAChildOfTheCallerSpan(t *testing.T) {
	rec := tracevanilla.NewRecorder(0)
	tracer := tracevanilla.New(tracevanilla.WithExporter(rec))
	var inside trace.SpanContext
	h := application.Chain[renameParty, string](
		application.HandlerFunc[renameParty, string](func(ctx context.Context, _ renameParty) (string, error) {
			inside = trace.FromContext(ctx).Context()
			return "", nil
		}),
		pipeline.Observed[renameParty, string](nil, tracer, nil),
	)

	ctx, server := tracer.Start(context.Background(), "GET /parties/{id}", trace.WithKind(trace.KindServer))
	if _, err := h.Handle(ctx, renameParty{}); err != nil {
		t.Fatal(err)
	}
	server.End()

	useCase := rec.Named(renamePartyType)[0]
	if useCase.ParentID != server.Context().SpanID || useCase.Context.TraceID != server.Context().TraceID {
		t.Fatalf("the use case span must be a child of the HTTP span: %+v", useCase)
	}
	if inside != useCase.Context {
		t.Fatal("the handler must run inside the use case span")
	}
}

func TestObserved_WithoutDestinationsDoesNothing(t *testing.T) {
	h := application.Chain[renameParty, string](
		application.HandlerFunc[renameParty, string](func(_ context.Context, in renameParty) (string, error) { return in.Name, nil }),
		pipeline.Observed[renameParty, string](nil, nil, nil),
	)
	if out, err := h.Handle(context.Background(), renameParty{Name: "x"}); err != nil || out != "x" {
		t.Fatalf("unexpected result: %q, %v", out, err)
	}
}

func TestLogging_TakesTheCorrelationFromTheContext(t *testing.T) {
	var logs bytes.Buffer
	h := application.Chain[renameParty, string](
		application.HandlerFunc[renameParty, string](func(_ context.Context, in renameParty) (string, error) { return in.Name, nil }),
		pipeline.Logging[renameParty, string](logvanilla.NewJSON(&logs, log.LevelInfo)),
	)
	if _, err := h.Handle(application.WithCorrelationID(context.Background(), "flow-8"), renameParty{}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(logs.String(), `"correlation_id":"flow-8"`); n != 1 {
		t.Fatalf("the line must carry the correlation once: %s", logs.String())
	}
}
