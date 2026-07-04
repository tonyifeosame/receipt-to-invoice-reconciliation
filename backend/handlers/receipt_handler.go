package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"receipt-reconciliation/jobs"
	"receipt-reconciliation/models"
	"receipt-reconciliation/services"
	"strconv"
	"strings"
)

type InvoiceRepository interface {
	GetInvoiceByNumber(string) (models.Invoice, error)
	IsDuplicatePayment(string, string) (bool, error)
	UpdateInvoiceStatus(string, string) error
	UpdateInvoiceBalance(string, float64) error
	CreatePayment(models.Payment) error
	CreateReconciliationRecord(string, string, string) error
	CreateAuditLog(string, string, string) error
}

type OCRService interface {
	ProcessFile(string) (models.OCRResponse, error)
}

type CompanyAPIService interface {
	SendOCRResults(models.OCRResponse) (*services.CompanyAPIResponse, error)
}

type ReceiptHandler struct {
	db       InvoiceRepository
	ocr      OCRService
	company  CompanyAPIService
	jobQueue *jobs.JobQueue
}

func NewReceiptHandler(db InvoiceRepository, jobQueue *jobs.JobQueue) *ReceiptHandler {
	return &ReceiptHandler{
		db:       db,
		ocr:      services.NewOCRService(),
		company:  services.NewCompanyAPIClient(),
		jobQueue: jobQueue,
	}
}

func (h *ReceiptHandler) UploadReceipt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var req models.ReceiptUploadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		log.Printf("Receipt upload requested via JSON: %s", req.FilePath)
		response := map[string]string{
			"message":   "Receipt uploaded successfully",
			"file_path": req.FilePath,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file upload", http.StatusBadRequest)
		return
	}
	defer file.Close()

	uploadDir := filepath.Join("..", "receipts")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		http.Error(w, "Failed to prepare upload directory", http.StatusInternalServerError)
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".pdf" {
		http.Error(w, "Unsupported file type", http.StatusBadRequest)
		return
	}

	targetPath := filepath.Join(uploadDir, header.Filename)
	storedFile, err := os.Create(targetPath)
	if err != nil {
		http.Error(w, "Failed to save uploaded file", http.StatusInternalServerError)
		return
	}
	defer storedFile.Close()

	if _, err := io.Copy(storedFile, file); err != nil {
		http.Error(w, "Failed to write uploaded file", http.StatusInternalServerError)
		return
	}

	log.Printf("Receipt upload requested: %s", targetPath)
	response := map[string]string{
		"message":   "Receipt uploaded successfully",
		"file_path": targetPath,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func saveUploadedFile(file multipart.File, header *multipart.FileHeader, uploadDir string) (string, error) {
	targetPath := filepath.Join(uploadDir, header.Filename)
	storedFile, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to create destination file: %w", err)
	}
	defer storedFile.Close()

	if _, err := io.Copy(storedFile, file); err != nil {
		return "", fmt.Errorf("failed to copy uploaded file: %w", err)
	}

	return targetPath, nil
}

func (h *ReceiptHandler) ProcessOCR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req models.OCRProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("OCR processing requested for: %s", req.ReceiptFile)

	// Create job for asynchronous processing
	if h.jobQueue != nil {
		payload := map[string]interface{}{
			"receipt_file": req.ReceiptFile,
			"request_id":   services.GenerateRequestID(),
		}
		jobID, err := h.jobQueue.Enqueue(jobs.JobOCRProcess, payload, 1)
		if err != nil {
			log.Printf("Failed to enqueue OCR job: %v", err)
			http.Error(w, "Failed to create job", http.StatusInternalServerError)
			return
		}
		log.Printf("OCR job created: %d for receipt: %s", jobID, req.ReceiptFile)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"job_id": jobID, "status": "queued"})
		return
	}

	// Fallback to synchronous processing if job queue not available
	if h.ocr == nil {
		h.ocr = services.NewOCRService()
	}

	response, err := h.ocr.ProcessFile(req.ReceiptFile)
	if err != nil {
		log.Printf("OCR processing failed: %v", err)
		http.Error(w, "OCR processing failed", http.StatusInternalServerError)
		return
	}

	// Set receipt ID and image name from file path
	response.ReceiptID = filepath.Base(req.ReceiptFile)
	response.ImageName = filepath.Base(req.ReceiptFile)

	// Send OCR results to company API
	if h.company != nil {
		companyResponse, err := h.company.SendOCRResults(response)
		if err != nil {
			log.Printf("Failed to send OCR results to company API: %v", err)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"ocr_results":      response,
				"company_response": map[string]any{"success": false, "message": err.Error(), "status": "api_error"},
			})
			return
		}
		log.Printf("Company API response: success=%v, message=%s", companyResponse.Success, companyResponse.Message)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ocr_results":      response,
			"company_response": companyResponse,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *ReceiptHandler) ReviewDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Review decisions are now handled by the company API
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": "Review decisions are now handled by the company API. Please use the company's system for approval/rejection decisions.",
	})
}

func (h *ReceiptHandler) GetJobStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobIDStr := r.URL.Query().Get("job_id")
	if jobIDStr == "" {
		http.Error(w, "job_id parameter is required", http.StatusBadRequest)
		return
	}

	jobID, err := strconv.Atoi(jobIDStr)
	if err != nil {
		http.Error(w, "Invalid job_id", http.StatusBadRequest)
		return
	}

	// Query job status from database
	if h.db != nil {
		// This would need to be implemented in the repository
		// For now, return a placeholder response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"job_id":  jobID,
			"status":  "unknown",
			"message": "Job status query not yet implemented",
		})
		return
	}

	http.Error(w, "Database not available", http.StatusInternalServerError)
}

func (h *ReceiptHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Reconciliation is now handled by the company API
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": "Reconciliation is now handled by the company API. OCR results are automatically sent to the company system for processing.",
	})
}
