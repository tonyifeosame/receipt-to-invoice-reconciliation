package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"receipt-reconciliation/models"
)

func TestReceiptHandler_UploadReceipt(t *testing.T) {
	handler := &ReceiptHandler{}

	reqBody := map[string]string{
		"file_path": "/path/to/receipt.jpg",
	}
	jsonBody, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/receipts/upload", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.UploadReceipt(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["message"] != "Receipt uploaded successfully" {
		t.Errorf("Expected success message, got %s", response["message"])
	}
}

func TestReceiptHandler_UploadReceiptMultipart(t *testing.T) {
	handler := &ReceiptHandler{}

	uploadDir := filepath.Join(t.TempDir(), "receipts")
	t.Setenv("RECEIPTS_DIR", uploadDir)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base("receipt.jpg"))
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}

	if _, err := part.Write([]byte("fake receipt content")); err != nil {
		t.Fatalf("failed to write file content: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/receipts/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	w := httptest.NewRecorder()
	handler.UploadReceipt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if response["message"] != "Receipt uploaded successfully" {
		t.Fatalf("expected success message, got %s", response["message"])
	}

	if response["file_path"] == "" {
		t.Fatalf("expected uploaded file path in response, got %+v", response)
	}

	if _, err := os.Stat(response["file_path"]); err != nil {
		t.Fatalf("expected uploaded file to be stored, got error: %v", err)
	}

	if filepath.Dir(response["file_path"]) != uploadDir {
		t.Fatalf("expected receipt to be stored in %s, got %s", uploadDir, response["file_path"])
	}
}

// stageReceipt points RECEIPTS_DIR at a temporary directory containing a receipt
// and returns its path. OCR requests must reference a file inside that directory.
func stageReceipt(t *testing.T, name string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "receipts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create receipts dir: %v", err)
	}
	t.Setenv("RECEIPTS_DIR", dir)

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("receipt bytes"), 0o644); err != nil {
		t.Fatalf("failed to stage receipt: %v", err)
	}
	return path
}

type stubOCRService struct {
	response models.OCRResponse
	err      error
}

func (s *stubOCRService) ProcessFile(filePath string) (models.OCRResponse, error) {
	return s.response, s.err
}

func TestReceiptHandler_ProcessOCR(t *testing.T) {
	receiptPath := stageReceipt(t, "receipt.jpg")

	handler := &ReceiptHandler{ocr: &stubOCRService{response: models.OCRResponse{
		ReceiptID: "REC-001",
		RequestID: "REQ-123",
		OCR: models.OCRMetadata{
			Confidence:       95,
			Engine:           "Tesseract",
			ProcessingTimeMs: 100,
		},
		Fields: models.ExtractedFields{
			InvoiceNumber: "INV-001",
			Amount:        1500.00,
			Date:          "2026-06-29",
			Customer:      "Acme Corporation",
			Reference:     "REF12345",
		},
		RawText:   "Sample OCR text",
		ImageName: "receipt.jpg",
	}}}

	reqBody := map[string]string{
		"receipt_file": receiptPath,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/ocr/process", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ProcessOCR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var response models.OCRResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Expected valid JSON response: %v", err)
	}

	if response.Fields.InvoiceNumber != "INV-001" {
		t.Fatalf("Expected invoice number INV-001, got %s", response.Fields.InvoiceNumber)
	}
}

func TestReceiptHandler_ProcessOCR_LowConfidenceReview(t *testing.T) {
	receiptPath := stageReceipt(t, "receipt.jpg")

	handler := &ReceiptHandler{ocr: &stubOCRService{response: models.OCRResponse{
		ReceiptID: "REC-001",
		RequestID: "REQ-123",
		OCR: models.OCRMetadata{
			Confidence:       70,
			Engine:           "Tesseract",
			ProcessingTimeMs: 100,
		},
		Fields: models.ExtractedFields{
			InvoiceNumber: "INV-001",
			Amount:        1500.00,
			Date:          "2026-06-29",
			Customer:      "Acme Corporation",
			Reference:     "REF12345",
		},
		RawText:   "Sample OCR text",
		ImageName: "receipt.jpg",
	}}}

	reqBody := map[string]string{
		"receipt_file": receiptPath,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/ocr/process", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ProcessOCR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var response models.OCRResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Expected valid JSON response: %v", err)
	}

	// With validation removed, all OCR results are preserved
	if response.Fields.InvoiceNumber != "INV-001" {
		t.Fatalf("Expected invoice number to be preserved, got %s", response.Fields.InvoiceNumber)
	}
}

func TestReceiptHandler_Reconcile(t *testing.T) {
	handler := &ReceiptHandler{}

	reqBody := map[string]interface{}{
		"invoice_number": "INV-001",
		"amount_paid":    1500.00,
		"payment_date":   "2026-06-29",
		"reference":      "REF12345",
		"receipt_file":   "/path/to/receipt.jpg",
	}
	jsonBody, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/reconcile", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.Reconcile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var response models.ReconcileResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Expected valid JSON response: %v", err)
	}

	// Reconciliation is owned by the company API. The backend must not decide the
	// outcome itself, so the endpoint reports that the request was not handled here.
	if response.Success {
		t.Fatalf("Expected reconciliation to be delegated to the company API, got %+v", response)
	}

	if response.Message == "" {
		t.Fatalf("Expected an explanatory message, got %+v", response)
	}
}
