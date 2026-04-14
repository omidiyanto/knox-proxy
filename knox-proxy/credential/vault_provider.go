package credential

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

// VaultProvider provides team credentials from HashiCorp Vault using AppRole auth.
// Uses a lightweight HTTP client to avoid heavy vault/api dependency.
type VaultProvider struct {
	vaultAddr  string
	roleID     string
	secretID   string
	secretPath string
	cacheTTL   time.Duration

	mu          sync.RWMutex
	cache       map[string]TeamCredential
	cacheExpiry time.Time
	vaultToken  string
	tokenExpiry time.Time
	httpClient  *http.Client
}

// NewVaultProvider creates a new VaultProvider and performs initial authentication.
func NewVaultProvider(vaultAddr, roleID, secretID, secretPath string, cacheTTLSeconds int) (*VaultProvider, error) {
	vp := &VaultProvider{
		vaultAddr:  vaultAddr,
		roleID:     roleID,
		secretID:   secretID,
		secretPath: secretPath,
		cacheTTL:   time.Duration(cacheTTLSeconds) * time.Second,
		cache:      make(map[string]TeamCredential),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	// Validate configuration
	if vaultAddr == "" || roleID == "" || secretID == "" {
		return nil, fmt.Errorf("vault configuration incomplete: VAULT_ADDR, VAULT_ROLE_ID, and VAULT_SECRET_ID are all required")
	}

	// Initial authentication
	if err := vp.authenticate(); err != nil {
		return nil, fmt.Errorf("vault authentication failed: %w", err)
	}

	// Initial cache load
	if err := vp.refreshCache(); err != nil {
		return nil, fmt.Errorf("vault secret read failed: %w", err)
	}

	// Start background refresh
	go vp.backgroundRefresh()

	return vp, nil
}

// authenticate performs AppRole authentication against Vault.
func (vp *VaultProvider) authenticate() error {
	reqBody, _ := json.Marshal(map[string]string{
		"role_id":   vp.roleID,
		"secret_id": vp.secretID,
	})

	resp, err := vp.httpClient.Post(
		vp.vaultAddr+"/v1/auth/approle/login",
		"application/json",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return fmt.Errorf("vault login request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vault login failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse vault login response: %w", err)
	}

	vp.vaultToken = result.Auth.ClientToken
	duration := time.Duration(result.Auth.LeaseDuration) * time.Second
	if duration == 0 {
		duration = 1 * time.Hour
	}
	// Renew 30 seconds before expiry
	vp.tokenExpiry = time.Now().Add(duration - 30*time.Second)

	slog.Info("Vault authentication successful", "lease_duration", duration)
	return nil
}

// refreshCache reads team credentials from Vault and updates the local cache.
func (vp *VaultProvider) refreshCache() error {
	// Re-authenticate if token is expired
	if time.Now().After(vp.tokenExpiry) {
		if err := vp.authenticate(); err != nil {
			return err
		}
	}

	req, err := http.NewRequest("GET", vp.vaultAddr+"/v1/"+vp.secretPath, nil)
	if err != nil {
		return fmt.Errorf("failed to create vault request: %w", err)
	}
	req.Header.Set("X-Vault-Token", vp.vaultToken)

	resp, err := vp.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vault read request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vault read failed with status %d: %s", resp.StatusCode, string(body))
	}

	// KV v2 response format: { "data": { "data": { ... } } }
	var result struct {
		Data struct {
			Data map[string]struct {
				Email string `json:"email"`
				Pass  string `json:"pass"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse vault secret: %w", err)
	}

	newCache := make(map[string]TeamCredential, len(result.Data.Data))
	for name, cred := range result.Data.Data {
		newCache[name] = TeamCredential{
			Email:    cred.Email,
			Password: cred.Pass,
		}
	}

	vp.mu.Lock()
	vp.cache = newCache
	vp.cacheExpiry = time.Now().Add(vp.cacheTTL)
	vp.mu.Unlock()

	slog.Info("Vault cache refreshed", "teams_count", len(newCache))
	return nil
}

// backgroundRefresh periodically refreshes the credential cache from Vault.
func (vp *VaultProvider) backgroundRefresh() {
	ticker := time.NewTicker(vp.cacheTTL)
	defer ticker.Stop()
	for range ticker.C {
		if err := vp.refreshCache(); err != nil {
			slog.Error("Vault cache refresh failed", "error", err)
		}
	}
}

// GetCredential returns credentials for the given team from the cache.
func (vp *VaultProvider) GetCredential(teamName string) (*TeamCredential, error) {
	vp.mu.RLock()
	defer vp.mu.RUnlock()

	cred, ok := vp.cache[teamName]
	if !ok {
		return nil, fmt.Errorf("team not found in vault: %s", teamName)
	}
	return &cred, nil
}

// ListTeams returns all team names currently in the cache.
func (vp *VaultProvider) ListTeams() []string {
	vp.mu.RLock()
	defer vp.mu.RUnlock()

	teams := make([]string, 0, len(vp.cache))
	for name := range vp.cache {
		teams = append(teams, name)
	}
	return teams
}

// ReadSecret reads a single key from a Vault KV v2 secret path.
// The secretPath should be the full KV v2 path (e.g., "secret/data/knox/config").
// Returns the value for the given key, or empty string if not found.
func (vp *VaultProvider) ReadSecret(secretPath, key string) (string, error) {
	// Re-authenticate if token is expired
	if time.Now().After(vp.tokenExpiry) {
		if err := vp.authenticate(); err != nil {
			return "", fmt.Errorf("vault re-auth failed: %w", err)
		}
	}

	req, err := http.NewRequest("GET", vp.vaultAddr+"/v1/"+secretPath, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create vault request: %w", err)
	}
	req.Header.Set("X-Vault-Token", vp.vaultToken)

	resp, err := vp.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vault read request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault read failed with status %d: %s", resp.StatusCode, string(body))
	}

	// KV v2 response: { "data": { "data": { "key": "value" } } }
	var result struct {
		Data struct {
			Data map[string]interface{} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse vault response: %w", err)
	}

	val, ok := result.Data.Data[key]
	if !ok {
		return "", nil
	}

	strVal, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("vault key '%s' is not a string", key)
	}

	return strVal, nil
}
