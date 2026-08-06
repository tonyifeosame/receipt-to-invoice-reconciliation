package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
)

const tokenIssuer = "receipt-reconciliation"

// Roles recognised by the system. They mirror the CHECK constraint on users.role
// in database/migrations/002_production_features.sql.
const (
	RoleAdmin          = "ADMIN"
	RoleFinanceManager = "FINANCE_MANAGER"
	RoleFinanceStaff   = "FINANCE_STAFF"
	RoleViewer         = "VIEWER"
)

// IsValidRole reports whether role is one the system recognises.
func IsValidRole(role string) bool {
	switch role {
	case RoleAdmin, RoleFinanceManager, RoleFinanceStaff, RoleViewer:
		return true
	default:
		return false
	}
}

// contextKey is unexported so that no other package can create or overwrite the
// values stored under these keys. Plain string keys are shared across every
// package writing to the same request context, which would let unrelated code
// (or a third-party middleware) inject an arbitrary user id or role.
type contextKey int

const (
	userIDKey contextKey = iota
	usernameKey
	roleKey
)

type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type AuthMiddleware struct {
	jwtSecret []byte
}

func NewAuthMiddleware(jwtSecret string) *AuthMiddleware {
	// An empty signing key would still produce verifiable tokens, so anyone could
	// mint one. Fail closed with an unguessable ephemeral key instead.
	if jwtSecret == "" {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			panic("failed to generate a JWT signing key: " + err.Error())
		}
		log.Println("WARNING: empty JWT secret supplied, using a random ephemeral key; all tokens are invalidated on restart")
		return &AuthMiddleware{jwtSecret: buf}
	}

	return &AuthMiddleware{
		jwtSecret: []byte(jwtSecret),
	}
}

// GenerateRandomSecret returns a hex-encoded cryptographically random secret.
func GenerateRandomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GenerateToken creates a new JWT token for a user
func (a *AuthMiddleware) GenerateToken(userID int, username, role string) (string, error) {
	expirationTime := time.Now().Add(24 * time.Hour)

	claims := &Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    tokenIssuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(a.jwtSecret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// ValidateToken validates a JWT token and returns the claims
func (a *AuthMiddleware) ValidateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Pin the algorithm: without this check a token header can select a
		// different signing method and have it verified against the same key.
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.jwtSecret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
	)

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}

	// Reject tokens without an expiry: exp is only enforced when it is present,
	// so a token minted without one would never expire.
	if claims.ExpiresAt == nil {
		return nil, fmt.Errorf("token is missing an expiry")
	}

	return claims, nil
}

// BearerToken extracts the credential from an "Authorization: Bearer <token>"
// header value.
func BearerToken(authHeader string) (string, bool) {
	parts := strings.Fields(authHeader)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

// Authenticate is middleware that checks for a valid JWT token
func (a *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		// Extract token from "Bearer <token>"
		tokenString, ok := BearerToken(authHeader)
		if !ok {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		claims, err := a.ValidateToken(tokenString)
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Add user info to request context
		ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
		ctx = context.WithValue(ctx, usernameKey, claims.Username)
		ctx = context.WithValue(ctx, roleKey, claims.Role)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole is middleware that checks if the user has the required role
func (a *AuthMiddleware) RequireRole(requiredRole string) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A non-string value here used to panic the request goroutine.
			userRole, ok := GetRoleFromContext(r)
			if !ok || userRole == "" {
				http.Error(w, "User role not found in context", http.StatusUnauthorized)
				return
			}

			if userRole != requiredRole && userRole != RoleAdmin {
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GetUserIDFromContext extracts user ID from request context
func GetUserIDFromContext(r *http.Request) (int, bool) {
	userID, ok := r.Context().Value(userIDKey).(int)
	return userID, ok
}

// GetUsernameFromContext extracts username from request context
func GetUsernameFromContext(r *http.Request) (string, bool) {
	username, ok := r.Context().Value(usernameKey).(string)
	return username, ok
}

// GetRoleFromContext extracts role from request context
func GetRoleFromContext(r *http.Request) (string, bool) {
	role, ok := r.Context().Value(roleKey).(string)
	return role, ok
}
