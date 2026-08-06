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
    cv::Mat applyThreshold(const cv::Mat& image);
    cv::Mat correctRotation(const cv::Mat& image);
    cv::Mat correctPerspective(const cv::Mat& image);
    cv::Mat enhanceContrast(const cv::Mat& image);
    cv::Mat advancedDenoise(const cv::Mat& image);
    cv::Mat morphologicalCleanup(const cv::Mat& image);
    cv::Mat cropToContent(const cv::Mat& image);

private:
    // Helper methods
    double calculateSkewAngle(const cv::Mat& image);
    cv::Mat rotateImage(const cv::Mat& image, double angle);
    std::vector<cv::Point> findLargestContour(const cv::Mat& image);
    cv::Mat fourPointTransform(const cv::Mat& image, const std::vector<cv::Point>& corners);
    std::vector<cv::Point> orderPoints(const std::vector<cv::Point>& points);
    double calculateRotationAngle(const cv::Mat& image);
    cv::Rect findContentBoundingBox(const cv::Mat& image);
};

#endif // IMAGE_PROCESSOR_H
