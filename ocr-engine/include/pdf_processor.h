#ifndef PDF_PROCESSOR_H
#define PDF_PROCESSOR_H

#include <string>
#include <vector>

class PDFProcessor {
public:
    PDFProcessor();
    ~PDFProcessor();

    // Convert PDF page to image
    std::string convertPageToImage(const std::string& pdfPath, int pageNum = 0);
    
    // Get number of pages in PDF
    int getPageCount(const std::string& pdfPath);
    
    // Convert all pages to images
    std::vector<std::string> convertAllPages(const std::string& pdfPath);
    
    // Check if file is a valid PDF
    static bool isPDF(const std::string& filePath);

private:
    // Helper methods
    std::string generateOutputPath(const std::string& pdfPath, int pageNum);
};

#endif // PDF_PROCESSOR_H
