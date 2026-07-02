package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthHandler_RegisterAndLogin(t *testing.T) {
	handler := NewAuthHandler(nil, "test-secret")

	registerBody := map[string]interface{}{
		"username": "finance1",
		"email":    "finance1@example.com",
		"password": "secret123",
		"role":     "FINANCE_STAFF",
	}
	registerJSON, _ := json.Marshal(registerBody)

	registerReq := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBuffer(registerJSON))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp := httptest.NewRecorder()
	handler.Register(registerResp, registerReq)

	if registerResp.Code != http.StatusOK {
		t.Fatalf("expected register status 200, got %d", registerResp.Code)
	}

	loginBody := map[string]string{
		"username": "finance1",
		"password": "secret123",
	}
	loginJSON, _ := json.Marshal(loginBody)

	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBuffer(loginJSON))
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp := httptest.NewRecorder()
	handler.Login(loginResp, loginReq)

	if loginResp.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d", loginResp.Code)
	}

	var loginResponse LoginResponse
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("expected valid login response: %v", err)
	}

	if loginResponse.Token == "" {
		t.Fatalf("expected jwt token in login response")
	}

	if loginResponse.Username != "finance1" {
		t.Fatalf("expected username finance1, got %s", loginResponse.Username)
	}
}
