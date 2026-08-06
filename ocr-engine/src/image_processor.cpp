#include "image_processor.h"
#include "logger.h"
#include <opencv2/imgproc.hpp>
#include <algorithm>
#include <functional>

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

    // Each stage is optional: if one fails on an unusual image we keep the best
    // result produced so far rather than aborting the whole preprocessing run.
    auto step = [](const char* name, const cv::Mat& input,
                   const std::function<cv::Mat(const cv::Mat&)>& fn) -> cv::Mat {
        try {
            cv::Mat output = fn(input);
            if (output.empty()) {
                LOG_WARNING(std::string("Preprocessing step '") + name +
                            "' produced an empty image, keeping previous result");
                return input;
            }
            return output;
        } catch (const cv::Exception& e) {
            LOG_WARNING(std::string("Preprocessing step '") + name + "' failed: " + e.what() +
                        " - keeping previous result");
            return input;
        }
    };

    // Apply enhanced preprocessing pipeline
    cv::Mat processed = step("grayscale", image, [this](const cv::Mat& m) { return convertToGrayscale(m); });
    processed = step("denoise", processed, [this](const cv::Mat& m) { return advancedDenoise(m); });
    processed = step("contrast", processed, [this](const cv::Mat& m) { return enhanceContrast(m); });
    processed = step("rotation", processed, [this](const cv::Mat& m) { return correctRotation(m); });
    processed = step("perspective", processed, [this](const cv::Mat& m) { return correctPerspective(m); });
    processed = step("threshold", processed, [this](const cv::Mat& m) { return applyThreshold(m); });
    processed = step("morphology", processed, [this](const cv::Mat& m) { return morphologicalCleanup(m); });
    processed = step("crop", processed, [this](const cv::Mat& m) { return cropToContent(m); });

    LOG_INFO("Image preprocessing completed");
    return processed;
}

cv::Mat ImageProcessor::convertToGrayscale(const cv::Mat& image) {
    if (image.channels() == 1) {
        LOG_DEBUG("Image is already grayscale");
        return image;
    }

    cv::Mat gray;
    cv::cvtColor(image, gray, image.channels() == 4 ? cv::COLOR_BGRA2GRAY : cv::COLOR_BGR2GRAY);
    LOG_DEBUG("Converted to grayscale");
    return gray;
}

cv::Mat ImageProcessor::advancedDenoise(const cv::Mat& image) {
    cv::Mat denoised;
    
    // Apply bilateral filter for edge-preserving denoising
    cv::Mat bilateral;
    cv::bilateralFilter(image, bilateral, 9, 75, 75);
    
    // Apply non-local means denoising for better results
    cv::fastNlMeansDenoising(bilateral, denoised, 10, 7, 21);
    
    LOG_DEBUG("Advanced denoising applied");
    return denoised;
}

cv::Mat ImageProcessor::enhanceContrast(const cv::Mat& image) {
    cv::Mat enhanced;
    
    // Apply CLAHE (Contrast Limited Adaptive Histogram Equalization)
    cv::Ptr<cv::CLAHE> clahe = cv::createCLAHE();
    clahe->setClipLimit(3.0);
    clahe->setTilesGridSize(cv::Size(8, 8));
    clahe->apply(image, enhanced);
    
    LOG_DEBUG("Contrast enhanced using CLAHE");
    return enhanced;
}

cv::Mat ImageProcessor::applyThreshold(const cv::Mat& image) {
    // Adaptive thresholding adapts to uneven lighting across a photographed
    // receipt. An Otsu pass used to be computed here as well and then discarded
    // without ever being used.
    cv::Mat thresholded;
    cv::adaptiveThreshold(image, thresholded, 255, cv::ADAPTIVE_THRESH_GAUSSIAN_C,
                          cv::THRESH_BINARY, 15, 8);

    // Apply morphological operations to clean up
    cv::Mat kernel = cv::getStructuringElement(cv::MORPH_RECT, cv::Size(2, 2));
    cv::morphologyEx(thresholded, thresholded, cv::MORPH_CLOSE, kernel);
    
    LOG_DEBUG("Applied adaptive thresholding");
    return thresholded;
}

cv::Mat ImageProcessor::morphologicalCleanup(const cv::Mat& image) {
    cv::Mat cleaned;

    // applyThreshold produces dark text on a light background, but OpenCV's
    // morphology treats *bright* pixels as the foreground. The operations are
    // therefore the duals of the ones named in a text-as-foreground pipeline:
    // MORPH_CLOSE removes small dark speckles and MORPH_OPEN reconnects broken
    // strokes. Running them the other way round erodes the glyphs instead: on a
    // clean 700x900 receipt it reduced Tesseract's output from the full 17-line
    // document to just the bold heading, because every regular-weight stroke was
    // thin enough to be wiped out.
    cv::Mat kernelDespeckle = cv::getStructuringElement(cv::MORPH_RECT, cv::Size(2, 2));
    cv::morphologyEx(image, cleaned, cv::MORPH_CLOSE, kernelDespeckle);

    // A 2x2 kernel is deliberate here: a 3x3 reconnect merges the two dots of a
    // colon into a full stop, which costs the parser the "Customer:" label.
    cv::Mat kernelReconnect = cv::getStructuringElement(cv::MORPH_RECT, cv::Size(2, 2));
    cv::morphologyEx(cleaned, cleaned, cv::MORPH_OPEN, kernelReconnect);

    LOG_DEBUG("Applied morphological cleanup (despeckle and reconnect)");
    return cleaned;
}

