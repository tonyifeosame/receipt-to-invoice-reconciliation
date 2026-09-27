package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"receipt-reconciliation/metrics"
	"receipt-reconciliation/models"
	"strconv"
	"strings"
	"time"
)

const (
	// maxResponseBytes bounds how much of a response is read into memory. A
	// misbehaving or hostile endpoint could otherwise stream indefinitely.
	maxResponseBytes = 1 << 20

	// totalTimeout caps the whole call including retries. Previously four
	// attempts at a 30s timeout plus three fixed 5s sleeps could block a request
	// for over two minutes.
	defaultTotalTimeout = 45 * time.Second

	// attemptTimeout bounds a single HTTP attempt.
	defaultAttemptTimeout = 15 * time.Second

	// maxBackoff caps the exponential delay between attempts.
	maxBackoff = 8 * time.Second
)

type CompanyAPIClient struct {
	baseURL        string
	httpClient     *http.Client
	apiKey         string
	maxRetries     int
	retryDelay     time.Duration
	totalTimeout   time.Duration
	attemptTimeout time.Duration
}

type CompanyAPIResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	Timestamp string `json:"timestamp"`

	// Reply is the company API's HTTP response exactly as received. The fields
	// above are a decoding of it, used for logging and the synchronous endpoint;
	// anything that stores or shows the company's answer must use Reply, which is
	// never re-encoded and so cannot drop fields, alter numbers or invent values.
	// Nil when no HTTP response was received (network failure, timeout).
	Reply *RawReply `json:"-"`
}

// RawReply is an HTTP response from the company API as received: the most
// recent one of the call, whatever its status.
type RawReply struct {
	StatusCode int
	Body       []byte
	// Truncated reports that the body exceeded maxResponseBytes and only its
	// first maxResponseBytes bytes were kept.
	Truncated bool
}

func NewCompanyAPIClient() *CompanyAPIClient {
	baseURL := os.Getenv("COMPANY_API_URL")
	if baseURL == "" {
		baseURL = "https://api.company.example.com"
	}

	apiKey := os.Getenv("COMPANY_API_KEY")
	if apiKey == "" {
		log.Println("Warning: COMPANY_API_KEY not set, using empty key")
	}

	maxRetries := 3
	retryDelay := 500 * time.Millisecond

	// Explicit transport timeouts: the default transport will wait indefinitely
	// for a TLS handshake or response headers from a stalled peer.
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &CompanyAPIClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		apiKey:         apiKey,
		maxRetries:     maxRetries,
		retryDelay:     retryDelay,
		totalTimeout:   defaultTotalTimeout,
		attemptTimeout: defaultAttemptTimeout,
	}
}

// SendOCRResults posts OCR results to the company API using a background
// context. Prefer SendOCRResultsContext so the call is cancelled when the
// originating request goes away.
func (c *CompanyAPIClient) SendOCRResults(ocrData models.OCRResponse) (*CompanyAPIResponse, error) {
	return c.SendOCRResultsContext(context.Background(), ocrData)
}

// SendOCRResultsContext posts OCR results to the company API, retrying only
// failures that can plausibly succeed on another attempt. The whole call,
// including retries, is bounded by ctx and by the client's total timeout, and a
// failure always yields a populated CompanyAPIResponse so the caller can report
// the outcome instead of losing the OCR result.
func (c *CompanyAPIClient) SendOCRResultsContext(ctx context.Context, ocrData models.OCRResponse) (*CompanyAPIResponse, error) {
	start := time.Now()
	defer func() { metrics.CompanyAPIDuration(time.Since(start)) }()

	endpoint := fmt.Sprintf("%s/api/receipts/ocr", c.baseURL)

	jsonData, err := json.Marshal(ocrData)
	if err != nil {
		metrics.CompanyAPIResult("marshal_error")
		return nil, fmt.Errorf("failed to marshal OCR data: %w", err)
	}

	ctx, cancel := c.withTotalTimeout(ctx)
	defer cancel()

	log.Printf("Sending OCR results to company API: %s (request_id: %s)", endpoint, ocrData.RequestID)

	var lastError error
	var lastReply *RawReply
	attempts := c.maxRetries + 1

	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			delay := c.backoffFor(attempt - 1)
			log.Printf("Retrying company API attempt %d/%d for request %s in %s",
				attempt, attempts, ocrData.RequestID, delay)
			if err := sleepContext(ctx, delay); err != nil {
				metrics.CompanyAPIResult("cancelled")
				response := c.failureResponse(fmt.Sprintf("Cancelled before attempt %d: %v", attempt, lastError))
				response.Reply = lastReply
				return response, fmt.Errorf("company API call cancelled after %d attempt(s): %w", attempt-1, errOr(lastError, err))
			}
		}

		response, retryable, err := c.sendOnce(ctx, endpoint, jsonData, ocrData.RequestID)
		if err == nil {
			metrics.CompanyAPIResult("success")
			return response, nil
		}

		lastError = err
		if response != nil && response.Reply != nil {
			lastReply = response.Reply
		}

		if !retryable {
			// 400/401 and other definitive answers: retrying cannot change them.
			metrics.CompanyAPIResult("rejected")
			return response, err
		}

		metrics.CompanyAPIRetry(retryReason(err))
		log.Printf("Company API attempt %d/%d failed: %v", attempt, attempts, err)

		// Stop early when there is no time budget left for another attempt.
		if ctx.Err() != nil {
			break
		}
	}

	metrics.CompanyAPIResult("exhausted")
	message := fmt.Sprintf("Failed after %d attempt(s): %v", attempts, lastError)
	response := c.failureResponse(message)
	response.Reply = lastReply
	return response, fmt.Errorf("failed to send OCR results after %d attempt(s): %w", attempts, lastError)
}

