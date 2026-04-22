package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"knox-proxy/auth"
	"knox-proxy/config"
	"knox-proxy/cookie"
	"knox-proxy/credential"
	"knox-proxy/jit"
	"knox-proxy/middleware"
	"knox-proxy/policy"
	"knox-proxy/proxy"
	"knox-proxy/audit"
)

func main() {
	// Setup structured JSON logging
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Initialize Audit Logger
	if err := audit.Init("audit.log"); err != nil {
		slog.Error("Failed to initialize audit logger", "error", err)
	} else {
		slog.Info("Audit logger initialized, writing to audit.log")
		audit.Log("Audit System Started")
	}

	slog.Info("=== KNOX IAM Gateway starting ===")

	// ─── Configuration ────────────────────────────────────────────────────
	cfg, err := config.LoadFromEnv()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}
	slog.Info("Configuration loaded",
		"port", cfg.ProxyPort,
		"backend", cfg.N8NBackendURL,
		"n8n_internal", cfg.N8NInternalURL,
		"oidc_issuer", cfg.OIDCIssuerURL,
		"enable_vault", cfg.EnableVault,
		"group_prefix", cfg.KeycloakGroupPrefix,
	)

	// ─── Session Manager ──────────────────────────────────────────────────
	sessionMgr := auth.NewSessionManager(cfg.SessionMaxAge)

	// ─── OIDC Provider ────────────────────────────────────────────────────
	ctx := context.Background()
	oidcAuth, err := auth.NewOIDCAuth(
		ctx,
		cfg.OIDCIssuerURL,
		cfg.OIDCClientID,
		cfg.OIDCClientSecret,
		cfg.OIDCRedirectURL,
		sessionMgr,
	)
	if err != nil {
		slog.Error("Failed to initialize OIDC provider", "error", err)
		os.Exit(1)
	}
	slog.Info("OIDC provider initialized", "issuer", cfg.OIDCIssuerURL)

	// ─── Credential Provider ──────────────────────────────────────────────
	var credProvider credential.CredentialProvider
	if cfg.EnableVault {
		vp, err := credential.NewVaultProvider(
			cfg.VaultAddr,
			cfg.VaultRoleID,
			cfg.VaultSecretID,
			cfg.VaultSecretPath,
			cfg.VaultCacheTTL,
		)
		if err != nil {
			slog.Error("Failed to initialize Vault credential provider", "error", err)
			os.Exit(1)
		}
		credProvider = vp
		slog.Info("Vault credential provider initialized", "addr", cfg.VaultAddr)

		// Optionally read KNOX API key from Vault (if not set via env var)
		if cfg.KnoxAPIKey == "" {
			apiKey, err := vp.ReadSecret(cfg.KnoxAPIKeyVaultPath, cfg.KnoxAPIKeyVaultField)
			if err != nil {
				slog.Warn("Failed to read API key from Vault (admin API will be disabled)",
					"path", cfg.KnoxAPIKeyVaultPath,
					"field", cfg.KnoxAPIKeyVaultField,
					"error", err,
				)
			} else if apiKey != "" {
				cfg.KnoxAPIKey = apiKey
				slog.Info("KNOX API key loaded from Vault",
					"path", cfg.KnoxAPIKeyVaultPath,
					"field", cfg.KnoxAPIKeyVaultField,
				)
			}
		}
	} else {
		// Convert config credentials to credential.TeamCredential
		credMap := make(map[string]credential.TeamCredential, len(cfg.TeamCredentials))
		for name, cc := range cfg.TeamCredentials {
			credMap[name] = credential.TeamCredential{
				Email:    cc.Email,
				Password: cc.Password,
			}
		}
		credProvider = credential.NewEnvProvider(credMap)
		slog.Info("ENV credential provider initialized", "teams", credProvider.ListTeams())
	}

	// ─── Cookie Pool ──────────────────────────────────────────────────────
	cookiePool := cookie.NewPool(cfg.N8NInternalURL, cfg.CookieTTL)

	// Pre-warm cookies for all teams at startup
	slog.Info("Pre-warming n8n cookies...")
	for _, teamName := range credProvider.ListTeams() {
		cred, err := credProvider.GetCredential(teamName)
		if err != nil {
			slog.Warn("Failed to get credential for pre-warm", "team", teamName, "error", err)
			continue
		}
		if _, err := cookiePool.GetCookie(cred.Email, cred.Password); err != nil {
			slog.Warn("Failed to pre-warm cookie (n8n may not be ready yet)",
				"team", teamName, "email", cred.Email, "error", err)
		} else {
			slog.Info("Cookie pre-warmed successfully", "team", teamName, "email", cred.Email)
		}
	}

	// ─── Policy Engine ────────────────────────────────────────────────────
	policyEngine := policy.NewEngine()
	sanitizer := policy.NewSanitizer(cfg.DangerousNodes, cfg.DangerousNodesFile)

	// ─── Reverse Proxy Handler ────────────────────────────────────────────
	proxyHandler, err := proxy.NewHandler(
		cfg.N8NBackendURL,
		cfg.KeycloakGroupPrefix,
		cookiePool,
		credProvider,
	)
	if err != nil {
		slog.Error("Failed to create reverse proxy handler", "error", err)
		os.Exit(1)
	}

	// ─── Backchannel Logout Handler ───────────────────────────────────────
	backchannelHandler := auth.NewBackchannelLogoutHandler(
		sessionMgr,
		oidcAuth.KeySet(),
		cfg.OIDCClientID,
		cfg.OIDCIssuerURL,
	)

	// ─── JIT Ticketing Database ──────────────────────────────────────────
	jitDB, err := jit.NewDB(cfg.JITDatabaseURL)
	if err != nil {
		slog.Error("Failed to connect to JIT database", "error", err)
		os.Exit(1)
	}
	defer jitDB.Close()

	if err := jitDB.Migrate(); err != nil {
		slog.Error("Failed to migrate JIT database schema", "error", err)
		os.Exit(1)
	}

	// ─── JIT Handler ─────────────────────────────────────────────────────
	jitRepo := jit.NewRepository(jitDB)
	maxDuration := time.Duration(cfg.JITMaxDurationDays) * 24 * time.Hour

	var jitHandler *jit.Handler
	jitScheduler := jit.NewScheduler(jitRepo, func(t jit.Ticket) {
		if jitHandler != nil {
			jitHandler.FireWebhook(&t)
		}
	})

	jitHandler = jit.NewHandler(
		jitRepo,
		jitScheduler,
		cfg.JITWebhookURL,
		maxDuration,
		cfg.KnoxAPIKey,
		cfg.KeycloakGroupPrefix,
	)

	slog.Info("JIT ticketing system initialized",
		"max_duration_days", cfg.JITMaxDurationDays,
		"webhook_configured", cfg.JITWebhookURL != "",
		"admin_api_enabled", cfg.KnoxAPIKey != "",
	)

	// Fetch pending states from DB and schedule into background timers
	if err := jitScheduler.InitFromDB(); err != nil {
		slog.Error("Failed to initialize JIT scheduler", "error", err)
	}

	// ─── HTTP Routes ──────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Auth routes (not behind middleware — these handle their own auth)
	mux.HandleFunc("/auth/login", oidcAuth.LoginHandler)
	mux.HandleFunc("/auth/callback", oidcAuth.CallbackHandler)
	mux.HandleFunc("/auth/logout", oidcAuth.LogoutHandler)
	mux.Handle("/auth/backchannel-logout", backchannelHandler)

	// Health check endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"knox","version":"1.0.0"}`))
	})

	// Admin API routes (protected by API Key internally, NOT by session Auth)
	mux.Handle("/knox-api/admin/", middleware.Logging(jitHandler))

	// User JIT API routes (protected by Session Auth)
	mux.Handle("/knox-api/", middleware.Logging(
		middleware.Auth(sessionMgr)(jitHandler),
	))

	// Protected routes — all other traffic goes through the middleware chain:
	// Logging → Auth → Policy → ReverseProxy
	protected := middleware.Logging(
		middleware.Auth(sessionMgr)(
			middleware.Policy(policyEngine, sanitizer)(
				proxyHandler,
			),
		),
	)
	mux.Handle("/", protected)

	// ─── HTTP Server ──────────────────────────────────────────────────────
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.ProxyPort),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // Disabled for WebSocket support
		IdleTimeout:  120 * time.Second,
	}

	// ─── Session Cleanup Goroutine ────────────────────────────────────────
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			count := sessionMgr.CleanExpired()
			if count > 0 {
				slog.Info("Cleaned expired sessions", "count", count)
			}
		}
	}()

	// ─── Graceful Shutdown ────────────────────────────────────────────────
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan
		slog.Info("Received shutdown signal", "signal", sig)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("Graceful shutdown failed", "error", err)
		}
	}()

	// ─── Start Server ─────────────────────────────────────────────────────
	slog.Info("KNOX IAM Gateway listening",
		"port", cfg.ProxyPort,
		"url", fmt.Sprintf("http://localhost:%d", cfg.ProxyPort),
	)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Server failed to start", "error", err)
		os.Exit(1)
	}

	slog.Info("=== KNOX IAM Gateway stopped ===")
}
