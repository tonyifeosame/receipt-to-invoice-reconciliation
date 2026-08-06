package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "middleware-unit-test-signing-key-value"

func TestValidateToken_AcceptsOwnToken(t *testing.T) {
	auth := NewAuthMiddleware(testSecret)

	token, err := auth.GenerateToken(7, "staffer", RoleFinanceStaff)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := auth.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected token to validate: %v", err)
	}
	if claims.UserID != 7 || claims.Username != "staffer" || claims.Role != RoleFinanceStaff {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

// alg=none must never be accepted.
func TestValidateToken_RejectsNoneAlgorithm(t *testing.T) {
	auth := NewAuthMiddleware(testSecret)

	claims := &Claims{
		UserID:   1,
		Username: "attacker",
		Role:     RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    tokenIssuer,
		},
	}

	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tokenString, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("failed to build unsigned token: %v", err)
	}

	if _, err := auth.ValidateToken(tokenString); err == nil {
		t.Fatal("an alg=none token was accepted")
	}
}

func TestValidateToken_RejectsForeignSignature(t *testing.T) {
	issuer := NewAuthMiddleware("some-other-signing-key-entirely-here")
	verifier := NewAuthMiddleware(testSecret)

	token, err := issuer.GenerateToken(1, "attacker", RoleAdmin)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	if _, err := verifier.ValidateToken(token); err == nil {
		t.Fatal("a token signed with another key was accepted")
	}
}

func TestValidateToken_RejectsExpiredAndUnexpiringTokens(t *testing.T) {
	auth := NewAuthMiddleware(testSecret)

	sign := func(claims *Claims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString([]byte(testSecret))
		if err != nil {
			t.Fatalf("failed to sign token: %v", err)
		}
		return signed
	}

	expired := sign(&Claims{
		UserID: 1, Role: RoleFinanceStaff,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			Issuer:    tokenIssuer,
		},
	})
	if _, err := auth.ValidateToken(expired); err == nil {
		t.Fatal("an expired token was accepted")
	}

	// No exp claim at all: such a token would otherwise be valid forever.
	noExpiry := sign(&Claims{
		UserID: 1, Role: RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{Issuer: tokenIssuer},
	})
	if _, err := auth.ValidateToken(noExpiry); err == nil {
		t.Fatal("a token without an expiry was accepted")
	}
}

func TestValidateToken_RejectsForeignIssuer(t *testing.T) {
	auth := NewAuthMiddleware(testSecret)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		UserID: 1, Role: RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "some-other-service",
		},
	})
	signed, err := token.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	if _, err := auth.ValidateToken(signed); err == nil {
		t.Fatal("a token from another issuer was accepted")
	}
}

// Context values must not be reachable or forgeable through plain string keys.
func TestContextValues_AreNotStringKeyed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)

	//nolint:staticcheck // deliberately using a string key to prove it is ignored
	ctx := context.WithValue(req.Context(), "role", RoleAdmin)
	//nolint:staticcheck
	ctx = context.WithValue(ctx, "user_id", 1234)
	req = req.WithContext(ctx)

	if role, ok := GetRoleFromContext(req); ok {
		t.Fatalf("string-keyed role was read back as %q", role)
	}
	if id, ok := GetUserIDFromContext(req); ok {
		t.Fatalf("string-keyed user id was read back as %d", id)
	}
}

// RequireRole must deny, not panic, when the context holds no or odd values.
func TestRequireRole_DeniesWithoutValidRole(t *testing.T) {
	auth := NewAuthMiddleware(testSecret)
	guarded := auth.RequireRole(RoleFinanceStaff)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("no role in context", func(t *testing.T) {
		w := httptest.NewRecorder()
		guarded.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("non-string role in context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		req = req.WithContext(context.WithValue(req.Context(), roleKey, 42))
		w := httptest.NewRecorder()
		guarded.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("wrong role", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		req = req.WithContext(context.WithValue(req.Context(), roleKey, RoleViewer))
		w := httptest.NewRecorder()
		guarded.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
	})
}

func TestAuthenticate_RejectsMalformedHeaders(t *testing.T) {
	auth := NewAuthMiddleware(testSecret)
	guarded := auth.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, header := range []string{"", "Bearer", "Bearer ", "Basic abc", "Bearer a b", "not-a-token"} {
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		w := httptest.NewRecorder()
		guarded.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for header %q, got %d", header, w.Code)
		}
	}
}

// An empty configured secret must not yield a predictable signing key.
func TestNewAuthMiddleware_EmptySecretIsNotShared(t *testing.T) {
	first := NewAuthMiddleware("")
	second := NewAuthMiddleware("")

	token, err := first.GenerateToken(1, "user", RoleFinanceStaff)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	if _, err := second.ValidateToken(token); err == nil {
		t.Fatal("two empty-secret instances shared a signing key")
	}

	empty := NewAuthMiddleware("")
	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		UserID: 1, Role: RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    tokenIssuer,
		},
	})
	signed, err := forged.SignedString([]byte(""))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	if _, err := empty.ValidateToken(signed); err == nil {
		t.Fatal("a token signed with an empty key was accepted")
	}
}
