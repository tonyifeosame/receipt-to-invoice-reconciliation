package jobs

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"receipt-reconciliation/models"
	"receipt-reconciliation/repository"
	"receipt-reconciliation/services"
)

// The OCR_PROCESS path as it runs in production: a job enqueued in PostgreSQL,
// claimed by the queue's own SQL, dispatched to ProcessOCRJob, and its status and
// result written back. OCR is a fake OCRService; the company API is the real
// CompanyAPIClient talking to a fake HTTP server. Skipped unless
// JOBS_INTEGRATION_DB=1, like the other queue integration tests.

// fakeOCR stands in for the C++ engine and records the files it was given.
type fakeOCR struct {
	mu       sync.Mutex
	response models.OCRResponse
	err      error
	files    []string
}

func (f *fakeOCR) ProcessFile(path string) (models.OCRResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files = append(f.files, path)
	return f.response, f.err
}

func (f *fakeOCR) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.files...)
}

// A low-confidence extraction with every field set: nothing may be dropped.
func lowConfidenceOCR() models.OCRResponse {
	return models.OCRResponse{
		ReceiptID: "engine-receipt-id",
		RequestID: "REQ-FROM-ENGINE",
		OCR:       models.OCRMetadata{Confidence: 41.5, Engine: "Tesseract", ProcessingTimeMs: 380},
		Fields: models.ExtractedFields{
			InvoiceNumber: "INV-5821", Amount: 123625, Date: "2026-07-15",
			Customer: "Blue Ocean Ltd", Reference: "TRX449120",
			PhoneNumber: "0803-123-4567", Email: "pay@blueocean.example",
		},
		RawText:   "Invoice No: INV-5821\nTOTAL: 123,625.00",
		ImageName: "engine-image-name",
	}
}

type companyRequest struct {
	requestID string
	auth      string
	body      models.OCRResponse
}

// fakeCompany answers each request with the next status/body in replies; the
// last one repeats.
type fakeCompany struct {
	mu       sync.Mutex
	replies  []fakeReply
	requests []companyRequest
}

type fakeReply struct {
	status int
	body   string
}

func (f *fakeCompany) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body models.OCRResponse
	_ = json.Unmarshal(raw, &body)

	f.mu.Lock()
	f.requests = append(f.requests, companyRequest{r.Header.Get("X-Request-ID"), r.Header.Get("Authorization"), body})
	reply := f.replies[len(f.replies)-1]
	if len(f.requests) <= len(f.replies) {
		reply = f.replies[len(f.requests)-1]
	}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(reply.status)
	_, _ = io.WriteString(w, reply.body)
}

func (f *fakeCompany) received() []companyRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]companyRequest(nil), f.requests...)
}

func (f *fakeCompany) setReplies(replies ...fakeReply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = replies
	f.requests = nil
}

const (
	acceptedReply = `{"success":true,"message":"received","data":{"decision":"APPROVED","matched_invoice":"INV-5821"},"timestamp":"2026-09-27T10:00:00Z"}`
	declinedReply = `{"success":false,"message":"no matching invoice","data":{"decision":"REJECTED"},"timestamp":"2026-09-27T10:00:00Z"}`
)

type ocrJobFixture struct {
	db      *repository.Database
	queue   *JobQueue
	ocr     *fakeOCR
	company *fakeCompany
	marker  string
}

func newOCRJobFixture(t *testing.T, ocr *fakeOCR, replies ...fakeReply) *ocrJobFixture {
	t.Helper()
	db := integrationDB(t)
	requireResultColumn(t, db)

	company := &fakeCompany{replies: replies}
	server := httptest.NewServer(company)
	t.Cleanup(server.Close)
	t.Setenv("COMPANY_API_URL", server.URL)
	t.Setenv("COMPANY_API_KEY", "test-company-key")

	queue := NewJobQueue(db, 1, ocr, services.NewCompanyAPIClient())
	queue.RegisterHandler(JobOCRProcess, ProcessOCRJob) // as main.go registers it

	marker := fmt.Sprintf("ocr-job-%s-%d", t.Name(), time.Now().UnixNano())
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM job_queue WHERE payload->>'test_marker' = $1`, marker); err != nil {
			t.Logf("cleanup failed for marker %s: %v", marker, err)
		}
	})

	return &ocrJobFixture{db: db, queue: queue, ocr: ocr, company: company, marker: marker}
}

func requireResultColumn(t *testing.T, db *repository.Database) {
	t.Helper()
	var exists bool
	err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
	                     WHERE table_name = 'job_queue' AND column_name = 'result')`).Scan(&exists)
	if err != nil || !exists {
		t.Fatalf("job_queue.result is missing: apply database/migrations/003_job_results.sql (err=%v)", err)
	}
}

