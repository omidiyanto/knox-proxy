package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsStreamingRequest(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		upgrade  string
		expected bool
	}{
		{"websocket upgrade", "/any/path", "websocket", true},
		{"SSE push endpoint", "/rest/push", "", true},
		{"SSE events endpoint", "/rest/events", "", true},
		{"normal GET", "/rest/workflows", "", false},
		{"POST request", "/rest/workflows/run", "", false},
		{"WebSocket case insensitive", "/any", "WebSocket", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.upgrade != "" {
				req.Header.Set("Upgrade", tt.upgrade)
			}
			got := isStreamingRequest(req)
			if got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestBufferedResponse_Write(t *testing.T) {
	br := newBufferedResponse()

	n, err := br.Write([]byte("hello "))
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if n != 6 {
		t.Errorf("expected 6 bytes written, got %d", n)
	}

	br.Write([]byte("world"))

	if br.body.String() != "hello world" {
		t.Errorf("expected 'hello world', got %q", br.body.String())
	}
}

func TestBufferedResponse_WriteHeader(t *testing.T) {
	br := newBufferedResponse()

	if br.statusCode != http.StatusOK {
		t.Errorf("expected default 200, got %d", br.statusCode)
	}

	br.WriteHeader(http.StatusNotFound)

	if br.statusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", br.statusCode)
	}
}

func TestBufferedResponse_WriteHeaderOnlyOnce(t *testing.T) {
	br := newBufferedResponse()

	br.WriteHeader(http.StatusCreated)
	br.WriteHeader(http.StatusNotFound) // should be ignored

	if br.statusCode != http.StatusCreated {
		t.Errorf("expected 201 (first call wins), got %d", br.statusCode)
	}
}

func TestBufferedResponse_FlushTo(t *testing.T) {
	br := newBufferedResponse()
	br.Header().Set("Content-Type", "application/json")
	br.WriteHeader(http.StatusCreated)
	br.Write([]byte(`{"status":"ok"}`))

	// Flush to real response
	rec := httptest.NewRecorder()
	br.flushTo(rec)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type header, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestNewBufferedResponse_Defaults(t *testing.T) {
	br := newBufferedResponse()

	if br.statusCode != http.StatusOK {
		t.Errorf("expected default status 200, got %d", br.statusCode)
	}
	if br.body.Len() != 0 {
		t.Errorf("expected empty body, got %d bytes", br.body.Len())
	}
	if br.Header() == nil {
		t.Error("expected non-nil header")
	}
}

func TestBufferedResponse_ImplicitOK(t *testing.T) {
	br := newBufferedResponse()

	// Write without calling WriteHeader first
	br.Write([]byte("data"))

	// Should implicitly set 200
	if br.statusCode != http.StatusOK {
		t.Errorf("expected implicit 200, got %d", br.statusCode)
	}
	if !br.wroteHeader {
		t.Error("expected wroteHeader to be true after Write")
	}
}
