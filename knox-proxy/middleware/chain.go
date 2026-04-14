package middleware

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"knox-proxy/auth"
	"knox-proxy/policy"
)

type contextKey string

const userContextKey contextKey = "knox_user"

// RequestUser holds user information for the current request, extracted from the session.
type RequestUser struct {
	Email       string
	DisplayName string
	Groups      []string
	JITRoles    []string
}

// SetUserInContext stores user info in the request context.
func SetUserInContext(ctx context.Context, user *RequestUser) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// GetUserFromContext retrieves user info from the request context.
func GetUserFromContext(ctx context.Context) *RequestUser {
	user, _ := ctx.Value(userContextKey).(*RequestUser)
	return user
}

// ── Logging Middleware ────────────────────────────────────────────────────────

// statusResponseWriter captures the HTTP status code from the response.
type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Flush implements http.Flusher for streaming/SSE support.
func (w *statusResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker for WebSocket upgrade support.
func (w *statusResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("upstream ResponseWriter does not implement http.Hijacker")
}

// Logging wraps a handler with structured request logging.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Capture the final enriched request as it passes through downstream middleware.
		// Auth middleware enriches the context via r.WithContext(), producing a new
		// *http.Request. We capture that enriched request here so we can read the
		// correct user context after ServeHTTP returns.
		var enrichedReq *http.Request
		capturingNext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			enrichedReq = r
			next.ServeHTTP(w, r)
		})

		capturingNext.ServeHTTP(wrapped, r)

		// Prefer the enriched request context (has user info injected by Auth middleware).
		// Fall back to the original request context if no downstream handler ran
		// (e.g., unauthenticated requests that were rejected before enrichment).
		logCtx := r.Context()
		if enrichedReq != nil {
			logCtx = enrichedReq.Context()
		}

		user := GetUserFromContext(logCtx)
		email := "anonymous"
		if user != nil {
			email = user.Email
		}

		// Suppress verbose logging for n8n UI auto-save requests that are
		// intentionally blocked (PATCH /rest/workflows/* → 403).
		isAutoSaveSpam := r.Method == "PATCH" &&
			strings.HasPrefix(r.URL.Path, "/rest/workflows/") &&
			wrapped.statusCode == http.StatusForbidden

		if isAutoSaveSpam {
			slog.Debug("request (auto-save blocked)",
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.statusCode,
				"duration_ms", time.Since(start).Milliseconds(),
				"user", email,
				"ip", r.RemoteAddr,
			)
		} else {
			slog.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.statusCode,
				"duration_ms", time.Since(start).Milliseconds(),
				"user", email,
				"ip", r.RemoteAddr,
			)
		}
	})
}

// ── Auth Middleware ───────────────────────────────────────────────────────────

// Auth ensures the request has a valid KNOX session.
// For API requests it returns 401 JSON; for browser requests it redirects to login.
func Auth(sessionMgr *auth.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessionID := sessionMgr.GetSessionIDFromRequest(r)

			var session *auth.UserSession
			if sessionID != "" {
				session = sessionMgr.GetSession(sessionID)
			}

			if session == nil {
				// Clear any stale cookie
				if sessionID != "" {
					sessionMgr.ClearSessionCookie(w)
				}

				// For API/AJAX requests, return 401 JSON instead of redirect
				if isAPIRequest(r) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"error":"Unauthorized","message":"Session expired or invalid. Please login again."}`))
					return
				}

				// For browser requests, redirect to OIDC login
				redirectParam := url.QueryEscape(r.URL.RequestURI())
				http.Redirect(w, r, "/auth/login?redirect="+redirectParam, http.StatusFound)
				return
			}

			// Enrich request context with user info for downstream handlers
			user := &RequestUser{
				Email:       session.Email,
				DisplayName: session.DisplayName,
				Groups:      session.Groups,
				JITRoles:    session.JITRoles,
			}
			ctx := SetUserInContext(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ── Policy Middleware ─────────────────────────────────────────────────────────

// Policy evaluates JIT access rules and optionally sanitizes request bodies.
func Policy(engine *policy.Engine, sanitizer *policy.Sanitizer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUserFromContext(r.Context())
			if user == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			var reqBody []byte
			var err error

			// Read and buffer the body for workflow/execution mutation requests.
			// The Policy engine needs to inspect the body to extract workflow IDs
			// from payloads like POST /rest/workflows/run.
			if isMutationMethod(r.Method) && (isWorkflowPath(r.URL.Path) || strings.HasPrefix(r.URL.Path, "/rest/executions")) {
				reqBody, err = io.ReadAll(r.Body)
				r.Body.Close()
				if err != nil {
					http.Error(w, "Failed to read request body", http.StatusBadRequest)
					return
				}
				// Restore body for downstream handlers (proxy, sanitizer)
				r.Body = io.NopCloser(bytes.NewReader(reqBody))
				r.ContentLength = int64(len(reqBody))
			}

			// Evaluate JIT policy
			decision := engine.Evaluate(r.Method, r.URL.Path, reqBody, user.JITRoles)
			if !decision.Allowed {
				// Suppress verbose logging for n8n UI auto-save spam
				isAutoSaveSpam := r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/rest/workflows/")

				if isAutoSaveSpam {
					slog.Debug("Policy denied (auto-save)",
						"user", user.Email,
						"method", r.Method,
						"path", r.URL.Path,
						"reason", decision.Reason,
					)
				} else {
					slog.Warn("Policy denied request",
						"user", user.Email,
						"method", r.Method,
						"path", r.URL.Path,
						"reason", decision.Reason,
					)
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, `{"error":"Forbidden","reason":"%s"}`, decision.Reason)
				return
			}

			// Sanitize body for edit operations (only when sanitizer is enabled)
			if sanitizer.IsEnabled() && isMutationMethod(r.Method) && isWorkflowPath(r.URL.Path) && len(reqBody) > 0 {
				if err := sanitizer.SanitizeWorkflowBody(reqBody); err != nil {
					slog.Warn("Body sanitization blocked request",
						"user", user.Email,
						"path", r.URL.Path,
						"error", err,
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprintf(w, `{"error":"Bad Request","reason":"%s"}`, err.Error())
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ── Helper Functions ──────────────────────────────────────────────────────────

// isAPIRequest checks if the request is an API/AJAX call based on headers and path.
func isAPIRequest(r *http.Request) bool {
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		return true
	}
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/rest/") || strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	return false
}

// isMutationMethod returns true for HTTP methods that modify state.
func isMutationMethod(method string) bool {
	return method == "POST" || method == "PATCH" || method == "PUT" || method == "DELETE"
}

// isWorkflowPath returns true if the URL path is a workflow resource.
func isWorkflowPath(urlPath string) bool {
	return strings.HasPrefix(urlPath, "/rest/workflows")
}
