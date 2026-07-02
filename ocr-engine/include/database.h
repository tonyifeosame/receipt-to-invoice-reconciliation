#ifndef DATABASE_H
#define DATABASE_H

#include <pqxx/pqxx>
#include <string>
#include <memory>
#include <vector>

struct Invoice {
    int id;
    std::string invoiceNumber;
    std::string customerName;
    double amount;
    std::string status;
    std::string dueDate;
};

struct Payment {
    int id;
    int invoiceId;
    std::string receiptFile;
    double paymentAmount;
    std::string paymentDate;
    std::string reference;
    std::string fileHash;
    int64_t fileSize;
    std::string mimeType;
};

class Database {
public:
    Database(const std::string& host, int port, const std::string& dbname, 
             const std::string& user, const std::string& password);
    ~Database();

    // Connection management
    bool connect();
    void disconnect();
    bool isConnected() const;

    // Invoice operations
    Invoice getInvoiceByNumber(const std::string& invoiceNumber);
    bool updateInvoiceStatus(const std::string& invoiceNumber, const std::string& status);
    bool updateInvoiceBalance(const std::string& invoiceNumber, double paymentAmount);
    
    // Payment operations
    bool createPayment(const Payment& payment);
    std::vector<Payment> getPaymentsByInvoice(int invoiceId);
    bool isDuplicatePayment(const std::string& invoiceNumber, const std::string& reference);
    bool isDuplicateFileHash(const std::string& fileHash);
    
    // OCR result storage
    bool createOCRResult(const std::string& receiptFile, const std::string& fileHash,
                         const std::string& rawText, double confidence, int processingTimeMs);
    
    // Reconciliation history with confidence
    bool createReconciliationRecord(const std::string& invoiceNumber, 
                                     const std::string& receiptName, 
                                     const std::string& status,
                                     double confidence = 0.0);
    
    // Manual review queue
    bool addToReviewQueue(const std::string& receiptFile, const std::string& fileHash,
                         const std::string& invoiceNumber, double amount, const std::string& date,
                         double confidence, const std::string& reason);
    
    // Metrics
    bool recordMetric(const std::string& metricName, double metricValue, 
                     const std::string& metricType = "counter");
    
    // Audit logging
    bool createAuditLog(const std::string& action, const std::string& description, 
                        const std::string& userName);

private:
    std::unique_ptr<pqxx::connection> connection_;
    std::string connectionString_;
    
    // Helper method to build connection string
    std::string buildConnectionString(const std::string& host, int port, 
                                      const std::string& dbname, 
                                      const std::string& user, 
                                      const std::string& password);
};

#endif // DATABASE_H
