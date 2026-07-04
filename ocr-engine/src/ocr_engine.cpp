#if defined(__has_include)
#  if __has_include(<tesseract/baseapi.h>)
#    include <tesseract/baseapi.h>
#  else
#    pragma message("tesseract/baseapi.h not found in include path - providing minimal stubs for editor/intellisense")
#    namespace tesseract {
#      class TessBaseAPI;
#      enum PageSegMode { PSM_AUTO = 0 };
#      enum OcrEngineMode { OEM_DEFAULT = 0 };
#    }
#  endif
#else
#  include <tesseract/baseapi.h>
#endif

#include "ocr_engine.h"
#include "logger.h"
#include <opencv2/opencv.hpp>

OCREngine::OCREngine(const std::string& dataPath, const std::string& language)
    : dataPath_(dataPath), language_(language), initialized_(false) {
    tess_ = std::make_unique<tesseract::TessBaseAPI>();
    LOG_INFO("OCREngine created with language: " + language);
}

OCREngine::~OCREngine() {
    if (tess_) {
        tess_->End();
    }
    LOG_INFO("OCREngine destroyed");
}

bool OCREngine::initialize() {
    if (initialized_) {
        LOG_WARNING("OCREngine already initialized");
        return true;
    }
    
    const char* dataPath = dataPath_.empty() ? nullptr : dataPath_.c_str();
    
  if (tess_->Init(
        dataPath,
        language_.c_str(),
        tesseract::OEM_LSTM_ONLY)) {
        LOG_ERROR("Failed to initialize Tesseract OCR with data path: " + dataPath_ + " and language: " + language_);
        return false;
    }
    
    // Set default parameters for better receipt OCR
    tess_->SetPageSegMode(tesseract::PSM_AUTO);
    
    initialized_ = true;
    LOG_INFO("Tesseract OCR initialized successfully");
    return true;
}

std::string OCREngine::extractText(const std::string& imagePath) {
    if (!initialized_) {
        LOG_ERROR("OCREngine not initialized");
        return "";
    }
    
    LOG_INFO("Extracting text from image: " + imagePath);
    
    // Load image using OpenCV
    cv::Mat image = cv::imread(imagePath, cv::IMREAD_GRAYSCALE);
    if (image.empty()) {
        LOG_ERROR("Failed to load image: " + imagePath);
        return "";
    }
    
    return extractTextFromMat(image);
}

std::string OCREngine::extractTextFromMat(const cv::Mat& image) {
    if (!initialized_) {
        LOG_ERROR("OCREngine not initialized");
        return "";
    }
    
    if (image.empty()) {
        LOG_ERROR("Empty image provided");
        return "";
    }
    
    // Set image for Tesseract
    tess_->SetImage(image.data, image.cols, image.rows, 1, 
                     static_cast<int>(image.step));
    
    // Get text
    char* text = tess_->GetUTF8Text();
    std::string result(text ? text : "");
    
    if (text) {
        delete[] text;
    }
    
    LOG_DEBUG("Extracted " + std::to_string(result.length()) + " characters");
    return result;
}

OCREngine::OCRResult OCREngine::extractTextWithConfidence(const std::string& imagePath) {
    if (!initialized_) {
        LOG_ERROR("OCREngine not initialized");
        return {"", 0.0};
    }
    
    LOG_INFO("Extracting text with confidence from image: " + imagePath);
    
    // Load image using OpenCV
    cv::Mat image = cv::imread(imagePath, cv::IMREAD_GRAYSCALE);
    if (image.empty()) {
        LOG_ERROR("Failed to load image: " + imagePath);
        return {"", 0.0};
    }
    
    return extractTextWithConfidenceFromMat(image);
}

OCREngine::OCRResult OCREngine::extractTextWithConfidenceFromMat(const cv::Mat& image) {
    if (!initialized_) {
        LOG_ERROR("OCREngine not initialized");
        return {"", 0.0};
    }
    
    if (image.empty()) {
        LOG_ERROR("Empty image provided");
        return {"", 0.0};
    }
    
    // Set image for Tesseract
    tess_->SetImage(image.data, image.cols, image.rows, 1, 
                     static_cast<int>(image.step));
    
    // Get text
    char* text = tess_->GetUTF8Text();
    std::string result(text ? text : "");
    
    // Get mean text confidence
    int meanConfidence = tess_->MeanTextConf();
    double confidence = static_cast<double>(meanConfidence);
    
    if (text) {
        delete[] text;
    }
    
    LOG_DEBUG("Extracted " + std::to_string(result.length()) + " characters with confidence: " + std::to_string(confidence));
    return {result, confidence};
}

void OCREngine::setPageSegMode(int mode) {
    if (!initialized_) {
        LOG_ERROR("OCREngine not initialized");
        return;
    }
    
    tess_->SetPageSegMode(tesseract::PSM_SINGLE_BLOCK);
    LOG_DEBUG("Page segmentation mode set to: " + std::to_string(mode));
}

void OCREngine::setOemEngine(int mode) {
    if (!initialized_) {
        LOG_ERROR("OCREngine not initialized");
        return;
    }

    LOG_WARNING(
        "OCR Engine Mode cannot be changed after initialization. "
        "Requested mode: " + std::to_string(mode)
    );
}
void OCREngine::setVariable(const std::string& key, const std::string& value) {
    if (!initialized_) {
        LOG_ERROR("OCREngine not initialized");
        return;
    }
    
    bool success = tess_->SetVariable(key.c_str(), value.c_str());
    if (success) {
        LOG_DEBUG("Set variable: " + key + " = " + value);
    } else {
        LOG_WARNING("Failed to set variable: " + key);
    }
}

bool OCREngine::isQualityAcceptable(double confidence, double threshold) {
    bool acceptable = confidence >= threshold;
    LOG_DEBUG("OCR confidence " + std::to_string(confidence) + 
               " vs threshold " + std::to_string(threshold) + 
               " - " + (acceptable ? "ACCEPTABLE" : "TOO LOW"));
    return acceptable;
}

std::string OCREngine::getQualityMessage(double confidence, double threshold) {
    if (confidence >= threshold) {
        LOG_INFO("OCR quality acceptable: " + std::to_string(confidence));
        return "";
    }
    
    std::string message = "Receipt quality is too low (confidence: " + 
                         std::to_string(confidence) + "/100). Please retake the photo with better lighting and focus.";
    LOG_WARNING(message);
    return message;
}
