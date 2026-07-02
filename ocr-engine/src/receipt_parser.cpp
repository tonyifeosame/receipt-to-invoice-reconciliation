#include "receipt_parser.h"
#include "logger.h"
#include <sstream>
#include <iomanip>
#include <algorithm>
#include <cctype>

ReceiptParser::ReceiptParser() {
    // Initialize regex patterns for common receipt formats
    // Invoice number patterns (INV-001, Invoice #123, etc.)
    invoiceNumberPattern_ = std::regex(R"((?:invoice|inv|bill)[\s:#-]*(\d+[A-Za-z0-9]*))", 
                                        std::regex_constants::icase);
    
    // Amount patterns ($123.45, 123.45, EUR 123.45, etc.)
    amountPattern_ = std::regex(R"((?:[\$€£]?\s*)(\d{1,3}(?:,\d{3})*(?:\.\d{2})?|\d+\.\d{2}))");
    
    // Date patterns (DD/MM/YYYY, MM/DD/YYYY, YYYY-MM-DD, etc.)
    datePattern_ = std::regex(R"((\d{1,2}[/-]\d{1,2}[/-]\d{2,4}|\d{4}[/-]\d{1,2}[/-]\d{1,2}))");
    
    // Bank reference patterns (REF: 12345, Reference: ABC123, etc.)
    bankRefPattern_ = std::regex(R"((?:ref|reference|transaction)[\s:#-]*([A-Za-z0-9-]+))", 
                                  std::regex_constants::icase);
    
    // Customer name patterns (typically after "Pay to", "Customer", etc.)
    customerNamePattern_ = std::regex(R"((?:customer|pay\s+to|recipient)[\s:]+([A-Za-z\s]+))", 
                                       std::regex_constants::icase);
    
    LOG_INFO("ReceiptParser initialized");
}

ReceiptParser::~ReceiptParser() {
    LOG_INFO("ReceiptParser destroyed");
}

ReceiptData ReceiptParser::parseReceipt(const std::string& ocrText, double confidence) {
    LOG_INFO("Parsing receipt text with confidence: " + std::to_string(confidence));
    
    ReceiptData data;
    data.isValid = false;
    data.confidence = confidence;
    data.requiresManualReview = false;
    
    // Extract fields
    data.invoiceNumber = extractInvoiceNumber(ocrText);
    std::string amountStr = extractAmount(ocrText);
    data.paymentDate = extractDate(ocrText);
    data.bankReference = extractBankReference(ocrText);
    data.customerName = extractCustomerName(ocrText);
    
    // Parse amount
    if (!amountStr.empty()) {
        data.amountPaid = parseAmount(amountStr);
    } else {
        data.amountPaid = 0.0;
    }
    
    // Validate required fields
    bool hasInvoice = isValidInvoiceNumber(data.invoiceNumber);
    bool hasAmount = data.amountPaid > 0;
    bool hasDate = isValidDate(data.paymentDate);
    
    // Check if manual review is needed based on confidence threshold
    const double CONFIDENCE_THRESHOLD = 85.0;
    if (confidence > 0 && confidence < CONFIDENCE_THRESHOLD) {
        data.requiresManualReview = true;
        LOG_WARNING("Low OCR confidence (" + std::to_string(confidence) + 
                    ") - manual review required");
    }
    
    if (hasInvoice && hasAmount) {
        data.isValid = true;
        LOG_INFO("Receipt parsed successfully - Invoice: " + data.invoiceNumber + 
                 ", Amount: " + std::to_string(data.amountPaid) +
                 ", Confidence: " + std::to_string(confidence) +
                 ", Manual Review: " + (data.requiresManualReview ? "Yes" : "No"));
    } else {
        LOG_WARNING("Receipt validation failed - Invoice: " + data.invoiceNumber + 
                    ", Amount: " + std::to_string(data.amountPaid) + 
                    ", Date: " + data.paymentDate);
    }
    
    return data;
}

