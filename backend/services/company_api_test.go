package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"receipt-reconciliation/models"
)

func testClient(t *testing.T, baseURL string) *CompanyAPIClient {
	t.Helper()

	client := NewCompanyAPIClient()
	client.baseURL = baseURL
	// Keep the retry cadence tight so the tests stay fast.
	client.retryDelay = time.Millisecond
	client.attemptTimeout = 2 * time.Second
	client.totalTimeout = 5 * time.Second
	return client
}

func sampleOCR() models.OCRResponse {
	return models.OCRResponse{
		ReceiptID: "receipt.jpg",
		RequestID: "REQ-TEST",
		Fields:    models.ExtractedFields{InvoiceNumber: "INV-1"},
	}
}

func TestSendOCRResults_RetriesServerErrorsThenSucceeds(t *testing.T) {
	var calls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "accepted"})
	}))
	defer server.Close()

	resp, err := testClient(t, server.URL).SendOCRResults(sampleOCR())
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if !resp.Success || resp.Message != "accepted" {
		t.Fatalf("unexpected response %+v", resp)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

// A validation error is a definitive answer: retrying it wastes the deadline.
func TestSendOCRResults_DoesNotRetryClientErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(status)
				fmt.Fprint(w, `{"detail":"nope"}`)
			}))
			defer server.Close()

			resp, err := testClient(t, server.URL).SendOCRResults(sampleOCR())
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Fatalf("expected exactly 1 attempt, got %d", got)
			}
			// The caller still receives a populated response to report.
			if resp == nil || resp.Success || resp.Message == "" {
				t.Fatalf("expected a populated failure response, got %+v", resp)
			}
		})
	}
}

func TestSendOCRResults_ExhaustsAttemptsAndStillReturnsResponse(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	resp, err := client.SendOCRResults(sampleOCR())

	if err == nil {
		t.Fatal("expected an error after exhausting attempts")
	}
	if resp == nil || resp.Success {
		t.Fatalf("expected a failure response the caller can surface, got %+v", resp)
	}
	if got := atomic.LoadInt32(&calls); got != int32(client.maxRetries+1) {
		t.Fatalf("expected %d attempts, got %d", client.maxRetries+1, got)
	}
	if !strings.Contains(resp.Message, "attempt") {
		t.Fatalf("expected the message to describe the failure, got %q", resp.Message)
	}
}

// A hung endpoint must not hold the caller past its deadline.
func TestSendOCRResultsContext_RespectsCallerCancellation(t *testing.T) {
	// The handler always returns on its own so that shutting the test server down
	// can never block, whatever the client does.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer server.Close()

	client := testClient(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	resp, err := client.SendOCRResultsContext(ctx, sampleOCR())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the context expires")
	}
	if resp == nil {
		t.Fatal("expected a failure response even on cancellation")
	}
	// Without context propagation this would block until the server responded.
	if elapsed > time.Second {
		t.Fatalf("call outlived its context by too much: %s", elapsed)
	}
}

func TestSendOCRResults_RetriesRateLimiting(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "ok"})
	}))
	defer server.Close()

	resp, err := testClient(t, server.URL).SendOCRResults(sampleOCR())
	if err != nil {
		t.Fatalf("expected success after a 429, got %v", err)
	}
	if !resp.Success {
		t.Fatalf("unexpected response %+v", resp)
	}
}