// enqueue adds an OCR job exactly as the /ocr/process handler does.
func (f *ocrJobFixture) enqueue(t *testing.T, payload map[string]interface{}) int {
	t.Helper()
	payload["test_marker"] = f.marker
	id, err := f.queue.Enqueue(JobOCRProcess, payload, 1)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	return id
}

// attempt lets the queue claim its next job, which must be jobID, and process it.
func (f *ocrJobFixture) attempt(t *testing.T, jobID int) {
	t.Helper()
	job, err := f.queue.fetchNextJob()
	if err != nil || job == nil {
		t.Fatalf("expected to claim job %d, got job=%v err=%v", jobID, job, err)
	}
	if job.ID != jobID {
		t.Fatalf("claimed job %d, expected %d (is the test database shared?)", job.ID, jobID)
	}
	f.queue.processJob(job, 0)
}

type jobRow struct {
	status    JobStatus
	attempts  int
	errorMsg  string
	result    *OCRJobResult
	rawResult string // job_queue.result as the database returns it
}

// assertCompanyReply checks that the job holds the company API's reply exactly:
// the HTTP status and the body, byte for byte.
func assertCompanyReply(t *testing.T, row jobRow, status int, body string) {
	t.Helper()
	if row.result == nil || row.result.CompanyHTTPStatus != status {
		t.Fatalf("expected company HTTP status %d, got %+v", status, row.result)
	}
	if row.result.CompanyResponse == nil || *row.result.CompanyResponse != body {
		got := "<nil>"
		if row.result.CompanyResponse != nil {
			got = *row.result.CompanyResponse
		}
		t.Fatalf("the company reply must be stored exactly\n got: %q\nwant: %q", got, body)
	}
}

func (f *ocrJobFixture) row(t *testing.T, jobID int) jobRow {
	t.Helper()
	var row jobRow
	var errMsg sql.NullString
	var raw []byte
	err := f.db.QueryRow(`SELECT status, attempts, error_message, result FROM job_queue WHERE id = $1`, jobID).
		Scan(&row.status, &row.attempts, &errMsg, &raw)
	if err != nil {
		t.Fatalf("failed to read job %d: %v", jobID, err)
	}
	row.errorMsg = errMsg.String
	row.rawResult = string(raw)
	if raw != nil {
		row.result = &OCRJobResult{}
		if err := json.Unmarshal(raw, row.result); err != nil {
			t.Fatalf("job %d has an unreadable result: %v", jobID, err)
		}
	}
	return row
}

func TestOCRJob_SendsTheCompleteResultAndStoresTheCompanyReply(t *testing.T) {
	ocr := &fakeOCR{response: lowConfidenceOCR()}
	f := newOCRJobFixture(t, ocr, fakeReply{http.StatusOK, acceptedReply})
	jobID := f.enqueue(t, map[string]interface{}{"receipt_file": "/app/receipts/blue-ocean.jpg", "request_id": "REQ-UPLOAD-1"})

	f.attempt(t, jobID)

	row := f.row(t, jobID)
	if row.status != StatusCompleted || row.attempts != 1 {
		t.Fatalf("expected COMPLETED after 1 attempt, got %s after %d (%s)", row.status, row.attempts, row.errorMsg)
	}
	if calls := ocr.calls(); len(calls) != 1 || calls[0] != "/app/receipts/blue-ocean.jpg" {
		t.Fatalf("expected the engine to be run once on the uploaded receipt, got %v", calls)
	}

	sent := f.company.received()
	if len(sent) != 1 {
		t.Fatalf("expected one company API request, got %d", len(sent))
	}
	want := lowConfidenceOCR()
	want.RequestID, want.ReceiptID, want.ImageName = "REQ-UPLOAD-1", "blue-ocean.jpg", "blue-ocean.jpg"
	if sent[0].body != want {
		t.Fatalf("the company API must receive the complete OCR result\n got: %+v\nwant: %+v", sent[0].body, want)
	}
	if sent[0].requestID != "REQ-UPLOAD-1" || sent[0].auth != "Bearer test-company-key" {
		t.Fatalf("expected X-Request-ID REQ-UPLOAD-1 and the API key, got %q / %q", sent[0].requestID, sent[0].auth)
	}

	result := row.result
	if result == nil || result.OCRResult == nil || *result.OCRResult != want {
		t.Fatalf("expected the complete OCR result on the job, got %+v", result)
	}
	if result.RequestID != "REQ-UPLOAD-1" || result.Error != "" {
		t.Fatalf("unexpected result metadata %+v", result)
	}
	assertCompanyReply(t, row, http.StatusOK, acceptedReply)
}

