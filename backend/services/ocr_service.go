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

// NativeOCREngine runs the C++ ocr_engine binary, the only OCR implementation.
// There is deliberately no fallback: a stand-in that invented receipt data used
// to be returned whenever the binary was missing, and that fabricated data would
// have been forwarded to the company API as if it had been read from the receipt.
type NativeOCREngine struct {
	enginePath string
}

// locateOCREngine returns OCR_ENGINE_PATH when set, otherwise the first local
// build output that exists (for running the backend outside Docker).
func locateOCREngine() string {
	if enginePath := os.Getenv("OCR_ENGINE_PATH"); enginePath != "" {
		return enginePath
	}

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
			return candidate
		}
	}
	return ""
}

// checkOCREngine reports why enginePath cannot be run as the OCR engine.
func checkOCREngine(enginePath string) error {
	if enginePath == "" {
		return fmt.Errorf("OCR engine binary not found: set OCR_ENGINE_PATH to the built ocr_engine")
	}

	info, err := os.Stat(enginePath)
	if err != nil {
		return fmt.Errorf("OCR engine binary not found at %s: %w", enginePath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("OCR engine path %s is a directory, not the ocr_engine binary", enginePath)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return fmt.Errorf("OCR engine binary at %s is not executable", enginePath)
	}
	return nil
}

// NewNativeOCREngine returns the engine at the located path, or why it cannot be used.
func NewNativeOCREngine() (*NativeOCREngine, error) {
	enginePath := locateOCREngine()
	if err := checkOCREngine(enginePath); err != nil {
		return nil, err
	}
	return &NativeOCREngine{enginePath: enginePath}, nil
}

// NewOCRService returns the real C++ OCR engine and stops the process when it
// cannot be found: the backend must not start in a state where every receipt
// would fail, or where OCR could be answered by anything other than the engine.
func NewOCRService() OCRService {
	engine, err := NewNativeOCREngine()
	if err != nil {
		log.Fatalf("FATAL: %v. The backend cannot run without the real OCR engine.", err)
	}
	log.Printf("Using OCR engine: %s", engine.enginePath)
	return engine
}

func (n *NativeOCREngine) ProcessFile(filePath string) (models.OCRResponse, error) {
	if filePath == "" {
		return models.OCRResponse{}, fmt.Errorf("receipt file path is required")
	}

	if _, err := os.Stat(filePath); err != nil {
		return models.OCRResponse{}, fmt.Errorf("receipt file not found: %w", err)
	}

	if err := checkOCREngine(n.enginePath); err != nil {
		return models.OCRResponse{}, err
	}

	// The engine resolves relative paths against its own working directory, so hand
	// it an absolute path to the receipt.
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	// The engine appends to ocr_engine.log in its working directory, and does so
	// before it reads LOG_FILE. Each run therefore gets its own directory, removed
	// afterwards, so no log file accumulates next to the backend.
	workDir, err := os.MkdirTemp("", "ocr-run-")
	if err != nil {
		return models.OCRResponse{}, fmt.Errorf("failed to create OCR working directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	// "--file" selects single-file mode; without it the engine batch-processes its
	// configured receipts directory and never emits a per-receipt JSON document.
	cmd := exec.Command(n.enginePath, "--file", absPath)
	cmd.Dir = workDir

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

func GenerateRequestID() string {
	return "REQ-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}
