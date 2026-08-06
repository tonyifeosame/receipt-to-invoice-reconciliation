-- Production-ready features migration
-- Adds confidence scores, duplicate detection, manual review queue, OCR storage, metrics, and authentication

-- Add confidence score to reconciliation_history
ALTER TABLE reconciliation_history ADD COLUMN IF NOT EXISTS ocr_confidence DECIMAL(5, 2);
ALTER TABLE reconciliation_history ADD COLUMN IF NOT EXISTS review_status VARCHAR(20) DEFAULT 'AUTO_MATCHED' CHECK (review_status IN ('AUTO_MATCHED', 'PENDING_REVIEW', 'MANUALLY_APPROVED', 'REJECTED'));
ALTER TABLE reconciliation_history ADD COLUMN IF NOT EXISTS reviewed_by VARCHAR(100);
ALTER TABLE reconciliation_history ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMP;

-- Add file hash for duplicate detection to payments
ALTER TABLE payments ADD COLUMN IF NOT EXISTS file_hash VARCHAR(64) UNIQUE;
ALTER TABLE payments ADD COLUMN IF NOT EXISTS file_size BIGINT;
ALTER TABLE payments ADD COLUMN IF NOT EXISTS mime_type VARCHAR(100);

-- Create OCR results storage table
CREATE TABLE IF NOT EXISTS ocr_results (
    id SERIAL PRIMARY KEY,
    receipt_file VARCHAR(255) NOT NULL,
    file_hash VARCHAR(64) UNIQUE NOT NULL,
    raw_text TEXT NOT NULL,
    confidence_score DECIMAL(5, 2),
    processing_time_ms INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create manual review queue table
CREATE TABLE IF NOT EXISTS review_queue (
    id SERIAL PRIMARY KEY,
    receipt_file VARCHAR(255) NOT NULL,
    file_hash VARCHAR(64) NOT NULL,
    invoice_number VARCHAR(50),
    extracted_amount DECIMAL(10, 2),
    extracted_date DATE,
    confidence_score DECIMAL(5, 2),
    reason VARCHAR(255),
    status VARCHAR(20) DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
    assigned_to VARCHAR(100),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    reviewed_at TIMESTAMP,
    review_notes TEXT
);

-- Create metrics table
CREATE TABLE IF NOT EXISTS processing_metrics (
    id SERIAL PRIMARY KEY,
    metric_name VARCHAR(100) NOT NULL,
    metric_value DECIMAL(15, 2) NOT NULL,
    metric_type VARCHAR(50) DEFAULT 'counter' CHECK (metric_type IN ('counter', 'gauge', 'histogram')),
    tags JSONB,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create users table for authentication
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(100) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(50) DEFAULT 'FINANCE_STAFF' CHECK (role IN ('ADMIN', 'FINANCE_MANAGER', 'FINANCE_STAFF', 'VIEWER')),
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_login TIMESTAMP
);

-- Create job queue table for background processing
CREATE TABLE IF NOT EXISTS job_queue (
    id SERIAL PRIMARY KEY,
    job_type VARCHAR(50) NOT NULL CHECK (job_type IN ('OCR_PROCESS', 'RECONCILIATION', 'NOTIFICATION')),
    payload JSONB NOT NULL,
    status VARCHAR(20) DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PROCESSING', 'COMPLETED', 'FAILED')),
    priority INTEGER DEFAULT 5,
    attempts INTEGER DEFAULT 0,
    max_attempts INTEGER DEFAULT 3,
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP
);

-- Create indexes for performance
CREATE INDEX IF NOT EXISTS idx_ocr_results_hash ON ocr_results(file_hash);
CREATE INDEX IF NOT EXISTS idx_ocr_results_created ON ocr_results(created_at);
CREATE INDEX IF NOT EXISTS idx_review_queue_status ON review_queue(status);
CREATE INDEX IF NOT EXISTS idx_review_queue_assigned ON review_queue(assigned_to);
CREATE INDEX IF NOT EXISTS idx_metrics_name ON processing_metrics(metric_name);
CREATE INDEX IF NOT EXISTS idx_metrics_recorded ON processing_metrics(recorded_at);
CREATE INDEX IF NOT EXISTS idx_job_queue_status ON job_queue(status);
CREATE INDEX IF NOT EXISTS idx_job_queue_priority ON job_queue(priority);
CREATE INDEX IF NOT EXISTS idx_job_queue_created ON job_queue(created_at);

-- Create trigger for automatic metric updates
CREATE OR REPLACE FUNCTION update_processing_metric()
RETURNS TRIGGER AS $$
BEGIN
    -- jsonb_build_object quotes and escapes the value. String concatenation
    -- produced {"status": MATCHED}, which is not valid JSON, so the cast failed
    -- and every INSERT into reconciliation_history was rejected.
    INSERT INTO processing_metrics (metric_name, metric_value, metric_type, tags)
    VALUES ('receipts_processed', 1, 'counter', jsonb_build_object('status', NEW.status));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Dropped first so this migration can be re-applied like the rest of the file.
DROP TRIGGER IF EXISTS trigger_receipt_processed ON reconciliation_history;

CREATE TRIGGER trigger_receipt_processed
AFTER INSERT ON reconciliation_history
FOR EACH ROW
EXECUTE FUNCTION update_processing_metric();
