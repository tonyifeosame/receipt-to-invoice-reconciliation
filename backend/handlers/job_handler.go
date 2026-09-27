package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"receipt-reconciliation/repository"
	"strconv"

	"github.com/gorilla/mux"
)

// JobStatusHandler reports queued jobs. It only reads: the job's result is
// whatever the OCR job stored — the OCR document and the company API's reply —
// and is returned as stored, without interpretation.
type JobStatusHandler struct {
	db *repository.Database
}

func NewJobStatusHandler(db *repository.Database) *JobStatusHandler {
	return &JobStatusHandler{db: db}
}

// GetJob serves GET /jobs/{id}.
func (h *JobStatusHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil || id < 1 {
		http.Error(w, "Invalid job id", http.StatusBadRequest)
		return
	}

	// Jobs only exist in the database; without one there is nothing to report.
	if h.db == nil {
		http.Error(w, "Job status unavailable: no database", http.StatusServiceUnavailable)
		return
	}

	job, err := h.db.GetJob(id)
	if err != nil {
		log.Printf("Failed to read job %d: %v", id, err)
		http.Error(w, "Failed to retrieve job", http.StatusInternalServerError)
		return
	}
	if job == nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}
