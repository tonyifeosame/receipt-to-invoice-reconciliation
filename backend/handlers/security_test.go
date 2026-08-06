package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"receipt-reconciliation/middleware"
	"receipt-reconciliation/models"
)

const testJWTSecret = "unit-test-signing-key-with-enough-length"

func registerRequest(t *testing.T, handler *AuthHandler, body map[string]any, bearer string) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal register body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	w := httptest.NewRecorder()
	handler.Register(w, req)
	return w
}

// An unauthenticated caller must not be able to grant itself a privileged role.
func TestRegister_RejectsPrivilegedRoleFromAnonymousCaller(t *testing.T) {
	for _, role := range []string{middleware.RoleAdmin, middleware.RoleFinanceManager, "admin"} {
		t.Run(role, func(t *testing.T) {
			handler := NewAuthHandler(nil, testJWTSecret)

			resp := registerRequest(t, handler, map[string]any{
				"username": "attacker-" + role,
				"email":    "attacker@example.com",
				"password": "password123",
				"role":     role,
			}, "")

			if resp.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for role %q, got %d", role, resp.Code)
			}

			handler.mu.RLock()
			_, exists := handler.users["attacker-"+role]
			handler.mu.RUnlock()
			if exists {
				t.Fatalf("account was created despite the role being rejected")
			}
		})
	}
}

// An ADMIN may still provision privileged accounts.
func TestRegister_AllowsPrivilegedRoleForAdminCaller(t *testing.T) {
	handler := NewAuthHandler(nil, testJWTSecret)

	adminToken, err := handler.GetAuthMiddleware().GenerateToken(99, "root", middleware.RoleAdmin)
	if err != nil {
		t.Fatalf("failed to mint admin token: %v", err)
	}

	resp := registerRequest(t, handler, map[string]any{
		"username": "manager",
		"password": "password123",
		"role":     middleware.RoleFinanceManager,
	}, adminToken)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected admin-issued registration to succeed, got %d: %s", resp.Code, resp.Body.String())
	}

	handler.mu.RLock()
	created := handler.users["manager"]
	handler.mu.RUnlock()
	if created.Role != middleware.RoleFinanceManager {
		t.Fatalf("expected role %s, got %s", middleware.RoleFinanceManager, created.Role)
	}
}

// A forged or non-admin token must not unlock privileged roles.
func TestRegister_RejectsPrivilegedRoleWithForeignToken(t *testing.T) {
	handler := NewAuthHandler(nil, testJWTSecret)

	// Signed with a different key: valid shape, wrong signature.
	foreign := middleware.NewAuthMiddleware("a-totally-different-signing-key-value")
	forged, err := foreign.GenerateToken(1, "root", middleware.RoleAdmin)
	if err != nil {
		t.Fatalf("failed to mint foreign token: %v", err)
	}

	resp := registerRequest(t, handler, map[string]any{
		"username": "forged",
		"password": "password123",
		"role":     middleware.RoleAdmin,
	}, forged)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a token signed with another key, got %d", resp.Code)
	}

	// A genuine but non-admin token must be rejected too.
	staffToken, err := handler.GetAuthMiddleware().GenerateToken(2, "staff", middleware.RoleFinanceStaff)
	if err != nil {
		t.Fatalf("failed to mint staff token: %v", err)
	}

	resp = registerRequest(t, handler, map[string]any{
		"username": "escalate",
		"password": "password123",
		"role":     middleware.RoleAdmin,
	}, staffToken)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-admin token, got %d", resp.Code)
	}
}

func TestRegister_RejectsWeakAndInvalidInput(t *testing.T) {
	handler := NewAuthHandler(nil, testJWTSecret)

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"short password", map[string]any{"username": "a", "password": "short"}, http.StatusBadRequest},
		{"empty password", map[string]any{"username": "b", "password": ""}, http.StatusBadRequest},
		{"blank username", map[string]any{"username": "   ", "password": "password123"}, http.StatusBadRequest},
		{"unknown role", map[string]any{"username": "c", "password": "password123", "role": "SUPERUSER"}, http.StatusBadRequest},
		{"oversized username", map[string]any{"username": strings.Repeat("x", 200), "password": "password123"}, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if resp := registerRequest(t, handler, tc.body, ""); resp.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, resp.Code, resp.Body.String())
			}
		})
	}
}

// The default self-service role still works without any credentials.
func TestRegister_DefaultRoleStillPublic(t *testing.T) {
	handler := NewAuthHandler(nil, testJWTSecret)

	resp := registerRequest(t, handler, map[string]any{
		"username": "staffer",
		"password": "password123",
	}, "")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected default registration to remain public, got %d", resp.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON response: %v", err)
	}
	if body["role"] != middleware.RoleFinanceStaff {
		t.Fatalf("expected default role %s, got %s", middleware.RoleFinanceStaff, body["role"])
	}
}

