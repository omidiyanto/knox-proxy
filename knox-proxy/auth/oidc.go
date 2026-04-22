package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"knox-proxy/audit"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCClaims represents the claims extracted from a Keycloak ID token.
type OIDCClaims struct {
	Email             string                          `json:"email"`
	Name              string                          `json:"name"`
	PreferredUsername string                          `json:"preferred_username"`
	Groups            []string                        `json:"groups"`
	SessionID         string                          `json:"sid"`
	ResourceAccess    map[string]ResourceAccessClaims `json:"resource_access"`
}

// ResourceAccessClaims holds client-level roles from Keycloak.
type ResourceAccessClaims struct {
	Roles []string `json:"roles"`
}

// OIDCAuth handles all OIDC authentication flows including login, callback,
// logout, and exposes session/key infrastructure for backchannel logout.
type OIDCAuth struct {
	provider     *oidc.Provider
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	sessionMgr   *SessionManager
	clientID     string
	issuerURL    string

	// State management for CSRF protection during OIDC flow
	statesMu sync.Mutex
	states   map[string]stateEntry

	// OIDC endpoints discovered from the provider
	endSessionEndpoint string
	jwksURL            string
}

type stateEntry struct {
	redirectURL string
	expiresAt   time.Time
}

// NewOIDCAuth initializes the OIDC authentication provider with retry logic
// for cold-start scenarios where Keycloak may not be ready yet.
func NewOIDCAuth(ctx context.Context, issuerURL, clientID, clientSecret, redirectURL string, sessionMgr *SessionManager) (*OIDCAuth, error) {
	var provider *oidc.Provider
	var err error
	for i := 0; i < 10; i++ {
		provider, err = oidc.NewProvider(ctx, issuerURL)
		if err == nil {
			break
		}
		slog.Warn("OIDC provider discovery failed, retrying...", "attempt", i+1, "error", err)
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider after retries: %w", err)
	}

	oauth2Config := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile", "groups"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	// Extract additional endpoints from OIDC discovery document
	var discoveryDoc struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
		JWKSURL            string `json:"jwks_uri"`
	}
	if err := provider.Claims(&discoveryDoc); err != nil {
		slog.Warn("Failed to extract discovery endpoints", "error", err)
	}

	auth := &OIDCAuth{
		provider:           provider,
		oauth2Config:       oauth2Config,
		verifier:           verifier,
		sessionMgr:         sessionMgr,
		clientID:           clientID,
		issuerURL:          issuerURL,
		states:             make(map[string]stateEntry),
		endSessionEndpoint: discoveryDoc.EndSessionEndpoint,
		jwksURL:            discoveryDoc.JWKSURL,
	}

	// Start background goroutine to clean expired state entries
	go auth.cleanStates()

	return auth, nil
}

// SessionMgr returns the underlying session manager.
func (a *OIDCAuth) SessionMgr() *SessionManager {
	return a.sessionMgr
}

// KeySet returns a remote JWKS key set for verifying logout token signatures.
func (a *OIDCAuth) KeySet() *oidc.RemoteKeySet {
	return oidc.NewRemoteKeySet(context.Background(), a.jwksURL)
}