func TestOCRJob_ACompanyDeclineIsTheCompanysAnswerNotAFailure(t *testing.T) {
	f := newOCRJobFixture(t, &fakeOCR{response: lowConfidenceOCR()}, fakeReply{http.StatusOK, declinedReply})
	jobID := f.enqueue(t, map[string]interface{}{"receipt_file": "/app/receipts/a.jpg", "request_id": "REQ-DECLINE"})

	f.attempt(t, jobID)

	row := f.row(t, jobID)
	if row.status != StatusCompleted || row.attempts != 1 {
		t.Fatalf("a decline must complete the job, got %s after %d", row.status, row.attempts)
	}
	assertCompanyReply(t, row, http.StatusOK, declinedReply)
	if len(f.company.received()) != 1 {
		t.Fatalf("a decline must not be retried, got %d requests", len(f.company.received()))
	}
}

func TestOCRJob_CompanyOutageRetriesThenFailsKeepingTheOCRResult(t *testing.T) {
	f := newOCRJobFixture(t, &fakeOCR{response: lowConfidenceOCR()}, fakeReply{http.StatusServiceUnavailable, `{"error":"down"}`})
	jobID := f.enqueue(t, map[string]interface{}{"receipt_file": "/app/receipts/a.jpg", "request_id": "REQ-OUTAGE"})

	f.attempt(t, jobID)
	row := f.row(t, jobID)
	if row.status != StatusPending || row.attempts != 1 {
		t.Fatalf("expected the job back in PENDING after a failed attempt, got %s after %d", row.status, row.attempts)
	}
	if row.result == nil || row.result.OCRResult == nil || row.result.OCRResult.Fields.InvoiceNumber != "INV-5821" {
		t.Fatalf("the OCR result must be kept when the company API fails, got %+v", row.result)
	}
	if !strings.HasPrefix(row.result.Error, "company_api:") {
		t.Fatalf("expected the company failure to be recorded, got %+v", row.result)
	}
	assertCompanyReply(t, row, http.StatusServiceUnavailable, `{"error":"down"}`)

	f.attempt(t, jobID)
	f.attempt(t, jobID)

	row = f.row(t, jobID)
	if row.status != StatusFailed || row.attempts != 3 || !strings.Contains(row.errorMsg, "REQ-OUTAGE") {
		t.Fatalf("expected FAILED after 3 attempts with the error recorded, got %s after %d: %q", row.status, row.attempts, row.errorMsg)
	}
	if row.result.Attempt != 3 || row.result.OCRResult == nil {
		t.Fatalf("expected the last attempt's result with its OCR document, got %+v", row.result)
	}

	// 3 job attempts, each with the client's own 4 tries — all under one request ID.
	sent := f.company.received()
	if len(sent) != 12 {
		t.Fatalf("expected 12 company API requests (3 attempts x 4 tries), got %d", len(sent))
	}
	for i, request := range sent {
		if request.requestID != "REQ-OUTAGE" || request.body.RequestID != "REQ-OUTAGE" {
			t.Fatalf("request %d used request ID %q/%q, expected REQ-OUTAGE", i, request.requestID, request.body.RequestID)
		}
	}
}

func TestOCRJob_CompletesOnceTheCompanyAPIRecovers(t *testing.T) {
	f := newOCRJobFixture(t, &fakeOCR{response: lowConfidenceOCR()}, fakeReply{http.StatusBadGateway, `{"error":"bad gateway"}`})
	jobID := f.enqueue(t, map[string]interface{}{"receipt_file": "/app/receipts/a.jpg", "request_id": "REQ-RECOVER"})

	f.attempt(t, jobID)
	if row := f.row(t, jobID); row.status != StatusPending {
		t.Fatalf("expected PENDING after the outage, got %s", row.status)
	}

	f.company.setReplies(fakeReply{http.StatusOK, acceptedReply})
	f.attempt(t, jobID)

	row := f.row(t, jobID)
	if row.status != StatusCompleted || row.attempts != 2 || row.result.Error != "" {
		t.Fatalf("expected COMPLETED on attempt 2, got %s/%d %+v", row.status, row.attempts, row.result)
	}
	assertCompanyReply(t, row, http.StatusOK, acceptedReply)
	if sent := f.company.received(); len(sent) != 1 || sent[0].requestID != "REQ-RECOVER" {
		t.Fatalf("expected the retry under the same request ID, got %+v", sent)
	}
}