func TestLogin_DoesNotDiscloseWhetherUserExists(t *testing.T) {
	handler := NewAuthHandler(nil, testJWTSecret)

	login := func(username, password string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]string{"username": username, "password": password})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.Login(w, req)
		return w
	}

	unknown := login("no-such-user", "password123")
	wrongPassword := login("finance", "definitely-wrong")

	if unknown.Code != http.StatusUnauthorized || wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("expected both to be 401, got %d and %d", unknown.Code, wrongPassword.Code)
	}

	if unknown.Body.String() != wrongPassword.Body.String() {
		t.Fatalf("responses differ and leak account existence: %q vs %q",
			unknown.Body.String(), wrongPassword.Body.String())
	}
}

func uploadReceipt(t *testing.T, handler *ReceiptHandler, fileName, content string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/receipts/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	handler.UploadReceipt(w, req)
	return w
}

func storedPath(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()

	if resp.Code != http.StatusOK {
		t.Fatalf("expected upload to succeed, got %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON response: %v", err)
	}
	return body["file_path"]
}

// Re-uploading the same filename must not overwrite the stored receipt.
func TestUploadReceipt_DoesNotOverwriteOnFilenameCollision(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "receipts")
	t.Setenv("RECEIPTS_DIR", uploadDir)

	handler := &ReceiptHandler{}

	first := storedPath(t, uploadReceipt(t, handler, "receipt.jpg", "original receipt"))
	second := storedPath(t, uploadReceipt(t, handler, "receipt.jpg", "attacker replacement"))

	if first == second {
		t.Fatalf("second upload reused the same path %q", first)
	}

	firstContent, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("first receipt is gone: %v", err)
	}
	if string(firstContent) != "original receipt" {
		t.Fatalf("first receipt was overwritten, content is now %q", string(firstContent))
	}

	secondContent, err := os.ReadFile(second)
	if err != nil {
		t.Fatalf("second receipt missing: %v", err)
	}
	if string(secondContent) != "attacker replacement" {
		t.Fatalf("unexpected second receipt content %q", string(secondContent))
	}
}

// A traversal sequence in the upload filename must stay inside the receipts dir.
func TestUploadReceipt_SanitisesTraversalFilenames(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "receipts")
	t.Setenv("RECEIPTS_DIR", uploadDir)

	handler := &ReceiptHandler{}
	stored := storedPath(t, uploadReceipt(t, handler, "../../evil.jpg", "payload"))

	absDir, err := filepath.Abs(uploadDir)
	if err != nil {
		t.Fatalf("failed to resolve upload dir: %v", err)
	}
	absStored, err := filepath.Abs(stored)
	if err != nil {
		t.Fatalf("failed to resolve stored path: %v", err)
	}
	if filepath.Dir(absStored) != absDir {
		t.Fatalf("file escaped the receipts directory: %s", absStored)
	}
}

type recordingOCRService struct {
	called bool
}

func (r *recordingOCRService) ProcessFile(string) (models.OCRResponse, error) {
	r.called = true
	return models.OCRResponse{}, nil
}

// The OCR endpoint must refuse paths outside the receipts directory.
func TestProcessOCR_RejectsPathsOutsideReceiptsDir(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "receipts")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("failed to create receipts dir: %v", err)
	}
	t.Setenv("RECEIPTS_DIR", uploadDir)

	// A real file outside the receipts directory, of an allowed type.
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, []byte("private"), 0o644); err != nil {
		t.Fatalf("failed to write outside file: %v", err)
	}

	cases := []string{
		outside,
		filepath.Join(uploadDir, "..", "..", "secret.png"),
		"../../../../etc/passwd.jpg",
		"/etc/shadow.jpg",
		"",
		filepath.Join(uploadDir, "notes.txt"),
	}

	for _, receiptFile := range cases {
		t.Run(receiptFile, func(t *testing.T) {
			ocr := &recordingOCRService{}
			handler := &ReceiptHandler{ocr: ocr}

			payload, _ := json.Marshal(map[string]string{"receipt_file": receiptFile})
			req := httptest.NewRequest(http.MethodPost, "/ocr/process", bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ProcessOCR(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %q, got %d", receiptFile, w.Code)
			}
			if ocr.called {
				t.Fatalf("OCR service was invoked for rejected path %q", receiptFile)
			}
		})
	}
}
