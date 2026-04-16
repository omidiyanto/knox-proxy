package credential

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVaultProvider_AuthenticateAndCache(t *testing.T) {
	// Mock Vault server
	mux := http.NewServeMux()

	// AppRole login
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"auth": map[string]interface{}{
				"client_token":   "mock-token-123",
				"lease_duration": 3600,
			},
		})
	})

	// KV v2 read
	mux.HandleFunc("/v1/secret/data/knox/n8n_teams", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "mock-token-123" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"finance": map[string]interface{}{
						"email": "fin@local",
						"pass":  "finVaultPass",
					},
					"developer": map[string]interface{}{
						"email": "dev@local",
						"pass":  "devVaultPass",
					},
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	vp, err := NewVaultProvider(server.URL, "test-role-id", "test-secret-id", "secret/data/knox/n8n_teams", 300)
	if err != nil {
		t.Fatalf("failed to create VaultProvider: %v", err)
	}

	// GetCredential
	cred, err := vp.GetCredential("finance")
	if err != nil {
		t.Fatalf("GetCredential failed: %v", err)
	}
	if cred.Email != "fin@local" {
		t.Errorf("expected fin@local, got %s", cred.Email)
	}
	if cred.Password != "finVaultPass" {
		t.Errorf("expected finVaultPass, got %s", cred.Password)
	}

	// ListTeams
	teams := vp.ListTeams()
	if len(teams) != 2 {
		t.Errorf("expected 2 teams, got %d", len(teams))
	}
}

func TestVaultProvider_GetCredential_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"auth": map[string]interface{}{
				"client_token":   "token",
				"lease_duration": 3600,
			},
		})
	})
	mux.HandleFunc("/v1/secret/data/knox/n8n_teams", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	vp, err := NewVaultProvider(server.URL, "role", "secret", "secret/data/knox/n8n_teams", 300)
	if err != nil {
		t.Fatalf("failed to create VaultProvider: %v", err)
	}

	_, err = vp.GetCredential("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent team")
	}
}

func TestVaultProvider_ReadSecret(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"auth": map[string]interface{}{
				"client_token":   "token",
				"lease_duration": 3600,
			},
		})
	})
	mux.HandleFunc("/v1/secret/data/knox/n8n_teams", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"data": map[string]interface{}{}},
		})
	})
	mux.HandleFunc("/v1/secret/data/knox/config", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"knox_api_key": "vault-api-key-123",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	vp, err := NewVaultProvider(server.URL, "role", "secret", "secret/data/knox/n8n_teams", 300)
	if err != nil {
		t.Fatalf("failed to create VaultProvider: %v", err)
	}

	val, err := vp.ReadSecret("secret/data/knox/config", "knox_api_key")
	if err != nil {
		t.Fatalf("ReadSecret failed: %v", err)
	}
	if val != "vault-api-key-123" {
		t.Errorf("expected vault-api-key-123, got %s", val)
	}
}

func TestVaultProvider_ReadSecret_KeyNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"auth": map[string]interface{}{"client_token": "token", "lease_duration": 3600},
		})
	})
	mux.HandleFunc("/v1/secret/data/knox/n8n_teams", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"data": map[string]interface{}{}},
		})
	})
	mux.HandleFunc("/v1/secret/data/knox/config", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	vp, _ := NewVaultProvider(server.URL, "role", "secret", "secret/data/knox/n8n_teams", 300)

	val, err := vp.ReadSecret("secret/data/knox/config", "missing_key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string, got %s", val)
	}
}

func TestVaultProvider_AuthFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":["invalid credentials"]}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	_, err := NewVaultProvider(server.URL, "bad-role", "bad-secret", "secret/data/knox/n8n_teams", 300)
	if err == nil {
		t.Error("expected error for failed authentication")
	}
}

func TestVaultProvider_IncompleteConfig(t *testing.T) {
	_, err := NewVaultProvider("", "role", "secret", "path", 300)
	if err == nil {
		t.Error("expected error for empty vault addr")
	}

	_, err = NewVaultProvider("http://vault:8200", "", "secret", "path", 300)
	if err == nil {
		t.Error("expected error for empty role ID")
	}

	_, err = NewVaultProvider("http://vault:8200", "role", "", "path", 300)
	if err == nil {
		t.Error("expected error for empty secret ID")
	}
}
