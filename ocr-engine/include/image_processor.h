#ifndef IMAGE_PROCESSOR_H
#define IMAGE_PROCESSOR_H

#include <opencv2/opencv.hpp>
#include <string>

class ImageProcessor {
public:
    ImageProcessor();
    ~ImageProcessor();

    // Preprocess receipt image for OCR
    cv::Mat preprocessImage(const std::string& imagePath);
    
    // Individual preprocessing steps
    cv::Mat convertToGrayscale(const cv::Mat& image);
    cv::Mat removeNoise(const cv::Mat& image);
    cv::Mat applyThreshold(const cv::Mat& image);
    cv::Mat correctRotation(const cv::Mat& image);
    cv::Mat detectReceiptBoundaries(const cv::Mat& image);
    
    // Save processed image
    bool saveImage(const cv::Mat& image, const std::string& outputPath);

private:
    // Helper methods
    double calculateSkewAngle(const cv::Mat& image);
    cv::Mat rotateImage(const cv::Mat& image, double angle);
    std::vector<cv::Point> findLargestContour(const cv::Mat& image);
};

#endif // IMAGE_PROCESSOR_H
