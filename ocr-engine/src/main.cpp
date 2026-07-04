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
                   const std::string& customerName = "", const std::string& phoneNumber = "",
                   const std::string& email = "", const std::string& rawText = "",
                   double confidence = 0.0, int processingTimeMs = 0) {
    json response;
    
    // Generate unique request ID
    std::string requestId = "REQ-" + std::to_string(std::chrono::duration_cast<std::chrono::nanoseconds>(
        std::chrono::high_resolution_clock::now().time_since_epoch()).count());
    
    // Generate receipt ID from file path
    std::string receiptId = imagePath;
    size_t lastSlash = receiptId.find_last_of("/\\");
    if (lastSlash != std::string::npos) {
        receiptId = receiptId.substr(lastSlash + 1);
    }
    
    response["receipt_id"] = receiptId;
    response["request_id"] = requestId;
    
    // OCR metadata section
    response["ocr"]["confidence"] = confidence;
    response["ocr"]["engine"] = "Tesseract";
    response["ocr"]["processing_time_ms"] = processingTimeMs;
    
    // Extracted fields section
    response["fields"]["invoice_number"] = invoiceNumber;
    response["fields"]["amount"] = amountPaid;
    response["fields"]["date"] = paymentDate;
    response["fields"]["customer"] = customerName;
    response["fields"]["reference"] = bankReference;
    response["fields"]["phone_number"] = phoneNumber;
    response["fields"]["email"] = email;
    
    // Raw OCR text
    response["raw_text"] = rawText;
    
    // Image name
    response["image_name"] = receiptId;
    
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

bool processReceipt(const std::string& imagePath, OCREngine& ocr, 
                    ImageProcessor& imgProcessor, ReceiptParser& parser, json& result) {
    auto startTime = std::chrono::high_resolution_clock::now();
    LOG_INFO("Processing receipt: " + imagePath);
    result = buildResponse(imagePath, false, "Processing started");
    
    // Step 1: Preprocess image
    cv::Mat processedImage = imgProcessor.preprocessImage(imagePath);
    if (processedImage.empty()) {
        LOG_ERROR("Image preprocessing failed");
        auto endTime = std::chrono::high_resolution_clock::now();
        auto totalDuration = std::chrono::duration_cast<std::chrono::milliseconds>(endTime - startTime).count();
        result = buildResponse(imagePath, false, "Image preprocessing failed", "", 0.0, "", "", "", "", "", "", 0.0, static_cast<int>(totalDuration));
        return false;
    }
    
    // Step 2: Extract text using OCR with confidence
    OCREngine::OCRResult ocrResult = ocr.extractTextWithConfidenceFromMat(processedImage);
    if (ocrResult.text.empty()) {
        LOG_ERROR("OCR extraction failed");
        auto endTime = std::chrono::high_resolution_clock::now();
        auto totalDuration = std::chrono::duration_cast<std::chrono::milliseconds>(endTime - startTime).count();
        result = buildResponse(imagePath, false, "OCR extraction failed", "", 0.0, "", "", "", "", "", "", ocrResult.confidence, static_cast<int>(totalDuration));
        return false;
    }
    
    LOG_DEBUG("OCR Text extracted: " + ocrResult.text.substr(0, 200) + "...");
    LOG_INFO("OCR Confidence: " + std::to_string(ocrResult.confidence));
    
    // Step 3: Parse receipt data
    ReceiptData receiptData = parser.parseReceipt(ocrResult.text, ocrResult.confidence);
    
    // Step 4: Extract phone and email from OCR text
    std::string extractedPhone = parser.extractPhoneNumber(ocrResult.text);
    std::string extractedEmail = parser.extractEmail(ocrResult.text);
    
    // Step 5: Return all OCR results without validation
    auto endTime = std::chrono::high_resolution_clock::now();
    auto totalDuration = std::chrono::duration_cast<std::chrono::milliseconds>(endTime - startTime).count();
    
    result = buildResponse(imagePath, true, "OCR processing completed", 
                           receiptData.invoiceNumber, receiptData.amountPaid, 
                           receiptData.paymentDate, receiptData.bankReference, 
                           receiptData.customerName, extractedPhone, extractedEmail, 
                           ocrResult.text, receiptData.confidence, 
                           static_cast<int>(totalDuration));
    
    LOG_INFO("OCR processing completed in " + std::to_string(totalDuration) + "ms");
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
    
    // Database operations are now handled by the company API
    // OCR engine only performs image processing and text extraction
    LOG_INFO("Database operations delegated to company API");
    
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
        if (processReceipt(imagePath, ocr, imgProcessor, parser, result)) {
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
                        
                        json result;
                        if (processReceipt(imagePath, ocr, imgProcessor, parser, result)) {
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
    
    return 0;
}
