package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"receipt-reconciliation/models"
)

type DashboardRepository interface {
	GetDashboardMetrics() (models.DashboardResponse, error)
}

type DashboardHandler struct {
	repo DashboardRepository
}

func NewDashboardHandler(repo DashboardRepository) *DashboardHandler {
	return &DashboardHandler{repo: repo}
}

func (h *DashboardHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := models.DashboardResponse{
		PendingReviews:            0,
		SuccessfulReconciliations: 0,
		FailedReconciliations:     0,
		RecentUploads:             0,
		AuditActivity:             []models.AuditLog{},
		DailyStats:                []models.DailyReconciliationStats{},
		RecentPayments:            []models.Payment{},
		RecentHistory:             []models.ReconciliationRecord{},
	}

	if h.repo != nil {
		metrics, err := h.repo.GetDashboardMetrics()
		if err != nil {
			log.Printf("dashboard metrics unavailable: %v", err)
		} else {
			response = metrics
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
