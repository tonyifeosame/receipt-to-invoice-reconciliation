#include "file_utils.h"
#include "logger.h"
#include <fstream>
#include <sstream>
#include <iomanip>
#include <filesystem>
#include <openssl/sha.h>

std::string FileUtils::calculateSHA256(const std::string& filePath) {
    LOG_INFO("Calculating SHA-256 hash for: " + filePath);
    
    std::ifstream file(filePath, std::ios::binary);
    if (!file) {
        LOG_ERROR("Failed to open file for hashing: " + filePath);
        return "";
    }
    
    SHA256_CTX sha256;
    SHA256_Init(&sha256);
    
    const int BUFFER_SIZE = 8192;
    char buffer[BUFFER_SIZE];
    
    while (file.read(buffer, BUFFER_SIZE)) {
        SHA256_Update(&sha256, buffer, file.gcount());
    }
    
    // Process remaining bytes
    if (file.gcount() > 0) {
        SHA256_Update(&sha256, buffer, file.gcount());
    }
    
    unsigned char hash[SHA256_DIGEST_LENGTH];
    SHA256_Final(hash, &sha256);
    
    // Convert to hex string
    std::ostringstream oss;
    for (int i = 0; i < SHA256_DIGEST_LENGTH; i++) {
        oss << std::hex << std::setw(2) << std::setfill('0') << static_cast<int>(hash[i]);
    }
    
    std::string hashString = oss.str();
    LOG_DEBUG("SHA-256 hash: " + hashString);
    return hashString;
}

int64_t FileUtils::getFileSize(const std::string& filePath) {
    try {
        std::filesystem::path path(filePath);
        return std::filesystem::file_size(path);
    } catch (const std::exception& e) {
        LOG_ERROR("Failed to get file size: " + std::string(e.what()));
        return -1;
    }
}

std::string FileUtils::getMimeType(const std::string& filePath) {
    std::filesystem::path path(filePath);
    std::string ext = path.extension().string();
    
    // Convert to lowercase
    std::transform(ext.begin(), ext.end(), ext.begin(), ::tolower);
    
    if (ext == ".jpg" || ext == ".jpeg") {
        return "image/jpeg";
    } else if (ext == ".png") {
        return "image/png";
    } else if (ext == ".pdf") {
        return "application/pdf";
    } else if (ext == ".tiff" || ext == ".tif") {
        return "image/tiff";
    } else {
        return "application/octet-stream";
    }
}

bool FileUtils::fileExists(const std::string& filePath) {
    return std::filesystem::exists(filePath);
}
