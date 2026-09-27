package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEngineEnv makes the test binary act as ocr_engine. NativeOCREngine runs its
// configured binary as a subprocess; pointing OCR_ENGINE_PATH at the test binary
// itself exercises that real exec, stdout and exit-code handling on any OS.
const (
	fakeEngineEnv  = "FAKE_OCR_ENGINE_MODE"
	fakeRecordEnv  = "FAKE_OCR_ENGINE_RECORD"
	newServiceMode = "call-new-ocr-service"
)

func TestMain(m *testing.M) {
	switch mode := os.Getenv(fakeEngineEnv); mode {
	case "":
		os.Exit(m.Run())
	case newServiceMode:
		NewOCRService() // must exit the process when the engine is missing
		os.Exit(0)
	default:
		os.Exit(runFakeEngine(mode))
	}
}

// runFakeEngine behaves like `ocr_engine --file <path>` in the given mode and
// records the arguments and working directory it was started with.
func runFakeEngine(mode string) int {
	if record := os.Getenv(fakeRecordEnv); record != "" {
		wd, _ := os.Getwd()
		data, _ := json.Marshal(map[string]any{"args": os.Args[1:], "wd": wd})
		_ = os.WriteFile(record, data, 0o644)
	}
	_ = os.WriteFile("ocr_engine.log", []byte("engine log line\n"), 0o644)
	fmt.Fprintln(os.Stderr, "engine diagnostics on stderr")

	document := `{
	  "receipt_id": "receipt.jpg", "request_id": "REQ-123",
	  "ocr": {"confidence": 42.5, "engine": "Tesseract", "processing_time_ms": 17},
	  "fields": {"invoice_number": "INV-2041", "amount": 1250.75, "date": "2026-07-15",
	             "customer": "Acme Ltd", "reference": "TRX998", "phone_number": "", "email": ""},
	  "raw_text": "INVOICE INV-2041\nTOTAL 1,250.75", "image_name": "receipt.jpg"}`

	switch mode {
	case "ok":
		fmt.Println(document)
		return 0
	case "failed-extraction":
		fmt.Println(`{"receipt_id":"receipt.jpg","request_id":"REQ-9","ocr":{"confidence":0},"fields":{},"raw_text":"","image_name":"receipt.jpg"}`)
		return 1
	case "garbage":
		fmt.Println("this is not JSON")
		return 0
	case "crash":
		fmt.Fprintln(os.Stderr, "boom: tesseract could not initialise")
		return 2
	}
	return 3
}

func useFakeEngine(t *testing.T, mode string) *NativeOCREngine {
	t.Helper()
	t.Setenv("OCR_ENGINE_PATH", os.Args[0])
	t.Setenv(fakeEngineEnv, mode)
	engine, err := NewNativeOCREngine()
	if err != nil {
		t.Fatalf("expected the test binary to be accepted as the engine, got %v", err)
	}
	return engine
}

func writeReceipt(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "receipt.jpg")
	if err := os.WriteFile(path, []byte("image bytes"), 0o644); err != nil {
		t.Fatalf("failed to write receipt: %v", err)
	}
	return path
}

func TestNewNativeOCREngine_MissingBinaryIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "ocr_engine")
	t.Setenv("OCR_ENGINE_PATH", missing)

	engine, err := NewNativeOCREngine()
	if err == nil || engine != nil {
		t.Fatalf("expected an error for a missing binary, got engine=%v err=%v", engine, err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("expected the error to name the missing path, got %v", err)
	}
}

func TestNewNativeOCREngine_DirectoryIsNotAnEngine(t *testing.T) {
	t.Setenv("OCR_ENGINE_PATH", t.TempDir())

	if _, err := NewNativeOCREngine(); err == nil {
		t.Fatal("expected a directory to be rejected as the engine binary")
	}
}

