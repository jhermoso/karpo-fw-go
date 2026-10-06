// Package observability is the telemetry contract of the framework: traces (Tracer, Span) and
// metrics (Meter, Counter, Histogram), plus the field and metric names shared with the C#
// framework so that both read the same.
//
// It depends on the standard library only. Implementations live in their own packages, one per
// technology (observability/inprocess, observability/prometheus; an OpenTelemetry adapter would
// live in a separate module so the root go.mod stays free of it). Without an implementation the
// Noop values are used and the framework behaves as if there were no telemetry at all.
package observability

import (
	"context"
	"strconv"
)

// Attr is an attribute of a span or a metric point. Values are strings so that metric labels and
// span attributes share one representation; use Int for numbers.
type Attr struct {
	Key   string
	Value string
}

// String builds a string attribute.
func String(key, value string) Attr { return Attr{Key: key, Value: value} }

// Int builds an integer attribute.
func Int(key string, value int) Attr { return Attr{Key: key, Value: strconv.Itoa(value)} }

// Telemetry groups the tracer and the meter a component reports to. Its zero value is valid and
// reports nothing.
type Telemetry struct {
	Tracer Tracer
	Meter  Meter
}

// Noop returns a Telemetry that reports nothing.
func Noop() Telemetry { return Telemetry{Tracer: NoopTracer(), Meter: NoopMeter()} }

// T returns the tracer, or the no-op tracer when none is configured.
func (t Telemetry) T() Tracer {
	if t.Tracer == nil {
		return NoopTracer()
	}
	return t.Tracer
}

// M returns the meter, or the no-op meter when none is configured.
func (t Telemetry) M() Meter {
	if t.Meter == nil {
		return NoopMeter()
	}
	return t.Meter
}

// Meter creates metric instruments. Asking twice for the same name returns an instrument that
// reports to the same series.
type Meter interface {
	// Counter creates a monotonic counter (unit as in UCUM: "1", "s", "By"...).
	Counter(name, unit, description string) Counter
	// Histogram creates a histogram with the given bucket upper bounds (DurationBuckets when empty).
	Histogram(name, unit, description string, buckets ...float64) Histogram
}

// Counter is a monotonic sum.
type Counter interface {
	Add(ctx context.Context, value float64, attrs ...Attr)
}

// Histogram records a distribution of values.
type Histogram interface {
	Record(ctx context.Context, value float64, attrs ...Attr)
}

// NoopMeter returns a meter whose instruments discard every value.
func NoopMeter() Meter { return noopMeter{} }

type noopMeter struct{}

func (noopMeter) Counter(string, string, string) Counter                 { return noopInstrument{} }
func (noopMeter) Histogram(string, string, string, ...float64) Histogram { return noopInstrument{} }

type noopInstrument struct{}

func (noopInstrument) Add(context.Context, float64, ...Attr)    {}
func (noopInstrument) Record(context.Context, float64, ...Attr) {}
