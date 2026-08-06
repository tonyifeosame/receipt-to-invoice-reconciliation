#ifndef RECEIPT_PARSER_H
#define RECEIPT_PARSER_H

#include <string>
#include <regex>
#include <map>
#include <vector>

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
    // Label patterns. Extraction is line oriented: a label locates the line, and
    // the value is then read from that line (or the line below it). Scanning the
    // whole document with a single pattern is what caused fields to be filled
    // with the first loosely matching text anywhere on the receipt.
    std::regex invoiceLabelPattern_;
    std::regex invoiceTokenPattern_;
    std::regex invoiceStandalonePattern_;
    std::regex numericFallbackPattern_;

    std::regex moneyPattern_;

    std::regex isoDatePattern_;
    std::regex numericDatePattern_;
    std::regex dayMonthNamePattern_;
    std::regex monthNameDayPattern_;

    std::regex bankRefPattern_;
    std::regex customerNamePattern_;
    std::regex phonePattern_;
    std::regex emailPattern_;

    // Helper methods
    double parseAmount(const std::string& amountStr);
    std::string formatDate(const std::string& dateStr);
};

#endif // RECEIPT_PARSER_H
