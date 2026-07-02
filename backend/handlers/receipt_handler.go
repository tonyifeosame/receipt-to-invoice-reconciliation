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
	"receipt-reconciliation/models"
	"receipt-reconciliation/services"
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

type ReceiptHandler struct {
	db  InvoiceRepository
	ocr OCRService
}

func NewReceiptHandler(db InvoiceRepository) *ReceiptHandler {
	return &ReceiptHandler{db: db, ocr: services.NewOCRService()}
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

	if h.ocr == nil {
		h.ocr = services.NewOCRService()
	}

	response, err := h.ocr.ProcessFile(req.ReceiptFile)
	if err != nil {
		log.Printf("OCR processing failed: %v", err)
		http.Error(w, "OCR processing failed", http.StatusInternalServerError)
		return
	}

	if !response.IsValid || response.Confidence < 85 {
		response.IsValid = false
		response.InvoiceNumber = ""
		response.AmountPaid = 0
		response.BankReference = ""
		response.CustomerName = ""
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *ReceiptHandler) ReviewDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req models.ReviewDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Decision != "reject" && req.Decision != "approve" {
		http.Error(w, "Decision must be approve or reject", http.StatusBadRequest)
		return
	}

	if h.db == nil {
		http.Error(w, "Database not initialized", http.StatusInternalServerError)
		return
	}

	status := "UNMATCHED"
	if req.Decision == "approve" {
		status = "MATCHED"
	}

	if err := h.db.CreateReconciliationRecord(req.InvoiceNumber, req.ReceiptFile, status); err != nil {
		log.Printf("Error creating review decision record: %v", err)
	}

	action := "REVIEW_REJECTED"
	if req.Decision == "approve" {
		action = "REVIEW_APPROVED"
	}

	description := fmt.Sprintf("Review %s for %s", req.Decision, req.ReceiptFile)
	if req.Reason != "" {
		description = fmt.Sprintf("%s - %s", description, req.Reason)
	}
	if err := h.db.CreateAuditLog(action, description, "system"); err != nil {
		log.Printf("Error creating audit log: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "Review decision recorded"})
}

func (h *ReceiptHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req models.ReconcileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Reconciliation requested for invoice: %s", req.InvoiceNumber)

	if h.db == nil {
		http.Error(w, "Database not initialized", http.StatusInternalServerError)
		return
	}

	// Get invoice from database
	invoice, err := h.db.GetInvoiceByNumber(req.InvoiceNumber)
	if err != nil {
		response := models.ReconcileResponse{
			Success: false,
			Message: "Invoice not found",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Check for duplicate payment
	isDuplicate, err := h.db.IsDuplicatePayment(req.InvoiceNumber, req.Reference)
	if err != nil {
		log.Printf("Error checking duplicate payment: %v", err)
	} else if isDuplicate {
		response := models.ReconcileResponse{
			Success: false,
			Message: "Duplicate payment detected",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Create payment record
	payment := models.Payment{
		InvoiceID:     invoice.ID,
		ReceiptFile:   req.ReceiptFile,
		PaymentAmount: req.AmountPaid,
		PaymentDate:   req.PaymentDate,
		Reference:     req.Reference,
	}

	if err := h.db.CreatePayment(payment); err != nil {
		log.Printf("Error creating payment: %v", err)
		response := models.ReconcileResponse{
			Success: false,
			Message: "Failed to create payment record",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Update invoice status
	if err := h.db.UpdateInvoiceStatus(req.InvoiceNumber, "PAID"); err != nil {
		log.Printf("Error updating invoice status: %v", err)
	}

	// Update invoice balance
	if err := h.db.UpdateInvoiceBalance(req.InvoiceNumber, req.AmountPaid); err != nil {
		log.Printf("Error updating invoice balance: %v", err)
	}

	// Create reconciliation record
	if err := h.db.CreateReconciliationRecord(req.InvoiceNumber, req.ReceiptFile, "MATCHED"); err != nil {
		log.Printf("Error creating reconciliation record: %v", err)
	}

	// Create audit log
	if err := h.db.CreateAuditLog("RECONCILIATION",
		"Receipt "+req.ReceiptFile+" matched to invoice "+req.InvoiceNumber, "system"); err != nil {
		log.Printf("Error creating audit log: %v", err)
	}

	response := models.ReconcileResponse{
		Success: true,
		Message: "Reconciliation completed successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