// LoginHandler redirects the user to the Keycloak login page.
func (a *OIDCAuth) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state, err := generateRandomString(32)
	if err != nil {
		slog.Error("Failed to generate state", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Store state with the original redirect URL for post-login redirect
	a.statesMu.Lock()
	a.states[state] = stateEntry{
		redirectURL: r.URL.Query().Get("redirect"),
		expiresAt:   time.Now().Add(10 * time.Minute),
	}
	a.statesMu.Unlock()

	authURL := a.oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// CallbackHandler handles the OIDC callback after Keycloak authentication.
func (a *OIDCAuth) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Validate state parameter (CSRF protection)
	state := r.URL.Query().Get("state")
	a.statesMu.Lock()
	entry, ok := a.states[state]
	if ok {
		delete(a.states, state)
	}
	a.statesMu.Unlock()

	if !ok || time.Now().After(entry.expiresAt) {
		http.Error(w, "Invalid or expired state parameter", http.StatusBadRequest)
		return
	}

	// Check for errors returned by Keycloak
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		slog.Error("OIDC callback error", "error", errParam, "description", errDesc)
		http.Error(w, fmt.Sprintf("Authentication error: %s", errDesc), http.StatusUnauthorized)
		return
	}

	// Exchange authorization code for tokens
	code := r.URL.Query().Get("code")
	oauth2Token, err := a.oauth2Config.Exchange(ctx, code)
	if err != nil {
		slog.Error("Failed to exchange code", "error", err)
		http.Error(w, "Failed to exchange authorization code", http.StatusInternalServerError)
		return
	}

	// Extract and verify ID token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "No ID token in response", http.StatusInternalServerError)
		return
	}

	idToken, err := a.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		slog.Error("Failed to verify ID token", "error", err)
		http.Error(w, "Invalid ID token", http.StatusUnauthorized)
		return
	}

	// Extract claims from ID token
	var claims OIDCClaims
	if err := idToken.Claims(&claims); err != nil {
		slog.Error("Failed to extract claims", "error", err)
		http.Error(w, "Failed to read user claims", http.StatusInternalServerError)
		return
	}

	// Extract JIT roles from resource_access.<clientID>.roles
	var jitRoles []string
	if ra, exists := claims.ResourceAccess[a.clientID]; exists {
		jitRoles = ra.Roles
	}

	// Resolve display name: prefer "name" claim, fall back to preferred_username, then email
	displayName := claims.Name
	if displayName == "" {
		displayName = claims.PreferredUsername
	}
	if displayName == "" {
		displayName = claims.Email
	}

	// Create KNOX session
	sessionID, err := a.sessionMgr.CreateSession(
		claims.Email,
		displayName,
		claims.Groups,
		jitRoles,
		claims.SessionID,
		rawIDToken,
	)
	if err != nil {
		slog.Error("Failed to create session", "error", err)
		http.Error(w, "Session creation failed", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	a.sessionMgr.SetSessionCookie(w, sessionID)

	slog.Info("User authenticated successfully",
		"email", claims.Email,
		"groups", claims.Groups,
		"jit_roles", jitRoles,
		"keycloak_sid", claims.SessionID,
	)
	audit.Log("User authenticated (Login)",
		"email", claims.Email,
		"keycloak_sid", claims.SessionID,
		"assigned_jit_roles", jitRoles,
	)

	// Redirect to original URL or home page
	redirectTo := "/"
	if entry.redirectURL != "" {
		redirectTo = entry.redirectURL
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// LogoutHandler handles user-initiated logout (RP-Initiated Logout).
func (a *OIDCAuth) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	// Destroy KNOX session
	sessionID := a.sessionMgr.GetSessionIDFromRequest(r)
	if sessionID != "" {
		session := a.sessionMgr.GetSession(sessionID)
		a.sessionMgr.DestroySession(sessionID)
		emailToLog := "unknown"
		if session != nil {
			emailToLog = session.Email
		}
		slog.Info("User logged out", "session_id", sessionID, "email", emailToLog)
		audit.Log("User logged out", "email", emailToLog, "session_id", sessionID)
	}
	a.sessionMgr.ClearSessionCookie(w)

	// Redirect to Keycloak end_session_endpoint for full SSO logout
	if a.endSessionEndpoint != "" {
		logoutURL, _ := url.Parse(a.endSessionEndpoint)
		q := logoutURL.Query()
		q.Set("client_id", a.clientID)
		redirectURL, _ := url.Parse(a.oauth2Config.RedirectURL)
		redirectURL.Path = "/"
		q.Set("post_logout_redirect_uri", redirectURL.String())
		logoutURL.RawQuery = q.Encode()
		http.Redirect(w, r, logoutURL.String(), http.StatusFound)
		return
	}

	// Fallback: redirect to KNOX login
	http.Redirect(w, r, "/auth/login", http.StatusFound)
}

// cleanStates periodically removes expired OIDC state entries.
func (a *OIDCAuth) cleanStates() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		a.statesMu.Lock()
		now := time.Now()
		for key, entry := range a.states {
			if now.After(entry.expiresAt) {
				delete(a.states, key)
			}
		}
		a.statesMu.Unlock()
	}
}

// generateRandomString generates a cryptographically random hex string.
func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
