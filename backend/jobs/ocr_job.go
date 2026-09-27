package jobs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"receipt-reconciliation/models"
	"receipt-reconciliation/repository"
	"receipt-reconciliation/services"
	"unicode/utf8"
)

// OCRJobResult is what an OCR_PROCESS job keeps in job_queue.result. It is
// written on every attempt, so a job that fails at the company API still holds
// the OCR document that was extracted.
//
// The company API's reply is kept exactly as received: its HTTP status and its
// body, byte for byte. The body is held as a string rather than as a JSON value,
// because PostgreSQL's jsonb would reorder its keys, drop duplicate keys and
// normalise its formatting; a string round-trips exactly. A body that is not
// valid UTF-8, or contains a NUL (which jsonb cannot store), is kept base64
// encoded in company_response_base64 instead. All four are absent when no HTTP
// response was received.
type OCRJobResult struct {
	RequestID                string              `json:"request_id"`
	ReceiptFile              string              `json:"receipt_file"`
	Attempt                  int                 `json:"attempt"`
	OCRResult                *models.OCRResponse `json:"ocr_result,omitempty"`
	CompanyHTTPStatus        int                 `json:"company_http_status,omitempty"`
	CompanyResponse          *string             `json:"company_response,omitempty"`
	CompanyResponseBase64    string              `json:"company_response_base64,omitempty"`
	CompanyResponseTruncated bool                `json:"company_response_truncated,omitempty"`
	Error                    string              `json:"error,omitempty"`
}

// setCompanyReply records the company API's reply as received.
func (r *OCRJobResult) setCompanyReply(response *services.CompanyAPIResponse) {
	if response == nil || response.Reply == nil {
		return
	}
	reply := response.Reply
	r.CompanyHTTPStatus = reply.StatusCode
	r.CompanyResponseTruncated = reply.Truncated
	if utf8.Valid(reply.Body) && !bytes.ContainsRune(reply.Body, 0) {
		body := string(reply.Body)
		r.CompanyResponse = &body
	} else {
		r.CompanyResponseBase64 = base64.StdEncoding.EncodeToString(reply.Body)
	}
}

// ProcessOCRJob is the OCR_PROCESS handler. It runs the C++ OCR engine on the
// uploaded receipt, sends the complete result to the company API and stores the
// company's reply as returned.
//
// It decides nothing about the receipt. Approval and reconciliation belong to the
// company API: a reply with success=false is its answer, stored like any other,
// and the job completes. Only a failure to reach an answer — OCR could not run,
// or the company API could not be reached — returns an error, and the queue
// retries the job until it runs out of attempts.
func ProcessOCRJob(job *Job, db *repository.Database, ocr services.OCRService, company *services.CompanyAPIClient) error {
	receiptFile, _ := job.Payload["receipt_file"].(string)
	if receiptFile == "" {
		return fmt.Errorf("OCR job %d has no receipt_file in its payload", job.ID)
	}
	if ocr == nil || company == nil {
		return fmt.Errorf("OCR job %d cannot run: OCR engine or company API client not configured", job.ID)
	}

	// One request ID for the receipt across every attempt, so the company API
	// can recognise a retry of a request it may already have seen.
	requestID, _ := job.Payload["request_id"].(string)
	if requestID == "" {
		requestID = fmt.Sprintf("REQ-JOB-%d", job.ID)
	}

	result := OCRJobResult{RequestID: requestID, ReceiptFile: receiptFile, Attempt: job.Attempts}

	ocrResponse, err := ocr.ProcessFile(receiptFile)
	if err != nil {
		result.Error = "ocr: " + err.Error()
		return storeAndFail(db, job, result, fmt.Errorf("OCR failed for %s: %w", receiptFile, err))
	}

	// The same identity the synchronous path uses; the request ID is the job's.
	ocrResponse.ReceiptID = filepath.Base(receiptFile)
	ocrResponse.ImageName = filepath.Base(receiptFile)
	ocrResponse.RequestID = requestID
	result.OCRResult = &ocrResponse

	companyResponse, err := company.SendOCRResultsContext(context.Background(), ocrResponse)
	result.setCompanyReply(companyResponse)
	if err != nil {
		result.Error = "company_api: " + err.Error()
		return storeAndFail(db, job, result, fmt.Errorf("company API did not accept request %s: %w", requestID, err))
	}

	if err := storeOCRJobResult(db, job.ID, result); err != nil {
		// The job is retried and the result re-sent under the same request ID.
		return fmt.Errorf("company API answered request %s but the reply could not be stored: %w", requestID, err)
	}
	return nil
}

// storeAndFail records a failed attempt's result, then returns cause so the
// queue retries or fails the job.
func storeAndFail(db *repository.Database, job *Job, result OCRJobResult, cause error) error {
	if err := storeOCRJobResult(db, job.ID, result); err != nil {
		return fmt.Errorf("%w (and the attempt's result could not be stored: %v)", cause, err)
	}
	return cause
}

func storeOCRJobResult(db *repository.Database, jobID int, result OCRJobResult) error {
	if db == nil {
		return fmt.Errorf("no database to store the result of job %d", jobID)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to encode the result of job %d: %w", jobID, err)
	}
	if _, err := db.Exec(`UPDATE job_queue SET result = $1 WHERE id = $2`, data, jobID); err != nil {
		return fmt.Errorf("failed to store the result of job %d: %w", jobID, err)
	}
	return nil
}
