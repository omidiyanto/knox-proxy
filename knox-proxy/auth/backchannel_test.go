package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackchannelLogoutHandler_WrongMethod(t *testing.T) {
	sm := NewSessionManager(3600)
	handler := &BackchannelLogoutHandler{
		sessionMgr: sm,
		clientID:   "test-client",
		issuerURL:  "http://keycloak/realms/test",
	}

	req := httptest.NewRequest("GET", "/auth/backchannel-logout", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestBackchannelLogoutHandler_MissingLogoutToken(t *testing.T) {
	sm := NewSessionManager(3600)
	handler := &BackchannelLogoutHandler{
		sessionMgr: sm,
		clientID:   "test-client",
		issuerURL:  "http://keycloak/realms/test",
	}

	req := httptest.NewRequest("POST", "/auth/backchannel-logout", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestValidateLogoutToken_ValidClaims(t *testing.T) {
	handler := &BackchannelLogoutHandler{
		clientID:  "test-client",
		issuerURL: "http://keycloak/realms/test",
	}

	claims := logoutTokenClaims{
		Issuer:    "http://keycloak/realms/test",
		Audience:  audience{"test-client"},
		SessionID: "sid-123",
		Events: map[string]interface{}{
			"http://schemas.openid.net/event/backchannel-logout": map[string]interface{}{},
		},
	}

	err := handler.validateLogoutToken(claims)
	if err != nil {
		t.Errorf("expected valid claims to pass, got: %v", err)
	}
}

func TestValidateLogoutToken_InvalidIssuer(t *testing.T) {
	handler := &BackchannelLogoutHandler{
		clientID:  "test-client",
		issuerURL: "http://keycloak/realms/test",
	}

	claims := logoutTokenClaims{
		Issuer:    "http://evil-keycloak/realms/test",
		Audience:  audience{"test-client"},
		SessionID: "sid-123",
		Events: map[string]interface{}{
			"http://schemas.openid.net/event/backchannel-logout": map[string]interface{}{},
		},
	}

	err := handler.validateLogoutToken(claims)
	if err == nil {
		t.Error("expected error for invalid issuer")
	}
}

func TestValidateLogoutToken_InvalidAudience(t *testing.T) {
	handler := &BackchannelLogoutHandler{
		clientID:  "test-client",
		issuerURL: "http://keycloak/realms/test",
	}

	claims := logoutTokenClaims{
		Issuer:    "http://keycloak/realms/test",
		Audience:  audience{"other-client"},
		SessionID: "sid-123",
		Events: map[string]interface{}{
			"http://schemas.openid.net/event/backchannel-logout": map[string]interface{}{},
		},
	}

	err := handler.validateLogoutToken(claims)
	if err == nil {
		t.Error("expected error for invalid audience")
	}
}

func TestValidateLogoutToken_MissingEvent(t *testing.T) {
	handler := &BackchannelLogoutHandler{
		clientID:  "test-client",
		issuerURL: "http://keycloak/realms/test",
	}

	claims := logoutTokenClaims{
		Issuer:    "http://keycloak/realms/test",
		Audience:  audience{"test-client"},
		SessionID: "sid-123",
		Events:    map[string]interface{}{},
	}

	err := handler.validateLogoutToken(claims)
	if err == nil {
		t.Error("expected error for missing backchannel-logout event")
	}
}

func TestValidateLogoutToken_MissingSID(t *testing.T) {
	handler := &BackchannelLogoutHandler{
		clientID:  "test-client",
		issuerURL: "http://keycloak/realms/test",
	}

	claims := logoutTokenClaims{
		Issuer:   "http://keycloak/realms/test",
		Audience: audience{"test-client"},
		Events: map[string]interface{}{
			"http://schemas.openid.net/event/backchannel-logout": map[string]interface{}{},
		},
	}

	err := handler.validateLogoutToken(claims)
	if err == nil {
		t.Error("expected error for missing SID")
	}
}

func TestAudience_UnmarshalJSON(t *testing.T) {
	// String value
	var a1 audience
	if err := json.Unmarshal([]byte(`"single-client"`), &a1); err != nil {
		t.Fatalf("failed to unmarshal string: %v", err)
	}
	if len(a1) != 1 || a1[0] != "single-client" {
		t.Errorf("unexpected audience: %v", a1)
	}

	// Array value
	var a2 audience
	if err := json.Unmarshal([]byte(`["client1", "client2"]`), &a2); err != nil {
		t.Fatalf("failed to unmarshal array: %v", err)
	}
	if len(a2) != 2 {
		t.Errorf("expected 2 elements, got %d", len(a2))
	}
}

func TestAudience_Contains(t *testing.T) {
	a := audience{"client1", "client2", "client3"}

	if !a.Contains("client2") {
		t.Error("expected Contains to return true for client2")
	}
	if a.Contains("unknown") {
		t.Error("expected Contains to return false for unknown")
	}
}
