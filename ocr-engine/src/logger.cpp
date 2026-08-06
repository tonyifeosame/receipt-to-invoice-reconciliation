#include "logger.h"
#include <iostream>
#include <sstream>
#include <ctime>

Logger::Logger() 
    : minLevel_(LogLevel::INFO), consoleOutput_(true), logFilePath_("ocr_engine.log") {
    logFile_.open(logFilePath_, std::ios::app);
    if (!logFile_.is_open()) {
        std::cerr << "Failed to open log file: " << logFilePath_ << std::endl;
    }
}

Logger::~Logger() {
    if (logFile_.is_open()) {
        logFile_.close();
    }
}

Logger& Logger::getInstance() {
    static Logger instance;
    return instance;
}

void Logger::log(LogLevel level, const std::string& message) {
    if (level < minLevel_) {
        return;
    }
    
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::string timestamp = getCurrentTimestamp();
    std::string levelStr = levelToString(level);
    
    std::ostringstream logMessage;
    logMessage << "[" << timestamp << "] "
               << "[" << levelStr << "] "
               << message;
    
    std::string fullMessage = logMessage.str();
    
    // Write to file
    if (logFile_.is_open()) {
        logFile_ << fullMessage << std::endl;
        logFile_.flush();
    }
    
    // Write to console if enabled. Everything goes to stderr: stdout is reserved
    // for the machine-readable OCR JSON consumed by the backend.
    if (consoleOutput_) {
        std::cerr << fullMessage << std::endl;
    }
}

void Logger::debug(const std::string& message) {
    log(LogLevel::DEBUG, message);
}

void Logger::info(const std::string& message) {
    log(LogLevel::INFO, message);
}

void Logger::warning(const std::string& message) {
    log(LogLevel::WARNING, message);
}

void Logger::error(const std::string& message) {
    log(LogLevel::ERROR, message);
}

void Logger::critical(const std::string& message) {
    log(LogLevel::CRITICAL, message);
}

void Logger::setLogFile(const std::string& logFile) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    if (logFile_.is_open()) {
        logFile_.close();
    }
    
    logFilePath_ = logFile;
    logFile_.open(logFilePath_, std::ios::app);
    
    if (!logFile_.is_open()) {
        std::cerr << "Failed to open log file: " << logFilePath_ << std::endl;
    }
}

void Logger::setLogLevel(LogLevel level) {
    std::lock_guard<std::mutex> lock(mutex_);
    minLevel_ = level;
}

void Logger::enableConsoleOutput(bool enable) {
    std::lock_guard<std::mutex> lock(mutex_);
    consoleOutput_ = enable;
}

std::string Logger::getCurrentTimestamp() {
    auto now = std::chrono::system_clock::now();
    auto time_t = std::chrono::system_clock::to_time_t(now);
    auto ms = std::chrono::duration_cast<std::chrono::milliseconds>(
        now.time_since_epoch()) % 1000;
    
    std::ostringstream oss;
    oss << std::put_time(std::localtime(&time_t), "%Y-%m-%d %H:%M:%S");
    oss << "." << std::setfill('0') << std::setw(3) << ms.count();
    
    return oss.str();
}

std::string Logger::levelToString(LogLevel level) {
    switch (level) {
        case LogLevel::DEBUG:   return "DEBUG";
        case LogLevel::INFO:    return "INFO";
        case LogLevel::WARNING: return "WARNING";
        case LogLevel::ERROR:   return "ERROR";
        case LogLevel::CRITICAL: return "CRITICAL";
        default:                return "UNKNOWN";
    }
}
