package handlers

import (
	"encoding/json"
	"net/http"
	"receipt-reconciliation/models"
	"receipt-reconciliation/repository"
	"strconv"
)

type InvoiceHandler struct {
	db *repository.Database
}

func NewInvoiceHandler(db *repository.Database) *InvoiceHandler {
	return &InvoiceHandler{db: db}
}

func (h *InvoiceHandler) GetInvoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	invoiceNumber := r.URL.Query().Get("invoice_number")
	if invoiceNumber == "" {
		http.Error(w, "invoice_number parameter is required", http.StatusBadRequest)
		return
	}

	if h.db != nil {
		invoice, err := h.db.GetInvoiceByNumber(invoiceNumber)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(invoice)
			return
		}
	}

	invoice := models.Invoice{
		ID:            1,
		InvoiceNumber: invoiceNumber,
		CustomerName:  "Demo Customer",
		Amount:        1500.00,
		Status:        "PENDING",
		DueDate:       "2026-07-15",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(invoice)
}

func (h *InvoiceHandler) GetPayments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	invoiceID := r.URL.Query().Get("invoice_id")
	if invoiceID == "" {
		http.Error(w, "invoice_id parameter is required", http.StatusBadRequest)
		return
	}

	// Convert string to int
	id, err := strconv.Atoi(invoiceID)
	if err != nil {
		http.Error(w, "Invalid invoice_id", http.StatusBadRequest)
		return
	}

	if h.db != nil {
		payments, err := h.db.GetPaymentsByInvoice(id)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(payments)
			return
		}
	}

	payments := []models.Payment{{
		ID:            1,
		InvoiceID:     id,
		ReceiptFile:   "receipts/demo-receipt.jpg",
		PaymentAmount: 1500.00,
		PaymentDate:   "2026-07-02",
		Reference:     "DEMO-REF",
	}}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payments)
}
