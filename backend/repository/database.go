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

	response.RecentHistory, err = d.GetReconciliationHistory()
	if err != nil {
		return models.DashboardResponse{}, err
	}

	return response, nil
}

func (d *Database) countPendingReviews() (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM reconciliation_history WHERE status = 'UNMATCHED'`).Scan(&count)
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

func (d *Database) countRecentUploads() (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&count)
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

func (d *Database) GetReconciliationHistory() ([]models.ReconciliationRecord, error) {
	query := `SELECT id, invoice_number, receipt_name, status, matched_at 
	          FROM reconciliation_history ORDER BY matched_at DESC`

	rows, err := d.db.Query(query)
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
