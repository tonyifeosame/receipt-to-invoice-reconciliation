package repository

import (
	"database/sql"
	"fmt"
	"log"
	"receipt-reconciliation/models"

	_ "github.com/lib/pq"
)

type Database struct {
	db *sql.DB
}

func NewDatabase(host, port, dbname, user, password string) (*Database, error) {
	connStr := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		host, port, dbname, user, password)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("Connected to database successfully")
	return &Database{db: db}, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) GetDashboardMetrics() (models.DashboardResponse, error) {
	if d == nil || d.db == nil {
		return models.DashboardResponse{}, nil
	}

	response := models.DashboardResponse{}

	pendingReviews, err := d.countPendingReviews()
	if err != nil {
		return models.DashboardResponse{}, err
	}
	response.PendingReviews = pendingReviews

	successfulReconciliations, err := d.countSuccessfulReconciliations()
	if err != nil {
		return models.DashboardResponse{}, err
	}
	response.SuccessfulReconciliations = successfulReconciliations

	failedReconciliations, err := d.countFailedReconciliations()
	if err != nil {
		return models.DashboardResponse{}, err
	}
	response.FailedReconciliations = failedReconciliations

	recentUploads, err := d.countRecentUploads()
	if err != nil {
		return models.DashboardResponse{}, err
	}
	response.RecentUploads = recentUploads

	response.AuditActivity, err = d.GetAuditLogs()
	if err != nil {
		return models.DashboardResponse{}, err
	}

	response.DailyStats, err = d.GetDailyReconciliationStats()
	if err != nil {
		return models.DashboardResponse{}, err
	}

	response.RecentPayments, err = d.GetRecentPayments()
	if err != nil {
		return models.DashboardResponse{}, err
	}

	// The dashboard shows a "recent" extract, not the entire history: the full
	// table is served by the /history endpoint.
	response.RecentHistory, err = d.getReconciliationHistory(recentHistoryLimit)
	if err != nil {
		return models.DashboardResponse{}, err
	}

	return response, nil
}

// recentUploadWindow is the period the "recent uploads" dashboard tile covers.
const recentUploadWindow = "24 hours"

// countPendingReviews counts reconciliations still awaiting a review decision.
// It previously ran the same query as countFailedReconciliations, so the
// "pending reviews" and "failed" tiles always showed an identical number.
func (d *Database) countPendingReviews() (int, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM reconciliation_history WHERE review_status = 'PENDING_REVIEW'`).Scan(&count)
	return count, err
}

func (d *Database) countSuccessfulReconciliations() (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM reconciliation_history WHERE status = 'MATCHED'`).Scan(&count)
	return count, err
}

func (d *Database) countFailedReconciliations() (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM reconciliation_history WHERE status = 'UNMATCHED'`).Scan(&count)
	return count, err
}

// countRecentUploads counts uploads inside the recent window. It used to count
// every payment ever recorded, so the tile was a lifetime total labelled
// "recent" and only ever grew.
func (d *Database) countRecentUploads() (int, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM payments WHERE created_at >= NOW() - INTERVAL '` + recentUploadWindow + `'`).Scan(&count)
	return count, err
}

func (d *Database) GetDailyReconciliationStats() ([]models.DailyReconciliationStats, error) {
	query := `SELECT TO_CHAR(matched_at, 'YYYY-MM-DD') AS day,
		SUM(CASE WHEN status = 'MATCHED' THEN 1 ELSE 0 END) AS matched,
		SUM(CASE WHEN status = 'UNMATCHED' THEN 1 ELSE 0 END) AS unmatched
		FROM reconciliation_history
		GROUP BY TO_CHAR(matched_at, 'YYYY-MM-DD')
		ORDER BY day DESC LIMIT 7`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []models.DailyReconciliationStats
	for rows.Next() {
		var item models.DailyReconciliationStats
		err := rows.Scan(&item.Date, &item.Matched, &item.Unmatched)
		if err != nil {
			return nil, err
		}
		stats = append(stats, item)
	}

	return stats, nil
}

func (d *Database) GetRecentPayments() ([]models.Payment, error) {
	query := `SELECT id, invoice_id, receipt_file, payment_amount, payment_date, reference, created_at
		FROM payments ORDER BY created_at DESC LIMIT 5`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payments []models.Payment
	for rows.Next() {
		var payment models.Payment
		err := rows.Scan(&payment.ID, &payment.InvoiceID, &payment.ReceiptFile,
			&payment.PaymentAmount, &payment.PaymentDate, &payment.Reference, &payment.CreatedAt)
		if err != nil {
			return nil, err
		}
		payments = append(payments, payment)
	}

	return payments, nil
}

