package handlers

import (
	"encoding/json"
	"net/http"
	"receipt-reconciliation/repository"
)

type HistoryHandler struct {
	db *repository.Database
}

func NewHistoryHandler(db *repository.Database) *HistoryHandler {
	return &HistoryHandler{db: db}
}

func (h *HistoryHandler) GetReconciliationHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	records, err := h.db.GetReconciliationHistory()
	if err != nil {
		http.Error(w, "Failed to retrieve reconciliation history", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}

func (h *HistoryHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logs, err := h.db.GetAuditLogs()
	if err != nil {
		http.Error(w, "Failed to retrieve audit logs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}
