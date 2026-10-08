package pipeline_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	logvanilla "github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
)

type loggedRename struct{}

// Logging writes the correlation id with any log.Logger, not only with those that read the
// context, and never twice.
func TestLogging_CarriesTheCorrelationWithAnyLogger(t *testing.T) {
	ctx := application.WithCorrelationID(context.Background(), "flow-42")
	run := func(logger log.Logger) {
		h := application.Chain[loggedRename, struct{}](
			application.HandlerFunc[loggedRename, struct{}](func(context.Context, loggedRename) (struct{}, error) {
				return struct{}{}, nil
			}),
			pipeline.Logging[loggedRename, struct{}](logger))
		if _, err := h.Handle(ctx, loggedRename{}); err != nil {
			t.Fatal(err)
		}
	}

	var plain bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&plain, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	run(log.Default())
	var line map[string]any
	if err := json.Unmarshal(plain.Bytes(), &line); err != nil || line["correlation_id"] != "flow-42" {
		t.Fatalf("log.Default must carry the correlation: %v %s", err, plain.String())
	}

	var enriched bytes.Buffer
	run(logvanilla.NewJSON(&enriched, log.LevelInfo))
	if n := strings.Count(enriched.String(), `"correlation_id"`); n != 1 || !strings.Contains(enriched.String(), `"flow-42"`) {
		t.Fatalf("log/vanilla must write the correlation once: %s", enriched.String())
	}
}

// log.Default hands the context to slog: a default handler that reads it (log/vanilla) enriches
// the line.
func TestDefault_PassesTheContextToSlog(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(logvanilla.NewJSON(&buf, log.LevelInfo).Slog())
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := application.WithCausationID(context.Background(), "msg-7")
	log.Default().With("relay", "outbox").WithContext(ctx).Warn("delivery failed")
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil || line["causation_id"] != "msg-7" || line["relay"] != "outbox" {
		t.Fatalf("the context must reach the default handler: %v %s", err, buf.String())
	}
}
