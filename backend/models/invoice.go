package models

import "time"

type Invoice struct {
	ID            int       `json:"id"`
	InvoiceNumber string    `json:"invoice_number"`
	CustomerName  string    `json:"customer_name"`
	Amount        float64   `json:"amount"`
	Status        string    `json:"status"`
	DueDate       string    `json:"due_date"`
	CreatedAt     time.Time `json:"created_at"`
}

type Payment struct {
	ID            int       `json:"id"`
	InvoiceID     int       `json:"invoice_id"`
	ReceiptFile   string    `json:"receipt_file"`
	PaymentAmount float64   `json:"payment_amount"`
	PaymentDate   string    `json:"payment_date"`
	Reference     string    `json:"reference"`
	CreatedAt     time.Time `json:"created_at"`
}

type ReconciliationRecord struct {
	ID            int       `json:"id"`
	InvoiceNumber string    `json:"invoice_number"`
	ReceiptName   string    `json:"receipt_name"`
	Status        string    `json:"status"`
	MatchedAt     time.Time `json:"matched_at"`
}

type AuditLog struct {
	ID          int       `json:"id"`
	Action      string    `json:"action"`
	Description string    `json:"description"`
	UserName    string    `json:"user_name"`
	Timestamp   time.Time `json:"timestamp"`
}

type ReceiptUploadRequest struct {
	FilePath string `json:"file_path"`
}

type OCRProcessRequest struct {
	ReceiptFile string `json:"receipt_file"`
}

type ReconcileRequest struct {
	InvoiceNumber string  `json:"invoice_number"`
	AmountPaid    float64 `json:"amount_paid"`
	PaymentDate   string  `json:"payment_date"`
	Reference     string  `json:"reference"`
	ReceiptFile   string  `json:"receipt_file"`
}

type ReviewDecisionRequest struct {
	InvoiceNumber string `json:"invoice_number"`
	ReceiptFile   string `json:"receipt_file"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
}

type OCRResponse struct {
	ReceiptID string          `json:"receipt_id"`
	RequestID string          `json:"request_id"`
	OCR       OCRMetadata     `json:"ocr"`
	Fields    ExtractedFields `json:"fields"`
	RawText   string          `json:"raw_text"`
	ImageName string          `json:"image_name"`
}

type OCRMetadata struct {
	Confidence       float64 `json:"confidence"`
	Engine           string  `json:"engine"`
	ProcessingTimeMs int     `json:"processing_time_ms"`
}

type ExtractedFields struct {
	InvoiceNumber string  `json:"invoice_number"`
	Amount        float64 `json:"amount"`
	Date          string  `json:"date"`
	Customer      string  `json:"customer"`
	Reference     string  `json:"reference"`
	PhoneNumber   string  `json:"phone_number"`
	Email         string  `json:"email"`
}

type ReviewItem struct {
	ID            int     `json:"id"`
	ReceiptFile   string  `json:"receipt_file"`
	InvoiceNumber string  `json:"invoice_number"`
	AmountPaid    float64 `json:"amount_paid"`
	Confidence    float64 `json:"confidence"`
	Status        string  `json:"status"`
	Reason        string  `json:"reason"`
}

type DailyReconciliationStats struct {
	Date      string `json:"date"`
	Matched   int    `json:"matched"`
	Unmatched int    `json:"unmatched"`
}

type DashboardResponse struct {
	PendingReviews            int                        `json:"pending_reviews"`
	SuccessfulReconciliations int                        `json:"successful_reconciliations"`
	FailedReconciliations     int                        `json:"failed_reconciliations"`
	RecentUploads             int                        `json:"recent_uploads"`
	AuditActivity             []AuditLog                 `json:"audit_activity"`
	DailyStats                []DailyReconciliationStats `json:"daily_stats"`
	RecentPayments            []Payment                  `json:"recent_payments"`
	RecentHistory             []ReconciliationRecord     `json:"recent_history"`
}

type ReconcileResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type JobStatusResponse struct {
	JobID       int        `json:"job_id"`
	Status      string     `json:"status"`
	Type        string     `json:"type"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
	Result      any        `json:"result,omitempty"`
}

type AuditLogEntry struct {
	JobID     int       `json:"job_id"`
	Action    string    `json:"action"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Metadata  any       `json:"metadata,omitempty"`
}
