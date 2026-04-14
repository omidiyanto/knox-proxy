package proxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"knox-proxy/cookie"
	"knox-proxy/credential"
	"knox-proxy/middleware"
)

type contextKey string

const ctxN8NCookieKey contextKey = "knox_n8n_cookie"

// maxBodyBufferSize is the maximum request body size KNOX will buffer in memory
// to enable retry on stale cookie (401 from n8n). Requests larger than this
// will not be retried — they will be proxied directly.
const maxBodyBufferSize = 32 * 1024 * 1024 // 32MB

// Handler is the reverse proxy that injects n8n team cookies.
// For each request, it:
//  1. Resolves the user's team from their Keycloak groups
//  2. Looks up the team's n8n credentials
//  3. Gets (or caches) an n8n-auth cookie for that team
//  4. Strips original cookies and injects the team cookie
//  5. Overrides browser-id header to match the JWT's browserId
//  6. Proxies the request to nginx-readonly
//  7. Strips Set-Cookie from the response to prevent token leakage
//  8. On 401 from n8n (stale cookie), invalidates cache and retries once
type Handler struct {
	proxy        *httputil.ReverseProxy
	cookiePool   *cookie.Pool
	credProvider credential.CredentialProvider
	groupPrefix  string
	targetURL    *url.URL
}

// NewHandler creates a new reverse proxy handler.
// backendURL should point to nginx-readonly (e.g., http://nginx-readonly:80).
func NewHandler(backendURL, groupPrefix string, cookiePool *cookie.Pool, credProvider credential.CredentialProvider) (*Handler, error) {
	target, err := url.Parse(backendURL)
	if err != nil {
		return nil, err
	}

	h := &Handler{
		cookiePool:   cookiePool,
		credProvider: credProvider,
		groupPrefix:  groupPrefix,
		targetURL:    target,
	}

	h.proxy = &httputil.ReverseProxy{
		Director:       h.director,
		ModifyResponse: h.modifyResponse,
		ErrorHandler:   h.errorHandler,
		// FlushInterval -1 enables immediate flushing after every write.
		// This is CRITICAL for Server-Sent Events (SSE) support — without it,
		// the proxy buffers the response and the client never receives events.
		// n8n's /rest/push endpoint uses SSE as a fallback when WebSocket fails,
		// and execution status updates are delivered through this channel.
		FlushInterval: -1,
	}

	return h, nil
}

// director modifies the outgoing request before it is sent to the backend.
// It sets the target URL, performs cookie sterilization, injects the n8n-auth
// cookie, and overrides the browser-id header with KNOX's fixed value.
func (h *Handler) director(req *http.Request) {
	// Set target backend
	// Save the original host from the client request BEFORE overriding the URL.
	// This is critical for n8n's WebSocket Origin validation (origin-validator.ts):
	//   n8n compares Origin header (browser's host:port) against X-Forwarded-Host
	//   or Host header. If we set Host to "nginx-readonly:80" (the backend),
	//   Origin "192.168.13.232:8443" != "nginx-readonly" → mismatch → ws.close(1008).
	originalHost := req.Host
	if originalHost == "" {
		originalHost = req.Header.Get("Host")
	}

	// Set target backend for URL routing
	req.URL.Scheme = h.targetURL.Scheme
	req.URL.Host = h.targetURL.Host
	// Preserve the original Host header so n8n's origin validation passes.
	// Go's ReverseProxy uses req.Host for the Host header sent to the backend.
	req.Host = originalHost

	// SECURITY: Cookie Sterilization — strip ALL original cookies from the client.
	// The user's browser cookies must never reach n8n directly.
	req.Header.Del("Cookie")

	// Inject the n8n-auth cookie from the cookie pool (stored in context by ServeHTTP)
	if n8nCookie, ok := req.Context().Value(ctxN8NCookieKey).(string); ok && n8nCookie != "" {
		req.Header.Set("Cookie", "n8n-auth="+n8nCookie)
	}

	// Override the browser-id header with KNOX's fixed value.
	// Browser APIs for SSE (EventSource) and WebSocket do NOT support custom
	// HTTP headers. If we let the browser's header through for API requests
	// but miss it for push, n8n sees inconsistent browserId.
	// KNOX always uses the same fixed browserId for login (stored in the JWT)
	// and overrides the header here for ALL requests.
	req.Header.Set("browser-id", h.cookiePool.BrowserID())

	// Set proxy headers for n8n's origin validation.
	// X-Forwarded-Host tells n8n the original host the client connected to.
	// X-Forwarded-Proto tells n8n the original protocol.
	// n8n's origin-validator checks these to validate the WebSocket Origin header.
	req.Header.Set("X-Forwarded-Host", originalHost)
	if req.Header.Get("X-Forwarded-Proto") == "" {
		req.Header.Set("X-Forwarded-Proto", "http")
	}
}

