package httpserver

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

var latencyBuckets = []float64{
	0.005,
	0.01,
	0.025,
	0.05,
	0.1,
	0.25,
	0.5,
	1,
	2.5,
	5,
	10,
	30,
	60,
}

type latencyKey struct {
	method      string
	route       string
	statusClass string
}

type latencyValue struct {
	buckets []uint64
	count   uint64
	sum     float64
}

type latencyHistogram struct {
	mu     sync.Mutex
	values map[latencyKey]*latencyValue
}

func (m *RuntimeMetrics) observeLatency(
	method string,
	route string,
	status int,
	duration time.Duration,
) {
	key := latencyKey{
		method:      method,
		route:       route,
		statusClass: fmt.Sprintf("%dxx", status/100),
	}

	seconds := duration.Seconds()

	m.latency.mu.Lock()
	defer m.latency.mu.Unlock()

	if m.latency.values == nil {
		m.latency.values = make(map[latencyKey]*latencyValue)
	}

	value := m.latency.values[key]
	if value == nil {
		value = &latencyValue{
			buckets: make([]uint64, len(latencyBuckets)),
		}
		m.latency.values[key] = value
	}

	value.count++
	value.sum += seconds

	for i, upper := range latencyBuckets {
		if seconds <= upper {
			value.buckets[i]++
		}
	}
}

func (m *RuntimeMetrics) writeLatency(w io.Writer) {
	m.latency.mu.Lock()
	defer m.latency.mu.Unlock()

	_, _ = fmt.Fprintln(
		w,
		"# TYPE tktsync_http_request_duration_seconds histogram",
	)

	for key, value := range m.latency.values {
		base := `method="` + prometheusEscape(key.method) +
			`",route="` + prometheusEscape(key.route) +
			`",status_class="` + prometheusEscape(key.statusClass) + `"`

		for i, upper := range latencyBuckets {
			_, _ = fmt.Fprintf(
				w,
				"tktsync_http_request_duration_seconds_bucket{%s,le=%q} %d\n",
				base,
				strconv.FormatFloat(upper, 'g', -1, 64),
				value.buckets[i],
			)
		}

		_, _ = fmt.Fprintf(
			w,
			"tktsync_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n",
			base,
			value.count,
		)
		_, _ = fmt.Fprintf(
			w,
			"tktsync_http_request_duration_seconds_sum{%s} %.9f\n",
			base,
			value.sum,
		)
		_, _ = fmt.Fprintf(
			w,
			"tktsync_http_request_duration_seconds_count{%s} %d\n",
			base,
			value.count,
		)
	}
}

func prometheusEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}
