#include "image_processor.h"
#include "ocr_engine.h"
#include "receipt_parser.h"
#include "database.h"
#include "logger.h"
#include "file_utils.h"
#include <iostream>
#include <fstream>
#include <nlohmann/json.hpp>
#include <filesystem>
#include <chrono>
#include <cstdlib>

using json = nlohmann::json;

json buildResponse(const std::string& imagePath, bool success, const std::string& message,
                   const std::string& invoiceNumber = "", double amountPaid = 0.0,
                   const std::string& paymentDate = "", const std::string& bankReference = "",
                   const std::string& customerName = "", bool isValid = false, double confidence = 0.0) {
    json response;
    response["file_path"] = imagePath;
    response["success"] = success;
    response["message"] = message;
    response["invoice_number"] = invoiceNumber;
    response["amount_paid"] = amountPaid;
    response["payment_date"] = paymentDate;
    response["bank_reference"] = bankReference;
    response["customer_name"] = customerName;
    response["is_valid"] = isValid;
    response["confidence"] = confidence;
    return response;
}

struct Config {
    std::string dbHost;
    int dbPort;
    std::string dbName;
    std::string dbUser;
    std::string dbPassword;
    std::string tesseractDataPath;
    std::string tesseractLanguage;
    std::string receiptsDir;
    std::string logFile;
};

Config loadConfig(const std::string& configPath) {
    LOG_INFO("Loading configuration from environment and file: " + configPath);
    
    Config config;
    
    // Helper function to get environment variable with fallback
    auto getEnv = [](const char* name, const std::string& defaultValue) -> std::string {
        const char* value = std::getenv(name);
        return value ? std::string(value) : defaultValue;
    };
    
    // Load from environment variables (highest priority)
    config.dbHost = getEnv("DB_HOST", "localhost");
    config.dbPort = std::stoi(getEnv("DB_PORT", "5432"));
    config.dbName = getEnv("DB_NAME", "receipt_reconciliation");
    config.dbUser = getEnv("DB_USER", "postgres");
    config.dbPassword = getEnv("DB_PASSWORD", "postgres");
    config.tesseractDataPath = getEnv("TESSERACT_DATA_PATH", "");
    config.tesseractLanguage = getEnv("TESSERACT_LANGUAGE", "eng");
    config.receiptsDir = getEnv("RECEIPTS_DIR", "../receipts");
    config.logFile = getEnv("LOG_FILE", "ocr_engine.log");
    
    // Try to load from config file as fallback
    try {
        std::ifstream configFile(configPath);
        if (configFile.is_open()) {
            json j;
            configFile >> j;
            
            // Only use config file values if environment variables are not set
            if (std::getenv("DB_HOST") == nullptr && j.contains("database")) {
                auto& db = j["database"];
                if (db.contains("host")) config.dbHost = db["host"];
                if (db.contains("port")) config.dbPort = db["port"];
                if (db.contains("dbname")) config.dbName = db["dbname"];
                if (db.contains("user")) config.dbUser = db["user"];
                if (db.contains("password")) config.dbPassword = db["password"];
            }
            
            if (std::getenv("TESSERACT_DATA_PATH") == nullptr && j.contains("tesseract")) {
                auto& tess = j["tesseract"];
                if (tess.contains("data_path")) config.tesseractDataPath = tess["data_path"];
                if (tess.contains("language")) config.tesseractLanguage = tess["language"];
            }
            
            if (std::getenv("RECEIPTS_DIR") == nullptr && j.contains("receipts_dir")) {
                config.receiptsDir = j["receipts_dir"];
            }
            
            if (std::getenv("LOG_FILE") == nullptr && j.contains("log_file")) {
                config.logFile = j["log_file"];
            }
            
            LOG_INFO("Configuration loaded successfully (environment variables take precedence)");
        } else {
            LOG_INFO("Config file not found, using environment variables and defaults");
        }
    } catch (const std::exception& e) {
        LOG_WARNING("Error loading config file: " + std::string(e.what()) + ", using environment variables");
    }
    
    LOG_INFO("Final configuration - DB: " + config.dbHost + ":" + std::to_string(config.dbPort) + 
             "/" + config.dbName + ", Receipts: " + config.receiptsDir);
    
    return config;
}

