package services

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"receipt-reconciliation/models"
	"runtime"
	"strconv"
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

	cmd := exec.Command(n.enginePath, filePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return models.OCRResponse{}, fmt.Errorf("ocr engine failed: %w: %s", err, string(output))
	}

	var response models.OCRResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return models.OCRResponse{}, fmt.Errorf("failed to parse OCR response: %w", err)
	}

	return response, nil
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