func TestOCRJob_OCRFailureRetriesAndNeverReachesTheCompanyAPI(t *testing.T) {
	ocr := &fakeOCR{err: fmt.Errorf("ocr engine failed: exit status 2: tesseract could not initialise")}
	f := newOCRJobFixture(t, ocr, fakeReply{http.StatusOK, acceptedReply})
	jobID := f.enqueue(t, map[string]interface{}{"receipt_file": "/app/receipts/a.jpg", "request_id": "REQ-OCR-DOWN"})

	f.attempt(t, jobID)
	row := f.row(t, jobID)
	if row.status != StatusPending || !strings.HasPrefix(row.result.Error, "ocr:") || row.result.OCRResult != nil {
		t.Fatalf("expected PENDING with the OCR error recorded and no OCR result, got %s %+v", row.status, row.result)
	}

	f.attempt(t, jobID)
	f.attempt(t, jobID)

	row = f.row(t, jobID)
	if row.status != StatusFailed || !strings.Contains(row.errorMsg, "tesseract could not initialise") {
		t.Fatalf("expected FAILED with the engine's error, got %s: %q", row.status, row.errorMsg)
	}
	if len(ocr.calls()) != 3 || len(f.company.received()) != 0 {
		t.Fatalf("expected 3 OCR attempts and no company request, got %d / %d", len(ocr.calls()), len(f.company.received()))
	}
}

func TestOCRJob_WithoutARequestIDUsesOneStableIDAcrossAttempts(t *testing.T) {
	f := newOCRJobFixture(t, &fakeOCR{response: lowConfidenceOCR()}, fakeReply{http.StatusBadRequest, `{"error":"invalid"}`})
	jobID := f.enqueue(t, map[string]interface{}{"receipt_file": "/app/receipts/a.jpg"})

	f.attempt(t, jobID)
	f.company.setReplies(fakeReply{http.StatusOK, acceptedReply})
	f.attempt(t, jobID)

	want := fmt.Sprintf("REQ-JOB-%d", jobID)
	row := f.row(t, jobID)
	if row.status != StatusCompleted || row.result.RequestID != want || row.result.OCRResult.RequestID != want {
		t.Fatalf("expected COMPLETED under %s, got %s %+v", want, row.status, row.result)
	}
	if sent := f.company.received(); len(sent) != 1 || sent[0].requestID != want {
		t.Fatalf("expected the retry to use %s, got %+v", want, sent)
	}
}

// --- The company API's reply is kept exactly as received ----------------------

// runWithReply runs one attempt against a company API that answers with body
// and returns the job row.
func runWithReply(t *testing.T, status int, body string) jobRow {
	t.Helper()
	f := newOCRJobFixture(t, &fakeOCR{response: lowConfidenceOCR()}, fakeReply{status, body})
	jobID := f.enqueue(t, map[string]interface{}{"receipt_file": "/app/receipts/a.jpg", "request_id": "REQ-EXACT"})
	f.attempt(t, jobID)
	return f.row(t, jobID)
}

func TestOCRJob_ArbitraryTopLevelFieldsSurvive(t *testing.T) {
	body := `{"decision":"APPROVED","reference_id":"CMP-77","reviewer":{"id":9,"tags":["a",null,true]},"success":true}`
	row := runWithReply(t, http.StatusOK, body)

	if row.status != StatusCompleted {
		t.Fatalf("expected COMPLETED, got %s", row.status)
	}
	assertCompanyReply(t, row, http.StatusOK, body)
}

func TestOCRJob_LargeAndPreciseNumbersSurviveExactly(t *testing.T) {
	body := `{"success":true,"big_id":12345678901234567890,"amount":123625.10,"tiny":1e-7,"negative_zero":-0.0}`
	row := runWithReply(t, http.StatusOK, body)

	assertCompanyReply(t, row, http.StatusOK, body)
	if !strings.Contains(*row.result.CompanyResponse, "12345678901234567890") {
		t.Fatal("the large integer was altered")
	}
}

