#include "database.h"
#include "logger.h"
#include <sstream>
#include <iomanip>

Database::Database(const std::string& host, int port, const std::string& dbname, 
                   const std::string& user, const std::string& password)
    : connectionString_(buildConnectionString(host, port, dbname, user, password)) {
    LOG_INFO("Database created with connection string");
}

Database::~Database() {
    disconnect();
    LOG_INFO("Database destroyed");
}

std::string Database::buildConnectionString(const std::string& host, int port, 
                                              const std::string& dbname, 
                                              const std::string& user, 
                                              const std::string& password) {
    std::ostringstream oss;
    oss << "host=" << host 
        << " port=" << port 
        << " dbname=" << dbname 
        << " user=" << user 
        << " password=" << password;
    return oss.str();
}

bool Database::connect() {
    try {
        connection_ = std::make_unique<pqxx::connection>(connectionString_);
        if (connection_->is_open()) {
            LOG_INFO("Connected to database successfully");
            return true;
        } else {
            LOG_ERROR("Failed to connect to database");
            return false;
        }
    } catch (const std::exception& e) {
        LOG_ERROR("Database connection error: " + std::string(e.what()));
        return false;
    }
}

void Database::disconnect() {
    if (connection_ && connection_->is_open()) {
        connection_->close();
        LOG_INFO("Disconnected from database");
    }
}

bool Database::isConnected() const {
    return connection_ && connection_->is_open();
}

Invoice Database::getInvoiceByNumber(const std::string& invoiceNumber) {
    LOG_INFO("Fetching invoice: " + invoiceNumber);
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return Invoice{};
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "SELECT id, invoice_number, customer_name, amount, status, due_date "
                           "FROM invoices WHERE invoice_number = $1";
        
        pqxx::result result = txn.exec_params(query, invoiceNumber);
        
        if (result.empty()) {
            LOG_WARNING("Invoice not found: " + invoiceNumber);
            return Invoice{};
        }
        
        const auto& row = result[0];
        Invoice invoice;
        invoice.id = row["id"].as<int>();
        invoice.invoiceNumber = row["invoice_number"].as<std::string>();
        invoice.customerName = row["customer_name"].as<std::string>();
        invoice.amount = row["amount"].as<double>();
        invoice.status = row["status"].as<std::string>();
        invoice.dueDate = row["due_date"].as<std::string>();
        
        txn.commit();
        LOG_INFO("Invoice fetched successfully");
        return invoice;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error fetching invoice: " + std::string(e.what()));
        return Invoice{};
    }
}

bool Database::updateInvoiceStatus(const std::string& invoiceNumber, const std::string& status) {
    LOG_INFO("Updating invoice status: " + invoiceNumber + " to " + status);
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return false;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "UPDATE invoices SET status = $1 WHERE invoice_number = $2";
        txn.exec_params(query, status, invoiceNumber);
        
        txn.commit();
        LOG_INFO("Invoice status updated successfully");
        return true;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error updating invoice status: " + std::string(e.what()));
        return false;
    }
}

bool Database::updateInvoiceBalance(const std::string& invoiceNumber, double paymentAmount) {
    LOG_INFO("Updating invoice balance: " + invoiceNumber + " by " + std::to_string(paymentAmount));
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return false;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "UPDATE invoices SET amount = amount - $1 WHERE invoice_number = $2";
        txn.exec_params(query, paymentAmount, invoiceNumber);
        
        txn.commit();
        LOG_INFO("Invoice balance updated successfully");
        return true;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error updating invoice balance: " + std::string(e.what()));
        return false;
    }
}

bool Database::createPayment(const Payment& payment) {
    LOG_INFO("Creating payment for invoice ID: " + std::to_string(payment.invoiceId));
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return false;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "INSERT INTO payments (invoice_id, receipt_file, payment_amount, payment_date, reference, file_hash, file_size, mime_type) "
                           "VALUES ($1, $2, $3, $4, $5, $6, $7, $8)";
        
        txn.exec_params(query, payment.invoiceId, payment.receiptFile, 
                       payment.paymentAmount, payment.paymentDate, payment.reference,
                       payment.fileHash, payment.fileSize, payment.mimeType);
        
        txn.commit();
        LOG_INFO("Payment created successfully");
        return true;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error creating payment: " + std::string(e.what()));
        return false;
    }
}

std::vector<Payment> Database::getPaymentsByInvoice(int invoiceId) {
    LOG_INFO("Fetching payments for invoice ID: " + std::to_string(invoiceId));
    
    std::vector<Payment> payments;
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return payments;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "SELECT id, invoice_id, receipt_file, payment_amount, payment_date, reference, file_hash, file_size, mime_type "
                           "FROM payments WHERE invoice_id = $1 ORDER BY created_at DESC";
        
        pqxx::result result = txn.exec_params(query, invoiceId);
        
        for (const auto& row : result) {
            Payment payment;
            payment.id = row["id"].as<int>();
            payment.invoiceId = row["invoice_id"].as<int>();
            payment.receiptFile = row["receipt_file"].as<std::string>();
            payment.paymentAmount = row["payment_amount"].as<double>();
            payment.paymentDate = row["payment_date"].as<std::string>();
            payment.reference = row["reference"].as<std::string>();
            payment.fileHash = row["file_hash"].as<std::string>();
            payment.fileSize = row["file_size"].as<int64_t>();
            payment.mimeType = row["mime_type"].as<std::string>();
            payments.push_back(payment);
        }
        
        txn.commit();
        LOG_INFO("Fetched " + std::to_string(payments.size()) + " payments");
        return payments;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error fetching payments: " + std::string(e.what()));
        return payments;
    }
}

