#ifndef FILE_UTILS_H
#define FILE_UTILS_H

#include <string>
#include <cstdint>

class FileUtils {
public:
    // Calculate SHA-256 hash of a file
    static std::string calculateSHA256(const std::string& filePath);
    
    // Get file size in bytes
    static int64_t getFileSize(const std::string& filePath);
    
    // Get MIME type based on file extension
    static std::string getMimeType(const std::string& filePath);
    
    // Check if file exists
    static bool fileExists(const std::string& filePath);
};

#endif // FILE_UTILS_H