func TestSendOCRResults_SendsCredentialsAndRequestID(t *testing.T) {
	var gotAuth, gotRequestID, gotContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotRequestID = r.Header.Get("X-Request-ID")
		gotContentType = r.Header.Get("Content-Type")
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	client.apiKey = "key-123"

	if _, err := client.SendOCRResults(sampleOCR()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotAuth != "Bearer key-123" {
		t.Fatalf("unexpected Authorization header %q", gotAuth)
	}
	if gotRequestID != "REQ-TEST" {
		t.Fatalf("unexpected X-Request-ID %q", gotRequestID)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected Content-Type %q", gotContentType)
	}
}

// The retry delay must grow and stay within the cap.
func TestBackoffFor_IsBoundedAndIncreasing(t *testing.T) {
	client := NewCompanyAPIClient()

	var previousUpper time.Duration
	for retry := 1; retry <= 8; retry++ {
		delay := client.backoffFor(retry)
		if delay <= 0 {
			t.Fatalf("retry %d produced a non-positive delay %s", retry, delay)
		}
		if delay > maxBackoff {
			t.Fatalf("retry %d exceeded the cap: %s > %s", retry, delay, maxBackoff)
		}
		if retry <= 4 && delay < previousUpper/4 {
			t.Fatalf("retry %d did not grow: %s after %s", retry, delay, previousUpper)
		}
		previousUpper = delay
	}
}

// An oversized body must not be read without limit.
func TestSendOCRResults_LimitsResponseSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		payload := strings.Repeat("A", 4<<20)
		fmt.Fprint(w, payload)
	}))
	defer server.Close()

	resp, err := testClient(t, server.URL).SendOCRResults(sampleOCR())
	if err == nil {
		t.Fatal("expected an error for a 400 response")
	}
	if len(resp.Message) > 4096 {
		t.Fatalf("failure message was not truncated: %d bytes", len(resp.Message))
	}
}

// The reply must be kept exactly as received: a decoding into
// CompanyAPIResponse drops unknown fields and rounds large numbers.
func TestSendOCRResults_KeepsTheReplyExactlyAsReceived(t *testing.T) {
	const body = `{ "decision":"APPROVED",  "reference_id":"CMP-77", "big_id":12345678901234567890, "amount":123625.10 }`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	resp, err := testClient(t, server.URL).SendOCRResults(sampleOCR())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reply == nil || resp.Reply.StatusCode != http.StatusOK || string(resp.Reply.Body) != body || resp.Reply.Truncated {
		t.Fatalf("expected the reply verbatim, got %+v", resp.Reply)
	}
}

func TestSendOCRResults_KeepsNon2xxRepliesAndTheirStatus(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"validation", http.StatusBadRequest, `{"error":"invalid invoice","field":"amount"}`},
		{"unauthorized", http.StatusUnauthorized, `{"error":"bad key"}`},
		{"unexpected", http.StatusConflict, "duplicate request"},
		{"server error after retries", http.StatusServiceUnavailable, "<html>maintenance</html>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			resp, err := testClient(t, server.URL).SendOCRResults(sampleOCR())
			if err == nil {
				t.Fatal("expected an error")
			}
			if resp == nil || resp.Reply == nil || resp.Reply.StatusCode != tc.status || string(resp.Reply.Body) != tc.body {
				t.Fatalf("expected status %d and body %q kept, got %+v", tc.status, tc.body, resp)
			}
		})
	}
}

func TestSendOCRResults_NoReplyWhenNothingWasReceived(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close() // connection refused

	resp, err := testClient(t, url).SendOCRResults(sampleOCR())
	if err == nil {
		t.Fatal("expected an error")
	}
	if resp == nil || resp.Reply != nil {
		t.Fatalf("expected a failure response without a reply, got %+v", resp)
	}
}

func TestSendOCRResults_MarksAnOversizedReplyAsTruncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, strings.Repeat("A", maxResponseBytes+10))
	}))
	defer server.Close()

	resp, _ := testClient(t, server.URL).SendOCRResults(sampleOCR())
	if resp.Reply == nil || !resp.Reply.Truncated || len(resp.Reply.Body) != maxResponseBytes {
		t.Fatalf("expected a truncated reply of %d bytes, got truncated=%v len=%d", maxResponseBytes, resp.Reply != nil && resp.Reply.Truncated, len(resp.Reply.Body))
	}
}

// The decoded fields are for logging and the synchronous endpoint only; the
// reply must not leak into that endpoint's JSON.
func TestCompanyAPIResponse_ReplyIsNotPartOfItsJSON(t *testing.T) {
	data, _ := json.Marshal(CompanyAPIResponse{Success: true, Reply: &RawReply{StatusCode: 200, Body: []byte("x")}})
	if strings.Contains(string(data), "Reply") || strings.Contains(string(data), "StatusCode") {
		t.Fatalf("Reply must not be serialised, got %s", data)
	}
}