// modifyResponse modifies the response from the backend before sending to the client.
func (h *Handler) modifyResponse(resp *http.Response) error {
	// SECURITY: Strip Set-Cookie headers from the n8n response.
	// This prevents the team's n8n-auth token from leaking to the user's browser.
	resp.Header.Del("Set-Cookie")
	return nil
}

// errorHandler handles proxy transport-level errors (e.g., backend unreachable).
func (h *Handler) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("Proxy transport error",
		"path", r.URL.Path,
		"method", r.Method,
		"error", err,
	)
	http.Error(w, "Bad Gateway", http.StatusBadGateway)
}

// ServeHTTP handles the proxied request with automatic stale-cookie retry.
//
// Flow:
//  1. Resolve team from Keycloak groups
//  2. Get cached n8n-auth cookie for the team
//  3. For streaming connections (WebSocket/SSE): proxy directly (no buffering)
//  4. For regular HTTP: buffer response to detect 401 from n8n
//  5. On 401: invalidate stale cookie, fetch fresh one, retry once
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get authenticated user from context (set by AuthMiddleware)
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Resolve team from user's Keycloak groups
	teamName := h.resolveTeam(user.Groups)
	if teamName == "" {
		slog.Warn("No team assignment found for user",
			"email", user.Email,
			"groups", user.Groups,
			"expected_prefix", h.groupPrefix,
		)
		http.Error(w, "No team assignment found. Ensure your Keycloak account belongs to a team group.", http.StatusForbidden)
		return
	}

	// Get team credentials
	cred, err := h.credProvider.GetCredential(teamName)
	if err != nil {
		slog.Error("Failed to get team credential",
			"team", teamName,
			"email", user.Email,
			"error", err,
		)
		http.Error(w, "Team configuration error. Contact administrator.", http.StatusInternalServerError)
		return
	}

	// ── Streaming connections: WebSocket and SSE ─────────────────────────────
	// These connections cannot be buffered. Proxy them directly.
	// Stale cookie retry is not supported for streaming — the client will
	// reconnect automatically on error.
	if isStreamingRequest(r) {
		n8nCookie, err := h.cookiePool.GetCookie(cred.Email, cred.Password)
		if err != nil {
			slog.Error("Failed to get n8n cookie for streaming request",
				"team", teamName,
				"n8n_email", cred.Email,
				"path", r.URL.Path,
				"error", err,
			)
			http.Error(w, "Failed to establish backend session", http.StatusBadGateway)
			return
		}
		ctx := context.WithValue(r.Context(), ctxN8NCookieKey, n8nCookie)
		h.proxy.ServeHTTP(w, r.WithContext(ctx))
		return
	}

	// ── Regular HTTP: buffer body and response for stale-cookie retry ────────

	// Buffer the request body so it can be replayed on retry.
	// Policy middleware already restores the body for workflow paths,
	// but we re-buffer here to cover all other endpoints.
	var bodyBytes []byte
	if r.Body != nil {
		limited := io.LimitReader(r.Body, maxBodyBufferSize)
		bodyBytes, err = io.ReadAll(limited)
		r.Body.Close()
		if err != nil {
			slog.Error("Failed to buffer request body", "path", r.URL.Path, "error", err)
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		r.ContentLength = int64(len(bodyBytes))
	}

	// Get initial n8n-auth cookie (may be cached)
	n8nCookie, err := h.cookiePool.GetCookie(cred.Email, cred.Password)
	if err != nil {
		slog.Error("Failed to get n8n cookie",
			"team", teamName,
			"n8n_email", cred.Email,
			"error", err,
		)
		http.Error(w, "Failed to establish backend session", http.StatusBadGateway)
		return
	}

	slog.Debug("Proxying request",
		"user", user.Email,
		"team", teamName,
		"method", r.Method,
		"path", r.URL.Path,
	)

	// First attempt — proxy through buffered response writer
	buf := newBufferedResponse()
	ctx := context.WithValue(r.Context(), ctxN8NCookieKey, n8nCookie)
	h.proxy.ServeHTTP(buf, r.WithContext(ctx))

	// ── Stale cookie detection ────────────────────────────────────────────────
	// If n8n returned 401, the cached cookie is no longer valid.
	// This happens when n8n restarts or its session store is cleared.
	// Strategy: invalidate cache → fetch fresh cookie → retry the request once.
	if buf.statusCode == http.StatusUnauthorized {
		slog.Warn("n8n returned 401 — cached cookie is stale, invalidating and retrying",
			"team", teamName,
			"user", user.Email,
			"path", r.URL.Path,
		)

		h.cookiePool.Invalidate(cred.Email)

		freshCookie, retryErr := h.cookiePool.GetCookie(cred.Email, cred.Password)
		if retryErr != nil {
			slog.Error("Failed to obtain fresh cookie after 401 — giving up",
				"team", teamName,
				"error", retryErr,
			)
			// Fall through to write the original 401 response
		} else {
			// Restore body for retry
			if len(bodyBytes) > 0 {
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				r.ContentLength = int64(len(bodyBytes))
			}

			buf2 := newBufferedResponse()
			ctx2 := context.WithValue(r.Context(), ctxN8NCookieKey, freshCookie)
			h.proxy.ServeHTTP(buf2, r.WithContext(ctx2))

			slog.Info("Retry after stale cookie succeeded",
				"team", teamName,
				"user", user.Email,
				"path", r.URL.Path,
				"status", buf2.statusCode,
			)
			buf2.flushTo(w)
			return
		}
	}

	// Write the (possibly first-attempt) buffered response to the real writer
	buf.flushTo(w)
}

