package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func scrape(t *testing.T) string {
	t.Helper()

	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from /metrics, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("unexpected content type %q", ct)
	}
	return w.Body.String()
}

func TestHandler_ExposesRuntimeGauges(t *testing.T) {
	body := scrape(t)

	for _, want := range []string{
		"# TYPE go_goroutines gauge",
		"# TYPE process_uptime_seconds gauge",
		"# TYPE http_requests_in_flight gauge",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in exposition:\n%s", want, body)
		}
	}
}

func TestObserveHTTPRequest_IsExposed(t *testing.T) {
	ObserveHTTPRequest(http.MethodPost, "/ocr/process", 200, 250*time.Millisecond)
	ObserveHTTPRequest(http.MethodPost, "/ocr/process", 500, 2*time.Second)

	body := scrape(t)

	for _, want := range []string{
		`http_requests_total{method="POST",route="/ocr/process",status="200"} 1`,
		`http_requests_total{method="POST",route="/ocr/process",status="500"} 1`,
		"# TYPE http_request_duration_seconds histogram",
		`http_request_duration_seconds_count{method="POST",route="/ocr/process"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in exposition:\n%s", want, body)
		}
	}
}

func TestHistogram_BucketsAreCumulative(t *testing.T) {
	hist := newHistogramVec("test_duration_seconds", "help", []float64{1, 5, 10}, "route")
	hist.Observe(0.5, "/a")
	hist.Observe(7, "/a")

	var sb strings.Builder
	hist.write(&sb)
	out := sb.String()

	for _, want := range []string{
		`test_duration_seconds_bucket{route="/a",le="1"} 1`,
		`test_duration_seconds_bucket{route="/a",le="5"} 1`,
		`test_duration_seconds_bucket{route="/a",le="10"} 2`,
		`test_duration_seconds_bucket{route="/a",le="+Inf"} 2`,
		`test_duration_seconds_count{route="/a"} 2`,
		`test_duration_seconds_sum{route="/a"} 7.5`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in:\n%s", want, out)
		}
	}
}

func TestRegisterGauge_ReplacesSameName(t *testing.T) {
	RegisterGauge("test_replaceable_gauge", "help", func() float64 { return 1 })
	RegisterGauge("test_replaceable_gauge", "help", func() float64 { return 42 })

	body := scrape(t)

	if strings.Count(body, "# TYPE test_replaceable_gauge gauge") != 1 {
		t.Fatalf("gauge was registered twice:\n%s", body)
	}
	if !strings.Contains(body, "test_replaceable_gauge 42") {
		t.Fatalf("expected the replacement value:\n%s", body)
	}
}

func TestLabelValuesAreEscaped(t *testing.T) {
	counter := newCounterVec("test_escaped_total", "help", "label")
	counter.Inc(`va"lue\with` + "\n")

	var sb strings.Builder
	counter.write(&sb)

	if !strings.Contains(sb.String(), `test_escaped_total{label="va\"lue\\with\n"} 1`) {
		t.Fatalf("label value was not escaped: %s", sb.String())
	}
}

// Counters are written from request handlers and job workers concurrently.
func TestCounterVec_ConcurrentUse(t *testing.T) {
	counter := newCounterVec("test_concurrent_total", "help", "kind")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				counter.Inc("a")
			}
		}()
	}
	wg.Wait()

	var sb strings.Builder
	counter.write(&sb)

	if !strings.Contains(sb.String(), `test_concurrent_total{kind="a"} 5000`) {
		t.Fatalf("lost counter increments: %s", sb.String())
	}
}

// A label-count mismatch must be ignored rather than panicking in a live path.
func TestWrongLabelCountIsIgnored(t *testing.T) {
	counter := newCounterVec("test_mismatch_total", "help", "a", "b")
	counter.Inc("only-one")

	hist := newHistogramVec("test_mismatch_seconds", "help", []float64{1}, "a")
	hist.Observe(1)

	var sb strings.Builder
	counter.write(&sb)
	hist.write(&sb)

	if sb.Len() != 0 {
		t.Fatalf("expected no series to be recorded, got: %s", sb.String())
	}
}
