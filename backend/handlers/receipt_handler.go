package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"regexp"
	"strings"
	"time"
)

type OCRService interface {
	ProcessFile(string) (models.OCRResponse, error)
}

type CompanyAPIService interface {
	SendOCRResultsContext(context.Context, models.OCRResponse) (*services.CompanyAPIResponse, error)
}

type ReceiptHandler struct {
	ocr      OCRService
	company  CompanyAPIService
	jobQueue *jobs.JobQueue
}

// NewReceiptHandler builds the receipt handler. Reconciliation and review
// decisions belong to the company API, so this handler holds no repository:
// it runs OCR and forwards the result.
func NewReceiptHandler(jobQueue *jobs.JobQueue) *ReceiptHandler {
	return &ReceiptHandler{
		ocr:      services.NewOCRService(),
		company:  services.NewCompanyAPIClient(),
		jobQueue: jobQueue,
	}
}

// receiptsDir resolves the directory uploads are written to. It defaults to the
// "receipts" directory next to the running binary, which is where the Docker image
// mounts the shared receipts volume (/app/receipts), and can be overridden with
// RECEIPTS_DIR. The OCR engine reads receipts from this same location.
func receiptsDir() string {
	if dir := os.Getenv("RECEIPTS_DIR"); dir != "" {
		return dir
	}
	return "receipts"
}

// allowedReceiptExtensions are the file types the OCR engine accepts.
var allowedReceiptExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".pdf":  true,
}

// safeStemPattern strips anything that is not a plain filename character so that
// the stored name cannot carry path separators, traversal sequences or shell
// metacharacters into the OCR engine's argument list.
var safeStemPattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// uniqueFileName derives a collision-free storage name from the uploaded name.
// Storing under the client-supplied name let one upload silently overwrite an
// earlier receipt simply by reusing its filename.
func uniqueFileName(originalName string) (string, error) {
	base := filepath.Base(originalName)
	ext := strings.ToLower(filepath.Ext(base))
	if !allowedReceiptExtensions[ext] {
		return "", fmt.Errorf("unsupported file type")
	}

	stem := safeStemPattern.ReplaceAllString(strings.TrimSuffix(base, filepath.Ext(base)), "_")
	stem = strings.Trim(stem, "._-")
	if len(stem) > 60 {
		stem = stem[:60]
	}
	if stem == "" {
		stem = "receipt"
	}

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("failed to generate a unique file name: %w", err)
	}

	return fmt.Sprintf("%s-%s-%s%s", stem, time.Now().UTC().Format("20060102T150405"), hex.EncodeToString(suffix), ext), nil
}

// resolveReceiptPath validates a client-supplied receipt path and returns its
// absolute location. The path is attacker-controlled and used to open files and
// to build the OCR engine's arguments, so it must be confined to the receipts
// directory: without this check "../../etc/passwd" or any absolute path was read
// straight off the host.
func resolveReceiptPath(requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", fmt.Errorf("receipt file path is required")
	}

	if strings.ContainsRune(requested, '\x00') {
		return "", fmt.Errorf("invalid receipt file path")
	}

	if !allowedReceiptExtensions[strings.ToLower(filepath.Ext(requested))] {
		return "", fmt.Errorf("unsupported file type")
	}

	baseDir, err := filepath.Abs(receiptsDir())
	if err != nil {
		return "", fmt.Errorf("failed to resolve receipts directory: %w", err)
	}

	candidate, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("invalid receipt file path")
	}

	if !isWithinDir(baseDir, candidate) {
		return "", fmt.Errorf("receipt file must be inside the receipts directory")
	}

	// Resolve symlinks so a link planted inside the receipts directory cannot
	// point the OCR engine at a file outside it.
	if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
		resolvedBase, baseErr := filepath.EvalSymlinks(baseDir)
		if baseErr != nil {
			resolvedBase = baseDir
		}
		if !isWithinDir(resolvedBase, resolved) {
			return "", fmt.Errorf("receipt file must be inside the receipts directory")
		}
		candidate = resolved
	}

	return candidate, nil
}

// isWithinDir reports whether target sits inside baseDir.
func isWithinDir(baseDir, target string) bool {
	rel, err := filepath.Rel(baseDir, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
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

	uploadDir := receiptsDir()
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		http.Error(w, "Failed to prepare upload directory", http.StatusInternalServerError)
		return
	}

	if !allowedReceiptExtensions[strings.ToLower(filepath.Ext(header.Filename))] {
		http.Error(w, "Unsupported file type", http.StatusBadRequest)
		return
	}

	targetPath, err := saveUploadedFile(file, header, uploadDir)
	if err != nil {
		log.Printf("Failed to store uploaded receipt: %v", err)
		http.Error(w, "Failed to save uploaded file", http.StatusInternalServerError)
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
	fileName, err := uniqueFileName(header.Filename)
	if err != nil {
		return "", err
	}

	targetPath := filepath.Join(uploadDir, fileName)

	// O_EXCL so an unexpected name clash fails loudly instead of overwriting a
	// receipt that is already stored.
	storedFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
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

	// The requested path is attacker-controlled and is handed to the OCR engine,
	// so confine it to the receipts directory before it is used or stored.
	receiptPath, err := resolveReceiptPath(req.ReceiptFile)
	if err != nil {
		log.Printf("Rejected OCR request for %q: %v", req.ReceiptFile, err)
		http.Error(w, "Invalid receipt file", http.StatusBadRequest)
		return
	}

	log.Printf("OCR processing requested for: %s", receiptPath)

	// Create job for asynchronous processing
	if h.jobQueue != nil {
		payload := map[string]interface{}{
			"receipt_file": receiptPath,
			"request_id":   services.GenerateRequestID(),
		}
		jobID, err := h.jobQueue.Enqueue(jobs.JobOCRProcess, payload, 1)
		if err != nil {
			log.Printf("Failed to enqueue OCR job: %v", err)
			http.Error(w, "Failed to create job", http.StatusInternalServerError)
			return
		}
		log.Printf("OCR job created: %d for receipt: %s", jobID, receiptPath)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"job_id": jobID, "status": "queued"})
		return
	}

	// Fallback to synchronous processing if job queue not available
	if h.ocr == nil {
		h.ocr = services.NewOCRService()
	}

	response, err := h.ocr.ProcessFile(receiptPath)
	if err != nil {
		log.Printf("OCR processing failed: %v", err)
		http.Error(w, "OCR processing failed", http.StatusInternalServerError)
		return
	}

	// Set receipt ID and image name from file path
	response.ReceiptID = filepath.Base(receiptPath)
	response.ImageName = filepath.Base(receiptPath)

	// Send OCR results to company API
	if h.company != nil {
		companyResponse, err := h.company.SendOCRResultsContext(r.Context(), response)
		if err != nil {
			// The OCR result is still returned so the caller keeps the extraction
			// and can see why the company API did not accept it.
			log.Printf("Failed to send OCR results to company API (receipt %s): %v", response.ReceiptID, err)
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