cv::Mat ImageProcessor::cropToContent(const cv::Mat& image) {
    cv::Rect bbox = findContentBoundingBox(image);
    
    // If bounding box is invalid or covers most of the image, return original
    if (bbox.x <= 0 && bbox.y <= 0 && 
        bbox.width >= image.cols - 10 && bbox.height >= image.rows - 10) {
        LOG_DEBUG("No significant cropping needed");
        return image;
    }
    
    // Add small padding around the content
    int padding = 10;
    bbox.x = std::max(0, bbox.x - padding);
    bbox.y = std::max(0, bbox.y - padding);
    bbox.width = std::min(image.cols - bbox.x, bbox.width + 2 * padding);
    bbox.height = std::min(image.rows - bbox.y, bbox.height + 2 * padding);
    
    cv::Mat cropped = image(bbox).clone();
    LOG_DEBUG("Cropped image from " + std::to_string(image.cols) + "x" + std::to_string(image.rows) + 
               " to " + std::to_string(cropped.cols) + "x" + std::to_string(cropped.rows));
    
    return cropped;
}

cv::Rect ImageProcessor::findContentBoundingBox(const cv::Mat& image) {
    cv::Mat binary;
    
    // Ensure image is binary
    if (image.channels() == 3) {
        cv::cvtColor(image, binary, cv::COLOR_BGR2GRAY);
        cv::threshold(binary, binary, 127, 255, cv::THRESH_BINARY);
    } else {
        binary = image.clone();
    }
    
    // Find non-zero pixels (text content)
    std::vector<std::vector<cv::Point>> contours;
    cv::findContours(binary, contours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);
    
    if (contours.empty()) {
        return cv::Rect(0, 0, image.cols, image.rows);
    }
    
    // Find bounding box of all content
    int minX = image.cols, minY = image.rows;
    int maxX = 0, maxY = 0;
    
    for (const auto& contour : contours) {
        cv::Rect bbox = cv::boundingRect(contour);
        minX = std::min(minX, bbox.x);
        minY = std::min(minY, bbox.y);
        maxX = std::max(maxX, bbox.x + bbox.width);
        maxY = std::max(maxY, bbox.y + bbox.height);
    }
    
    // Filter out very small contours (noise)
    int minContentSize = 100;
    if ((maxX - minX) < minContentSize || (maxY - minY) < minContentSize) {
        return cv::Rect(0, 0, image.cols, image.rows);
    }
    
    return cv::Rect(minX, minY, maxX - minX, maxY - minY);
}

