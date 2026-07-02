package handlers

import (
	"encoding/json"
	"net/http"
	"receipt-reconciliation/middleware"
	"receipt-reconciliation/repository"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	db             *repository.Database
	authMiddleware *middleware.AuthMiddleware
	users          map[string]storedUser
	mu             sync.RWMutex
}

type storedUser struct {
	ID       int
	Username string
	Email    string
	Password string
	Role     string
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string `json:"token"`
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expires_at"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func NewAuthHandler(db *repository.Database, jwtSecret string) *AuthHandler {
	handler := &AuthHandler{
		db:             db,
		authMiddleware: middleware.NewAuthMiddleware(jwtSecret),
		users:          make(map[string]storedUser),
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("finance123"), bcrypt.DefaultCost)
	if err == nil {
		handler.users["finance"] = storedUser{
			ID:       1,
			Username: "finance",
			Email:    "finance@example.com",
			Password: string(hashedPassword),
			Role:     "FINANCE_STAFF",
		}
	}

	return handler
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.mu.RLock()
	stored, ok := h.users[req.Username]
	h.mu.RUnlock()
	if !ok {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(stored.Password), []byte(req.Password)); err != nil {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	token, err := h.authMiddleware.GenerateToken(stored.ID, stored.Username, stored.Role)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	response := LoginResponse{
		Token:     token,
		UserID:    stored.ID,
		Username:  stored.Username,
		Role:      stored.Role,
		ExpiresAt: expiresAt,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	role := req.Role
	if role == "" {
		role = "FINANCE_STAFF"
	}

	h.mu.Lock()
	if _, exists := h.users[req.Username]; exists {
		h.mu.Unlock()
		http.Error(w, "Username already exists", http.StatusConflict)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.mu.Unlock()
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	user := storedUser{
		ID:       len(h.users) + 1,
		Username: req.Username,
		Email:    req.Email,
		Password: string(hashedPassword),
		Role:     role,
	}
	h.users[req.Username] = user
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message":  "User registered successfully",
		"username": req.Username,
		"role":     role,
	})
}

func (h *AuthHandler) GetAuthMiddleware() *middleware.AuthMiddleware {
	return h.authMiddleware
}
