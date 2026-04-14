package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
)

// BackchannelLogoutHandler handles Keycloak backchannel logout requests.
// When an admin terminates a user's session in Keycloak, Keycloak sends a
// POST request to this endpoint with a signed logout_token JWT.
// KNOX validates the token, extracts the Keycloak session ID (sid),
// and immediately destroys the matching local session.
//
// Endpoint: POST /auth/backchannel-logout
// Content-Type: application/x-www-form-urlencoded
// Body: logout_token=<JWT>
type BackchannelLogoutHandler struct {
	sessionMgr *SessionManager
	keySet     *oidc.RemoteKeySet
	clientID   string
	issuerURL  string
}

// NewBackchannelLogoutHandler creates a new handler for Keycloak backchannel logout.
func NewBackchannelLogoutHandler(sessionMgr *SessionManager, keySet *oidc.RemoteKeySet, clientID, issuerURL string) *BackchannelLogoutHandler {
	return &BackchannelLogoutHandler{
		sessionMgr: sessionMgr,
		keySet:     keySet,
		clientID:   clientID,
		issuerURL:  issuerURL,
	}
}

// logoutTokenClaims represents the claims in a Keycloak logout token JWT.
type logoutTokenClaims struct {
	Issuer    string                 `json:"iss"`
	Subject   string                 `json:"sub"`
	Audience  audience               `json:"aud"`
	IAT       json.Number            `json:"iat"`
	JTI       string                 `json:"jti"`
	Events    map[string]interface{} `json:"events"`
	SessionID string                 `json:"sid"`
}

// audience handles the JWT "aud" claim which can be either a string or array.
type audience []string

func (a *audience) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*a = []string{s}
		return nil
	}
	// Then try array of strings
	var ss []string
	if err := json.Unmarshal(data, &ss); err != nil {
		return err
	}
	*a = ss
	return nil
}

func (a audience) Contains(target string) bool {
	for _, v := range a {
		if v == target {
			return true
		}
	}
	return false
}

// ServeHTTP handles the backchannel logout POST request from Keycloak.
func (h *BackchannelLogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		slog.Error("Backchannel logout: failed to parse form", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	logoutToken := r.FormValue("logout_token")
	if logoutToken == "" {
		slog.Error("Backchannel logout: missing logout_token")
		http.Error(w, "Missing logout_token", http.StatusBadRequest)
		return
	}

	// Verify JWT signature using Keycloak's JWKS endpoint
	ctx := context.Background()
	payload, err := h.keySet.VerifySignature(ctx, logoutToken)
	if err != nil {
		slog.Error("Backchannel logout: signature verification failed", "error", err)
		http.Error(w, "Invalid token signature", http.StatusBadRequest)
		return
	}

	// Parse claims
	var claims logoutTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		slog.Error("Backchannel logout: failed to parse claims", "error", err)
		http.Error(w, "Invalid token claims", http.StatusBadRequest)
		return
	}

	// Validate the logout token
	if err := h.validateLogoutToken(claims); err != nil {
		slog.Error("Backchannel logout: validation failed", "error", err)
		http.Error(w, fmt.Sprintf("Token validation failed: %s", err), http.StatusBadRequest)
		return
	}

	// Destroy the session matching the Keycloak session ID — instant revocation
	if claims.SessionID != "" {
		destroyed := h.sessionMgr.DestroyByKeycloakSID(claims.SessionID)
		if destroyed {
			slog.Info("Backchannel logout: session destroyed successfully",
				"keycloak_sid", claims.SessionID,
				"subject", claims.Subject,
			)
		} else {
			slog.Warn("Backchannel logout: no matching session found",
				"keycloak_sid", claims.SessionID,
				"subject", claims.Subject,
			)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// validateLogoutToken validates the claims of a Keycloak logout token.
func (h *BackchannelLogoutHandler) validateLogoutToken(claims logoutTokenClaims) error {
	// Verify issuer matches our configured OIDC issuer
	if claims.Issuer != h.issuerURL {
		return fmt.Errorf("invalid issuer: got %s, expected %s", claims.Issuer, h.issuerURL)
	}

	// Verify audience contains our client ID
	if !claims.Audience.Contains(h.clientID) {
		return fmt.Errorf("client ID %s not found in audience", h.clientID)
	}

	// Verify the events claim contains the backchannel-logout event
	if _, ok := claims.Events["http://schemas.openid.net/event/backchannel-logout"]; !ok {
		return fmt.Errorf("missing backchannel-logout event in token")
	}

	// Verify session ID is present
	if claims.SessionID == "" {
		return fmt.Errorf("missing session ID (sid) in logout token")
	}

	return nil
}
