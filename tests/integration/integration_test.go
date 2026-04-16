package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ── Config ───────────────────────────────────────────────────────────────────

var (
	knoxURL     = envOr("KNOX_TEST_BASE_URL", "http://localhost:8443")
	keycloakURL = envOr("KEYCLOAK_URL", "http://localhost:8080")
	knoxAPIKey  = envOr("KNOX_API_KEY", "test-api-key-for-ci")
	testUser    = envOr("TEST_USER", "testuser")
	testPass    = envOr("TEST_PASS", "testpass")
	adminUser   = envOr("ADMIN_USER", "adminuser")
	adminPass   = envOr("ADMIN_PASS", "testpass")
	restrictedUser = envOr("RESTRICTED_USER", "restricteduser")
	restrictedPass = envOr("RESTRICTED_PASS", "testpass")
	testWorkflowID = envOr("TEST_WORKFLOW_ID", "")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func customTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == "keycloak:8080" {
				addr = "127.0.0.1:8080"
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
}

func newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Transport: customTransport(),
		Jar:       jar,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}
}

func newFollowingClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Transport: customTransport(),
		Jar:       jar,
		Timeout:   30 * time.Second,
	}
}

// loginAsKeycloak performs OIDC login via Keycloak and returns an authenticated HTTP client
func loginAsKeycloak(t *testing.T, username, password string) *http.Client {
	t.Helper()

	client := newFollowingClient()

	// Step 1: Hit /auth/login → should redirect to Keycloak
	resp, err := client.Get(knoxURL + "/auth/login")
	if err != nil {
		t.Fatalf("failed to start login: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Step 2: Extract login form action URL from Keycloak HTML
	actionRegex := regexp.MustCompile(`action="([^"]*)"`)
	matches := actionRegex.FindSubmatch(body)
	if len(matches) < 2 {
		t.Fatalf("could not find login form action in Keycloak page. Status: %d, Body: %.500s", resp.StatusCode, string(body))
	}
	actionURL := strings.ReplaceAll(string(matches[1]), "&amp;", "&")

	// Step 3: POST credentials to Keycloak
	formData := url.Values{
		"username": {username},
		"password": {password},
	}
	resp2, err := client.PostForm(actionURL, formData)
	if err != nil {
		t.Fatalf("failed to POST login form: %v", err)
	}
	defer resp2.Body.Close()

	// Step 4: Verify we got an authenticated session (look for knox_session cookie)
	knoxParsed, _ := url.Parse(knoxURL)
	cookies := client.Jar.Cookies(knoxParsed)
	hasSession := false
	for _, c := range cookies {
		if c.Name == "knox_session" {
			hasSession = true
			break
		}
	}
	if !hasSession {
		body2, _ := io.ReadAll(resp2.Body)
		t.Fatalf("no knox_session cookie after login. Status: %d, Final URL: %s, Body: %.500s",
			resp2.StatusCode, resp2.Request.URL.String(), string(body2))
	}

	return client
}

// ── Health Tests ─────────────────────────────────────────────────────────────

func TestHealthEndpoint(t *testing.T) {
	resp, err := http.Get(knoxURL + "/healthz")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}
}

func TestKeycloakDiscovery(t *testing.T) {
	resp, err := http.Get(keycloakURL + "/realms/knox-test/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("OIDC discovery failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var config map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&config)
	if config["authorization_endpoint"] == nil {
		t.Error("missing authorization_endpoint in OIDC config")
	}
}

// ── OIDC Flow Tests ──────────────────────────────────────────────────────────

func TestUnauthenticatedBrowserRedirect(t *testing.T) {
	client := newClient()
	req, _ := http.NewRequest("GET", knoxURL+"/", nil)
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "/auth/login") {
		t.Errorf("expected redirect to /auth/login, got %s", loc)
	}
}

func TestUnauthenticatedAPIRequest(t *testing.T) {
	client := newClient()
	req, _ := http.NewRequest("GET", knoxURL+"/rest/workflows", nil)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestLoginRedirectToKeycloak(t *testing.T) {
	client := newClient()

	resp, err := client.Get(knoxURL + "/auth/login")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "keycloak") && !strings.Contains(loc, "/realms/") {
		t.Errorf("expected redirect to Keycloak, got %s", loc)
	}
}