// sendOnce performs a single attempt. The second return value reports whether
// the failure is worth retrying.
func (c *CompanyAPIClient) sendOnce(ctx context.Context, endpoint string, payload []byte, requestID string) (*CompanyAPIResponse, bool, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.attemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("X-Request-ID", requestID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network-level failures are transient often enough to be worth a retry.
		return nil, true, fmt.Errorf("failed to send request to company API: %w", err)
	}
	// Closed on every path: the previous code deferred this inside the retry
	// loop, so connections were held until the whole call returned.
	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
	}()

	// One byte past the limit shows whether the body was cut.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, true, fmt.Errorf("failed to read response body: %w", err)
	}
	reply := &RawReply{StatusCode: resp.StatusCode, Body: body}
	if len(body) > maxResponseBytes {
		reply.Body, reply.Truncated = body[:maxResponseBytes], true
	}
	body = reply.Body

	// failure returns a failure response that still carries the reply as received.
	failure := func(message string) *CompanyAPIResponse {
		response := c.failureResponse(message)
		response.Reply = reply
		return response
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		var apiResponse CompanyAPIResponse
		if err := json.Unmarshal(body, &apiResponse); err != nil {
			// A malformed body from a 200 is not going to parse next time.
			return failure("Company API returned a response that is not valid JSON"), false,
				fmt.Errorf("failed to parse company API response: %w", err)
		}
		apiResponse.Reply = reply
		log.Printf("Company API response: success=%v, message=%s", apiResponse.Success, apiResponse.Message)
		return &apiResponse, false, nil

	case resp.StatusCode == http.StatusBadRequest:
		return failure(fmt.Sprintf("Validation error: %s", truncateBody(body))), false,
			fmt.Errorf("company API returned validation error (400): %s", truncateBody(body))

	case resp.StatusCode == http.StatusUnauthorized:
		return failure("Authentication failed - invalid API credentials"), false,
			fmt.Errorf("company API returned unauthorized (401): check API credentials")

	case resp.StatusCode == http.StatusTooManyRequests:
		if wait := retryAfter(resp); wait > 0 {
			sleepContext(ctx, wait)
		}
		return failure("Rate limited (429)"), true, fmt.Errorf("company API rate limited the request (429)")

	case resp.StatusCode >= 500:
		return failure(fmt.Sprintf("Server error (%d)", resp.StatusCode)), true,
			fmt.Errorf("company API returned server error (%d): %s", resp.StatusCode, truncateBody(body))

	default:
		return failure(fmt.Sprintf("Unexpected error: %s", truncateBody(body))), false,
			fmt.Errorf("company API returned unexpected status %d: %s", resp.StatusCode, truncateBody(body))
	}
}

func (c *CompanyAPIClient) failureResponse(message string) *CompanyAPIResponse {
	return &CompanyAPIResponse{
		Success:   false,
		Message:   message,
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// withTotalTimeout bounds the call, without extending a deadline the caller has
// already set.
func (c *CompanyAPIClient) withTotalTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.totalTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < c.totalTimeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.totalTimeout)
}

// backoffFor returns an exponentially increasing, jittered delay. Fixed delays
// make every client retry in lockstep against an already struggling service.
func (c *CompanyAPIClient) backoffFor(retry int) time.Duration {
	base := c.retryDelay
	if base <= 0 {
		base = 500 * time.Millisecond
	}

	delay := base << (retry - 1)
	if delay > maxBackoff || delay <= 0 {
		delay = maxBackoff
	}

	// Full jitter over [delay/2, delay].
	half := delay / 2
	return half + time.Duration(rand.Int63n(int64(half)+1))
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryAfter(resp *http.Response) time.Duration {
	value := resp.Header.Get("Retry-After")
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		wait := time.Duration(seconds) * time.Second
		if wait > maxBackoff {
			return maxBackoff
		}
		return wait
	}
	return 0
}

func retryReason(err error) string {
	switch {
	case err == nil:
		return "unknown"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "transient"
	}
}

func errOr(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}

// truncateBody returns a short, printable excerpt of a reply for error messages.
// The excerpt ends up in job results and error_message columns, which PostgreSQL
// rejects if they contain NUL bytes or invalid UTF-8, so those are replaced. The
// exact reply is kept separately in CompanyAPIResponse.Reply.
func truncateBody(body []byte) string {
	const limit = 512
	suffix := ""
	if len(body) > limit {
		body, suffix = body[:limit], "... (truncated)"
	}
	excerpt := strings.ToValidUTF8(string(body), "�")
	return strings.ReplaceAll(excerpt, "\x00", "�") + suffix
}

func (c *CompanyAPIClient) GetReceiptStatus(receiptFile string) (*CompanyAPIResponse, error) {
	endpoint := fmt.Sprintf("%s/api/receipts/status?file=%s", c.baseURL, receiptFile)

	ctx, cancel := context.WithTimeout(context.Background(), c.attemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to company API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("company API returned error status %d: %s", resp.StatusCode, string(body))
	}

	var apiResponse CompanyAPIResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse company API response: %w", err)
	}

	return &apiResponse, nil
}
