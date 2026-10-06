// Package prometheus exposes an inprocess.Registry in the Prometheus text exposition format
// (version 0.0.4), with the standard library only, so that a service can offer GET /metrics
// without a collector and without third-party code.
//
// Names are translated as OpenTelemetry does when it exports to Prometheus: dots become
// underscores, the unit is appended (seconds, bytes...) and counters get the _total suffix.
// So http.server.request.duration becomes http_server_request_duration_seconds.
package prometheus

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/observability"
	"github.com/jhermoso/karpo-fw-go/pkg/observability/inprocess"
)

// ContentType is the media type of the text exposition format.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// Handler serves the metrics of reg.
func Handler(reg *inprocess.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ContentType)
		_ = Write(w, reg.Snapshot())
	})
}

// Write renders metrics in the text exposition format.
func Write(w io.Writer, metrics []inprocess.MetricSnapshot) error {
	var b strings.Builder
	for _, m := range metrics {
		name := Name(m.Metric)
		typ := "counter"
		if m.Kind == inprocess.KindHistogram {
			typ = "histogram"
		}
		if m.Description != "" {
			fmt.Fprintf(&b, "# HELP %s %s\n", name, escapeHelp(m.Description))
		}
		fmt.Fprintf(&b, "# TYPE %s %s\n", name, typ)
		for _, s := range m.Series {
			if m.Kind == inprocess.KindCounter {
				fmt.Fprintf(&b, "%s%s %s\n", name, labels(s.Attrs, ""), num(s.Value))
				continue
			}
			var cum uint64
			for i, bound := range m.Buckets {
				cum += s.Counts[i]
				fmt.Fprintf(&b, "%s_bucket%s %d\n", name, labels(s.Attrs, num(bound)), cum)
			}
			cum += s.Counts[len(m.Buckets)]
			fmt.Fprintf(&b, "%s_bucket%s %d\n", name, labels(s.Attrs, "+Inf"), cum)
			fmt.Fprintf(&b, "%s_sum%s %s\n", name, labels(s.Attrs, ""), num(s.Sum))
			fmt.Fprintf(&b, "%s_count%s %d\n", name, labels(s.Attrs, ""), s.Count)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

var units = map[string]string{"s": "seconds", "ms": "milliseconds", "By": "bytes", "1": ""}

// Name is the Prometheus name of a metric.
func Name(m inprocess.Metric) string {
	n := sanitize(m.Name)
	if u, ok := units[m.Unit]; ok && u != "" && !strings.HasSuffix(n, "_"+u) {
		n += "_" + u
	}
	if m.Kind == inprocess.KindCounter && !strings.HasSuffix(n, "_total") {
		n += "_total"
	}
	return n
}

func sanitize(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == ':', r >= '0' && r <= '9' && i > 0:
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func labels(attrs []observability.Attr, le string) string {
	if len(attrs) == 0 && le == "" {
		return ""
	}
	parts := make([]string, 0, len(attrs)+1)
	for _, a := range attrs {
		parts = append(parts, sanitize(a.Key)+`="`+escapeLabel(a.Value)+`"`)
	}
	if le != "" {
		parts = append(parts, `le="`+le+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func num(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
var helpEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`)

func escapeLabel(s string) string { return labelEscaper.Replace(s) }
func escapeHelp(s string) string  { return helpEscaper.Replace(s) }
