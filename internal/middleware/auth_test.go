package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/model"

	"github.com/google/uuid"
)

func TestJWTGenerationAndVerification(t *testing.T) {
	secret := []byte("my-super-secret-signing-key-123")
	tenantID := uuid.New()
	userID := uuid.New()

	claims := &middleware.JWTClaims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     model.RoleOperator,
		Email:    "operator@tenant.com",
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	}

	token, err := middleware.GenerateJWT(claims, secret)
	if err != nil {
		t.Fatalf("failed to generate JWT: %v", err)
	}

	verified, err := middleware.VerifyJWT(token, secret)
	if err != nil {
		t.Fatalf("failed to verify JWT: %v", err)
	}

	if verified.UserID != userID || verified.TenantID != tenantID || verified.Role != model.RoleOperator {
		t.Errorf("verified claims mismatch: %+v", verified)
	}

	// Test invalid signature
	_, err = middleware.VerifyJWT(token, []byte("wrong-secret-key-456"))
	if err == nil {
		t.Error("expected signature validation failure, but verification succeeded")
	}
}

func TestJWTAuthMiddleware(t *testing.T) {
	secret := []byte("my-super-secret-signing-key-123")
	tenantID := uuid.New()
	userID := uuid.New()

	claims := &middleware.JWTClaims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     model.RoleAdmin,
		Email:    "admin@tenant.com",
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	}

	token, err := middleware.GenerateJWT(claims, secret)
	if err != nil {
		t.Fatalf("failed to generate JWT: %v", err)
	}

	// 1. Create a dummy handler that asserts context injection
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tID, ok := db.TenantIDFromContext(r.Context())
		if !ok || tID != tenantID {
			t.Errorf("context tenant_id mismatch or not injected")
		}

		injectedClaims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok || injectedClaims.UserID != userID || injectedClaims.Role != model.RoleAdmin {
			t.Errorf("context claims mismatch or not injected")
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	jwtMiddleware := middleware.JWTAuth(secret)
	server := httptest.NewServer(jwtMiddleware(dummyHandler))
	defer server.Close()

	// Request with valid token
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	// Request with missing token
	reqMissing, _ := http.NewRequest("GET", server.URL, nil)
	respMissing, _ := http.DefaultClient.Do(reqMissing)
	defer respMissing.Body.Close()
	if respMissing.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for missing token, got %d", respMissing.StatusCode)
	}

	// Request with expired token
	expiredClaims := &middleware.JWTClaims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     model.RoleAdmin,
		Email:    "admin@tenant.com",
		Exp:      time.Now().Add(-1 * time.Hour).Unix(),
	}
	expiredToken, _ := middleware.GenerateJWT(expiredClaims, secret)
	reqExpired, _ := http.NewRequest("GET", server.URL, nil)
	reqExpired.Header.Set("Authorization", "Bearer "+expiredToken)
	respExpired, _ := http.DefaultClient.Do(reqExpired)
	defer respExpired.Body.Close()
	if respExpired.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for expired token, got %d", respExpired.StatusCode)
	}
}

func TestRequireRoleMiddleware(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	roleMiddleware := middleware.RequireRole(model.RoleAdmin, model.RoleOperator)
	handlerUnderTest := roleMiddleware(dummyHandler)

	// 1. Success case: user is operator
	reqSuccess := httptest.NewRequest("GET", "/", nil)
	ctxSuccess := context.WithValue(context.Background(), middleware.ClaimsContextKey, &middleware.JWTClaims{
		Role: model.RoleOperator,
	})
	recSuccess := httptest.NewRecorder()
	handlerUnderTest.ServeHTTP(recSuccess, reqSuccess.WithContext(ctxSuccess))

	if recSuccess.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", recSuccess.Code)
	}

	// 2. Forbidden case: user is viewer
	reqForbidden := httptest.NewRequest("GET", "/", nil)
	ctxForbidden := context.WithValue(context.Background(), middleware.ClaimsContextKey, &middleware.JWTClaims{
		Role: model.RoleViewer,
	})
	recForbidden := httptest.NewRecorder()
	handlerUnderTest.ServeHTTP(recForbidden, reqForbidden.WithContext(ctxForbidden))

	if recForbidden.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", recForbidden.Code)
	}

	// 3. Unauthorized case: no claims in context
	reqNoClaims := httptest.NewRequest("GET", "/", nil)
	recNoClaims := httptest.NewRecorder()
	handlerUnderTest.ServeHTTP(recNoClaims, reqNoClaims)

	if recNoClaims.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", recNoClaims.Code)
	}
}
