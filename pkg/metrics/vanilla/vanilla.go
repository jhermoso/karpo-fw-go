// Package vanilla implements metrics.Meter with the standard library only: an in-memory Registry
// (tests read from it) that also serves its content in the Prometheus text exposition format.
//
// Serve Registry.Handler on an internal port, never on the public API port.
package vanilla

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"sync"

	fwmetrics "github.com/jhermoso/karpo-fw-go/pkg/metrics"
)

// DefaultBuckets are the histogram bounds used when none are given: durations in seconds, from
// 1 ms to 10 s.
var DefaultBuckets = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type kind int

const (
	kindCounter kind = iota
	kindHistogram
	kindGauge
)

// Registry is the dependency-free metrics.Meter.
type Registry struct {
	mu       sync.Mutex
	families map[string]*family
}

var _ fwmetrics.Meter = (*Registry)(nil)

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry { return &Registry{families: map[string]*family{}} }

type family struct {
	reg        *Registry
	name, unit string
	help       string
	kind       kind
	buckets    []float64
	series     map[string]*series
}

type series struct {
	labels []string // key/value pairs
	value  float64  // counter
	counts []uint64 // histogram: one per bucket, plus +Inf
	sum    float64
	count  uint64
	read   func() float64 // gauge
}

func (r *Registry) family(name, unit, help string, k kind, buckets []float64) *family {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f, ok := r.families[name]; ok {
		if f.kind != k {
			panic(fmt.Sprintf("metrics: %q is already registered as another kind of instrument", name))
		}
		return f
	}
	f := &family{reg: r, name: name, unit: unit, help: help, kind: k, series: map[string]*series{}}
	if k == kindHistogram {
		if len(buckets) == 0 {
			buckets = DefaultBuckets
		}
		f.buckets = slices.Clone(buckets)
		slices.Sort(f.buckets)
	}
	r.families[name] = f
	return f
}

// Counter implements metrics.Meter.
func (r *Registry) Counter(name, unit, help string) fwmetrics.Counter {
	return counter{r.family(name, unit, help, kindCounter, nil)}
}

// Histogram implements metrics.Meter.
func (r *Registry) Histogram(name, unit, help string, buckets ...float64) fwmetrics.Histogram {
	return histogram{r.family(name, unit, help, kindHistogram, buckets)}
}

// Gauge implements metrics.Meter. Registering the same name and labels again replaces read.
func (r *Registry) Gauge(name, unit, help string, read func() float64, labels ...string) {
	f := r.family(name, unit, help, kindGauge, nil)
	r.mu.Lock()
	defer r.mu.Unlock()
	f.at(labels).read = read
}

// at returns the series of labels, creating it. The registry lock must be held.
func (f *family) at(labels []string) *series {
	labels = labels[:len(labels)&^1]
	key := strings.Join(labels, "\xff")
	s, ok := f.series[key]
	if !ok {
		s = &series{labels: slices.Clone(labels)}
		if f.kind == kindHistogram {
			s.counts = make([]uint64, len(f.buckets)+1)
		}
		f.series[key] = s
	}
	return s
}

type counter struct{ f *family }

func (c counter) Add(_ context.Context, n float64, labels ...string) {
	if n < 0 || math.IsNaN(n) {
		return
	}
	c.f.reg.mu.Lock()
	defer c.f.reg.mu.Unlock()
	c.f.at(labels).value += n
}

type histogram struct{ f *family }

func (h histogram) Record(_ context.Context, v float64, labels ...string) {
	if math.IsNaN(v) {
		return
	}
	h.f.reg.mu.Lock()
	defer h.f.reg.mu.Unlock()
	s := h.f.at(labels)
	i, _ := slices.BinarySearch(h.f.buckets, v) // first bound >= v
	s.counts[i]++
	s.sum += v
	s.count++
}

func (r *Registry) find(name string, k kind, labels []string) (*family, *series) {
	f, ok := r.families[name]
	if !ok || f.kind != k {
		return nil, nil
	}
	labels = labels[:len(labels)&^1]
	return f, f.series[strings.Join(labels, "\xff")]
}

// CounterValue returns the value of the counter series with exactly these labels (0 if absent).
func (r *Registry) CounterValue(name string, labels ...string) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, s := r.find(name, kindCounter, labels); s != nil {
		return s.value
	}
	return 0
}

// HistogramCount returns how many observations the histogram series with exactly these labels
// has (0 if absent).
func (r *Registry) HistogramCount(name string, labels ...string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, s := r.find(name, kindHistogram, labels); s != nil {
		return s.count
	}
	return 0
}

// HistogramTotal returns the observations of a histogram across all its series.
func (r *Registry) HistogramTotal(name string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n uint64
	if f, ok := r.families[name]; ok && f.kind == kindHistogram {
		for _, s := range f.series {
			n += s.count
		}
	}
	return n
}

// GaugeValue reads the gauge series with exactly these labels.
func (r *Registry) GaugeValue(name string, labels ...string) (float64, bool) {
	r.mu.Lock()
	_, s := r.find(name, kindGauge, labels)
	r.mu.Unlock()
	if s == nil || s.read == nil {
		return 0, false
	}
	return s.read(), true
}