bool Database::isDuplicatePayment(const std::string& invoiceNumber, const std::string& reference) {
    LOG_INFO("Checking for duplicate payment: " + invoiceNumber + " ref: " + reference);
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return false;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "SELECT COUNT(*) FROM payments p "
                           "JOIN invoices i ON p.invoice_id = i.id "
                           "WHERE i.invoice_number = $1 AND p.reference = $2";
        
        pqxx::result result = txn.exec_params(query, invoiceNumber, reference);
        
        int count = result[0][0].as<int>();
        txn.commit();
        
        bool isDuplicate = count > 0;
        if (isDuplicate) {
            LOG_WARNING("Duplicate payment detected");
        }
        
        return isDuplicate;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error checking duplicate payment: " + std::string(e.what()));
        return false;
    }
}

bool Database::isDuplicateFileHash(const std::string& fileHash) {
    LOG_INFO("Checking for duplicate file hash: " + fileHash);
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return false;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "SELECT COUNT(*) FROM payments WHERE file_hash = $1";
        pqxx::result result = txn.exec_params(query, fileHash);
        
        int count = result[0][0].as<int>();
        txn.commit();
        
        bool isDuplicate = count > 0;
        if (isDuplicate) {
            LOG_WARNING("Duplicate file hash detected");
        }
        
        return isDuplicate;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error checking duplicate file hash: " + std::string(e.what()));
        return false;
    }
}

bool Database::createOCRResult(const std::string& receiptFile, const std::string& fileHash,
                               const std::string& rawText, double confidence, int processingTimeMs) {
    LOG_INFO("Creating OCR result for: " + receiptFile);
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return false;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "INSERT INTO ocr_results (receipt_file, file_hash, raw_text, confidence_score, processing_time_ms) "
                           "VALUES ($1, $2, $3, $4, $5) "
                           "ON CONFLICT (file_hash) DO UPDATE SET raw_text = EXCLUDED.raw_text, "
                           "confidence_score = EXCLUDED.confidence_score, processing_time_ms = EXCLUDED.processing_time_ms";
        
        txn.exec_params(query, receiptFile, fileHash, rawText, confidence, processingTimeMs);
        
        txn.commit();
        LOG_INFO("OCR result created successfully");
        return true;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error creating OCR result: " + std::string(e.what()));
        return false;
    }
}

bool Database::createReconciliationRecord(const std::string& invoiceNumber, 
                                          const std::string& receiptName, 
                                          const std::string& status,
                                          double confidence) {
    LOG_INFO("Creating reconciliation record for invoice: " + invoiceNumber);
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return false;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "INSERT INTO reconciliation_history (invoice_number, receipt_name, status, ocr_confidence, matched_at) "
                           "VALUES ($1, $2, $3, $4, NOW())";
        
        txn.exec_params(query, invoiceNumber, receiptName, status, confidence);
        
        txn.commit();
        LOG_INFO("Reconciliation record created successfully");
        return true;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error creating reconciliation record: " + std::string(e.what()));
        return false;
    }
}

bool Database::addToReviewQueue(const std::string& receiptFile, const std::string& fileHash,
                                const std::string& invoiceNumber, double amount, const std::string& date,
                                double confidence, const std::string& reason) {
    LOG_INFO("Adding to review queue: " + receiptFile);
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return false;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "INSERT INTO review_queue (receipt_file, file_hash, invoice_number, extracted_amount, "
                           "extracted_date, confidence_score, reason, status) "
                           "VALUES ($1, $2, $3, $4, $5, $6, $7, 'PENDING')";
        
        txn.exec_params(query, receiptFile, fileHash, invoiceNumber, amount, date, confidence, reason);
        
        txn.commit();
        LOG_INFO("Added to review queue successfully");
        return true;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error adding to review queue: " + std::string(e.what()));
        return false;
    }
}

bool Database::recordMetric(const std::string& metricName, double metricValue, 
                           const std::string& metricType) {
    LOG_DEBUG("Recording metric: " + metricName + " = " + std::to_string(metricValue));
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return false;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "INSERT INTO processing_metrics (metric_name, metric_value, metric_type, tags) "
                           "VALUES ($1, $2, $3, '{}'::jsonb)";
        
        txn.exec_params(query, metricName, metricValue, metricType);
        
        txn.commit();
        return true;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error recording metric: " + std::string(e.what()));
        return false;
    }
}

bool Database::createAuditLog(const std::string& action, const std::string& description, 
                              const std::string& userName) {
    LOG_INFO("Creating audit log: " + action);
    
    if (!isConnected()) {
        LOG_ERROR("Database not connected");
        return false;
    }
    
    try {
        pqxx::work txn(*connection_);
        
        std::string query = "INSERT INTO audit_logs (action, description, user_name, timestamp) "
                           "VALUES ($1, $2, $3, NOW())";
        
        txn.exec_params(query, action, description, userName);
        
        txn.commit();
        LOG_INFO("Audit log created successfully");
        return true;
        
    } catch (const std::exception& e) {
        LOG_ERROR("Error creating audit log: " + std::string(e.what()));
        return false;
    }
}
