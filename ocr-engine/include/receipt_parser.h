#ifndef RECEIPT_PARSER_H
#define RECEIPT_PARSER_H

#include <string>
#include <regex>
#include <map>

struct ReceiptData {
    std::string invoiceNumber;
    double amountPaid;
    std::string paymentDate;
    std::string bankReference;
    std::string customerName;
    double confidence;
    bool isValid;
    bool requiresManualReview;
};

class ReceiptParser {
public:
    ReceiptParser();
    ~ReceiptParser();

    // Parse OCR text into structured data
    ReceiptData parseReceipt(const std::string& ocrText, double confidence = 0.0);
    
    // Validation methods
    bool isValidInvoiceNumber(const std::string& invoiceNumber);
    bool isValidAmount(const std::string& amountStr);
    bool isValidDate(const std::string& dateStr);
    
    // Extract specific fields
    std::string extractInvoiceNumber(const std::string& text);
    std::string extractAmount(const std::string& text);
    std::string extractDate(const std::string& text);
    std::string extractBankReference(const std::string& text);
    std::string extractCustomerName(const std::string& text);

private:
    // Regex patterns
    std::regex invoiceNumberPattern_;
    std::regex amountPattern_;
    std::regex datePattern_;
    std::regex bankRefPattern_;
    std::regex customerNamePattern_;
    
    // Helper methods
    double parseAmount(const std::string& amountStr);
    std::string formatDate(const std::string& dateStr);
};

#endif // RECEIPT_PARSER_H