// SeriesLabels returns the label sets recorded for a metric (to assert that no series carries
// an identifier).
func (r *Registry) SeriesLabels(name string) [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.families[name]
	if !ok {
		return nil
	}
	out := make([][]string, 0, len(f.series))
	for _, k := range sortedKeys(f.series) {
		out = append(out, slices.Clone(f.series[k].labels))
	}
	return out
}

// Handler serves the registry in the Prometheus text exposition format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = r.WriteText(w)
	})
}

type gaugeRead struct {
	labels []string
	read   func() float64
}

// WriteText writes the registry in the Prometheus text exposition format, sorted by name.
func (r *Registry) WriteText(w io.Writer) error {
	var sb strings.Builder
	gauges := map[string][]gaugeRead{}

	r.mu.Lock()
	names := sortedKeys(r.families)
	for _, name := range names {
		f := r.families[name]
		if f.kind == kindGauge {
			// Gauges are read outside the lock: read functions may be slow or use the registry.
			for _, k := range sortedKeys(f.series) {
				gauges[name] = append(gauges[name], gaugeRead{f.series[k].labels, f.series[k].read})
			}
		}
	}
	type header struct{ pname, help, typ string }
	headers := map[string]header{}
	bodies := map[string]string{}
	for _, name := range names {
		f := r.families[name]
		pname := promName(f.name, f.unit)
		var body strings.Builder
		switch f.kind {
		case kindCounter:
			pname += "_total"
			headers[name] = header{pname, f.help, "counter"}
			for _, k := range sortedKeys(f.series) {
				s := f.series[k]
				fmt.Fprintf(&body, "%s%s %s\n", pname, promLabels(s.labels), promFloat(s.value))
			}
		case kindHistogram:
			headers[name] = header{pname, f.help, "histogram"}
			for _, k := range sortedKeys(f.series) {
				s := f.series[k]
				var cum uint64
				for i, b := range f.buckets {
					cum += s.counts[i]
					fmt.Fprintf(&body, "%s_bucket%s %d\n", pname, promLabels(s.labels, "le", promFloat(b)), cum)
				}
				fmt.Fprintf(&body, "%s_bucket%s %d\n", pname, promLabels(s.labels, "le", "+Inf"), s.count)
				fmt.Fprintf(&body, "%s_sum%s %s\n", pname, promLabels(s.labels), promFloat(s.sum))
				fmt.Fprintf(&body, "%s_count%s %d\n", pname, promLabels(s.labels), s.count)
			}
		case kindGauge:
			headers[name] = header{pname, f.help, "gauge"}
		}
		bodies[name] = body.String()
	}
	r.mu.Unlock()

	for _, name := range names {
		h := headers[name]
		body := bodies[name]
		for _, g := range gauges[name] {
			if g.read != nil {
				body += fmt.Sprintf("%s%s %s\n", h.pname, promLabels(g.labels), promFloat(g.read()))
			}
		}
		if body == "" {
			continue
		}
		if h.help != "" {
			fmt.Fprintf(&sb, "# HELP %s %s\n", h.pname, strings.NewReplacer("\\", `\\`, "\n", `\n`).Replace(h.help))
		}
		fmt.Fprintf(&sb, "# TYPE %s %s\n", h.pname, h.typ)
		sb.WriteString(body)
	}
	_, err := io.WriteString(w, sb.String())
	return err
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// promName turns "http.server.request.duration" (unit s) into
// "http_server_request_duration_seconds".
func promName(name, unit string) string {
	n := sanitize(name)
	switch unit {
	case fwmetrics.UnitSeconds:
		n += "_seconds"
	case "By":
		n += "_bytes"
	}
	return n
}

func sanitize(s string) string {
	b := []byte(s)
	for i, c := range b {
		ok := c == '_' || c == ':' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			b[i] = '_'
		}
	}
	return string(b)
}

func promLabels(pairs []string, extra ...string) string {
	all := append(slices.Clone(pairs), extra...)
	if len(all) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteByte('{')
	for i := 0; i+1 < len(all); i += 2 {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strings.ReplaceAll(sanitize(all[i]), ":", "_"))
		sb.WriteString(`="`)
		sb.WriteString(strings.NewReplacer("\\", `\\`, "\"", `\"`, "\n", `\n`).Replace(all[i+1]))
		sb.WriteByte('"')
	}
	sb.WriteByte('}')
	return sb.String()
}

func promFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// RegisterRuntime registers the Go runtime gauges (goroutines, heap in use, GC cycles) on m.
func RegisterRuntime(m fwmetrics.Meter) {
	read := func(name string) func() float64 {
		return func() float64 {
			s := []metrics.Sample{{Name: name}}
			metrics.Read(s)
			switch s[0].Value.Kind() {
			case metrics.KindUint64:
				return float64(s[0].Value.Uint64())
			case metrics.KindFloat64:
				return s[0].Value.Float64()
			}
			return 0
		}
	}
	m.Gauge("go.goroutines", fwmetrics.UnitNone, "Live goroutines.", read("/sched/goroutines:goroutines"))
	m.Gauge("go.memory.heap", "By", "Bytes of memory occupied by live heap objects and dead ones not yet collected.", read("/memory/classes/heap/objects:bytes"))
	m.Gauge("go.gc.cycles", fwmetrics.UnitNone, "Completed garbage collection cycles.", read("/gc/cycles/total:gc-cycles"))
}
