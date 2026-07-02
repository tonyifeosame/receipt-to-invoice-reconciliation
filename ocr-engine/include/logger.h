#ifndef LOGGER_H
#define LOGGER_H

#include <string>
#include <fstream>
#include <mutex>
#include <memory>
#include <chrono>
#include <iomanip>

enum class LogLevel {
    DEBUG,
    INFO,
    WARNING,
    ERROR,
    CRITICAL
};

class Logger {
public:
    static Logger& getInstance();
    
    // Log methods
    void log(LogLevel level, const std::string& message);
    void debug(const std::string& message);
    void info(const std::string& message);
    void warning(const std::string& message);
    void error(const std::string& message);
    void critical(const std::string& message);
    
    // Configuration
    void setLogFile(const std::string& logFile);
    void setLogLevel(LogLevel level);
    void enableConsoleOutput(bool enable);

private:
    Logger();
    ~Logger();
    Logger(const Logger&) = delete;
    Logger& operator=(const Logger&) = delete;
    
    // Helper methods
    std::string getCurrentTimestamp();
    std::string levelToString(LogLevel level);
    
    std::ofstream logFile_;
    std::mutex mutex_;
    LogLevel minLevel_;
    bool consoleOutput_;
    std::string logFilePath_;
};

// Convenience macros
#define LOG_DEBUG(msg) Logger::getInstance().debug(msg)
#define LOG_INFO(msg) Logger::getInstance().info(msg)
#define LOG_WARNING(msg) Logger::getInstance().warning(msg)
#define LOG_ERROR(msg) Logger::getInstance().error(msg)
#define LOG_CRITICAL(msg) Logger::getInstance().critical(msg)

#endif // LOGGER_H
