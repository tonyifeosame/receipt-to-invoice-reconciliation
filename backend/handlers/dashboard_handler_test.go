package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"receipt-reconciliation/models"
	"testing"
	"time"
)

type stubDashboardRepository struct {
	response models.DashboardResponse
	err      error
}

func (s stubDashboardRepository) GetDashboardMetrics() (models.DashboardResponse, error) {
	return s.response, s.err
}

func TestDashboardHandler_GetDashboard(t *testing.T) {
	expected := models.DashboardResponse{
		PendingReviews:            2,
		SuccessfulReconciliations: 5,
		FailedReconciliations:     1,
		RecentUploads:             3,
		AuditActivity: []models.AuditLog{{
			ID:          1,
			Action:      "RECONCILE",
			Description: "Matched invoice INV-001",
			UserName:    "finance",
			Timestamp:   time.Date(2026, 7, 2, 16, 0, 0, 0, time.UTC),
		}},
		DailyStats: []models.DailyReconciliationStats{{
			Date:      "2026-07-02",
			Matched:   5,
			Unmatched: 1,
		}},
	}

	handler := &DashboardHandler{repo: stubDashboardRepository{response: expected}}
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rr := httptest.NewRecorder()

	handler.GetDashboard(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var actual models.DashboardResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &actual); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if actual.PendingReviews != expected.PendingReviews {
		t.Fatalf("expected pending reviews %d, got %d", expected.PendingReviews, actual.PendingReviews)
	}

	if actual.SuccessfulReconciliations != expected.SuccessfulReconciliations {
		t.Fatalf("expected successful reconciliations %d, got %d", expected.SuccessfulReconciliations, actual.SuccessfulReconciliations)
	}

	if actual.RecentUploads != expected.RecentUploads {
		t.Fatalf("expected recent uploads %d, got %d", expected.RecentUploads, actual.RecentUploads)
	}
}
