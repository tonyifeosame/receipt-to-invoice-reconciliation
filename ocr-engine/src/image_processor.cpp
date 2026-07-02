#include "image_processor.h"
#include "logger.h"
#include <opencv2/imgproc.hpp>
#include <algorithm>

ImageProcessor::ImageProcessor() {
    LOG_INFO("ImageProcessor initialized");
}

ImageProcessor::~ImageProcessor() {
    LOG_INFO("ImageProcessor destroyed");
}

cv::Mat ImageProcessor::preprocessImage(const std::string& imagePath) {
    LOG_INFO("Preprocessing image: " + imagePath);
    
    // Load image
    cv::Mat image = cv::imread(imagePath);
    if (image.empty()) {
        LOG_ERROR("Failed to load image: " + imagePath);
        return cv::Mat();
    }
    
    LOG_DEBUG("Original image size: " + std::to_string(image.cols) + "x" + std::to_string(image.rows));
    
    // Apply preprocessing pipeline
    cv::Mat processed = convertToGrayscale(image);
    processed = removeNoise(processed);
    processed = correctRotation(processed);
    processed = detectReceiptBoundaries(processed);
    processed = applyThreshold(processed);
    
    LOG_INFO("Image preprocessing completed");
    return processed;
}

cv::Mat ImageProcessor::convertToGrayscale(const cv::Mat& image) {
    cv::Mat gray;
    cv::cvtColor(image, gray, cv::COLOR_BGR2GRAY);
    LOG_DEBUG("Converted to grayscale");
    return gray;
}

cv::Mat ImageProcessor::removeNoise(const cv::Mat& image) {
    cv::Mat denoised;
    cv::medianBlur(image, denoised, 3);
    LOG_DEBUG("Noise removed using median blur");
    return denoised;
}

cv::Mat ImageProcessor::applyThreshold(const cv::Mat& image) {
    cv::Mat thresholded;
    cv::adaptiveThreshold(image, thresholded, 255, cv::ADAPTIVE_THRESH_GAUSSIAN_C,
                         cv::THRESH_BINARY, 11, 2);
    LOG_DEBUG("Applied adaptive thresholding");
    return thresholded;
}

cv::Mat ImageProcessor::correctRotation(const cv::Mat& image) {
    double angle = calculateSkewAngle(image);
    
    if (std::abs(angle) < 0.5) {
        LOG_DEBUG("No significant rotation detected");
        return image;
    }
    
    cv::Mat corrected = rotateImage(image, angle);
    LOG_DEBUG("Corrected rotation by " + std::to_string(angle) + " degrees");
    return corrected;
}

double ImageProcessor::calculateSkewAngle(const cv::Mat& image) {
    cv::Mat edges;
    cv::Canny(image, edges, 50, 150, 3);
    
    std::vector<cv::Vec2f> lines;
    cv::HoughLines(edges, lines, 1, CV_PI/180, 100);
    
    if (lines.empty()) {
        return 0.0;
    }
    
    // Calculate average angle
    double angleSum = 0.0;
    int count = 0;
    
    for (const auto& line : lines) {
        double angle = line[1] * 180.0 / CV_PI;
        if (std::abs(angle) < 45) {
            angleSum += angle;
            count++;
        }
    }
    
    return count > 0 ? angleSum / count : 0.0;
}

cv::Mat ImageProcessor::rotateImage(const cv::Mat& image, double angle) {
    cv::Mat rotated;
    cv::Point2f center(image.cols/2.0, image.rows/2.0);
    cv::Mat rotationMatrix = cv::getRotationMatrix2D(center, angle, 1.0);
    cv::warpAffine(image, rotated, rotationMatrix, image.size(), 
                   cv::INTER_LINEAR, cv::BORDER_CONSTANT, cv::Scalar(255, 255, 255));
    return rotated;
}

cv::Mat ImageProcessor::detectReceiptBoundaries(const cv::Mat& image) {
    cv::Mat blurred;
    cv::GaussianBlur(image, blurred, cv::Size(5, 5), 0);
    
    cv::Mat edges;
    cv::Canny(blurred, edges, 75, 200);
    
    std::vector<std::vector<cv::Point>> contours;
    cv::findContours(edges, contours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);
    
    if (contours.empty()) {
        LOG_DEBUG("No contours found, returning original image");
        return image;
    }
    
    std::vector<cv::Point> largestContour = findLargestContour(edges);
    
    if (largestContour.size() < 4) {
        LOG_DEBUG("Largest contour has less than 4 points, returning original image");
        return image;
    }
    
    // Approximate contour to polygon
    std::vector<cv::Point> approx;
    double epsilon = 0.02 * cv::arcLength(largestContour, true);
    cv::approxPolyDP(largestContour, approx, epsilon, true);
    
    if (approx.size() == 4) {
        // Perspective transform could be applied here
        LOG_DEBUG("Detected receipt boundaries with 4 corners");
    }
    
    return image;
}

std::vector<cv::Point> ImageProcessor::findLargestContour(const cv::Mat& image) {
    std::vector<std::vector<cv::Point>> contours;
    cv::findContours(image.clone(), contours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);
    
    if (contours.empty()) {
        return std::vector<cv::Point>();
    }
    
    auto maxContour = std::max_element(contours.begin(), contours.end(),
        [](const std::vector<cv::Point>& a, const std::vector<cv::Point>& b) {
            return cv::contourArea(a) < cv::contourArea(b);
        });
    
    return *maxContour;
}

bool ImageProcessor::saveImage(const cv::Mat& image, const std::string& outputPath) {
    bool success = cv::imwrite(outputPath, image);
    if (success) {
        LOG_INFO("Image saved to: " + outputPath);
    } else {
        LOG_ERROR("Failed to save image to: " + outputPath);
    }
    return success;
}