cv::Mat ImageProcessor::correctRotation(const cv::Mat& image) {
    double angle = calculateRotationAngle(image);
    
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

double ImageProcessor::calculateRotationAngle(const cv::Mat& image) {
    // Use multiple methods for more robust rotation detection
    
    // Method 1: Hough lines (existing)
    double houghAngle = calculateSkewAngle(image);
    
    // Method 2: Projection profile for text lines
    cv::Mat edges;
    cv::Canny(image, edges, 50, 150, 3);
    
    std::vector<cv::Vec4i> lines;
    cv::HoughLinesP(edges, lines, 1, CV_PI/180, 80, 100, 10);
    
    if (lines.empty()) {
        return houghAngle;
    }
    
    // Calculate angle from line segments
    std::vector<double> angles;
    for (const auto& line : lines) {
        double dx = line[2] - line[0];
        double dy = line[3] - line[1];
        double angle = std::atan2(dy, dx) * 180.0 / CV_PI;
        
        // Normalize to -90 to 90
        if (angle > 45) angle -= 90;
        if (angle < -45) angle += 90;
        
        if (std::abs(angle) < 45) {
            angles.push_back(angle);
        }
    }
    
    if (angles.empty()) {
        return houghAngle;
    }
    
    // Use median angle for robustness
    std::sort(angles.begin(), angles.end());
    double medianAngle = angles[angles.size() / 2];
    
    // Weight the two methods
    return (houghAngle * 0.3 + medianAngle * 0.7);
}

cv::Mat ImageProcessor::rotateImage(const cv::Mat& image, double angle) {
    cv::Mat rotated;
    cv::Point2f center(image.cols/2.0, image.rows/2.0);
    cv::Mat rotationMatrix = cv::getRotationMatrix2D(center, angle, 1.0);
    cv::warpAffine(image, rotated, rotationMatrix, image.size(), 
                   cv::INTER_LINEAR, cv::BORDER_CONSTANT, cv::Scalar(255, 255, 255));
    return rotated;
}

cv::Mat ImageProcessor::correctPerspective(const cv::Mat& image) {
    // The preprocessing pipeline hands us a single-channel image; converting it
    // again with COLOR_BGR2GRAY would throw (cvtColor asserts scn == 3 || scn == 4).
    cv::Mat gray = convertToGrayscale(image);

    cv::Mat blurred;
    cv::GaussianBlur(gray, blurred, cv::Size(5, 5), 0);
    
    cv::Mat edges;
    cv::Canny(blurred, edges, 75, 200);
    
    std::vector<std::vector<cv::Point>> contours;
    cv::findContours(edges, contours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);
    
    if (contours.empty()) {
        LOG_DEBUG("No contours found for perspective correction");
        return image;
    }
    
    std::vector<cv::Point> largestContour = findLargestContour(edges);
    
    if (largestContour.size() < 4) {
        LOG_DEBUG("Contour too small for perspective correction");
        return image;
    }
    
    // Approximate contour to quadrilateral
    std::vector<cv::Point> approx;
    double epsilon = 0.02 * cv::arcLength(largestContour, true);
    cv::approxPolyDP(largestContour, approx, epsilon, true);
    
    if (approx.size() != 4) {
        LOG_DEBUG("Could not find 4 corners for perspective correction");
        return image;
    }
    
    // Order the corners
    std::vector<cv::Point> orderedCorners = orderPoints(approx);
    
    // Apply perspective transform
    cv::Mat corrected = fourPointTransform(image, orderedCorners);
    
    LOG_DEBUG("Perspective correction applied");
    return corrected;
}

cv::Mat ImageProcessor::fourPointTransform(const cv::Mat& image, const std::vector<cv::Point>& corners) {
    // Order the corners: top-left, top-right, bottom-right, bottom-left
    std::vector<cv::Point2f> srcPts(4);
    for (int i = 0; i < 4; i++) {
        srcPts[i] = cv::Point2f(static_cast<float>(corners[i].x), static_cast<float>(corners[i].y));
    }
    
    // Calculate width and height of the new image
    double widthA = std::sqrt(std::pow(srcPts[2].x - srcPts[3].x, 2) + std::pow(srcPts[2].y - srcPts[3].y, 2));
    double widthB = std::sqrt(std::pow(srcPts[1].x - srcPts[0].x, 2) + std::pow(srcPts[1].y - srcPts[0].y, 2));
    double maxWidth = std::max(widthA, widthB);
    
    double heightA = std::sqrt(std::pow(srcPts[1].x - srcPts[2].x, 2) + std::pow(srcPts[1].y - srcPts[2].y, 2));
    double heightB = std::sqrt(std::pow(srcPts[0].x - srcPts[3].x, 2) + std::pow(srcPts[0].y - srcPts[3].y, 2));
    double maxHeight = std::max(heightA, heightB);

    // A degenerate quadrilateral would produce an empty destination size and make
    // warpPerspective throw; the original image is the safer result.
    if (maxWidth < 10.0 || maxHeight < 10.0) {
        LOG_DEBUG("Detected quadrilateral too small for perspective transform");
        return image;
    }

    // Destination points
    std::vector<cv::Point2f> dstPts = {
        cv::Point2f(0.0f, 0.0f),
        cv::Point2f(static_cast<float>(maxWidth) - 1, 0.0f),
        cv::Point2f(static_cast<float>(maxWidth) - 1, static_cast<float>(maxHeight) - 1),
        cv::Point2f(0.0f, static_cast<float>(maxHeight) - 1)
    };
    
    // Get perspective transform matrix
    cv::Mat M = cv::getPerspectiveTransform(srcPts, dstPts);
    
    // Apply warp perspective
    cv::Mat warped;
    cv::warpPerspective(image, warped, M, cv::Size(static_cast<int>(maxWidth), static_cast<int>(maxHeight)));
    
    return warped;
}

std::vector<cv::Point> ImageProcessor::orderPoints(const std::vector<cv::Point>& points) {
    std::vector<cv::Point> ordered(4);
    
    // Sort by x-coordinate
    std::vector<cv::Point> sorted = points;
    std::sort(sorted.begin(), sorted.end(), [](const cv::Point& a, const cv::Point& b) {
        return a.x < b.x;
    });
    
    // Leftmost points
    std::vector<cv::Point> leftMost = {sorted[0], sorted[1]};
    std::sort(leftMost.begin(), leftMost.end(), [](const cv::Point& a, const cv::Point& b) {
        return a.y < b.y;
    });
    
    // Rightmost points
    std::vector<cv::Point> rightMost = {sorted[2], sorted[3]};
    std::sort(rightMost.begin(), rightMost.end(), [](const cv::Point& a, const cv::Point& b) {
        return a.y < b.y;
    });
    
    ordered[0] = leftMost[0];  // Top-left
    ordered[1] = rightMost[0]; // Top-right
    ordered[2] = rightMost[1]; // Bottom-right
    ordered[3] = leftMost[1];  // Bottom-left
    
    return ordered;
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