// GetJob returns one queued job with its stored result, or nil if there is no
// job with that id. The result column is returned as stored, never rewritten.
func (d *Database) GetJob(id int) (*models.JobStatusResponse, error) {
	var job models.JobStatusResponse
	var errorMessage sql.NullString
	var startedAt, completedAt sql.NullTime
	var result []byte

	err := d.db.QueryRow(`SELECT id, job_type, status, attempts, max_attempts, error_message,
	                             created_at, started_at, completed_at, result
	                      FROM job_queue WHERE id = $1`, id).Scan(
		&job.JobID, &job.Type, &job.Status, &job.Attempts, &job.MaxAttempts, &errorMessage,
		&job.CreatedAt, &startedAt, &completedAt, &result,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	job.ErrorMessage = errorMessage.String
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	if result != nil {
		job.Result = result
	}
	return &job, nil
}

// Helper methods for direct SQL access (used by job queue)
func (d *Database) QueryRow(query string, args ...interface{}) *sql.Row {
	return d.db.QueryRow(query, args...)
}

func (d *Database) Exec(query string, args ...interface{}) (sql.Result, error) {
	return d.db.Exec(query, args...)
}

func (d *Database) GetInvoiceByNumber(invoiceNumber string) (models.Invoice, error) {
	var invoice models.Invoice
	query := `SELECT id, invoice_number, customer_name, amount, status, due_date, created_at 
	          FROM invoices WHERE invoice_number = $1`

	err := d.db.QueryRow(query, invoiceNumber).Scan(
		&invoice.ID, &invoice.InvoiceNumber, &invoice.CustomerName,
		&invoice.Amount, &invoice.Status, &invoice.DueDate, &invoice.CreatedAt,
	)

	if err != nil {
		return models.Invoice{}, err
	}

	return invoice, nil
}

func (d *Database) UpdateInvoiceStatus(invoiceNumber, status string) error {
	query := `UPDATE invoices SET status = $1 WHERE invoice_number = $2`
	_, err := d.db.Exec(query, status, invoiceNumber)
	return err
}

func (d *Database) UpdateInvoiceBalance(invoiceNumber string, paymentAmount float64) error {
	query := `UPDATE invoices SET amount = amount - $1 WHERE invoice_number = $2`
	_, err := d.db.Exec(query, paymentAmount, invoiceNumber)
	return err
}

func (d *Database) CreatePayment(payment models.Payment) error {
	query := `INSERT INTO payments (invoice_id, receipt_file, payment_amount, payment_date, reference) 
	          VALUES ($1, $2, $3, $4, $5)`
	_, err := d.db.Exec(query, payment.InvoiceID, payment.ReceiptFile,
		payment.PaymentAmount, payment.PaymentDate, payment.Reference)
	return err
}

func (d *Database) GetPaymentsByInvoice(invoiceID int) ([]models.Payment, error) {
	query := `SELECT id, invoice_id, receipt_file, payment_amount, payment_date, reference, created_at 
	          FROM payments WHERE invoice_id = $1 ORDER BY created_at DESC`

	rows, err := d.db.Query(query, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payments []models.Payment
	for rows.Next() {
		var payment models.Payment
		err := rows.Scan(&payment.ID, &payment.InvoiceID, &payment.ReceiptFile,
			&payment.PaymentAmount, &payment.PaymentDate, &payment.Reference, &payment.CreatedAt)
		if err != nil {
			return nil, err
		}
		payments = append(payments, payment)
	}

	return payments, nil
}

func (d *Database) IsDuplicatePayment(invoiceNumber, reference string) (bool, error) {
	query := `SELECT COUNT(*) FROM payments p 
	          JOIN invoices i ON p.invoice_id = i.id 
	          WHERE i.invoice_number = $1 AND p.reference = $2`

	var count int
	err := d.db.QueryRow(query, invoiceNumber, reference).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (d *Database) CreateReconciliationRecord(invoiceNumber, receiptName, status string) error {
	query := `INSERT INTO reconciliation_history (invoice_number, receipt_name, status, matched_at) 
	          VALUES ($1, $2, $3, NOW())`
	_, err := d.db.Exec(query, invoiceNumber, receiptName, status)
	return err
}

// recentHistoryLimit is how many reconciliation records the dashboard shows.
const recentHistoryLimit = 10

// GetReconciliationHistory returns the full reconciliation history.
func (d *Database) GetReconciliationHistory() ([]models.ReconciliationRecord, error) {
	return d.getReconciliationHistory(0)
}

// getReconciliationHistory returns the history, newest first, optionally capped
// at limit rows (limit <= 0 returns everything).
func (d *Database) getReconciliationHistory(limit int) ([]models.ReconciliationRecord, error) {
	// main.go keeps serving with a nil *Database when the database is
	// unavailable ("demo mode"), so every exported read has to tolerate it the
	// way GetDashboardMetrics does. Without this guard GET /history panicked the
	// request goroutine on a nil dereference and dropped the connection.
	if d == nil || d.db == nil {
		return nil, nil
	}

	query := `SELECT id, invoice_number, receipt_name, status, matched_at
	          FROM reconciliation_history ORDER BY matched_at DESC`

	args := []interface{}{}
	if limit > 0 {
		query += ` LIMIT $1`
		args = append(args, limit)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []models.ReconciliationRecord
	for rows.Next() {
		var record models.ReconciliationRecord
		err := rows.Scan(&record.ID, &record.InvoiceNumber, &record.ReceiptName,
			&record.Status, &record.MatchedAt)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, nil
}

func (d *Database) CreateAuditLog(action, description, userName string) error {
	query := `INSERT INTO audit_logs (action, description, user_name, timestamp) 
	          VALUES ($1, $2, $3, NOW())`
	_, err := d.db.Exec(query, action, description, userName)
	return err
}

func (d *Database) GetAuditLogs() ([]models.AuditLog, error) {
	// Reachable with a nil *Database from GET /audit-logs in demo mode; see the
	// note on getReconciliationHistory.
	if d == nil || d.db == nil {
		return nil, nil
	}

	query := `SELECT id, action, description, user_name, timestamp
	          FROM audit_logs ORDER BY timestamp DESC LIMIT 100`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.AuditLog
	for rows.Next() {
		var log models.AuditLog
		err := rows.Scan(&log.ID, &log.Action, &log.Description, &log.UserName, &log.Timestamp)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}

	return logs, nil
}
