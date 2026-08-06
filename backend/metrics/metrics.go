// Package metrics implements a dependency-free Prometheus exposition endpoint.
//
// monitoring/prometheus.yml scrapes the backend on the default /metrics path,
// which previously did not exist, so the target was permanently down. The metric
// types implemented here are the subset the service needs: labelled counters, a
// latency histogram, and gauges sampled at scrape time.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const contentType = "text/plain; version=0.0.4; charset=utf-8"

var processStart = time.Now()

// ---------------------------------------------------------------------------
// counter vectors
// ---------------------------------------------------------------------------

type counterVec struct {
	name   string
	help   string
	labels []string

	mu     sync.RWMutex
	series map[string]*counterSeries
}

type counterSeries struct {
	labelValues []string
	value       uint64
}

func newCounterVec(name, help string, labels ...string) *counterVec {
	return &counterVec{
		name:   name,
		help:   help,
		labels: labels,
		series: make(map[string]*counterSeries),
	}
}

func (c *counterVec) Add(delta uint64, labelValues ...string) {
	if len(labelValues) != len(c.labels) {
		return
	}

	key := strings.Join(labelValues, "\x00")

	c.mu.RLock()
	series, ok := c.series[key]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		if series, ok = c.series[key]; !ok {
			series = &counterSeries{labelValues: append([]string(nil), labelValues...)}
			c.series[key] = series
		}
		c.mu.Unlock()
	}

	atomic.AddUint64(&series.value, delta)
}

func (c *counterVec) Inc(labelValues ...string) { c.Add(1, labelValues...) }

func (c *counterVec) write(w io.Writer) {
	c.mu.RLock()
	keys := make([]string, 0, len(c.series))
	for key := range c.series {
		keys = append(keys, key)
	}
	series := make(map[string]*counterSeries, len(c.series))
	for key, value := range c.series {
		series[key] = value
	}
	c.mu.RUnlock()

	if len(keys) == 0 {
		return
	}
	sort.Strings(keys)

	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
	for _, key := range keys {
		item := series[key]
		fmt.Fprintf(w, "%s%s %d\n", c.name, formatLabels(c.labels, item.labelValues), atomic.LoadUint64(&item.value))
	}
}

// ---------------------------------------------------------------------------
// histogram vectors
// ---------------------------------------------------------------------------

type histogramVec struct {
	name    string
	help    string
	labels  []string
	buckets []float64

	mu     sync.Mutex
	series map[string]*histogramSeries
}

type histogramSeries struct {
	labelValues []string
	counts      []uint64
	sum         float64
	count       uint64
}

func newHistogramVec(name, help string, buckets []float64, labels ...string) *histogramVec {
	return &histogramVec{
		name:    name,
		help:    help,
		labels:  labels,
		buckets: buckets,
		series:  make(map[string]*histogramSeries),
	}
}

func (h *histogramVec) Observe(value float64, labelValues ...string) {
	if len(labelValues) != len(h.labels) {
		return
	}

	key := strings.Join(labelValues, "\x00")

	h.mu.Lock()
	defer h.mu.Unlock()

	series, ok := h.series[key]
	if !ok {
		series = &histogramSeries{
			labelValues: append([]string(nil), labelValues...),
			counts:      make([]uint64, len(h.buckets)),
		}
		h.series[key] = series
	}

	for i, upper := range h.buckets {
		if value <= upper {
			series.counts[i]++
		}
	}
	series.sum += value
	series.count++
}

func (h *histogramVec) write(w io.Writer) {
	h.mu.Lock()
	keys := make([]string, 0, len(h.series))
	for key := range h.series {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	type snapshot struct {
		labelValues []string
		counts      []uint64
		sum         float64
		count       uint64
	}
	snapshots := make([]snapshot, 0, len(keys))
	for _, key := range keys {
		series := h.series[key]
		snapshots = append(snapshots, snapshot{
			labelValues: series.labelValues,
			counts:      append([]uint64(nil), series.counts...),
			sum:         series.sum,
			count:       series.count,
		})
	}
	h.mu.Unlock()

	if len(snapshots) == 0 {
		return
	}

	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
	for _, item := range snapshots {
		for i, upper := range h.buckets {
			labels := appendLabel(h.labels, item.labelValues, "le", strconv.FormatFloat(upper, 'g', -1, 64))
			fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, labels, item.counts[i])
		}
		infLabels := appendLabel(h.labels, item.labelValues, "le", "+Inf")
		fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, infLabels, item.count)
		fmt.Fprintf(w, "%s_sum%s %g\n", h.name, formatLabels(h.labels, item.labelValues), item.sum)
		fmt.Fprintf(w, "%s_count%s %d\n", h.name, formatLabels(h.labels, item.labelValues), item.count)
	}
}

// ---------------------------------------------------------------------------
// gauges sampled at scrape time
// ---------------------------------------------------------------------------

type gauge struct {
	name string
	help string
	fn   func() float64
}

var (
	gaugesMu sync.RWMutex
	gauges   []gauge
)

// RegisterGauge publishes a value that is sampled each time /metrics is scraped.
// Registering the same name twice replaces the previous sampler.
func RegisterGauge(name, help string, fn func() float64) {
	gaugesMu.Lock()
	defer gaugesMu.Unlock()

	for i := range gauges {
		if gauges[i].name == name {
			gauges[i] = gauge{name: name, help: help, fn: fn}
			return
		}
	}
	gauges = append(gauges, gauge{name: name, help: help, fn: fn})
}