func TestNewOCRService_ExitsAtStartupWhenTheEngineIsMissing(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		fakeEngineEnv+"="+newServiceMode,
		"OCR_ENGINE_PATH="+filepath.Join(t.TempDir(), "ocr_engine"),
	)
	output, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("expected NewOCRService to exit non-zero, got err=%v output=%s", err, output)
	}
	if !strings.Contains(string(output), "cannot run without the real OCR engine") {
		t.Fatalf("expected a fatal startup message, got %s", output)
	}
}

func TestNewOCRService_ReturnsTheNativeEngine(t *testing.T) {
	t.Setenv("OCR_ENGINE_PATH", os.Args[0])

	service := NewOCRService()
	if _, ok := service.(*NativeOCREngine); !ok {
		t.Fatalf("expected the native C++ engine, got %T", service)
	}
}

func TestProcessFile_ReturnsEverythingTheEngineExtracted(t *testing.T) {
	engine := useFakeEngine(t, "ok")
	record := filepath.Join(t.TempDir(), "record.json")
	t.Setenv(fakeRecordEnv, record)
	receipt := writeReceipt(t)

	response, err := engine.ProcessFile(receipt)
	if err != nil {
		t.Fatalf("expected a parsed response, got %v", err)
	}

	if response.Fields.InvoiceNumber != "INV-2041" || response.Fields.Amount != 1250.75 ||
		response.Fields.Date != "2026-07-15" || response.Fields.Customer != "Acme Ltd" ||
		response.Fields.Reference != "TRX998" {
		t.Fatalf("expected every extracted field to be preserved, got %+v", response.Fields)
	}
	if response.RawText != "INVOICE INV-2041\nTOTAL 1,250.75" {
		t.Fatalf("expected raw_text to be preserved, got %q", response.RawText)
	}
	if response.OCR.Confidence != 42.5 || response.RequestID != "REQ-123" {
		t.Fatalf("expected confidence and request_id to be preserved, got %+v / %q", response.OCR, response.RequestID)
	}

	var started struct {
		Args []string `json:"args"`
		Wd   string   `json:"wd"`
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the engine did not record its invocation: %v", err)
	}
	if err := json.Unmarshal(data, &started); err != nil {
		t.Fatalf("invalid invocation record: %v", err)
	}
	absReceipt, _ := filepath.Abs(receipt)
	if len(started.Args) != 2 || started.Args[0] != "--file" || started.Args[1] != absReceipt {
		t.Fatalf("expected `--file <absolute path>`, got %v", started.Args)
	}

	// The engine ran in a throwaway directory, which is gone together with the
	// ocr_engine.log it wrote there.
	if _, err := os.Stat(started.Wd); !os.IsNotExist(err) {
		t.Fatalf("expected the engine's working directory %s to be removed, stat err=%v", started.Wd, err)
	}
}

func TestProcessFile_KeepsAFailedExtraction(t *testing.T) {
	engine := useFakeEngine(t, "failed-extraction")

	response, err := engine.ProcessFile(writeReceipt(t))
	if err != nil {
		t.Fatalf("a failed extraction with a valid document must still be returned, got %v", err)
	}
	if response.RequestID != "REQ-9" {
		t.Fatalf("expected the engine's document, got %+v", response)
	}
}

func TestProcessFile_RejectsOutputThatIsNotJSON(t *testing.T) {
	engine := useFakeEngine(t, "garbage")

	if _, err := engine.ProcessFile(writeReceipt(t)); err == nil {
		t.Fatal("expected unparseable engine output to be an error")
	}
}

func TestProcessFile_ReportsAnEngineCrash(t *testing.T) {
	engine := useFakeEngine(t, "crash")

	_, err := engine.ProcessFile(writeReceipt(t))
	if err == nil || !strings.Contains(err.Error(), "tesseract could not initialise") {
		t.Fatalf("expected the engine's stderr in the error, got %v", err)
	}
}

func TestProcessFile_MissingReceiptIsAnError(t *testing.T) {
	engine := useFakeEngine(t, "ok")

	if _, err := engine.ProcessFile(filepath.Join(t.TempDir(), "absent.jpg")); err == nil {
		t.Fatal("expected a missing receipt file to be an error")
	}
}
