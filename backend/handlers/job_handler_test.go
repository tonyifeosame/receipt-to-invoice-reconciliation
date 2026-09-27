package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"receipt-reconciliation/middleware"
	"receipt-reconciliation/models"
	"receipt-reconciliation/repository"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

const jobTestSecret = "job-handler-test-signing-key-0123456789"

// jobRouter wires GET /jobs/{id} behind the same middleware main.go uses.
func jobRouter(t *testing.T, db *repository.Database) (*mux.Router, *middleware.AuthMiddleware) {
	t.Helper()
	auth := middleware.NewAuthMiddleware(jobTestSecret)
	router := mux.NewRouter()
	finance := router.PathPrefix("").Subrouter()
	finance.Use(auth.Authenticate)
	finance.Use(auth.RequireRole(middleware.RoleFinanceStaff))
	finance.HandleFunc("/jobs/{id}", NewJobStatusHandler(db).GetJob).Methods("GET")
	return router, auth
}

func getJob(t *testing.T, router *mux.Router, auth *middleware.AuthMiddleware, role, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/jobs/"+id, nil)
	if role != "" {
		token, err := auth.GenerateToken(1, "tester", role)
		if err != nil {
			t.Fatalf("failed to issue token: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestGetJob_RequiresAFinanceLogin(t *testing.T) {
	router, auth := jobRouter(t, nil)

	if rr := getJob(t, router, auth, "", "1"); rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d", rr.Code)
	}
	if rr := getJob(t, router, auth, middleware.RoleViewer, "1"); rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a viewer, got %d", rr.Code)
	}
}

func TestGetJob_RejectsAnInvalidID(t *testing.T) {
	router, auth := jobRouter(t, nil)

	for _, id := range []string{"abc", "0", "-3", "1.5"} {
		if rr := getJob(t, router, auth, middleware.RoleFinanceStaff, id); rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for id %q, got %d", id, rr.Code)
		}
	}
}

func TestGetJob_WithoutADatabaseIsUnavailable(t *testing.T) {
	router, auth := jobRouter(t, nil)

	if rr := getJob(t, router, auth, middleware.RoleFinanceStaff, "1"); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without a database, got %d", rr.Code)
	}
}

// The tests below read real job_queue rows. Like the job queue integration
// tests, they are skipped unless JOBS_INTEGRATION_DB=1.

func jobsTestDB(t *testing.T) *repository.Database {
	t.Helper()
	if os.Getenv("JOBS_INTEGRATION_DB") != "1" {
		t.Skip("set JOBS_INTEGRATION_DB=1 to run job status integration tests")
	}
	env := func(key, fallback string) string {
		if value := os.Getenv(key); value != "" {
			return value
		}
		return fallback
	}
	db, err := repository.NewDatabase(env("DB_HOST", "localhost"), env("DB_PORT", "5432"),
		env("DB_NAME", "receipt_reconciliation"), env("DB_USER", "postgres"), env("DB_PASSWORD", "postgres"))
	if err != nil {
		t.Fatalf("failed to connect to the test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertJob(t *testing.T, db *repository.Database, status string, attempts int, errorMessage, result any) int {
	t.Helper()
	marker := fmt.Sprintf("job-handler-%s-%d", t.Name(), time.Now().UnixNano())
	payload := fmt.Sprintf(`{"test_marker": %q, "receipt_file": "/app/receipts/r.png"}`, marker)
	var id int
	err := db.QueryRow(`INSERT INTO job_queue (job_type, payload, status, attempts, max_attempts, error_message, result, started_at, completed_at)
	                    VALUES ('OCR_PROCESS', $1::jsonb, $2::varchar, $3::int, 3, $4::text, $5::jsonb,
	                            CASE WHEN $3::int > 0 THEN NOW() END,
	                            CASE WHEN $2::varchar IN ('COMPLETED', 'FAILED') THEN NOW() END)
	                    RETURNING id`, payload, status, attempts, errorMessage, result).Scan(&id)
	if err != nil {
		t.Fatalf("failed to insert job: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM job_queue WHERE id = $1`, id) })
	return id
}

func decodeJob(t *testing.T, rr *httptest.ResponseRecorder) models.JobStatusResponse {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var job models.JobStatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &job); err != nil {
		t.Fatalf("invalid JSON %s: %v", rr.Body.String(), err)
	}
	return job
}

func TestGetJob_ReturnsACompletedJobWithItsStoredResultUnchanged(t *testing.T) {
	db := jobsTestDB(t)
	router, auth := jobRouter(t, db)
	stored := `{"attempt": 1, "request_id": "REQ-42", "ocr_result": {"fields": {"amount": 123625, "invoice_number": "INV-5821"}, "raw_text": "TOTAL: 123,625.00", "ocr": {"confidence": 41.5}}, "company_response": {"data": {"decision": "APPROVED"}, "message": "received", "success": true}}`
	id := insertJob(t, db, "COMPLETED", 1, nil, stored)

	job := decodeJob(t, getJob(t, router, auth, middleware.RoleFinanceStaff, fmt.Sprint(id)))

	if job.JobID != id || job.Type != "OCR_PROCESS" || job.Status != "COMPLETED" || job.Attempts != 1 || job.MaxAttempts != 3 {
		t.Fatalf("unexpected job metadata %+v", job)
	}
	if job.StartedAt == nil || job.CompletedAt == nil {
		t.Fatalf("expected started_at and completed_at, got %+v", job)
	}
	var want, got any
	json.Unmarshal([]byte(stored), &want)
	json.Unmarshal(job.Result, &got)
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if !bytes.Equal(wantJSON, gotJSON) {
		t.Fatalf("the stored result must be returned unchanged\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func TestGetJob_AFailedJobCarriesItsError(t *testing.T) {
	db := jobsTestDB(t)
	router, auth := jobRouter(t, db)
	id := insertJob(t, db, "FAILED", 3, "company API did not accept request REQ-7", `{"attempt": 3, "error": "company_api: down"}`)

	job := decodeJob(t, getJob(t, router, auth, middleware.RoleFinanceStaff, fmt.Sprint(id)))

	if job.Status != "FAILED" || job.ErrorMessage != "company API did not accept request REQ-7" {
		t.Fatalf("expected the failure to be reported, got %+v", job)
	}
}

func TestGetJob_APendingJobHasANullResult(t *testing.T) {
	db := jobsTestDB(t)
	router, auth := jobRouter(t, db)
	id := insertJob(t, db, "PENDING", 0, nil, nil)

	rr := getJob(t, router, auth, middleware.RoleFinanceStaff, fmt.Sprint(id))
	job := decodeJob(t, rr)

	if job.Status != "PENDING" || job.StartedAt != nil || job.CompletedAt != nil {
		t.Fatalf("unexpected pending job %+v", job)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"result":null`)) {
		t.Fatalf("expected an explicit null result, got %s", rr.Body.String())
	}
}

func TestGetJob_UnknownJobIsNotFound(t *testing.T) {
	db := jobsTestDB(t)
	router, auth := jobRouter(t, db)

	if rr := getJob(t, router, auth, middleware.RoleFinanceStaff, "2147483000"); rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}
