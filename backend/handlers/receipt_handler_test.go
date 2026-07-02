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
	"time"

	"receipt-reconciliation/models"
)

// Mock database for testing
type mockDatabase struct{}

func (m *mockDatabase) GetInvoiceByNumber(invoiceNumber string) (models.Invoice, error) {
	return models.Invoice{
		ID:            1,
		InvoiceNumber: invoiceNumber,
		CustomerName:  "Test Customer",
		Amount:        1500.00,
		Status:        "PENDING",
		DueDate:       "2026-07-15",
		CreatedAt:     time.Now(),
	}, nil
}

func (m *mockDatabase) IsDuplicatePayment(invoiceNumber, reference string) (bool, error) {
	return false, nil
}

func (m *mockDatabase) UpdateInvoiceStatus(invoiceNumber, status string) error {
	return nil
}

func (m *mockDatabase) UpdateInvoiceBalance(invoiceNumber string, paymentAmount float64) error {
	return nil
}

func (m *mockDatabase) CreatePayment(payment models.Payment) error {
	return nil
}

func (m *mockDatabase) CreateReconciliationRecord(invoiceNumber, receiptName, status string) error {
	return nil
}

func (m *mockDatabase) CreateAuditLog(action, description, userName string) error {
	return nil
}

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
}

type stubOCRService struct {
	response models.OCRResponse
	err      error
}

func (s *stubOCRService) ProcessFile(filePath string) (models.OCRResponse, error) {
	return s.response, s.err
}

func TestReceiptHandler_ProcessOCR(t *testing.T) {
	handler := &ReceiptHandler{ocr: &stubOCRService{response: models.OCRResponse{
		InvoiceNumber: "INV-001",
		AmountPaid:    1500.00,
		PaymentDate:   "2026-06-29",
		BankReference: "REF12345",
		CustomerName:  "Acme Corporation",
		IsValid:       true,
		Confidence:    95,
	}}}

	reqBody := map[string]string{
		"receipt_file": "/path/to/receipt.jpg",
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

	if response.InvoiceNumber != "INV-001" {
		t.Fatalf("Expected invoice number INV-001, got %s", response.InvoiceNumber)
	}
}

func TestReceiptHandler_ProcessOCR_LowConfidenceReview(t *testing.T) {
	handler := &ReceiptHandler{ocr: &stubOCRService{response: models.OCRResponse{
		InvoiceNumber: "INV-001",
		AmountPaid:    1500.00,
		PaymentDate:   "2026-06-29",
		BankReference: "REF12345",
		CustomerName:  "Acme Corporation",
		IsValid:       true,
		Confidence:    70,
	}}}

	reqBody := map[string]string{
		"receipt_file": "/path/to/receipt.jpg",
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

	if response.InvoiceNumber != "" {
		t.Fatalf("Expected low-confidence OCR to be cleared for review, got %s", response.InvoiceNumber)
	}

	if response.IsValid {
		t.Fatalf("Expected low-confidence OCR to be marked invalid for review")
	}
}

func TestReceiptHandler_Reconcile(t *testing.T) {
	handler := &ReceiptHandler{db: &mockDatabase{}}

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

	if !response.Success {
		t.Fatalf("Expected reconciliation to succeed, got %+v", response)
	}
}
