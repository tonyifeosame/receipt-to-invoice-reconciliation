package jobs

import (
	"encoding/json"
	"log"
	"receipt-reconciliation/repository"
	"sync"
	"time"
)

type JobType string

const (
	JobOCRProcess    JobType = "OCR_PROCESS"
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

type JobHandler func(job *Job, db *repository.Database) error

type JobQueue struct {
	db            *repository.Database
	handlers      map[JobType]JobHandler
	workerCount   int
	stopChan      chan struct{}
	wg            sync.WaitGroup
	pollInterval  time.Duration
}

func NewJobQueue(db *repository.Database, workerCount int) *JobQueue {
	return &JobQueue{
		db:           db,
		handlers:     make(map[JobType]JobHandler),
		workerCount:  workerCount,
		stopChan:     make(chan struct{}),
		pollInterval: 5 * time.Second,
	}
}

func (jq *JobQueue) RegisterHandler(jobType JobType, handler JobHandler) {
	jq.handlers[jobType] = handler
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
	log.Printf("Starting job queue with %d workers", jq.workerCount)
	
	for i := 0; i < jq.workerCount; i++ {
		jq.wg.Add(1)
		go jq.worker(i)
	}
}

func (jq *JobQueue) Stop() {
	log.Println("Stopping job queue...")
	close(jq.stopChan)
	jq.wg.Wait()
	log.Println("Job queue stopped")
}

func (jq *JobQueue) worker(workerID int) {
	defer jq.wg.Done()
	
	log.Printf("Worker %d started", workerID)
	
	for {
		select {
		case <-jq.stopChan:
			log.Printf("Worker %d stopping", workerID)
			return
		default:
			job, err := jq.fetchNextJob()
			if err != nil {
				log.Printf("Worker %d error fetching job: %v", workerID, err)
				time.Sleep(jq.pollInterval)
				continue
			}
			
			if job == nil {
				// No jobs available, wait and retry
				time.Sleep(jq.pollInterval)
				continue
			}
			
			log.Printf("Worker %d processing job %d (type: %s)", workerID, job.ID, job.Type)
			jq.processJob(job, workerID)
		}
	}
}

func (jq *JobQueue) fetchNextJob() (*Job, error) {
	query := `SELECT id, job_type, payload, status, priority, attempts, max_attempts, created_at 
	          FROM job_queue 
	          WHERE status = $1 
	          ORDER BY priority DESC, created_at ASC 
	          LIMIT 1 
	          FOR UPDATE SKIP LOCKED`
	
	row := jq.db.QueryRow(query, StatusPending)
	
	var job Job
	var payloadJSON []byte
	
	err := row.Scan(
		&job.ID, &job.Type, &payloadJSON, &job.Status,
		&job.Priority, &job.Attempts, &job.MaxAttempts, &job.CreatedAt,
	)
	
	if err != nil {
		return nil, err
	}
	
	if err := json.Unmarshal(payloadJSON, &job.Payload); err != nil {
		return nil, err
	}
	
	// Mark as processing
	now := time.Now()
	job.Status = StatusProcessing
	job.StartedAt = &now
	
	updateQuery := `UPDATE job_queue SET status = $1, started_at = $2 WHERE id = $3`
	_, err = jq.db.Exec(updateQuery, StatusProcessing, now, job.ID)
	if err != nil {
		return nil, err
	}
	
	return &job, nil
}

func (jq *JobQueue) processJob(job *Job, workerID int) {
	handler, exists := jq.handlers[job.Type]
	if !exists {
		jq.markJobFailed(job, "No handler registered for job type")
		return
	}
	
	job.Attempts++
	
	err := handler(job, jq.db)
	if err != nil {
		log.Printf("Worker %d job %d failed: %v", workerID, job.ID, err)
		
		if job.Attempts >= job.MaxAttempts {
			jq.markJobFailed(job, err.Error())
		} else {
			jq.markJobPending(job)
		}
		return
	}
	
	jq.markJobCompleted(job)
	log.Printf("Worker %d job %d completed successfully", workerID, job.ID)
}

func (jq *JobQueue) markJobCompleted(job *Job) {
	now := time.Now()
	job.Status = StatusCompleted
	job.CompletedAt = &now
	
	query := `UPDATE job_queue SET status = $1, completed_at = $2, attempts = $3 WHERE id = $4`
	_, err := jq.db.Exec(query, StatusCompleted, now, job.Attempts, job.ID)
	if err != nil {
		log.Printf("Error marking job %d as completed: %v", job.ID, err)
	}
}

func (jq *JobQueue) markJobFailed(job *Job, errorMsg string) {
	now := time.Now()
	job.Status = StatusFailed
	job.CompletedAt = &now
	job.Error = errorMsg
	
	query := `UPDATE job_queue SET status = $1, completed_at = $2, attempts = $3, error_message = $4 WHERE id = $5`
	_, err := jq.db.Exec(query, StatusFailed, now, job.Attempts, errorMsg, job.ID)
	if err != nil {
		log.Printf("Error marking job %d as failed: %v", job.ID, err)
	}
}

func (jq *JobQueue) markJobPending(job *Job) {
	query := `UPDATE job_queue SET status = $1, attempts = $2 WHERE id = $3`
	_, err := jq.db.Exec(query, StatusPending, job.Attempts, job.ID)
	if err != nil {
		log.Printf("Error marking job %d as pending: %v", job.ID, err)
	}
}
