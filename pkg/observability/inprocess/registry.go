package inprocess

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/observability"
)

// Kind tells counters from histograms in a snapshot.
type Kind int

const (
	KindCounter Kind = iota
	KindHistogram
)

// Metric is the description of an instrument.
type Metric struct {
	Name        string
	Unit        string
	Description string
	Kind        Kind
	Buckets     []float64 // histograms only: upper bounds, ascending
}

// Series is the current value of one combination of attributes.
type Series struct {
	Attrs []observability.Attr // sorted by key
	// Counter: Value is the sum. Histogram: Count, Sum and Counts (per bucket, not cumulative;
	// the last element counts the values above the last bound).
	Value  float64
	Count  uint64
	Sum    float64
	Counts []uint64
}

// Attr returns the value of the attribute key, or "".
func (s Series) Attr(key string) string {
	for _, a := range s.Attrs {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}

// MetricSnapshot is a metric and its series.
type MetricSnapshot struct {
	Metric
	Series []Series
}

// Registry is an observability.Meter that keeps every series in memory.
type Registry struct {
	mu      sync.Mutex
	metrics map[string]*metric
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{metrics: map[string]*metric{}} }

var _ observability.Meter = (*Registry)(nil)

type metric struct {
	Metric
	series map[string]*Series
}

// Counter implements observability.Meter.
func (r *Registry) Counter(name, unit, description string) observability.Counter {
	return instrument{r.metric(Metric{Name: name, Unit: unit, Description: description, Kind: KindCounter}), r}
}

// Histogram implements observability.Meter.
func (r *Registry) Histogram(name, unit, description string, buckets ...float64) observability.Histogram {
	if len(buckets) == 0 {
		buckets = observability.DurationBuckets
	}
	b := slices.Clone(buckets)
	sort.Float64s(b)
	return instrument{r.metric(Metric{Name: name, Unit: unit, Description: description, Kind: KindHistogram, Buckets: b}), r}
}

func (r *Registry) metric(m Metric) *metric {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.metrics[m.Name]; ok {
		return existing // first registration wins: same name, same series
	}
	nm := &metric{Metric: m, series: map[string]*Series{}}
	r.metrics[m.Name] = nm
	return nm
}

type instrument struct {
	m *metric
	r *Registry
}

func (i instrument) Add(_ context.Context, v float64, attrs ...observability.Attr) {
	if v < 0 {
		return // counters are monotonic
	}
	i.r.mu.Lock()
	defer i.r.mu.Unlock()
	i.series(attrs).Value += v
}

func (i instrument) Record(_ context.Context, v float64, attrs ...observability.Attr) {
	i.r.mu.Lock()
	defer i.r.mu.Unlock()
	s := i.series(attrs)
	if i.m.Kind != KindHistogram {
		s.Value += v
		return
	}
	s.Count++
	s.Sum += v
	idx, _ := slices.BinarySearch(i.m.Buckets, v) // first bound >= v
	s.Counts[idx]++
}

// series returns the series of attrs; the caller holds the registry lock.
func (i instrument) series(attrs []observability.Attr) *Series {
	sorted := slices.Clone(attrs)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Key < sorted[b].Key })
	var key strings.Builder
	for _, a := range sorted {
		key.WriteString(a.Key)
		key.WriteByte(0)
		key.WriteString(a.Value)
		key.WriteByte(0)
	}
	s, ok := i.m.series[key.String()]
	if !ok {
		s = &Series{Attrs: sorted}
		if i.m.Kind == KindHistogram {
			s.Counts = make([]uint64, len(i.m.Buckets)+1)
		}
		i.m.series[key.String()] = s
	}
	return s
}

// Snapshot returns a copy of every metric, sorted by name, with its series sorted by attributes.
func (r *Registry) Snapshot() []MetricSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]MetricSnapshot, 0, len(r.metrics))
	for _, m := range r.metrics {
		ms := MetricSnapshot{Metric: m.Metric}
		keys := make([]string, 0, len(m.series))
		for k := range m.series {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s := *m.series[k]
			s.Attrs = slices.Clone(s.Attrs)
			s.Counts = slices.Clone(s.Counts)
			ms.Series = append(ms.Series, s)
		}
		out = append(out, ms)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// Find returns the snapshot of the metric name.
func (r *Registry) Find(name string) (MetricSnapshot, bool) {
	for _, m := range r.Snapshot() {
		if m.Name == name {
			return m, true
		}
	}
	return MetricSnapshot{}, false
}
