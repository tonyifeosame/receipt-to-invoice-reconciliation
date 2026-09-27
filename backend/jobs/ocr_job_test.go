package jobs

import (
	"strings"
	"testing"

	"receipt-reconciliation/services"
)

// Checks that run before any database access, so they need no PostgreSQL.

func TestProcessOCRJob_RequiresAReceiptFile(t *testing.T) {
	ocr := &fakeOCR{response: lowConfidenceOCR()}
	err := ProcessOCRJob(&Job{ID: 7, Payload: map[string]interface{}{"request_id": "REQ-1"}}, nil, ocr, &services.CompanyAPIClient{})

	if err == nil || !strings.Contains(err.Error(), "receipt_file") {
		t.Fatalf("expected a missing receipt_file to be an error, got %v", err)
	}
	if len(ocr.calls()) != 0 {
		t.Fatal("the OCR engine must not run without a receipt")
	}
}

func TestProcessOCRJob_RequiresTheOCREngineAndCompanyClient(t *testing.T) {
	job := &Job{ID: 8, Payload: map[string]interface{}{"receipt_file": "/app/receipts/a.jpg"}}

	if err := ProcessOCRJob(job, nil, nil, &services.CompanyAPIClient{}); err == nil {
		t.Fatal("expected an error without an OCR engine")
	}
	if err := ProcessOCRJob(job, nil, &fakeOCR{}, nil); err == nil {
		t.Fatal("expected an error without a company API client")
	}
}
