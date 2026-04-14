package cookie

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const knoxBrowserID = "knox-proxy-session"

type cachedCookie struct {
	value     string
	expiresAt time.Time
}

// Pool manages in-memory n8n authentication cookies for team accounts.
// It lazily authenticates against n8n's login API and caches the resulting
// n8n-auth cookie with a configurable TTL.
//
// Cookies are cached per email (one cookie per team account). All requests
// for a team share the same cookie, which uses the fixed knoxBrowserID.
type Pool struct {
	mu         sync.RWMutex
	cookies    map[string]*cachedCookie // keyed by email address
	n8nURL     string
	ttl        time.Duration
	httpClient *http.Client
}

// NewPool creates a new cookie pool manager.
// n8nInternalURL is the direct n8n URL (e.g., http://n8n:5678).
// ttlSeconds is how long to cache each cookie before re-authenticating.
func NewPool(n8nInternalURL string, ttlSeconds int) *Pool {
	return &Pool{
		cookies: make(map[string]*cachedCookie),
		n8nURL:  n8nInternalURL,
		ttl:     time.Duration(ttlSeconds) * time.Second,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			// Don't follow redirects — we need the Set-Cookie header from the login response
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// BrowserID returns the fixed browser identifier that KNOX uses for all
// n8n logins. The proxy Director must set the `browser-id` request header
// to this value on EVERY outgoing request to ensure consistency with the
// JWT cookie's embedded browserId.
func (p *Pool) BrowserID() string {
	return knoxBrowserID
}

// GetCookie returns a valid n8n-auth cookie for the given email/password.
// If a cached cookie exists and is not expired, it returns the cached value.
// Otherwise, it performs a fresh login to n8n.
func (p *Pool) GetCookie(email, password string) (string, error) {
	// Check cache first
	p.mu.RLock()
	cached, ok := p.cookies[email]
	p.mu.RUnlock()

	if ok && time.Now().Before(cached.expiresAt) {
		return cached.value, nil
	}

	// Cache miss or expired — login to n8n
	slog.Debug("Cookie cache miss, logging in to n8n", "email", email)
	cookieValue, err := p.loginToN8N(email, password)
	if err != nil {
		return "", err
	}

	// Update cache
	p.mu.Lock()
	p.cookies[email] = &cachedCookie{
		value:     cookieValue,
		expiresAt: time.Now().Add(p.ttl),
	}
	p.mu.Unlock()

	return cookieValue, nil
}

// loginToN8N performs a POST /rest/login request to n8n and extracts the n8n-auth cookie.
// Uses "emailOrLdapLoginId" field to match n8n v2.x API format.
func (p *Pool) loginToN8N(email, password string) (string, error) {
	body := map[string]string{
		"emailOrLdapLoginId": email,
		"password":           password,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal login body: %w", err)
	}

	req, err := http.NewRequest("POST", p.n8nURL+"/rest/login", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	req.Header.Set("browser-id", knoxBrowserID)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("n8n login request failed: %w", err)
	}
	defer resp.Body.Close()
	// Drain body to allow connection reuse
	io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("n8n login failed with HTTP %d for email %s", resp.StatusCode, email)
	}

	// Extract n8n-auth cookie from Set-Cookie header
	for _, c := range resp.Cookies() {
		if c.Name == "n8n-auth" {
			slog.Debug("Successfully obtained n8n-auth cookie", "email", email)
			return c.Value, nil
		}
	}

	return "", fmt.Errorf("n8n-auth cookie not found in login response for %s", email)
}

// Invalidate removes a cached cookie for the given email, forcing re-login on next access.
func (p *Pool) Invalidate(email string) {
	p.mu.Lock()
	delete(p.cookies, email)
	p.mu.Unlock()
	slog.Debug("Cookie invalidated", "email", email)
}

// InvalidateAll clears the entire cookie cache.
func (p *Pool) InvalidateAll() {
	p.mu.Lock()
	p.cookies = make(map[string]*cachedCookie)
	p.mu.Unlock()
	slog.Info("All cookies invalidated")
}