// resolveTeam extracts the team name from the user's Keycloak groups.
// It looks for groups matching the configured prefix (default: "/n8n-prod-")
// and returns the suffix as the team name (e.g., "/n8n-prod-finance" → "finance").
func (h *Handler) resolveTeam(groups []string) string {
	for _, group := range groups {
		if strings.HasPrefix(group, h.groupPrefix) {
			teamName := strings.TrimPrefix(group, h.groupPrefix)
			if teamName != "" {
				return teamName
			}
		}
	}
	return ""
}

// isStreamingRequest returns true for connections that must not be buffered:
// WebSocket upgrades and Server-Sent Events (SSE) endpoints.
// These connections are long-lived and incompatible with response buffering.
func isStreamingRequest(r *http.Request) bool {
	// WebSocket upgrade
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return true
	}
	// n8n push (SSE/WebSocket) and events endpoints
	if strings.HasPrefix(r.URL.Path, "/rest/push") ||
		strings.HasPrefix(r.URL.Path, "/rest/events") {
		return true
	}
	return false
}

// ── Buffered Response Writer ─────────────────────────────────────────────────
// bufferedResponse captures a full HTTP response (headers + body) in memory
// so we can inspect the status code before deciding whether to replay or retry.

type bufferedResponse struct {
	headers     http.Header
	statusCode  int
	body        bytes.Buffer
	wroteHeader bool
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{
		headers:    make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (b *bufferedResponse) Header() http.Header {
	return b.headers
}

func (b *bufferedResponse) WriteHeader(code int) {
	if !b.wroteHeader {
		b.statusCode = code
		b.wroteHeader = true
	}
}

func (b *bufferedResponse) Write(data []byte) (int, error) {
	// Implicit 200 if WriteHeader was never called
	if !b.wroteHeader {
		b.statusCode = http.StatusOK
		b.wroteHeader = true
	}
	return b.body.Write(data)
}

// flushTo replays the buffered response to the real http.ResponseWriter.
func (b *bufferedResponse) flushTo(w http.ResponseWriter) {
	dst := w.Header()
	for k, vs := range b.headers {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
	w.WriteHeader(b.statusCode)
	if b.body.Len() > 0 {
		w.Write(b.body.Bytes())
	}
}