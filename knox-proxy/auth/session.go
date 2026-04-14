package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const cookieName = "knox_session"

// UserSession represents an authenticated user's session data stored in memory.
type UserSession struct {
	ID          string
	Email       string
	DisplayName string   // Full name from Keycloak "name" claim
	Groups      []string
	JITRoles    []string
	KeycloakSID string // Keycloak session ID — used for backchannel logout correlation
	IDToken     string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// SessionManager manages user sessions in memory with support for
// standard CRUD operations and Keycloak backchannel logout by SID.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*UserSession // sessionID → session
	sidIndex map[string]string       // keycloakSID → sessionID (reverse index)
	maxAge   int
}

// NewSessionManager creates a new in-memory session manager.
func NewSessionManager(maxAge int) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*UserSession),
		sidIndex: make(map[string]string),
		maxAge:   maxAge,
	}
}

// CreateSession creates a new session and returns the session ID.
func (sm *SessionManager) CreateSession(email, displayName string, groups, jitRoles []string, keycloakSID, idToken string) (string, error) {
	id, err := generateSessionID()
	if err != nil {
		return "", err
	}

	session := &UserSession{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
		Groups:      groups,
		JITRoles:    jitRoles,
		KeycloakSID: keycloakSID,
		IDToken:     idToken,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Duration(sm.maxAge) * time.Second),
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.sessions[id] = session
	if keycloakSID != "" {
		sm.sidIndex[keycloakSID] = id
	}

	return id, nil
}

// GetSession retrieves a session by session ID. Returns nil if not found or expired.
func (sm *SessionManager) GetSession(sessionID string) *UserSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[sessionID]
	if !ok || time.Now().After(session.ExpiresAt) {
		return nil
	}
	return session
}

// DestroySession removes a session by session ID.
func (sm *SessionManager) DestroySession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, ok := sm.sessions[sessionID]; ok {
		if session.KeycloakSID != "" {
			delete(sm.sidIndex, session.KeycloakSID)
		}
		delete(sm.sessions, sessionID)
	}
}

// DestroyByKeycloakSID removes a session matching the given Keycloak session ID.
// This is the core mechanism for backchannel logout — when Keycloak revokes a
// session, KNOX can instantly invalidate it via this reverse lookup.
func (sm *SessionManager) DestroyByKeycloakSID(keycloakSID string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sessionID, ok := sm.sidIndex[keycloakSID]
	if !ok {
		return false
	}
	delete(sm.sessions, sessionID)
	delete(sm.sidIndex, keycloakSID)
	return true
}

// GetSessionIDFromRequest extracts the session ID from the request cookie.
func (sm *SessionManager) GetSessionIDFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// SetSessionCookie sets the session cookie on the response.
func (sm *SessionManager) SetSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   sm.maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie from the client.
func (sm *SessionManager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// CleanExpired removes all expired sessions. Call periodically via a goroutine.
func (sm *SessionManager) CleanExpired() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	count := 0
	now := time.Now()
	for id, session := range sm.sessions {
		if now.After(session.ExpiresAt) {
			if session.KeycloakSID != "" {
				delete(sm.sidIndex, session.KeycloakSID)
			}
			delete(sm.sessions, id)
			count++
		}
	}
	return count
}

// generateSessionID generates a cryptographically random 64-character hex string.
func generateSessionID() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
