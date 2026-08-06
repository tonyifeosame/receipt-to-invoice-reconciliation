#include "image_processor.h"
#include "ocr_engine.h"
#include "receipt_parser.h"
#include "logger.h"
#include <iostream>
#include <fstream>
#include <nlohmann/json.hpp>
#include <filesystem>
#include <chrono>
#include <cstdlib>
#include <cctype>
#include <string>
#include <vector>

using json = nlohmann::json;

// Builds the JSON document written to stdout. The caller signals success through
// the process exit code, so no success/message fields are carried here.
json buildResponse(const std::string& imagePath,
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

struct CLIOptions {
    std::string configPath = "config.json";
    std::string imagePath;
    bool singleFile = false;
    bool valid = true;
};

void printUsage(const char* programName) {
    std::cerr << "Usage: " << programName << " [--config <config.json>] [--file <receipt-image>]\n"
              << "       " << programName << " <receipt-image>            (single-file mode)\n"
              << "       " << programName << " <config.json> <receipt-image>  (legacy single-file mode)\n"
              << "       " << programName << "                            (batch mode over receipts_dir)\n\n"
              << "In single-file mode the extracted OCR JSON is written to stdout and nothing else;\n"
              << "all logging is written to stderr and to the log file." << std::endl;
}

bool hasJsonExtension(const std::string& path) {
    if (path.length() < 5) {
        return false;
    }
    std::string suffix = path.substr(path.length() - 5);
    for (char& c : suffix) {
        c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
    }
    return suffix == ".json";
}

CLIOptions parseArguments(int argc, char* argv[]) {
    CLIOptions options;
    std::vector<std::string> positional;

    for (int i = 1; i < argc; i++) {
        std::string arg = argv[i];

        if (arg == "--help" || arg == "-h") {
            options.valid = false;
            return options;
        }

        if (arg == "--config" || arg == "-c") {
            if (i + 1 >= argc) {
                std::cerr << "Error: " << arg << " requires a path argument" << std::endl;
                options.valid = false;
                return options;
            }
            options.configPath = argv[++i];
            continue;
        }

        if (arg == "--file" || arg == "-f") {
            if (i + 1 >= argc) {
                std::cerr << "Error: " << arg << " requires a path argument" << std::endl;
                options.valid = false;
                return options;
            }
            options.imagePath = argv[++i];
            options.singleFile = true;
            continue;
        }

        positional.push_back(arg);
    }

    // Positional fallbacks keep the historic invocations working:
    //   ocr_engine <config.json> <image>  -> config + single file
    //   ocr_engine <image>                -> single file
    //   ocr_engine <config.json>          -> batch mode with a custom config
    if (options.imagePath.empty()) {
        if (positional.size() >= 2) {
            options.configPath = positional[0];
            options.imagePath = positional[1];
            options.singleFile = true;
        } else if (positional.size() == 1) {
            if (hasJsonExtension(positional[0])) {
                options.configPath = positional[0];
            } else {
                options.imagePath = positional[0];
                options.singleFile = true;
            }
        }
    } else if (!positional.empty()) {
        options.configPath = positional[0];
    }

    return options;
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

bool processReceiptUnguarded(const std::string& imagePath, OCREngine& ocr,
                             ImageProcessor& imgProcessor, ReceiptParser& parser, json& result) {
    auto startTime = std::chrono::high_resolution_clock::now();
    LOG_INFO("Processing receipt: " + imagePath);
    result = buildResponse(imagePath);
    
    // Step 1: Preprocess image
    cv::Mat processedImage = imgProcessor.preprocessImage(imagePath);
    if (processedImage.empty()) {
        LOG_ERROR("Image preprocessing failed");
        auto endTime = std::chrono::high_resolution_clock::now();
        auto totalDuration = std::chrono::duration_cast<std::chrono::milliseconds>(endTime - startTime).count();
        result = buildResponse(imagePath, "", 0.0, "", "", "", "", "", "", 0.0, static_cast<int>(totalDuration));
        return false;
    }
    
    // Step 2: Extract text using OCR with confidence
    OCREngine::OCRResult ocrResult = ocr.extractTextWithConfidenceFromMat(processedImage);
    if (ocrResult.text.empty()) {
        LOG_ERROR("OCR extraction failed");
        auto endTime = std::chrono::high_resolution_clock::now();
        auto totalDuration = std::chrono::duration_cast<std::chrono::milliseconds>(endTime - startTime).count();
        result = buildResponse(imagePath, "", 0.0, "", "", "", "", "", "", ocrResult.confidence, static_cast<int>(totalDuration));
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
    
    result = buildResponse(imagePath, 
                           receiptData.invoiceNumber, receiptData.amountPaid, 
                           receiptData.paymentDate, receiptData.bankReference, 
                           receiptData.customerName, extractedPhone, extractedEmail, 
                           ocrResult.text, receiptData.confidence, 
                           static_cast<int>(totalDuration));
    
    LOG_INFO("OCR processing completed in " + std::to_string(totalDuration) + "ms");
    return true;
}

// Guarantees a JSON result for every receipt: OpenCV/Tesseract throw on malformed
// input, and an uncaught exception here would kill the process before the caller
// ever receives a parseable response.
bool processReceipt(const std::string& imagePath, OCREngine& ocr,
                    ImageProcessor& imgProcessor, ReceiptParser& parser, json& result) {
    try {
        return processReceiptUnguarded(imagePath, ocr, imgProcessor, parser, result);
    } catch (const std::exception& e) {
        LOG_ERROR("Unhandled error while processing " + imagePath + ": " + std::string(e.what()));
        result = buildResponse(imagePath);
        return false;
    }
}

int main(int argc, char* argv[]) {
    CLIOptions options = parseArguments(argc, argv);
    if (!options.valid) {
        printUsage(argv[0]);
        return 1;
    }

    // stdout is reserved for machine-readable JSON, so the banner goes to stderr.
    std::cerr << "=== Receipt-to-Invoice Reconciliation System ===" << std::endl;
    std::cerr << "OCR Engine v1.0" << std::endl << std::endl;

    Config config = loadConfig(options.configPath);
    
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
    if (options.singleFile) {
        const std::string& imagePath = options.imagePath;
        LOG_INFO("Processing single receipt: " + imagePath);

        json result;
        bool ok = processReceipt(imagePath, ocr, imgProcessor, parser, result);

        // Always emit the JSON document: a failed extraction is still a result the
        // caller forwards to the company API, and stdout carries nothing else.
        std::cout << result.dump(2) << std::endl;
        std::cout.flush();

        if (!ok) {
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