bool ReceiptParser::isValidInvoiceNumber(const std::string& invoiceNumber) {
    if (invoiceNumber.empty() || invoiceNumber.length() < 3) {
        return false;
    }
    
    // Check if it contains at least one alphanumeric character
    bool hasAlnum = false;
    for (char c : invoiceNumber) {
        if (std::isalnum(c)) {
            hasAlnum = true;
            break;
        }
    }
    
    return hasAlnum;
}

bool ReceiptParser::isValidAmount(const std::string& amountStr) {
    if (amountStr.empty()) {
        return false;
    }
    
    try {
        double amount = parseAmount(amountStr);
        return amount > 0;
    } catch (...) {
        return false;
    }
}

bool ReceiptParser::isValidDate(const std::string& dateStr) {
    if (dateStr.empty()) {
        return false;
    }
    
    // Basic validation - check if it matches the date pattern
    std::smatch match;
    return std::regex_search(dateStr, match, datePattern_);
}

std::string ReceiptParser::extractInvoiceNumber(const std::string& text) {
    std::smatch match;
    if (std::regex_search(text, match, invoiceNumberPattern_) && match.size() > 1) {
        std::string invoice = match[1].str();
        // Remove any whitespace
        invoice.erase(std::remove_if(invoice.begin(), invoice.end(), ::isspace), invoice.end());
        LOG_DEBUG("Extracted invoice number: " + invoice);
        return invoice;
    }
    LOG_DEBUG("No invoice number found");
    return "";
}

std::string ReceiptParser::extractAmount(const std::string& text) {
    std::smatch match;
    if (std::regex_search(text, match, amountPattern_) && match.size() > 1) {
        std::string amount = match[1].str();
        // Remove commas from thousands separators
        amount.erase(std::remove(amount.begin(), amount.end(), ','), amount.end());
        LOG_DEBUG("Extracted amount: " + amount);
        return amount;
    }
    LOG_DEBUG("No amount found");
    return "";
}

std::string ReceiptParser::extractDate(const std::string& text) {
    std::smatch match;
    if (std::regex_search(text, match, datePattern_) && match.size() > 1) {
        std::string date = match[1].str();
        LOG_DEBUG("Extracted date: " + date);
        return formatDate(date);
    }
    LOG_DEBUG("No date found");
    return "";
}

std::string ReceiptParser::extractBankReference(const std::string& text) {
    std::smatch match;
    if (std::regex_search(text, match, bankRefPattern_) && match.size() > 1) {
        std::string ref = match[1].str();
        ref.erase(std::remove_if(ref.begin(), ref.end(), ::isspace), ref.end());
        LOG_DEBUG("Extracted bank reference: " + ref);
        return ref;
    }
    LOG_DEBUG("No bank reference found");
    return "";
}

std::string ReceiptParser::extractCustomerName(const std::string& text) {
    std::smatch match;
    if (std::regex_search(text, match, customerNamePattern_) && match.size() > 1) {
        std::string name = match[1].str();
        // Trim whitespace
        size_t start = name.find_first_not_of(" \t\n\r");
        size_t end = name.find_last_not_of(" \t\n\r");
        if (start != std::string::npos && end != std::string::npos) {
            name = name.substr(start, end - start + 1);
        }
        LOG_DEBUG("Extracted customer name: " + name);
        return name;
    }
    LOG_DEBUG("No customer name found");
    return "";
}

double ReceiptParser::parseAmount(const std::string& amountStr) {
    std::string cleaned = amountStr;
    // Remove any currency symbols and commas
    cleaned.erase(std::remove_if(cleaned.begin(), cleaned.end(), 
                                 [](char c) { return c == '$' || c == '€' || c == '£' || c == ','; }), 
                  cleaned.end());
    
    try {
        return std::stod(cleaned);
    } catch (const std::exception& e) {
        LOG_ERROR("Failed to parse amount: " + amountStr + " - " + e.what());
        return 0.0;
    }
}

std::string ReceiptParser::formatDate(const std::string& dateStr) {
    // Try to standardize date format to YYYY-MM-DD
    // This is a simplified implementation
    std::string result = dateStr;
    
    // Replace slashes with dashes
    std::replace(result.begin(), result.end(), '/', '-');
    
    return result;
}
