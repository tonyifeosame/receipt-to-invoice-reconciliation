cmake_minimum_required(VERSION 3.15)
project(OCREngine VERSION 1.0 LANGUAGES CXX)
option(BUILD_TESTS "Build unit tests" OFF)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# Enable testing
enable_testing()

# Find required packages
find_package(OpenCV REQUIRED)
find_package(PkgConfig REQUIRED)
find_package(nlohmann_json CONFIG REQUIRED)
pkg_check_modules(Tesseract REQUIRED tesseract)
pkg_check_modules(Leptonica REQUIRED lept)

# Find PostgreSQL library
find_package(PostgreSQL REQUIRED)

# Find OpenSSL for SHA-256 hashing
find_package(OpenSSL REQUIRED)

if(BUILD_TESTS)
    find_package(GTest QUIET)

    if(NOT GTest_FOUND)
        include(FetchContent)
        FetchContent_Declare(
            googletest
            GIT_REPOSITORY https://github.com/google/googletest.git
            GIT_TAG release-1.12.1
        )
        FetchContent_MakeAvailable(googletest)
    endif()
endif()

# Include directories
include_directories(
    ${OpenCV_INCLUDE_DIRS}
    ${Tesseract_INCLUDE_DIRS}
    ${Leptonica_INCLUDE_DIRS}
    ${PostgreSQL_INCLUDE_DIRS}
    ${CMAKE_CURRENT_SOURCE_DIR}/include
)

# Source files
set(SOURCES
    src/main.cpp
    src/image_processor.cpp
    src/ocr_engine.cpp
    src/receipt_parser.cpp
    src/database.cpp
    src/logger.cpp
    src/file_utils.cpp
    src/pdf_processor.cpp
)

# Create executable
add_executable(ocr_engine ${SOURCES})

# Link libraries
target_link_libraries(ocr_engine
    ${OpenCV_LIBS}
    ${Tesseract_LIBRARIES}
    ${Leptonica_LIBRARIES}
    pqxx
    ${PostgreSQL_LIBRARIES}
    OpenSSL::Crypto
    nlohmann_json::nlohmann_json
)

# Compiler warnings
if(MSVC)
    target_compile_options(ocr_engine PRIVATE /W4)
else()
    target_compile_options(ocr_engine PRIVATE -Wall -Wextra -Wpedantic)
endif()

# Copy config file to build directory
configure_file(${CMAKE_CURRENT_SOURCE_DIR}/config.json 
               ${CMAKE_CURRENT_BINARY_DIR}/config.json 
               COPYONLY)

# Create test executable
if(BUILD_TESTS)
    add_executable(
        receipt_parser_test
        tests/receipt_parser_test.cpp
        src/receipt_parser.cpp
        src/logger.cpp
    )

    target_link_libraries(
        receipt_parser_test
        GTest::gtest
        GTest::gtest_main
    )

    add_test(NAME ReceiptParserTest COMMAND receipt_parser_test)
endif()

# Register tests
add_test(NAME ReceiptParserTest COMMAND receipt_parser_test)
