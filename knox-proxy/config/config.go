package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// TeamCredentialConfig represents n8n local account credentials for a team.
// JSON field "pass" matches the N8N_TEAM_CREDENTIALS format.
type TeamCredentialConfig struct {
	Email    string `json:"email"`
	Password string `json:"pass"`
}

// Config holds all configuration for the KNOX proxy.
type Config struct {
	// Server
	ProxyPort int

	// Backend URLs
	N8NBackendURL  string // proxied through nginx-readonly
	N8NInternalURL string // direct connection to n8n for cookie login

	// OIDC / Keycloak
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string

	// Session
	SessionSecret string
	SessionMaxAge int // seconds

	// Team Mapping
	KeycloakGroupPrefix string
	TeamCredentials     map[string]TeamCredentialConfig

	// Vault (Optional)
	EnableVault     bool
	VaultAddr       string
	VaultRoleID     string
	VaultSecretID   string
	VaultSecretPath string
	VaultCacheTTL   int // seconds

	// Security / Dangerous Nodes
	DangerousNodes     []string
	DangerousNodesFile string

	// Cookie Pool
	CookieTTL int // seconds

	// JIT Ticketing
	JITDatabaseURL       string // PostgreSQL DSN for ticket storage
	JITWebhookURL        string // Webhook endpoint for ticket notifications (optional)
	JITMaxDurationDays   int    // Max allowed request duration in days (default: 7)
	KnoxAPIKey           string // API key for admin/external endpoints
	KnoxAPIKeyVaultPath  string // Vault KV v2 path for API key (optional, used when ENABLE_VAULT=true)
	KnoxAPIKeyVaultField string // Key name inside the Vault secret (default: "knox_api_key")
}

// LoadFromEnv loads configuration from environment variables.
func LoadFromEnv() (*Config, error) {
	cfg := &Config{}

	// Server
	cfg.ProxyPort = getEnvInt("PROXY_PORT", 8443)

	// Backend
	cfg.N8NBackendURL = getEnv("N8N_BACKEND_URL", "http://nginx-readonly:80")
	cfg.N8NInternalURL = getEnv("N8N_INTERNAL_URL", "http://n8n:5678")

	// OIDC
	cfg.OIDCIssuerURL = getEnv("OIDC_ISSUER_URL", "")
	cfg.OIDCClientID = getEnv("OIDC_CLIENT_ID", "n8n-proxy")
	cfg.OIDCClientSecret = getEnv("OIDC_CLIENT_SECRET", "")
	cfg.OIDCRedirectURL = getEnv("OIDC_REDIRECT_URL", "")

	// Session
	cfg.SessionSecret = getEnv("SESSION_SECRET", "")
	if cfg.SessionSecret == "" {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("failed to generate session secret: %w", err)
		}
		cfg.SessionSecret = base64.StdEncoding.EncodeToString(secret)
		slog.Info("Auto-generated session secret (will change on restart)")
	}
	cfg.SessionMaxAge = getEnvInt("SESSION_MAX_AGE", 3600)

	// Team Mapping
	cfg.KeycloakGroupPrefix = getEnv("KEYCLOAK_GROUP_PREFIX", "/n8n-prod-")
	cfg.TeamCredentials = make(map[string]TeamCredentialConfig)

	// Vault
	cfg.EnableVault = getEnvBool("ENABLE_VAULT", false)
	cfg.VaultAddr = getEnv("VAULT_ADDR", "")
	cfg.VaultRoleID = getEnv("VAULT_ROLE_ID", "")
	cfg.VaultSecretID = getEnv("VAULT_SECRET_ID", "")
	cfg.VaultSecretPath = getEnv("VAULT_SECRET_PATH", "secret/data/knox/n8n_teams")
	cfg.VaultCacheTTL = getEnvInt("VAULT_CACHE_TTL", 300)

	// Security
	cfg.DangerousNodesFile = getEnv("DANGEROUS_NODES_FILE", "")
	nodesStr := getEnv("DANGEROUS_NODES", "")
	if nodesStr != "" {
		cfg.DangerousNodes = strings.Split(nodesStr, ",")
		for i := range cfg.DangerousNodes {
			cfg.DangerousNodes[i] = strings.TrimSpace(cfg.DangerousNodes[i])
		}
	}

	// Cookie Pool
	cfg.CookieTTL = getEnvInt("COOKIE_TTL", 3600)

	// JIT Ticketing
	cfg.JITDatabaseURL = getEnv("JIT_DATABASE_URL", "postgres://knox:knoxSecurePass@knox-db:5432/knox?sslmode=disable")
	cfg.JITWebhookURL = getEnv("JIT_WEBHOOK_URL", "")
	cfg.JITMaxDurationDays = getEnvInt("JIT_MAX_DURATION_DAYS", 7)
	cfg.KnoxAPIKey = getEnv("KNOX_API_KEY", "")
	cfg.KnoxAPIKeyVaultPath = getEnv("KNOX_API_KEY_VAULT_PATH", "secret/data/knox/config")
	cfg.KnoxAPIKeyVaultField = getEnv("KNOX_API_KEY_VAULT_FIELD", "knox_api_key")

	// Parse team credentials from ENV if not using Vault
	if !cfg.EnableVault {
		credJSON := getEnv("N8N_TEAM_CREDENTIALS", "{}")
		if err := json.Unmarshal([]byte(credJSON), &cfg.TeamCredentials); err != nil {
			return nil, fmt.Errorf("failed to parse N8N_TEAM_CREDENTIALS: %w", err)
		}
	}

	// Auto-generate redirect URL if not set
	if cfg.OIDCRedirectURL == "" {
		cfg.OIDCRedirectURL = fmt.Sprintf("http://localhost:%d/auth/callback", cfg.ProxyPort)
	}

	// Validate required fields
	if cfg.OIDCIssuerURL == "" {
		return nil, fmt.Errorf("OIDC_ISSUER_URL is required")
	}
	if cfg.OIDCClientSecret == "" {
		return nil, fmt.Errorf("OIDC_CLIENT_SECRET is required")
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultValue
}
