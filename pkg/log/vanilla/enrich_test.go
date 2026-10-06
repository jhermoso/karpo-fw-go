package vanilla

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/observability"
	"github.com/jhermoso/karpo-fw-go/pkg/observability/inprocess"
)

func decode(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func TestEnrich_StampsCorrelationAndTrace(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSON(&buf, log.LevelInfo)
	ctx := application.WithCausationID(application.WithCorrelationID(context.Background(), "corr-1"), "msg-9")
	ctx, span := inprocess.NewTracer(0).Start(ctx, "op")

	logger.WithContext(ctx).Info("with context")
	logger.Info("without context")
	logger.WithContext(ctx).Info("explicit wins", "correlation_id", "explicit")
	logger.With("trace_id", "from-with").WithContext(ctx).Info("with wins")

	lines := decode(t, &buf)
	first := lines[0]
	if first[observability.FieldCorrelationID] != "corr-1" || first[observability.FieldCausationID] != "msg-9" ||
		first[observability.FieldTraceID] != span.Context().TraceID.String() || first[observability.FieldSpanID] != span.Context().SpanID.String() {
		t.Fatalf("enriched line: %v", first)
	}
	if _, ok := lines[1][observability.FieldCorrelationID]; ok {
		t.Fatalf("no context, no correlation: %v", lines[1])
	}
	if lines[2][observability.FieldCorrelationID] != "explicit" || strings.Count(strings.Split(buf.String(), "\n")[2], `"correlation_id"`) != 1 {
		t.Fatalf("duplicate key: %s", strings.Split(buf.String(), "\n")[2])
	}
	if lines[3][observability.FieldTraceID] != "from-with" || strings.Count(strings.Split(buf.String(), "\n")[3], `"trace_id"`) != 1 {
		t.Fatalf("duplicate key from With: %s", strings.Split(buf.String(), "\n")[3])
	}
}

func TestFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	var buf bytes.Buffer
	fromEnv(&buf, env(nil), "parties").Info("json by default")
	fromEnv(&buf, env(nil), "parties").Debug("filtered at info")
	line := decode(t, &buf)
	if len(line) != 1 || line[0][observability.FieldService] != "parties" || line[0]["level"] != "INFO" {
		t.Fatalf("default: %v", line)
	}

	buf.Reset()
	fromEnv(&buf, env(map[string]string{EnvLogFormat: "TEXT", EnvLogLevel: "debug"}), "").Debug("text")
	if !strings.HasPrefix(buf.String(), "time=") || !strings.Contains(buf.String(), "level=DEBUG") {
		t.Fatalf("text: %q", buf.String())
	}
	if ParseLevel("Warning") != log.LevelWarn || ParseLevel("error") != log.LevelError || ParseLevel("nonsense") != log.LevelInfo {
		t.Fatal("ParseLevel")
	}
}

func TestDefault_UsesSlogDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	l := log.Default() // created before SetDefault: still resolved at call time
	slog.SetDefault(NewJSON(&buf, log.LevelInfo).Slog())
	l.With("component", "relay").WithContext(application.WithCorrelationID(context.Background(), "c-2")).Warn("hello", "n", 1)
	line := decode(t, &buf)[0]
	if line["msg"] != "hello" || line["component"] != "relay" || line["level"] != "WARN" || line[observability.FieldCorrelationID] != "c-2" {
		t.Fatalf("default logger: %v", line)
	}
}
