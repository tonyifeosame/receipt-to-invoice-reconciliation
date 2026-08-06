package middleware

import (
	"net/http"
	"receipt-reconciliation/metrics"
	"time"

	"github.com/gorilla/mux"
)

func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Logging records request counts and latency so that /metrics reflects real
// traffic. It reports the matched route template rather than the raw path to
// keep label cardinality bounded.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		metrics.IncHTTPInFlight()
		defer func() {
			metrics.DecHTTPInFlight()
			metrics.ObserveHTTPRequest(r.Method, routeTemplate(r), recorder.status, time.Since(start))
		}()

		next.ServeHTTP(recorder, r)
	})
}

// routeTemplate returns the registered path template for the request, e.g.
// "/ocr/process". Unmatched requests share a single label value.
func routeTemplate(r *http.Request) string {
	route := mux.CurrentRoute(r)
	if route == nil {
		return "unmatched"
	}
	template, err := route.GetPathTemplate()
	if err != nil || template == "" {
		return "unmatched"
	}
	return template
}

// statusRecorder captures the response status for metrics without altering the
// response itself.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(status int) {
	if !s.wroteHeader {
		s.status = status
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}

// Flush keeps streaming responses (e.g. the static file server) working.
func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
