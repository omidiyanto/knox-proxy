package middleware

import (
	"knox-proxy/auth"
	"knox-proxy/policy"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── Auth Middleware Tests ─────────────────────────────────────────────────────

func TestAuth_ValidSession(t *testing.T) {
	sm := auth.NewSessionManager(3600)
	id, _ := sm.CreateSession("user@test.com", "Test User", []string{"/team"}, []string{"run:wf1"}, "", "")

	mw := Auth(sm)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUserFromContext(r.Context())
		if user == nil {
			t.Error("expected user in context")
		}
		if user.Email != "user@test.com" {
			t.Errorf("expected user@test.com, got %s", user.Email)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "knox_session", Value: id})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuth_NoSession_BrowserRequest(t *testing.T) {
	sm := auth.NewSessionManager(3600)
	mw := Auth(sm)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/home/workflows", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", rec.Code)
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "/auth/login") {
		t.Errorf("expected redirect to /auth/login, got %s", location)
	}
}

func TestAuth_NoSession_APIRequest(t *testing.T) {
	sm := auth.NewSessionManager(3600)
	mw := Auth(sm)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/rest/workflows", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// Note: Public paths (auth/*, healthz) are handled by the main router,
// not the Auth middleware. They are registered on separate mux routes
// that bypass the middleware chain entirely.

// ── Policy Middleware Tests ──────────────────────────────────────────────────

func TestPolicy_ReadOnlyAllowed(t *testing.T) {
	engine := policy.NewEngine()
	sanitizer := policy.NewSanitizer(nil, "")
	mw := Policy(engine, sanitizer)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/rest/workflows", nil)
	// Simulate authenticated context with user
	ctx := SetUserInContext(req.Context(), &RequestUser{
		Email:    "user@test.com",
		JITRoles: nil,
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", rec.Code)
	}
}

func TestPolicy_MutationDeniedWithoutRole(t *testing.T) {
	engine := policy.NewEngine()
	sanitizer := policy.NewSanitizer(nil, "")
	mw := Policy(engine, sanitizer)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for denied request")
	}))

	req := httptest.NewRequest("PATCH", "/rest/workflows/wf1", nil)
	ctx := SetUserInContext(req.Context(), &RequestUser{
		Email:    "user@test.com",
		JITRoles: nil,
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestPolicy_MutationAllowedWithRole(t *testing.T) {
	engine := policy.NewEngine()
	sanitizer := policy.NewSanitizer(nil, "")
	mw := Policy(engine, sanitizer)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("PATCH", "/rest/workflows/wf1", nil)
	ctx := SetUserInContext(req.Context(), &RequestUser{
		Email:    "user@test.com",
		JITRoles: []string{"edit:wf1"},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with edit role, got %d", rec.Code)
	}
}

func TestPolicy_SanitizerBlocks(t *testing.T) {
	engine := policy.NewEngine()
	sanitizer := policy.NewSanitizer([]string{"n8n-nodes-base.executeCommand"}, "")
	mw := Policy(engine, sanitizer)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for blocked node")
	}))

	body := `{"nodes":[{"type":"n8n-nodes-base.executeCommand","name":"Shell"}]}`
	req := httptest.NewRequest("PATCH", "/rest/workflows/wf1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := SetUserInContext(req.Context(), &RequestUser{
		Email:    "user@test.com",
		JITRoles: []string{"edit:wf1"},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for dangerous node, got %d", rec.Code)
	}
}

// ── Helper Function Tests ────────────────────────────────────────────────────

func TestIsAPIRequest(t *testing.T) {
	tests := []struct {
		name     string
		accept   string
		xhr      string
		path     string
		expected bool
	}{
		{"JSON accept", "application/json", "", "/rest/workflows", true},
		{"XHR header", "", "XMLHttpRequest", "/rest/workflows", true},
		{"REST path", "", "", "/rest/workflows", true},
		{"API path", "", "", "/api/something", true},
		{"HTML browser", "text/html", "", "/home/workflows", false},
		{"No headers, non-API path", "", "", "/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			if tt.xhr != "" {
				req.Header.Set("X-Requested-With", tt.xhr)
			}
			got := isAPIRequest(req)
			if got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestIsMutationMethod(t *testing.T) {
	mutations := []string{"POST", "PUT", "PATCH", "DELETE"}
	for _, m := range mutations {
		if !isMutationMethod(m) {
			t.Errorf("expected %s to be mutation", m)
		}
	}

	readOnly := []string{"GET", "HEAD", "OPTIONS"}
	for _, m := range readOnly {
		if isMutationMethod(m) {
			t.Errorf("expected %s to not be mutation", m)
		}
	}
}

func TestIsWorkflowPath(t *testing.T) {
	if !isWorkflowPath("/rest/workflows") {
		t.Error("expected /rest/workflows to be workflow path")
	}
	if !isWorkflowPath("/rest/workflows/abc123") {
		t.Error("expected /rest/workflows/abc123 to be workflow path")
	}
	if isWorkflowPath("/rest/credentials") {
		t.Error("expected /rest/credentials to not be workflow path")
	}
	if isWorkflowPath("/") {
		t.Error("expected / to not be workflow path")
	}
}
