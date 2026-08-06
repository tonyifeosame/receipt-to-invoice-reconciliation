package jobs

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"receipt-reconciliation/metrics"
	"receipt-reconciliation/repository"
	"receipt-reconciliation/services"
	"sync"
	"time"
)

type JobType string

const (
	JobOCRProcess     JobType = "OCR_PROCESS"
	JobReconciliation JobType = "RECONCILIATION"
	JobNotification   JobType = "NOTIFICATION"
)

type JobStatus string

const (
	StatusPending    JobStatus = "PENDING"
	StatusProcessing JobStatus = "PROCESSING"
	StatusCompleted  JobStatus = "COMPLETED"
	StatusFailed     JobStatus = "FAILED"
)

type Job struct {
	ID          int                    `json:"id"`
	Type        JobType                `json:"type"`
	Payload     map[string]interface{} `json:"payload"`
	Status      JobStatus              `json:"status"`
	Priority    int                    `json:"priority"`
	Attempts    int                    `json:"attempts"`
	MaxAttempts int                    `json:"max_attempts"`
	Error       string                 `json:"error,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
}

type JobHandler func(job *Job, db *repository.Database, ocrService services.OCRService, companyAPI *services.CompanyAPIClient) error

// staleJobTimeout is how long a job may stay in PROCESSING before it is treated
// as abandoned. A worker that dies mid-job would otherwise leave the row claimed
// forever, and the work would never be retried.
const staleJobTimeout = 15 * time.Minute

type JobQueue struct {
	db           *repository.Database
	workerCount  int
	stopChan     chan struct{}
	stopOnce     sync.Once
	startOnce    sync.Once
	wg           sync.WaitGroup
	pollInterval time.Duration
	ocrService   services.OCRService
	companyAPI   *services.CompanyAPIClient

	// handlers is read by every worker while main may still be registering, so
	// it needs a lock rather than a bare map.
	handlersMu sync.RWMutex
	handlers   map[JobType]JobHandler
}

func NewJobQueue(db *repository.Database, workerCount int, ocrService services.OCRService, companyAPI *services.CompanyAPIClient) *JobQueue {
	if workerCount < 1 {
		workerCount = 1
	}

	return &JobQueue{
		db:           db,
		handlers:     make(map[JobType]JobHandler),
		workerCount:  workerCount,
		stopChan:     make(chan struct{}),
		pollInterval: 5 * time.Second,
		ocrService:   ocrService,
		companyAPI:   companyAPI,
	}
}

func (jq *JobQueue) RegisterHandler(jobType JobType, handler JobHandler) {
	jq.handlersMu.Lock()
	defer jq.handlersMu.Unlock()
	jq.handlers[jobType] = handler
}

func (jq *JobQueue) handlerFor(jobType JobType) (JobHandler, bool) {
	jq.handlersMu.RLock()
	defer jq.handlersMu.RUnlock()
	handler, ok := jq.handlers[jobType]
	return handler, ok
}

// PendingCount reports how many jobs are waiting to be claimed. Used by the
// /metrics endpoint to expose queue depth.
func (jq *JobQueue) PendingCount() (int, error) {
	var count int
	err := jq.db.QueryRow(`SELECT COUNT(*) FROM job_queue WHERE status = $1`, StatusPending).Scan(&count)
	return count, err
}

func (jq *JobQueue) Enqueue(jobType JobType, payload map[string]interface{}, priority int) (int, error) {
	// Insert job into database
	query := `INSERT INTO job_queue (job_type, payload, status, priority, attempts, max_attempts) 
	          VALUES ($1, $2, $3, $4, 0, 3) RETURNING id`

	payloadJSON, _ := json.Marshal(payload)

	var jobID int
	err := jq.db.QueryRow(query, jobType, payloadJSON, StatusPending, priority).Scan(&jobID)
	if err != nil {
		return 0, err
	}

	return jobID, nil
}

func (jq *JobQueue) Start() {
	jq.startOnce.Do(func() {
		log.Printf("Starting job queue with %d workers", jq.workerCount)

		// Recover anything a previous process left claimed before workers begin.
		jq.requeueStaleJobs()

		for i := 0; i < jq.workerCount; i++ {
			jq.wg.Add(1)
			go jq.worker(i)
		}

		jq.wg.Add(1)
		go jq.reaper()
	})
}

// Stop is safe to call more than once; it previously panicked on a second call
// by closing an already-closed channel.
func (jq *JobQueue) Stop() {
	jq.stopOnce.Do(func() {
		log.Println("Stopping job queue...")
		close(jq.stopChan)
		jq.wg.Wait()
		log.Println("Job queue stopped")
	})
}

// sleep waits for d, returning false if the queue is shutting down. Plain
// time.Sleep made shutdown wait for a full poll interval per worker.
func (jq *JobQueue) sleep(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-jq.stopChan:
		return false
	case <-timer.C:
		return true
	}
}

// jitter spreads worker polling so that all workers do not query in lockstep.
func (jq *JobQueue) jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return d/2 + time.Duration(rand.Int63n(int64(d)))
}

func (jq *JobQueue) worker(workerID int) {
	defer jq.wg.Done()

	log.Printf("Worker %d started", workerID)

	consecutiveErrors := 0

	for {
		select {
		case <-jq.stopChan:
			log.Printf("Worker %d stopping", workerID)
			return
		default:
		}

		job, err := jq.fetchNextJob()
		if err != nil {
			consecutiveErrors++
			metrics.JobQueueError("claim")
			log.Printf("Worker %d error fetching job: %v", workerID, err)

			// Back off when the database is unhealthy instead of hammering it.
			backoff := time.Duration(consecutiveErrors) * jq.pollInterval
			if backoff > time.Minute {
				backoff = time.Minute
			}
			if !jq.sleep(backoff) {
				return
			}
			continue
		}

		consecutiveErrors = 0

		if job == nil {
			// No jobs available, wait and retry
			if !jq.sleep(jq.jitter(jq.pollInterval)) {
				return
			}
			continue
		}

		log.Printf("Worker %d processing job %d (type: %s, attempt %d/%d)",
			workerID, job.ID, job.Type, job.Attempts, job.MaxAttempts)
		jq.processJob(job, workerID)
	}
}

// fetchNextJob claims exactly one job.
//
// The previous implementation ran "SELECT ... FOR UPDATE SKIP LOCKED" through
// database/sql directly, which runs in its own implicit transaction: the row
// lock was released the moment the SELECT returned, and the status UPDATE was a
// separate statement. Two workers could therefore select the same row and both
// process it. Claiming with a single UPDATE ... WHERE id = (SELECT ... FOR
// UPDATE SKIP LOCKED) statement makes selection and claiming atomic, so a job is
// handed to exactly one worker. The attempt counter is incremented in the same
// statement, which also removes the read-modify-write race on attempts.
func (jq *JobQueue) fetchNextJob() (*Job, error) {
	query := `UPDATE job_queue
	          SET status = $1, started_at = NOW(), attempts = attempts + 1
	          WHERE id = (
	              SELECT id FROM job_queue
	              WHERE status = $2
	              ORDER BY priority DESC, created_at ASC
	              FOR UPDATE SKIP LOCKED
	              LIMIT 1
	          )
	          RETURNING id, job_type, payload, status, priority, attempts, max_attempts, created_at, started_at`

	var job Job
	var payloadJSON []byte
	var startedAt sql.NullTime

	err := jq.db.QueryRow(query, StatusProcessing, StatusPending).Scan(
		&job.ID, &job.Type, &payloadJSON, &job.Status,
		&job.Priority, &job.Attempts, &job.MaxAttempts, &job.CreatedAt, &startedAt,
	)

	if err == sql.ErrNoRows {
		// An empty queue is the normal case, not an error to log every 5s.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if startedAt.Valid {
		started := startedAt.Time
		job.StartedAt = &started
	}

	if err := json.Unmarshal(payloadJSON, &job.Payload); err != nil {
		// The row is already claimed, so returning an error here would leave it
		// stuck in PROCESSING. Fail it outright: retrying cannot fix bad JSON.
		log.Printf("Job %d has an unreadable payload, failing it: %v", job.ID, err)
		jq.markJobFailed(&job, fmt.Sprintf("invalid payload: %v", err))
		metrics.JobFailed(string(job.Type))
		return nil, nil
	}

	metrics.JobClaimed(string(job.Type))
	return &job, nil
}

// reaper periodically returns abandoned jobs to the queue.
func (jq *JobQueue) reaper() {
	defer jq.wg.Done()

	for {
		if !jq.sleep(jq.jitter(staleJobTimeout / 3)) {
			return
		}
		jq.requeueStaleJobs()
	}
}

// requeueStaleJobs recovers jobs left in PROCESSING by a crashed or killed
// worker: those with attempts left go back to PENDING, the rest are failed so
// they stop occupying the queue.
func (jq *JobQueue) requeueStaleJobs() {
	requeue := `UPDATE job_queue
	            SET status = $1, started_at = NULL
	            WHERE status = $2
	              AND started_at IS NOT NULL
	              AND started_at < NOW() - make_interval(secs => $3)
	              AND attempts < max_attempts`

	result, err := jq.db.Exec(requeue, StatusPending, StatusProcessing, staleJobTimeout.Seconds())
	if err != nil {
		metrics.JobQueueError("reap")
		log.Printf("Failed to requeue stale jobs: %v", err)
		return
	}
	if affected, err := result.RowsAffected(); err == nil && affected > 0 {
		metrics.JobReaped("requeued", uint64(affected))
		log.Printf("Requeued %d stale job(s) left in progress", affected)
	}

	exhaust := `UPDATE job_queue
	            SET status = $1, completed_at = NOW(), error_message = $2
	            WHERE status = $3
	              AND started_at IS NOT NULL
	              AND started_at < NOW() - make_interval(secs => $4)
	              AND attempts >= max_attempts`

	result, err = jq.db.Exec(exhaust, StatusFailed, "abandoned in progress and out of attempts", StatusProcessing, staleJobTimeout.Seconds())
	if err != nil {
		metrics.JobQueueError("reap")
		log.Printf("Failed to expire stale jobs: %v", err)
		return
	}
	if affected, err := result.RowsAffected(); err == nil && affected > 0 {
		metrics.JobReaped("failed", uint64(affected))
		log.Printf("Marked %d abandoned job(s) as failed", affected)
	}
}

func (jq *JobQueue) processJob(job *Job, workerID int) {
	handler, exists := jq.handlerFor(job.Type)
	if !exists {
		jq.markJobFailed(job, "No handler registered for job type")
		metrics.JobFailed(string(job.Type))
		return
	}

	// job.Attempts was incremented atomically when the job was claimed, so a
	// worker that dies mid-job cannot cause the attempt to be replayed forever.
	err := jq.runHandler(handler, job, workerID)
	if err != nil {
		log.Printf("Worker %d job %d failed on attempt %d/%d: %v",
			workerID, job.ID, job.Attempts, job.MaxAttempts, err)

		if job.Attempts >= job.MaxAttempts {
			jq.markJobFailed(job, err.Error())
			metrics.JobFailed(string(job.Type))
		} else {
			jq.markJobPending(job)
			metrics.JobRetried(string(job.Type))
		}
		return
	}

	jq.markJobCompleted(job)
	metrics.JobCompleted(string(job.Type))
	log.Printf("Worker %d job %d completed successfully", workerID, job.ID)
}

// runHandler isolates handler panics. An unrecovered panic in a worker goroutine
// terminates the entire process, taking the HTTP server down with it.
func (jq *JobQueue) runHandler(handler JobHandler, job *Job, workerID int) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			metrics.JobPanic(string(job.Type))
			log.Printf("Worker %d recovered from panic in job %d: %v", workerID, job.ID, recovered)
			err = fmt.Errorf("handler panicked: %v", recovered)
		}
	}()

	return handler(job, jq.db, jq.ocrService, jq.companyAPI)
}

func (jq *JobQueue) markJobCompleted(job *Job) {
	now := time.Now()
	job.Status = StatusCompleted
	job.CompletedAt = &now

	// completed_at is set by the database. job_queue timestamps are TIMESTAMP
	// without time zone, so mixing a client clock with the server's NOW() (used
	// for created_at and started_at) skews every duration by the offset between
	// the two hosts.
	query := `UPDATE job_queue SET status = $1, completed_at = NOW() WHERE id = $2`
	_, err := jq.db.Exec(query, StatusCompleted, job.ID)
	if err != nil {
		metrics.JobQueueError("complete")
		log.Printf("Error marking job %d as completed: %v", job.ID, err)
	}
}

func (jq *JobQueue) markJobFailed(job *Job, errorMsg string) {
	now := time.Now()
	job.Status = StatusFailed
	job.CompletedAt = &now
	job.Error = errorMsg

	query := `UPDATE job_queue SET status = $1, completed_at = NOW(), error_message = $2 WHERE id = $3`
	_, err := jq.db.Exec(query, StatusFailed, errorMsg, job.ID)
	if err != nil {
		metrics.JobQueueError("fail")
		log.Printf("Error marking job %d as failed: %v", job.ID, err)
	}
}

// markJobPending returns a job to the queue for another attempt. started_at is
// cleared so the stale-job reaper does not treat the next claim as abandoned.
func (jq *JobQueue) markJobPending(job *Job) {
	query := `UPDATE job_queue SET status = $1, started_at = NULL WHERE id = $2`
	_, err := jq.db.Exec(query, StatusPending, job.ID)
	if err != nil {
		metrics.JobQueueError("requeue")
		log.Printf("Error marking job %d as pending: %v", job.ID, err)
	}
}
