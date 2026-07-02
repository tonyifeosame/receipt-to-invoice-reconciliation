package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"receipt-reconciliation/models"
	"testing"
)

func TestInvoiceHandler_GetInvoiceWithoutDatabaseReturnsDemoInvoice(t *testing.T) {
	handler := &InvoiceHandler{}
	req := httptest.NewRequest(http.MethodGet, "/invoices?invoice_number=INV-001", nil)
	rr := httptest.NewRecorder()

	handler.GetInvoice(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var invoice models.Invoice
	if err := json.Unmarshal(rr.Body.Bytes(), &invoice); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if invoice.InvoiceNumber != "INV-001" {
		t.Fatalf("expected demo invoice INV-001, got %s", invoice.InvoiceNumber)
	}
}

func TestInvoiceHandler_GetPaymentsWithoutDatabaseReturnsDemoPayments(t *testing.T) {
	handler := &InvoiceHandler{}
	req := httptest.NewRequest(http.MethodGet, "/payments?invoice_id=1", nil)
	rr := httptest.NewRecorder()

	handler.GetPayments(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var payments []models.Payment
	if err := json.Unmarshal(rr.Body.Bytes(), &payments); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if len(payments) == 0 {
		t.Fatalf("expected demo payments to be returned")
	}
}