bool processReceipt(const std::string& imagePath, Database& db, OCREngine& ocr, 
                    ImageProcessor& imgProcessor, ReceiptParser& parser, json& result) {
    auto startTime = std::chrono::high_resolution_clock::now();
    LOG_INFO("Processing receipt: " + imagePath);
    result = buildResponse(imagePath, false, "Processing started");
    
    // Step 1: Calculate file hash for duplicate detection
    std::string fileHash = FileUtils::calculateSHA256(imagePath);
    if (fileHash.empty()) {
        LOG_ERROR("Failed to calculate file hash");
        result = buildResponse(imagePath, false, "Failed to calculate file hash");
        return false;
    }
    
    // Step 2: Check for duplicate file
    if (db.isDuplicateFileHash(fileHash)) {
        LOG_ERROR("Duplicate file detected - file already processed");
        db.recordMetric("duplicate_receipts_detected", 1.0, "counter");
        result = buildResponse(imagePath, false, "Duplicate file detected");
        return false;
    }
    
    // Step 3: Get file metadata
    int64_t fileSize = FileUtils::getFileSize(imagePath);
    std::string mimeType = FileUtils::getMimeType(imagePath);
    
    // Step 4: Preprocess image
    cv::Mat processedImage = imgProcessor.preprocessImage(imagePath);
    if (processedImage.empty()) {
        LOG_ERROR("Image preprocessing failed");
        db.recordMetric("preprocessing_failures", 1.0, "counter");
        result = buildResponse(imagePath, false, "Image preprocessing failed");
        return false;
    }
    
    // Step 5: Extract text using OCR with confidence
    OCREngine::OCRResult ocrResult = ocr.extractTextWithConfidenceFromMat(processedImage);
    if (ocrResult.text.empty()) {
        LOG_ERROR("OCR extraction failed");
        db.recordMetric("ocr_failures", 1.0, "counter");
        result = buildResponse(imagePath, false, "OCR extraction failed", "", 0.0, "", "", "", false, ocrResult.confidence);
        return false;
    }
    
    LOG_DEBUG("OCR Text extracted: " + ocrResult.text.substr(0, 200) + "...");
    LOG_INFO("OCR Confidence: " + std::to_string(ocrResult.confidence));
    
    // Step 6: Store OCR result for debugging
    auto ocrEndTime = std::chrono::high_resolution_clock::now();
    auto ocrDuration = std::chrono::duration_cast<std::chrono::milliseconds>(ocrEndTime - startTime).count();
    db.createOCRResult(imagePath, fileHash, ocrResult.text, ocrResult.confidence, static_cast<int>(ocrDuration));
    
    // Step 7: Parse receipt data with confidence
    ReceiptData receiptData = parser.parseReceipt(ocrResult.text, ocrResult.confidence);
    if (!receiptData.isValid) {
        LOG_ERROR("Receipt parsing failed - invalid data");
        db.recordMetric("parsing_failures", 1.0, "counter");
        
        // Add to review queue for manual inspection
        db.addToReviewQueue(imagePath, fileHash, "", 0.0, "", ocrResult.confidence, "Parsing failed");
        result = buildResponse(imagePath, false, "Parsing failed", receiptData.invoiceNumber,
                               receiptData.amountPaid, receiptData.paymentDate,
                               receiptData.bankReference, receiptData.customerName,
                               false, receiptData.confidence);
        return false;
    }
    
    LOG_INFO("Parsed receipt - Invoice: " + receiptData.invoiceNumber + 
             ", Amount: " + std::to_string(receiptData.amountPaid) +
             ", Confidence: " + std::to_string(receiptData.confidence) +
             ", Manual Review: " + (receiptData.requiresManualReview ? "Yes" : "No"));
    
    // Step 8: Handle low confidence - send to review queue
    if (receiptData.requiresManualReview) {
        LOG_WARNING("Low confidence - sending to review queue");
        db.addToReviewQueue(imagePath, fileHash, receiptData.invoiceNumber, 
                           receiptData.amountPaid, receiptData.paymentDate, 
                           receiptData.confidence, "Low OCR confidence");
        db.recordMetric("manual_review_required", 1.0, "counter");
        db.createReconciliationRecord(receiptData.invoiceNumber, imagePath, "PENDING_REVIEW", receiptData.confidence);
        result = buildResponse(imagePath, true, "Needs manual review", receiptData.invoiceNumber,
                               receiptData.amountPaid, receiptData.paymentDate,
                               receiptData.bankReference, receiptData.customerName,
                               false, receiptData.confidence);
        return true; // Return true as it was handled (just needs review)
    }
    
    // Step 9: Lookup invoice in database
    Invoice invoice = db.getInvoiceByNumber(receiptData.invoiceNumber);
    if (invoice.id == 0) {
        LOG_ERROR("Invoice not found: " + receiptData.invoiceNumber);
        db.addToReviewQueue(imagePath, fileHash, receiptData.invoiceNumber, 
                           receiptData.amountPaid, receiptData.paymentDate, 
                           receiptData.confidence, "Invoice not found");
        db.createReconciliationRecord(receiptData.invoiceNumber, imagePath, "UNMATCHED", receiptData.confidence);
        db.recordMetric("invoice_not_found", 1.0, "counter");
        result = buildResponse(imagePath, false, "Invoice not found", receiptData.invoiceNumber,
                               receiptData.amountPaid, receiptData.paymentDate,
                               receiptData.bankReference, receiptData.customerName,
                               false, receiptData.confidence);
        return false;
    }
    
    LOG_INFO("Invoice found - ID: " + std::to_string(invoice.id) + 
             ", Customer: " + invoice.customerName);
    
    // Step 10: Validate payment amount
    if (receiptData.amountPaid > invoice.amount) {
        LOG_WARNING("Payment amount exceeds invoice amount");
        db.addToReviewQueue(imagePath, fileHash, receiptData.invoiceNumber, 
                           receiptData.amountPaid, receiptData.paymentDate, 
                           receiptData.confidence, "Payment exceeds invoice amount");
    }
    
    // Step 11: Check for duplicate payment
    if (db.isDuplicatePayment(receiptData.invoiceNumber, receiptData.bankReference)) {
        LOG_ERROR("Duplicate payment detected");
        db.recordMetric("duplicate_payments_detected", 1.0, "counter");
        return false;
    }
    
    // Step 12: Create payment record with file metadata
    Payment payment;
    payment.invoiceId = invoice.id;
    payment.receiptFile = imagePath;
    payment.paymentAmount = receiptData.amountPaid;
    payment.paymentDate = receiptData.paymentDate;
    payment.reference = receiptData.bankReference;
    payment.fileHash = fileHash;
    payment.fileSize = fileSize;
    payment.mimeType = mimeType;
    
    if (!db.createPayment(payment)) {
        LOG_ERROR("Failed to create payment record");
        db.recordMetric("payment_creation_failures", 1.0, "counter");
        return false;
    }
    
    // Step 13: Update invoice status
    if (!db.updateInvoiceStatus(receiptData.invoiceNumber, "PAID")) {
        LOG_ERROR("Failed to update invoice status");
        return false;
    }
    
    // Step 14: Update invoice balance
    if (!db.updateInvoiceBalance(receiptData.invoiceNumber, receiptData.amountPaid)) {
        LOG_ERROR("Failed to update invoice balance");
        return false;
    }
    
    // Step 15: Create reconciliation record with confidence
    if (!db.createReconciliationRecord(receiptData.invoiceNumber, imagePath, "MATCHED", receiptData.confidence)) {
        LOG_ERROR("Failed to create reconciliation record");
        return false;
    }
    
    // Step 16: Create audit log
    db.createAuditLog("RECONCILIATION", 
                     "Receipt " + imagePath + " matched to invoice " + receiptData.invoiceNumber + 
                     " with confidence " + std::to_string(receiptData.confidence),
                     "system");
    
    // Step 17: Record metrics
    auto endTime = std::chrono::high_resolution_clock::now();
    auto totalDuration = std::chrono::duration_cast<std::chrono::milliseconds>(endTime - startTime).count();
    
    db.recordMetric("receipts_processed", 1.0, "counter");
    db.recordMetric("successful_reconciliations", 1.0, "counter");
    db.recordMetric("processing_time_ms", static_cast<double>(totalDuration), "gauge");
    db.recordMetric("ocr_confidence", receiptData.confidence, "gauge");
    
    result = buildResponse(imagePath, true, "Receipt processed successfully", receiptData.invoiceNumber,
                           receiptData.amountPaid, receiptData.paymentDate,
                           receiptData.bankReference, receiptData.customerName,
                           true, receiptData.confidence);
    LOG_INFO("Receipt processed and reconciled successfully in " + std::to_string(totalDuration) + "ms");
    return true;
}

