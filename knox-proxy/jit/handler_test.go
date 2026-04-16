package jit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// ── Route Dispatch Tests ─────────────────────────────────────────────────────

func TestHandler_RouteDispatch_UnknownPath(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest("GET", "/knox-api/unknown", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown route, got %d", rec.Code)
	}
}

func TestHandler_RouteDispatch_CorrectRouting(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"request-jit requires POST", "GET", "/knox-api/request-jit"},
		{"tickets is GET", "POST", "/knox-api/tickets"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{}
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			// Wrong method should return 404 (unknown route)
			if rec.Code != http.StatusNotFound {
				t.Errorf("expected 404 for wrong method, got %d", rec.Code)
			}
		})
	}
}

// ── Validation Tests ─────────────────────────────────────────────────────────

func TestValidateCreateRequest(t *testing.T) {
	h := &Handler{
		maxDuration: 7 * 24 * time.Hour,
	}

	tests := []struct {
		name        string
		body        CreateTicketRequest
		expectCount int // number of expected errors
	}{
		{
			"valid request",
			CreateTicketRequest{
				WorkflowID:  "wf123",
				AccessType:  []string{"run"},
				Description: "This is a valid test request for access",
				PeriodStart: time.Now().Add(1 * time.Minute).Format(time.RFC3339),
				PeriodEnd:   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
			0,
		},
		{
			"missing workflow ID",
			CreateTicketRequest{
				WorkflowID:  "",
				AccessType:  []string{"run"},
				Description: "Valid description here",
				PeriodStart: time.Now().Format(time.RFC3339),
				PeriodEnd:   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
			1,
		},
		{
			"missing access type",
			CreateTicketRequest{
				WorkflowID:  "wf123",
				AccessType:  []string{},
				Description: "Valid description here",
				PeriodStart: time.Now().Format(time.RFC3339),
				PeriodEnd:   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
			1,
		},
		{
			"invalid access type",
			CreateTicketRequest{
				WorkflowID:  "wf123",
				AccessType:  []string{"admin"},
				Description: "Valid description here",
				PeriodStart: time.Now().Format(time.RFC3339),
				PeriodEnd:   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
			1,
		},
		{
			"description too short",
			CreateTicketRequest{
				WorkflowID:  "wf123",
				AccessType:  []string{"run"},
				Description: "short",
				PeriodStart: time.Now().Format(time.RFC3339),
				PeriodEnd:   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
			1,
		},
		{
			"end before start",
			CreateTicketRequest{
				WorkflowID:  "wf123",
				AccessType:  []string{"run"},
				Description: "Valid description here",
				PeriodStart: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				PeriodEnd:   time.Now().Format(time.RFC3339),
			},
			1,
		},
		{
			"duration exceeds max (>7 days)",
			CreateTicketRequest{
				WorkflowID:  "wf123",
				AccessType:  []string{"run"},
				Description: "Valid description here",
				PeriodStart: time.Now().Format(time.RFC3339),
				PeriodEnd:   time.Now().Add(8 * 24 * time.Hour).Format(time.RFC3339),
			},
			1,
		},
		{
			"empty description",
			CreateTicketRequest{
				WorkflowID:  "wf123",
				AccessType:  []string{"run"},
				Description: "",
				PeriodStart: time.Now().Format(time.RFC3339),
				PeriodEnd:   time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
			1,
		},
		{
			"invalid workflow ID characters",
			CreateTicketRequest{
				WorkflowID:  "wf/../123",
				AccessType:  []string{"run"},
				Description: "Valid description here",
				PeriodStart: time.Now().Format(time.RFC3339),
				PeriodEnd:   time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
			1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := h.validateCreateRequest(&tt.body)
			if len(errs) != tt.expectCount {
				t.Errorf("expected %d errors, got %d: %v", tt.expectCount, len(errs), errs)
			}
		})
	}
}

// ── Helper Function Tests ────────────────────────────────────────────────────

func TestExtractPathParam(t *testing.T) {
	tests := []struct {
		path     string
		prefix   string
		expected string
	}{
		{"/knox-api/tickets/abc123", "/knox-api/tickets/", "abc123"},
		{"/knox-api/admin/tickets/def456", "/knox-api/admin/tickets/", "def456"},
		{"/knox-api/tickets/", "/knox-api/tickets/", ""},
		{"/knox-api/tickets", "/knox-api/tickets/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractPathParam(tt.path, tt.prefix)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestResolveTeam(t *testing.T) {
	tests := []struct {
		name     string
		groups   []string
		prefix   string
		expected string
	}{
		{"match found", []string{"/n8n-test-finance"}, "/n8n-test-", "finance"},
		{"first match", []string{"/other-group", "/n8n-test-dev"}, "/n8n-test-", "dev"},
		{"no match", []string{"/other-group"}, "/n8n-test-", ""},
		{"empty groups", []string{}, "/n8n-test-", ""},
		{"nil groups", nil, "/n8n-test-", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTeam(tt.groups, tt.prefix)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		value    string
		def      int
		expected int
	}{
		{"10", 5, 10},
		{"", 5, 5},
		{"abc", 5, 5},
		{"0", 5, 0},
		{"-1", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got := parseIntParam(tt.value, tt.def)
			if got != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestRequireAPIKey_Valid(t *testing.T) {
	h := &Handler{apiKey: "test-key-123"}

	req := httptest.NewRequest("GET", "/knox-api/admin/tickets", nil)
	req.Header.Set("X-Knox-API-Key", "test-key-123")
	rec := httptest.NewRecorder()

	passed := false
	h.requireAPIKey(rec, req, func(w http.ResponseWriter, r *http.Request) {
		passed = true
		w.WriteHeader(http.StatusOK)
	})

	if !passed {
		t.Error("expected handler to be called with valid API key")
	}
}

func TestRequireAPIKey_Missing(t *testing.T) {
	h := &Handler{apiKey: "test-key-123"}

	req := httptest.NewRequest("GET", "/knox-api/admin/tickets", nil)
	rec := httptest.NewRecorder()

	passed := false
	h.requireAPIKey(rec, req, func(w http.ResponseWriter, r *http.Request) {
		passed = true
	})

	if passed {
		t.Error("expected handler NOT to be called with missing API key")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAPIKey_Wrong(t *testing.T) {
	h := &Handler{apiKey: "test-key-123"}

	req := httptest.NewRequest("GET", "/knox-api/admin/tickets", nil)
	req.Header.Set("X-Knox-API-Key", "wrong-key")
	rec := httptest.NewRecorder()

	passed := false
	h.requireAPIKey(rec, req, func(w http.ResponseWriter, r *http.Request) {
		passed = true
	})

	if passed {
		t.Error("expected handler NOT to be called with wrong API key")
	}
}

func TestRequireAPIKey_Disabled(t *testing.T) {
	h := &Handler{apiKey: ""}

	req := httptest.NewRequest("GET", "/knox-api/admin/tickets", nil)
	rec := httptest.NewRecorder()

	h.requireAPIKey(rec, req, func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when API key is disabled")
	})

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when API key disabled, got %d", rec.Code)
	}
}

func TestRequireAPIKey_BearerToken(t *testing.T) {
	h := &Handler{apiKey: "test-key-123"}

	req := httptest.NewRequest("GET", "/knox-api/admin/tickets", nil)
	req.Header.Set("Authorization", "Bearer test-key-123")
	rec := httptest.NewRecorder()

	passed := false
	h.requireAPIKey(rec, req, func(w http.ResponseWriter, r *http.Request) {
		passed = true
		w.WriteHeader(http.StatusOK)
	})

	if !passed {
		t.Error("expected handler to be called with Bearer token")
	}
}
