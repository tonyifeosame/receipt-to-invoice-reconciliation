#ifndef OCR_ENGINE_H
#define OCR_ENGINE_H

#include <tesseract/baseapi.h>
#include <leptonica/allheaders.h>
#include <opencv2/opencv.hpp>
#include <string>
#include <memory>

class OCREngine {
public:
    OCREngine(const std::string& dataPath = "", const std::string& language = "eng");
    ~OCREngine();

    // Initialize Tesseract
    bool initialize();
    
    // Extract text from image
    std::string extractText(const std::string& imagePath);
    std::string extractTextFromMat(const cv::Mat& image);
    
    // Extract text with confidence score
    struct OCRResult {
        std::string text;
        double confidence;
    };
    OCRResult extractTextWithConfidence(const std::string& imagePath);
    OCRResult extractTextWithConfidenceFromMat(const cv::Mat& image);
    
    // Set OCR parameters
    void setPageSegMode(int mode);
    void setOemEngine(int mode);
    void setVariable(const std::string& key, const std::string& value);
    
    // Quality assessment
    bool isQualityAcceptable(double confidence, double threshold = 70.0);
    std::string getQualityMessage(double confidence, double threshold = 70.0);

private:
    std::unique_ptr<tesseract::TessBaseAPI> tess_;
    std::string dataPath_;
    std::string language_;
    bool initialized_;
};

#endif // OCR_ENGINE_H