int main(int argc, char* argv[]) {
    std::cout << "=== Receipt-to-Invoice Reconciliation System ===" << std::endl;
    std::cout << "OCR Engine v1.0" << std::endl << std::endl;
    
    // Load configuration
    std::string configPath = "config.json";
    if (argc > 1) {
        configPath = argv[1];
    }
    
    Config config = loadConfig(configPath);
    
    // Configure logger
    Logger::getInstance().setLogFile(config.logFile);
    Logger::getInstance().setLogLevel(LogLevel::INFO);
    
    LOG_INFO("Starting OCR Engine");
    
    // Initialize database connection
    Database db(config.dbHost, config.dbPort, config.dbName, 
                config.dbUser, config.dbPassword);
    if (!db.connect()) {
        LOG_CRITICAL("Failed to connect to database");
        return 1;
    }
    
    // Initialize OCR engine
    OCREngine ocr(config.tesseractDataPath, config.tesseractLanguage);
    if (!ocr.initialize()) {
        LOG_CRITICAL("Failed to initialize OCR engine");
        return 1;
    }
    
    // Initialize image processor
    ImageProcessor imgProcessor;
    
    // Initialize receipt parser
    ReceiptParser parser;
    
    LOG_INFO("All components initialized successfully");
    
    // Process receipts from directory or single file
    if (argc > 2) {
        // Process single file
        std::string imagePath = argv[2];
        LOG_INFO("Processing single receipt: " + imagePath);
        
        json result;
        if (processReceipt(imagePath, db, ocr, imgProcessor, parser, result)) {
            std::cout << result.dump(2) << std::endl;
        } else {
            std::cout << result.dump(2) << std::endl;
            return 1;
        }
    } else {
        // Process all receipts in directory
        std::string receiptsDir = config.receiptsDir;
        LOG_INFO("Processing all receipts in directory: " + receiptsDir);
        
        int processed = 0;
        int failed = 0;
        
        try {
            for (const auto& entry : std::filesystem::directory_iterator(receiptsDir)) {
                if (entry.is_regular_file()) {
                    std::string ext = entry.path().extension().string();
                    if (ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".JPG" || 
                        ext == ".JPEG" || ext == ".PNG") {
                        std::string imagePath = entry.path().string();
                        
                        if (processReceipt(imagePath, db, ocr, imgProcessor, parser)) {
                            processed++;
                        } else {
                            failed++;
                        }
                    }
                }
            }
        } catch (const std::exception& e) {
            LOG_ERROR("Error iterating receipts directory: " + std::string(e.what()));
        }
        
        std::cout << "Processing complete: " << processed << " succeeded, " 
                  << failed << " failed" << std::endl;
    }
    
    LOG_INFO("OCR Engine shutting down");
    db.disconnect();
    
    return 0;
}
