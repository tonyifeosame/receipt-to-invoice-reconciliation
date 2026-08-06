package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"receipt-reconciliation/repository"
	"receipt-reconciliation/services"
)

// These tests exercise the claim logic against a real PostgreSQL instance,
// because the bug they cover (two workers claiming the same job) only appears
// with genuine row locking. They are skipped unless JOBS_INTEGRATION_DB=1.
//
//	JOBS_INTEGRATION_DB=1 go test ./jobs/
func integrationDB(t *testing.T) *repository.Database {
	t.Helper()

	if os.Getenv("JOBS_INTEGRATION_DB") != "1" {
		t.Skip("set JOBS_INTEGRATION_DB=1 to run job queue integration tests")
	}

	env := func(key, fallback string) string {
		if value := os.Getenv(key); value != "" {
			return value
		}
		return fallback
	}

	db, err := repository.NewDatabase(
		env("DB_HOST", "localhost"),
		env("DB_PORT", "5432"),
		env("DB_NAME", "receipt_reconciliation"),
		env("DB_USER", "postgres"),
		env("DB_PASSWORD", "postgres"),
	)
	if err != nil {
		t.Fatalf("failed to connect to the test database: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// seedJobs inserts count jobs tagged with a unique marker and removes them when
// the test finishes.
//
// startedAgo is how long ago the job was claimed, or nil for a job that has
// never been claimed. The timestamp is computed by the database rather than in
// Go: job_queue columns are TIMESTAMP without time zone, so a client-side value
// is skewed by the offset between the application host and the database host.
func seedJobs(t *testing.T, db *repository.Database, marker string, count int, status JobStatus, attempts int, startedAgo *time.Duration) []int {
	t.Helper()

	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM job_queue WHERE payload->>'test_marker' = $1`, marker); err != nil {
			t.Logf("cleanup failed for marker %s: %v", marker, err)
		}
	})

	ids := make([]int, 0, count)
	for i := 0; i < count; i++ {
		payload, err := json.Marshal(map[string]any{"test_marker": marker, "index": i})
		if err != nil {
			t.Fatalf("failed to marshal payload: %v", err)
		}

		var id int
		if startedAgo == nil {
			err = db.QueryRow(
				`INSERT INTO job_queue (job_type, payload, status, priority, attempts, max_attempts, started_at)
				 VALUES ($1, $2, $3, 5, $4, 3, NULL) RETURNING id`,
				JobOCRProcess, payload, status, attempts,
			).Scan(&id)
		} else {
			err = db.QueryRow(
				`INSERT INTO job_queue (job_type, payload, status, priority, attempts, max_attempts, started_at)
				 VALUES ($1, $2, $3, 5, $4, 3, NOW() - make_interval(secs => $5)) RETURNING id`,
				JobOCRProcess, payload, status, attempts, startedAgo.Seconds(),
			).Scan(&id)
		}
		if err != nil {
			t.Fatalf("failed to insert job: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// A job must be handed to exactly one worker. The previous implementation
// released its row lock when the SELECT returned, so concurrent workers could
// claim and process the same job.
func TestFetchNextJob_ClaimsEachJobExactlyOnce(t *testing.T) {
	db := integrationDB(t)

	const (
		jobCount   = 40
		workerPool = 8
	)
	marker := fmt.Sprintf("claim-once-%d", time.Now().UnixNano())
	seeded := seedJobs(t, db, marker, jobCount, StatusPending, 0, nil)

	queue := NewJobQueue(db, workerPool, nil, nil)

	var (
		mu      sync.Mutex
		claimed []int
		wg      sync.WaitGroup
	)

	for i := 0; i < workerPool; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				job, err := queue.fetchNextJob()
				if err != nil {
					t.Errorf("fetchNextJob returned an error: %v", err)
					return
				}
				if job == nil {
					return
				}
				// Only count the jobs this test seeded.
				if job.Payload["test_marker"] != marker {
					continue
				}
				mu.Lock()
				claimed = append(claimed, job.ID)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	seen := make(map[int]int, len(claimed))
	for _, id := range claimed {
		seen[id]++
	}

	for id, times := range seen {
		if times != 1 {
			t.Fatalf("job %d was claimed %d times; concurrent workers double-processed it", id, times)
		}
	}
	if len(seen) != jobCount {
		t.Fatalf("expected all %d jobs to be claimed, got %d", jobCount, len(seen))
	}

	// The attempt counter must advance exactly once per claim.
	for _, id := range seeded {
		var attempts int
		var status string
		if err := db.QueryRow(`SELECT attempts, status FROM job_queue WHERE id = $1`, id).Scan(&attempts, &status); err != nil {
			t.Fatalf("failed to read job %d: %v", id, err)
		}
		if attempts != 1 {
			t.Fatalf("job %d has attempts=%d, expected exactly 1", id, attempts)
		}
		if status != string(StatusProcessing) {
			t.Fatalf("job %d has status=%s, expected %s", id, status, StatusProcessing)
		}
	}
}

// An empty queue is a normal condition, not an error.
func TestFetchNextJob_EmptyQueueReturnsNoError(t *testing.T) {
	db := integrationDB(t)
	queue := NewJobQueue(db, 1, nil, nil)

	// Drain whatever is pending so the queue is genuinely empty.
	for {
		job, err := queue.fetchNextJob()
		if err != nil {
			t.Fatalf("unexpected error while draining: %v", err)
		}
		if job == nil {
			break
		}
	}

	job, err := queue.fetchNextJob()
	if err != nil {
		t.Fatalf("an empty queue must not report an error, got %v", err)
	}
	if job != nil {
		t.Fatalf("expected no job, got %+v", job)
	}
}

// A worker that dies mid-job leaves the row claimed; the reaper must recover it.
func TestRequeueStaleJobs_RecoversAbandonedWork(t *testing.T) {
	db := integrationDB(t)
	queue := NewJobQueue(db, 1, nil, nil)

	stale := 2 * staleJobTimeout

	retryable := fmt.Sprintf("stale-retryable-%d", time.Now().UnixNano())
	exhausted := fmt.Sprintf("stale-exhausted-%d", time.Now().UnixNano())

	retryableIDs := seedJobs(t, db, retryable, 1, StatusProcessing, 1, &stale)
	exhaustedIDs := seedJobs(t, db, exhausted, 1, StatusProcessing, 3, &stale)

	queue.requeueStaleJobs()

	var status string
	var startedAt *time.Time
	if err := db.QueryRow(`SELECT status, started_at FROM job_queue WHERE id = $1`, retryableIDs[0]).Scan(&status, &startedAt); err != nil {
		t.Fatalf("failed to read job: %v", err)
	}
	if status != string(StatusPending) {
		t.Fatalf("expected the abandoned job to be requeued, status is %s", status)
	}
	if startedAt != nil {
		t.Fatalf("expected started_at to be cleared, got %v", startedAt)
	}

	if err := db.QueryRow(`SELECT status FROM job_queue WHERE id = $1`, exhaustedIDs[0]).Scan(&status); err != nil {
		t.Fatalf("failed to read job: %v", err)
	}
	if status != string(StatusFailed) {
		t.Fatalf("expected the out-of-attempts job to be failed, status is %s", status)
	}
}

// A job still within the stale window must be left alone.
func TestRequeueStaleJobs_LeavesActiveJobsAlone(t *testing.T) {
	db := integrationDB(t)
	queue := NewJobQueue(db, 1, nil, nil)

	marker := fmt.Sprintf("stale-active-%d", time.Now().UnixNano())
	justStarted := time.Duration(0)
	ids := seedJobs(t, db, marker, 1, StatusProcessing, 1, &justStarted)

	queue.requeueStaleJobs()

	var status string
	if err := db.QueryRow(`SELECT status FROM job_queue WHERE id = $1`, ids[0]).Scan(&status); err != nil {
		t.Fatalf("failed to read job: %v", err)
	}
	if status != string(StatusProcessing) {
		t.Fatalf("an in-flight job was reaped early, status is %s", status)
	}
}

// Stop must tolerate being called twice: main defers it and shutdown calls it.
func TestStop_IsIdempotent(t *testing.T) {
	queue := NewJobQueue(nil, 1, nil, nil)
	queue.Stop()
	queue.Stop()
}

// A panicking handler must fail its job, not take the process down.
func TestRunHandler_RecoversPanics(t *testing.T) {
	queue := NewJobQueue(nil, 1, nil, nil)
	job := &Job{ID: 1, Type: JobOCRProcess}

	err := queue.runHandler(func(*Job, *repository.Database, services.OCRService, *services.CompanyAPIClient) error {
		panic("boom")
	}, job, 0)

	if err == nil {
		t.Fatal("expected the recovered panic to be reported as an error")
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("unexpected error %v", err)
	}
}
