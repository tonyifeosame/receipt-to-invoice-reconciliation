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
    std::string extractPhoneNumber(const std::string& text);
    std::string extractEmail(const std::string& text);

private:
    // Regex patterns
    std::vector<std::regex> invoiceNumberPatterns_;
    std::regex amountPattern_;
    std::vector<std::regex> datePatterns_;
    std::regex bankRefPattern_;
    std::regex customerNamePattern_;
    std::regex phonePattern_;
    std::regex emailPattern_;
    
    // Pattern matching helpers
    std::string tryMultiplePatterns(const std::string& text, const std::vector<std::regex>& patterns);
    
    // Helper methods
    double parseAmount(const std::string& amountStr);
    std::string formatDate(const std::string& dateStr);
};

#endif // RECEIPT_PARSER_H