func TestFullOIDCLoginFlow(t *testing.T) {
	client := loginAsKeycloak(t, testUser, testPass)

	// Verify we can access protected resources
	resp, err := client.Get(knoxURL + "/rest/workflows")
	if err != nil {
		t.Fatalf("failed to fetch workflows: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200 after login, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestLogoutFlow(t *testing.T) {
	client := loginAsKeycloak(t, testUser, testPass)

	// Logout
	resp, err := client.Get(knoxURL + "/auth/logout")
	if err != nil {
		t.Fatalf("logout failed: %v", err)
	}
	defer resp.Body.Close()

	// After logout, next request should be unauthorized
	resp2, err := client.Get(knoxURL + "/rest/workflows")
	if err != nil {
		t.Fatalf("post-logout request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode == http.StatusOK {
		t.Error("expected non-200 after logout, session should be invalid")
	}
}

// ── Proxy & Policy Tests ─────────────────────────────────────────────────────

func TestReadOnlyAccess(t *testing.T) {
	client := loginAsKeycloak(t, testUser, testPass)

	resp, err := client.Get(knoxURL + "/rest/workflows")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", resp.StatusCode)
	}
}

func TestMutationDeniedWithoutRole(t *testing.T) {
	client := loginAsKeycloak(t, restrictedUser, restrictedPass)

	if testWorkflowID == "" {
		t.Skip("TEST_WORKFLOW_ID not set")
	}

	req, _ := http.NewRequest("PATCH", knoxURL+"/rest/workflows/"+testWorkflowID, strings.NewReader(`{"name":"hacked"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for restricted user, got %d", resp.StatusCode)
	}
}

func TestWildcardRoleAllowsAll(t *testing.T) {
	client := loginAsKeycloak(t, adminUser, adminPass)

	if testWorkflowID == "" {
		t.Skip("TEST_WORKFLOW_ID not set")
	}

	req, _ := http.NewRequest("PATCH", knoxURL+"/rest/workflows/"+testWorkflowID, strings.NewReader(`{"name":"Admin Edit"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Error("expected admin with edit:* to be allowed, got 403")
	}
}

func TestWebhookBlocked(t *testing.T) {
	for _, path := range []string{"/webhook", "/webhook-test"} {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(knoxURL + path)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("expected 403 for %s, got %d", path, resp.StatusCode)
			}
		})
	}
}

func TestCustomAssetsServed(t *testing.T) {
	resp, err := http.Get(knoxURL + "/custom-assets/knox-ui.js")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "javascript") && !strings.Contains(ct, "text") {
		t.Errorf("expected JS content type, got %s", ct)
	}
}

// ── JIT Ticketing API Tests ──────────────────────────────────────────────────

func TestCreateJITTicket(t *testing.T) {
	client := loginAsKeycloak(t, testUser, testPass)

	body := fmt.Sprintf(`{
		"workflow_id": "%s",
		"access_type": ["run"],
		"period_start": "%s",
		"period_end": "%s",
		"description": "Integration test ticket for CI pipeline validation"
	}`, envOr("TEST_WORKFLOW_ID", "test-wf-1"),
		time.Now().Add(1*time.Minute).Format(time.RFC3339),
		time.Now().Add(24*time.Hour).Format(time.RFC3339))

	req, _ := http.NewRequest("POST", knoxURL+"/knox-api/request-jit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 201/200, got %d: %s", resp.StatusCode, string(respBody))
	}
}

func TestCreateTicketValidation(t *testing.T) {
	client := loginAsKeycloak(t, testUser, testPass)

	// Empty body
	req, _ := http.NewRequest("POST", knoxURL+"/knox-api/request-jit", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid request, got %d", resp.StatusCode)
	}
}

func TestListMyTickets(t *testing.T) {
	client := loginAsKeycloak(t, testUser, testPass)

	resp, err := client.Get(knoxURL + "/knox-api/tickets")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["tickets"] == nil {
		t.Error("expected 'tickets' key in response")
	}
}

func TestGetUserInfo(t *testing.T) {
	client := loginAsKeycloak(t, testUser, testPass)

	resp, err := client.Get(knoxURL + "/knox-api/user-info")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["email"] == nil {
		t.Error("expected 'email' in user info")
	}
	if result["team"] == nil {
		t.Error("expected 'team' in user info")
	}
}

// ── Admin API Tests ──────────────────────────────────────────────────────────

func TestAdminListTickets(t *testing.T) {
	req, _ := http.NewRequest("GET", knoxURL+"/knox-api/admin/tickets", nil)
	req.Header.Set("X-Knox-API-Key", knoxAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestAdminAPIWithoutKey(t *testing.T) {
	resp, err := http.Get(knoxURL + "/knox-api/admin/tickets")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without API key, got %d", resp.StatusCode)
	}
}

func TestAdminAPIWrongKey(t *testing.T) {
	req, _ := http.NewRequest("GET", knoxURL+"/knox-api/admin/tickets", nil)
	req.Header.Set("X-Knox-API-Key", "wrong-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong key, got %d", resp.StatusCode)
	}
}
