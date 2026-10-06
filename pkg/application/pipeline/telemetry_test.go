package pipeline_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	"github.com/jhermoso/karpo-fw-go/pkg/observability"
	"github.com/jhermoso/karpo-fw-go/pkg/observability/inprocess"
)

type getThing struct{ Fail bool }

func TestTelemetry_SpanAndDurationByUseCaseAndOutcome(t *testing.T) {
	tr, reg := inprocess.NewTracer(8), inprocess.NewRegistry()
	h := application.Chain[getThing, string](application.HandlerFunc[getThing, string](func(_ context.Context, in getThing) (string, error) {
		if in.Fail {
			return "", errors.New("store down")
		}
		return "ok", nil
	}), pipeline.Telemetry[getThing, string](observability.Telemetry{Tracer: tr, Meter: reg}))

	ctx, parent := tr.Start(context.Background(), "GET /things")
	_, _ = h.Handle(ctx, getThing{})
	_, _ = h.Handle(ctx, getThing{Fail: true})
	parent.End()

	spans := tr.Finished()
	if len(spans) != 3 || spans[0].Name != "pipeline_test.getThing" || spans[0].Parent != parent.Context() ||
		spans[1].Status != observability.StatusError || spans[1].Errors[0] != "store down" {
		t.Fatalf("spans: %+v", spans)
	}
	m, _ := reg.Find(observability.MetricUseCaseDuration)
	if len(m.Series) != 2 || m.Series[0].Attr(observability.AttrOutcome) != observability.OutcomeError ||
		m.Series[1].Attr(observability.AttrOutcome) != observability.OutcomeOK || m.Series[1].Attr(observability.AttrUseCase) != "pipeline_test.getThing" {
		t.Fatalf("metric: %+v", m)
	}

	// The zero Telemetry is a no-op and changes nothing.
	plain := application.Chain[getThing, string](application.HandlerFunc[getThing, string](func(context.Context, getThing) (string, error) {
		return "ok", nil
	}), pipeline.Telemetry[getThing, string](observability.Telemetry{}))
	if out, err := plain.Handle(context.Background(), getThing{}); out != "ok" || err != nil {
		t.Fatal(out, err)
	}
}