func TestOCRJob_MissingFieldsAreNotInvented(t *testing.T) {
	body := `{"ok":true}`
	row := runWithReply(t, http.StatusOK, body)

	assertCompanyReply(t, row, http.StatusOK, body)
	for _, invented := range []string{"timestamp", `"message"`, `"success"`, `"data"`} {
		if strings.Contains(row.rawResult, invented) {
			t.Fatalf("the stored result contains %s, which the company API never sent: %s", invented, row.rawResult)
		}
	}
}

func TestOCRJob_FormattingAndDuplicateKeysAreKept(t *testing.T) {
	// A JSON value in jsonb would lose all of this; the stored string does not.
	body := "{ \"b\": 1,\n  \"a\": 2, \"a\": 3 }"
	row := runWithReply(t, http.StatusOK, body)

	assertCompanyReply(t, row, http.StatusOK, body)
}

func TestOCRJob_Non2xxRepliesArePreservedWithTheirStatus(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		attempts int // job attempts to run
	}{
		{"validation error", http.StatusBadRequest, `{"error":"invalid invoice","field":"amount","code":4001}`, 1},
		{"unauthorized", http.StatusUnauthorized, `{"error":"bad key"}`, 1},
		{"unexpected status, plain text", http.StatusConflict, "duplicate request REQ-EXACT", 1},
		{"server error, HTML, after retries", http.StatusServiceUnavailable, "<html><body>maintenance</body></html>", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOCRJobFixture(t, &fakeOCR{response: lowConfidenceOCR()}, fakeReply{tc.status, tc.body})
			jobID := f.enqueue(t, map[string]interface{}{"receipt_file": "/app/receipts/a.jpg", "request_id": "REQ-EXACT"})
			for i := 0; i < tc.attempts; i++ {
				f.attempt(t, jobID)
			}

			row := f.row(t, jobID)
			if row.result.OCRResult == nil || !strings.HasPrefix(row.result.Error, "company_api:") {
				t.Fatalf("expected the OCR result kept and the failure recorded, got %+v", row.result)
			}
			assertCompanyReply(t, row, tc.status, tc.body)
			if tc.attempts == 3 && row.status != StatusFailed {
				t.Fatalf("expected FAILED after 3 attempts, got %s", row.status)
			}
		})
	}
}

func TestOCRJob_ADeclineIsKeptExactly(t *testing.T) {
	body := `{"decision":"REJECTED","reason":"amount does not match INV-5821","success":false}`
	row := runWithReply(t, http.StatusOK, body)

	if row.status != StatusCompleted {
		t.Fatalf("a decline is the company's answer and must complete the job, got %s", row.status)
	}
	assertCompanyReply(t, row, http.StatusOK, body)
}

func TestOCRJob_ABodyThatIsNotUTF8IsKeptAsBase64(t *testing.T) {
	body := "\xff\xfe binary \x00 reply"
	f := newOCRJobFixture(t, &fakeOCR{response: lowConfidenceOCR()}, fakeReply{http.StatusBadGateway, body})
	jobID := f.enqueue(t, map[string]interface{}{"receipt_file": "/app/receipts/a.jpg", "request_id": "REQ-EXACT"})
	f.attempt(t, jobID)

	row := f.row(t, jobID)
	if row.result == nil {
		t.Fatalf("the attempt's result was not stored (error_message: %q)", row.errorMsg)
	}
	decoded, err := base64.StdEncoding.DecodeString(row.result.CompanyResponseBase64)
	if err != nil || string(decoded) != body || row.result.CompanyResponse != nil || row.result.CompanyHTTPStatus != http.StatusBadGateway {
		t.Fatalf("expected the exact bytes in company_response_base64, got %+v (err=%v)", row.result, err)
	}

	// The error text quotes the reply, so it must still be storable: the job
	// reaches FAILED with its error recorded instead of sticking in PROCESSING.
	f.attempt(t, jobID)
	f.attempt(t, jobID)
	row = f.row(t, jobID)
	if row.status != StatusFailed || !strings.Contains(row.errorMsg, "502") {
		t.Fatalf("expected FAILED with the error recorded, got %s: %q", row.status, row.errorMsg)
	}
}
