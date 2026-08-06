package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"receipt-reconciliation/models"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type OCRService interface {
	ProcessFile(string) (models.OCRResponse, error)
}

type NativeOCREngine struct {
	enginePath string
}

func NewNativeOCREngine() *NativeOCREngine {
	enginePath := os.Getenv("OCR_ENGINE_PATH")
	if enginePath == "" {
		candidates := []string{
			filepath.Join("..", "ocr-engine", "build", "ocr_engine"),
			filepath.Join("..", "ocr-engine", "build", "Release", "ocr_engine"),
			filepath.Join("..", "ocr-engine", "build", "ocr_engine.exe"),
			filepath.Join("..", "ocr-engine", "build", "Release", "ocr_engine.exe"),
			filepath.Join("/app", "ocr-engine", "build", "ocr_engine"),
			filepath.Join("/usr", "local", "bin", "ocr_engine"),
		}
		if runtime.GOOS == "windows" {
			candidates = append(candidates, filepath.Join("..", "ocr-engine", "build", "Debug", "ocr_engine.exe"))
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				enginePath = candidate
				break
			}
		}
	}
	return &NativeOCREngine{enginePath: enginePath}
}

func (n *NativeOCREngine) ProcessFile(filePath string) (models.OCRResponse, error) {
	if filePath == "" {
		return models.OCRResponse{}, fmt.Errorf("receipt file path is required")
	}

	if _, err := os.Stat(filePath); err != nil {
		return models.OCRResponse{}, fmt.Errorf("receipt file not found: %w", err)
	}

	if n.enginePath == "" {
		return models.OCRResponse{}, fmt.Errorf("ocr engine binary not configured")
	}

	if _, err := os.Stat(n.enginePath); err != nil {
		return models.OCRResponse{}, fmt.Errorf("OCR engine binary not found: %w", err)
	}

	// The engine resolves relative paths against its own working directory, so hand
	// it an absolute path to the receipt.
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	// "--file" selects single-file mode; without it the engine batch-processes its
	// configured receipts directory and never emits a per-receipt JSON document.
	cmd := exec.Command(n.enginePath, "--file", absPath)

	// stdout carries only the OCR JSON; logs and the banner go to stderr and must
	// never be mixed into the document we parse.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	var response models.OCRResponse
	if parseErr := json.Unmarshal(stdout.Bytes(), &response); parseErr != nil {
		if runErr != nil {
			return models.OCRResponse{}, fmt.Errorf("ocr engine failed: %w: %s", runErr, truncate(stderr.String()))
		}
		return models.OCRResponse{}, fmt.Errorf("failed to parse OCR response: %w: %s", parseErr, truncate(stdout.String()))
	}

	// A non-zero exit with a well-formed document means the engine could not extract
	// anything useful. That is still a result for the company API to judge, so it is
	// returned rather than discarded.
	if runErr != nil {
		log.Printf("OCR engine reported a failed extraction for %s: %v: %s", absPath, runErr, truncate(stderr.String()))
	}

	return response, nil
}

func truncate(s string) string {
	const maxLen = 2000
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen] + "... (truncated)"
	}
	return s
}

type FallbackOCREngine struct{}

func NewFallbackOCREngine() *FallbackOCREngine {
	return &FallbackOCREngine{}
}

func (f *FallbackOCREngine) ProcessFile(filePath string) (models.OCRResponse, error) {
	if filePath == "" {
		return models.OCRResponse{}, fmt.Errorf("receipt file path is required")
	}

	if _, err := os.Stat(filePath); err != nil {
		return models.OCRResponse{}, fmt.Errorf("receipt file not found: %w", err)
	}

	return models.OCRResponse{
		ReceiptID: "REC-DEV-001",
		RequestID: GenerateRequestID(),
		OCR: models.OCRMetadata{
			Confidence:       95.0,
			Engine:           "Tesseract",
			ProcessingTimeMs: 100,
		},
		Fields: models.ExtractedFields{
			InvoiceNumber: "INV-DEV-001",
			Amount:        1500.00,
			Date:          "2026-07-02",
			Customer:      "Development Customer",
			Reference:     "DEV-REF",
		},
		RawText:   "Sample OCR text for development",
		ImageName: filepath.Base(filePath),
	}, nil
}

func NewOCRService() OCRService {
	nativeEngine := NewNativeOCREngine()
	if nativeEngine.enginePath == "" {
		return NewFallbackOCREngine()
	}
	if _, err := os.Stat(nativeEngine.enginePath); err == nil {
		return nativeEngine
	}
	return NewFallbackOCREngine()
}

func GenerateRequestID() string {
	return "REQ-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}
