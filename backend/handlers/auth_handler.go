package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"receipt-reconciliation/middleware"
	"receipt-reconciliation/repository"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// minPasswordLength is the shortest password accepted at registration.
	minPasswordLength = 8
	// maxUsernameLength bounds the in-memory user map keys.
	maxUsernameLength = 64
	// maxPasswordLength guards against bcrypt's 72-byte truncation being reached
	// via oversized input, and against memory-heavy request bodies.
	maxPasswordLength = 128
)

// dummyHash is compared against when an unknown username is supplied so that a
// failed lookup costs the same as a wrong password. Without it, response timing
// reveals which usernames exist.
var dummyHash = func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("timing-equalisation-placeholder"), bcrypt.DefaultCost)
	if err != nil {
		return []byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinvalidinv")
	}
	return hash
}()

type AuthHandler struct {
	db             *repository.Database
	authMiddleware *middleware.AuthMiddleware
	users          map[string]storedUser
	nextUserID     int
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

// IsProduction reports whether the service is configured for production, which
// switches off development conveniences that are unsafe to expose.
func IsProduction() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
}

// publicRegistrationAllowed reports whether unauthenticated callers may create
// accounts. Defaults to true to preserve existing behaviour; set
// ALLOW_PUBLIC_REGISTRATION=false to require an ADMIN token for registration.
func publicRegistrationAllowed() bool {
	value := strings.TrimSpace(os.Getenv("ALLOW_PUBLIC_REGISTRATION"))
	if value == "" {
		return true
	}
	return !strings.EqualFold(value, "false") && value != "0"
}

func NewAuthHandler(db *repository.Database, jwtSecret string) *AuthHandler {
	handler := &AuthHandler{
		db:             db,
		authMiddleware: middleware.NewAuthMiddleware(jwtSecret),
		users:          make(map[string]storedUser),
		nextUserID:     1,
	}

	handler.seedDemoUser()

	return handler
}

// seedDemoUser creates the built-in development account. Its credentials are in
// the source tree, so it must never exist in a production deployment.
func (h *AuthHandler) seedDemoUser() {
	if IsProduction() {
		log.Println("Production mode: built-in demo account not created")
		return
	}

	password := os.Getenv("DEMO_USER_PASSWORD")
	if password == "" {
		password = "finance123"
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Failed to seed demo user: %v", err)
		return
	}

	h.users["finance"] = storedUser{
		ID:       h.nextUserID,
		Username: "finance",
		Email:    "finance@example.com",
		Password: string(hashedPassword),
		Role:     middleware.RoleFinanceStaff,
	}
	h.nextUserID++
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

	username := strings.TrimSpace(req.Username)

	h.mu.RLock()
	stored, ok := h.users[username]
	h.mu.RUnlock()

	// Always run a bcrypt comparison so that an unknown username takes the same
	// time as a known one, and never disclose which of the two was wrong.
	hash := []byte(stored.Password)
	if !ok {
		hash = dummyHash
	}

	if err := bcrypt.CompareHashAndPassword(hash, []byte(req.Password)); err != nil || !ok {
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

	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	if len(username) > maxUsernameLength {
		http.Error(w, "Username is too long", http.StatusBadRequest)
		return
	}

	if len(req.Password) < minPasswordLength {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	if len(req.Password) > maxPasswordLength {
		http.Error(w, "Password is too long", http.StatusBadRequest)
		return
	}

	callerIsAdmin := h.callerHasAdminRole(r)

	if !publicRegistrationAllowed() && !callerIsAdmin {
		http.Error(w, "Registration requires an administrator", http.StatusForbidden)
		return
	}

	role := strings.ToUpper(strings.TrimSpace(req.Role))
	if role == "" {
		role = middleware.RoleFinanceStaff
	}

	if !middleware.IsValidRole(role) {
		http.Error(w, "Unknown role", http.StatusBadRequest)
		return
	}

	// Privilege escalation guard: the role is attacker-controlled input on a public
	// endpoint, so anything above the default self-service role requires a caller
	// who already holds ADMIN.
	if role != middleware.RoleFinanceStaff && !callerIsAdmin {
		log.Printf("Rejected registration for %q requesting role %s without administrator credentials", username, role)
		http.Error(w, "Insufficient permissions to assign this role", http.StatusForbidden)
		return
	}

	h.mu.Lock()
	if _, exists := h.users[username]; exists {
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

	// A length-derived id repeats as soon as any account is removed, which would
	// hand one user another user's identity in freshly issued tokens.
	user := storedUser{
		ID:       h.nextUserID,
		Username: username,
		Email:    strings.TrimSpace(req.Email),
		Password: string(hashedPassword),
		Role:     role,
	}
	h.nextUserID++
	h.users[username] = user
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message":  "User registered successfully",
		"username": username,
		"role":     role,
	})
}

// callerHasAdminRole reports whether the request carries a valid token belonging
// to an administrator. Register is a public route, so the token is read from the
// header rather than from the request context.
func (h *AuthHandler) callerHasAdminRole(r *http.Request) bool {
	tokenString, ok := middleware.BearerToken(r.Header.Get("Authorization"))
	if !ok {
		return false
	}

	claims, err := h.authMiddleware.ValidateToken(tokenString)
	if err != nil {
		return false
	}

	return claims.Role == middleware.RoleAdmin
}

func (h *AuthHandler) GetAuthMiddleware() *middleware.AuthMiddleware {
	return h.authMiddleware
}
