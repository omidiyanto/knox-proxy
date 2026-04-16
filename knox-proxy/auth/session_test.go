package auth

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSessionManager_CreateAndGet(t *testing.T) {
	sm := NewSessionManager(3600)

	id, err := sm.CreateSession("user@test.com", "Test User", []string{"/n8n-test-team"}, []string{"run:wf1"}, "kc-sid-1", "raw-id-token")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}

	session := sm.GetSession(id)
	if session == nil {
		t.Fatal("expected session, got nil")
	}
	if session.Email != "user@test.com" {
		t.Errorf("expected email user@test.com, got %s", session.Email)
	}
	if session.DisplayName != "Test User" {
		t.Errorf("expected display name 'Test User', got %s", session.DisplayName)
	}
	if len(session.Groups) != 1 || session.Groups[0] != "/n8n-test-team" {
		t.Errorf("unexpected groups: %v", session.Groups)
	}
	if len(session.JITRoles) != 1 || session.JITRoles[0] != "run:wf1" {
		t.Errorf("unexpected JIT roles: %v", session.JITRoles)
	}
	if session.KeycloakSID != "kc-sid-1" {
		t.Errorf("expected keycloak SID kc-sid-1, got %s", session.KeycloakSID)
	}
	if session.IDToken != "raw-id-token" {
		t.Errorf("unexpected ID token")
	}
}

func TestSessionManager_GetUnknownSession(t *testing.T) {
	sm := NewSessionManager(3600)

	session := sm.GetSession("nonexistent")
	if session != nil {
		t.Error("expected nil for unknown session ID")
	}
}

func TestSessionManager_ExpiredSession(t *testing.T) {
	sm := NewSessionManager(1) // 1 second TTL

	id, _ := sm.CreateSession("user@test.com", "User", nil, nil, "", "")

	// Wait for expiration
	time.Sleep(1100 * time.Millisecond)

	session := sm.GetSession(id)
	if session != nil {
		t.Error("expected nil for expired session")
	}
}

func TestSessionManager_DestroySession(t *testing.T) {
	sm := NewSessionManager(3600)

	id, _ := sm.CreateSession("user@test.com", "User", nil, nil, "kc-sid", "")

	sm.DestroySession(id)

	session := sm.GetSession(id)
	if session != nil {
		t.Error("expected nil after destroy")
	}
}

func TestSessionManager_DestroyByKeycloakSID(t *testing.T) {
	sm := NewSessionManager(3600)

	sm.CreateSession("user@test.com", "User", nil, nil, "kc-sid-abc", "")

	destroyed := sm.DestroyByKeycloakSID("kc-sid-abc")
	if !destroyed {
		t.Error("expected successful destroy by Keycloak SID")
	}

	// Try again — should fail
	destroyed = sm.DestroyByKeycloakSID("kc-sid-abc")
	if destroyed {
		t.Error("expected false for already destroyed session")
	}
}

func TestSessionManager_DestroyByKeycloakSID_Unknown(t *testing.T) {
	sm := NewSessionManager(3600)

	destroyed := sm.DestroyByKeycloakSID("unknown-sid")
	if destroyed {
		t.Error("expected false for unknown SID")
	}
}

func TestSessionManager_CleanExpired(t *testing.T) {
	sm := NewSessionManager(1)

	sm.CreateSession("user1@test.com", "User1", nil, nil, "", "")
	sm.CreateSession("user2@test.com", "User2", nil, nil, "", "")

	time.Sleep(1100 * time.Millisecond)

	// Create a non-expired session
	id3, _ := sm.CreateSession("user3@test.com", "User3", nil, nil, "", "")

	count := sm.CleanExpired()
	if count != 2 {
		t.Errorf("expected 2 cleaned, got %d", count)
	}

	// Session 3 should still exist
	if sm.GetSession(id3) == nil {
		t.Error("expected session 3 to still exist")
	}
}

func TestSessionManager_CookieRoundTrip(t *testing.T) {
	sm := NewSessionManager(3600)

	id, _ := sm.CreateSession("user@test.com", "User", nil, nil, "", "")

	// Set cookie on response
	rec := httptest.NewRecorder()
	sm.SetSessionCookie(rec, id)

	// Extract cookie from response
	resp := rec.Result()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookie to be set")
	}

	knoxCookie := cookies[0]
	if knoxCookie.Name != cookieName {
		t.Errorf("expected cookie name %s, got %s", cookieName, knoxCookie.Name)
	}
	if knoxCookie.Value != id {
		t.Errorf("expected cookie value %s, got %s", id, knoxCookie.Value)
	}
	if !knoxCookie.HttpOnly {
		t.Error("expected HttpOnly cookie")
	}

	// Use cookie in request
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: id})

	extractedID := sm.GetSessionIDFromRequest(req)
	if extractedID != id {
		t.Errorf("expected %s, got %s", id, extractedID)
	}
}

func TestSessionManager_ClearSessionCookie(t *testing.T) {
	sm := NewSessionManager(3600)

	rec := httptest.NewRecorder()
	sm.ClearSessionCookie(rec)

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected clear cookie to be set")
	}
	if cookies[0].MaxAge != -1 {
		t.Errorf("expected MaxAge -1, got %d", cookies[0].MaxAge)
	}
}

func TestSessionManager_GetSessionIDFromRequest_NoCookie(t *testing.T) {
	sm := NewSessionManager(3600)

	req := httptest.NewRequest("GET", "/", nil)
	id := sm.GetSessionIDFromRequest(req)
	if id != "" {
		t.Errorf("expected empty, got %s", id)
	}
}

func TestSessionManager_ConcurrentAccess(t *testing.T) {
	sm := NewSessionManager(3600)
	var wg sync.WaitGroup

	// Create many sessions concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.CreateSession("user@test.com", "User", nil, nil, "", "")
		}()
	}
	wg.Wait()

	// Clean expired concurrently with reads
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			sm.CleanExpired()
		}()
		go func() {
			defer wg.Done()
			sm.GetSession("some-id")
		}()
	}
	wg.Wait()
}

func TestGenerateSessionID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := generateSessionID()
		if err != nil {
			t.Fatalf("failed to generate session ID: %v", err)
		}
		if len(id) != 64 { // 32 bytes = 64 hex chars
			t.Errorf("expected 64 char hex string, got %d chars", len(id))
		}
		if ids[id] {
			t.Errorf("duplicate session ID: %s", id)
		}
		ids[id] = true
	}
}