// ---------------------------------------------------------------------------
// metric definitions
// ---------------------------------------------------------------------------

var (
	httpRequests = newCounterVec(
		"http_requests_total",
		"Total HTTP requests handled, by method, matched route and status class.",
		"method", "route", "status")

	httpDuration = newHistogramVec(
		"http_request_duration_seconds",
		"HTTP request latency in seconds.",
		[]float64{0.005, 0.025, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		"method", "route")

	jobsClaimed = newCounterVec("jobs_claimed_total", "Jobs claimed by a worker.", "type")
	jobsDone    = newCounterVec("jobs_completed_total", "Jobs that completed successfully.", "type")
	jobsFailed  = newCounterVec("jobs_failed_total", "Jobs that exhausted their attempts.", "type")
	jobsRetried = newCounterVec("jobs_retried_total", "Jobs returned to the queue for another attempt.", "type")
	jobsPanics  = newCounterVec("job_handler_panics_total", "Panics recovered inside job handlers.", "type")
	jobErrors   = newCounterVec("job_queue_errors_total", "Job queue infrastructure errors.", "operation")
	jobsReaped  = newCounterVec("jobs_reaped_total", "Stale in-flight jobs recovered after a crash.", "outcome")

	companyRequests = newCounterVec(
		"company_api_requests_total",
		"Calls to the external company API, by outcome.",
		"outcome")
	companyRetries = newCounterVec("company_api_retries_total", "Retried company API attempts.", "reason")
	companyLatency = newHistogramVec(
		"company_api_request_duration_seconds",
		"Total wall-clock time of a company API call including retries.",
		[]float64{0.1, 0.5, 1, 2.5, 5, 10, 20, 30, 45, 60})

	httpInFlight int64
)

// HTTP instrumentation.
func ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	httpRequests.Inc(method, route, strconv.Itoa(status))
	httpDuration.Observe(duration.Seconds(), method, route)
}

func IncHTTPInFlight() { atomic.AddInt64(&httpInFlight, 1) }
func DecHTTPInFlight() { atomic.AddInt64(&httpInFlight, -1) }

// Job queue instrumentation.
func JobClaimed(jobType string)          { jobsClaimed.Inc(jobType) }
func JobCompleted(jobType string)        { jobsDone.Inc(jobType) }
func JobFailed(jobType string)           { jobsFailed.Inc(jobType) }
func JobRetried(jobType string)          { jobsRetried.Inc(jobType) }
func JobPanic(jobType string)            { jobsPanics.Inc(jobType) }
func JobQueueError(operation string)     { jobErrors.Inc(operation) }
func JobReaped(outcome string, n uint64) { jobsReaped.Add(n, outcome) }

// Company API instrumentation.
func CompanyAPIResult(outcome string)           { companyRequests.Inc(outcome) }
func CompanyAPIRetry(reason string)             { companyRetries.Inc(reason) }
func CompanyAPIDuration(duration time.Duration) { companyLatency.Observe(duration.Seconds()) }

// ---------------------------------------------------------------------------
// exposition
// ---------------------------------------------------------------------------

// Handler renders the current metric values in the Prometheus text format.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf strings.Builder

		httpRequests.write(&buf)
		httpDuration.write(&buf)
		jobsClaimed.write(&buf)
		jobsDone.write(&buf)
		jobsFailed.write(&buf)
		jobsRetried.write(&buf)
		jobsPanics.write(&buf)
		jobErrors.write(&buf)
		jobsReaped.write(&buf)
		companyRequests.write(&buf)
		companyRetries.write(&buf)
		companyLatency.write(&buf)

		writeGauge(&buf, "http_requests_in_flight", "HTTP requests currently being served.",
			float64(atomic.LoadInt64(&httpInFlight)))

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		writeGauge(&buf, "go_goroutines", "Number of goroutines that currently exist.", float64(runtime.NumGoroutine()))
		writeGauge(&buf, "go_memstats_alloc_bytes", "Bytes allocated and still in use.", float64(mem.Alloc))
		writeGauge(&buf, "go_memstats_heap_objects", "Number of allocated objects.", float64(mem.HeapObjects))
		writeGauge(&buf, "process_uptime_seconds", "Seconds since the process started.", time.Since(processStart).Seconds())

		gaugesMu.RLock()
		registered := append([]gauge(nil), gauges...)
		gaugesMu.RUnlock()

		sort.Slice(registered, func(i, j int) bool { return registered[i].name < registered[j].name })
		for _, g := range registered {
			writeGauge(&buf, g.name, g.help, g.fn())
		}

		w.Header().Set("Content-Type", contentType)
		io.WriteString(w, buf.String())
	})
}

func writeGauge(w io.Writer, name, help string, value float64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %g\n", name, help, name, name, value)
}

func formatLabels(names, values []string) string {
	if len(names) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		value := ""
		if i < len(values) {
			value = values[i]
		}
		fmt.Fprintf(&b, "%s=\"%s\"", name, escapeLabelValue(value))
	}
	b.WriteByte('}')
	return b.String()
}

func appendLabel(names, values []string, extraName, extraValue string) string {
	return formatLabels(append(append([]string(nil), names...), extraName),
		append(append([]string(nil), values...), extraValue))
}

func escapeLabelValue(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return replacer.Replace(value)
}
