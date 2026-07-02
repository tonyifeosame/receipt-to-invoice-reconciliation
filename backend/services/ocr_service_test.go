package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFallbackOCREngineReturnsStructuredResponse(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "receipt.jpg")
	if err := os.WriteFile(tempFile, []byte("dummy image"), 0o644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	engine := NewFallbackOCREngine()
	response, err := engine.ProcessFile(tempFile)
	if err != nil {
		t.Fatalf("expected fallback OCR response, got error: %v", err)
	}

	if !response.IsValid {
		t.Fatalf("expected fallback response to be valid, got %+v", response)
	}

	if response.InvoiceNumber == "" {
		t.Fatalf("expected fallback response to contain an invoice number, got %+v", response)
	}
}
