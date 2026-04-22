package jit

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"knox-proxy/audit"
	"knox-proxy/middleware"
)

// workflowIDRegex validates that a workflow ID contains safe characters only.
var workflowIDRegex = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)

// Handler serves all JIT ticketing API endpoints. It implements http.Handler
// and internally routes requests based on path and method.
type Handler struct {
	repo        *Repository
	scheduler   *Scheduler
	webhookURL  string
	maxDuration time.Duration
	apiKey      string
	groupPrefix string
	httpClient  *http.Client
}

// NewHandler creates a new JIT API handler.
func NewHandler(repo *Repository, scheduler *Scheduler, webhookURL string, maxDuration time.Duration, apiKey, groupPrefix string) *Handler {
	return &Handler{
		repo:        repo,
		scheduler:   scheduler,
		webhookURL:  webhookURL,
		maxDuration: maxDuration,
		apiKey:      apiKey,
		groupPrefix: groupPrefix,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// ServeHTTP routes incoming requests to the appropriate handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")

	switch {
	// ── User-facing endpoints (protected by Knox session via middleware) ──

	case path == "/knox-api/request-jit" && r.Method == http.MethodPost:
		h.handleRequestJIT(w, r)

	case path == "/knox-api/tickets" && r.Method == http.MethodGet:
		h.handleListTickets(w, r)

	case strings.HasPrefix(path, "/knox-api/tickets/") &&
		strings.HasSuffix(path, "/cancel") &&
		r.Method == http.MethodPatch:
		h.handleCancelTicket(w, r)

	case strings.HasPrefix(path, "/knox-api/tickets/") &&
		!strings.HasPrefix(path, "/knox-api/admin/") &&
		r.Method == http.MethodGet:
		h.handleGetTicket(w, r)

	case path == "/knox-api/user-info" && r.Method == http.MethodGet:
		h.handleUserInfo(w, r)

	// ── Admin/External endpoints (protected by API key) ──

	case path == "/knox-api/admin/tickets" && r.Method == http.MethodGet:
		h.requireAPIKey(w, r, h.handleAdminListTickets)

	case strings.HasPrefix(path, "/knox-api/admin/tickets/") &&
		strings.HasSuffix(path, "/status") &&
		r.Method == http.MethodPatch:
		h.requireAPIKey(w, r, h.handleAdminUpdateStatus)

	case strings.HasPrefix(path, "/knox-api/admin/tickets/") &&
		r.Method == http.MethodGet:
		h.requireAPIKey(w, r, h.handleAdminGetTicket)

	default:
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error: "Not Found", Message: "Unknown knox-api endpoint",
		})
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// User-Facing Handlers
// ═══════════════════════════════════════════════════════════════════════════════

// handleRequestJIT processes a new JIT access request.
func (h *Handler) handleRequestJIT(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Unauthorized"})
		return
	}

	// Parse request body
	var req CreateTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "Bad Request", Message: "Invalid JSON body",
		})
		return
	}

	// Validate
	if errs := h.validateCreateRequest(&req); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "Validation Error", Message: strings.Join(errs, "; "),
		})
		return
	}

	// Parse times
	periodStart, _ := time.Parse(time.RFC3339, req.PeriodStart)
	periodEnd, _ := time.Parse(time.RFC3339, req.PeriodEnd)

	// Resolve team from user's Keycloak groups
	team := resolveTeam(user.Groups, h.groupPrefix)

	// Build access type string
	sort.Strings(req.AccessType)
	accessType := strings.Join(req.AccessType, ",")

	// Create ticket
	ticket := &Ticket{
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Team:        team,
		WorkflowID:  req.WorkflowID,
		AccessType:  accessType,
		Description: req.Description,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	}

	if err := h.repo.CreateTicket(ticket); err != nil {
		slog.Error("Failed to create ticket", "error", err, "user", user.Email)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "Internal Error", Message: "Failed to create ticket. Please try again.",
		})
		return
	}

	slog.Info("JIT ticket created",
		"ticket_number", ticket.TicketNumber,
		"user", user.Email,
		"team", team,
		"workflow_id", req.WorkflowID,
		"access_type", accessType,
		"duration", ticket.Duration(),
	)
	audit.Log("JIT Ticket Requested",
		"ticket_number", ticket.TicketNumber,
		"user", user.Email,
		"workflow_id", req.WorkflowID,
		"access_type", accessType,
		"duration", ticket.Duration(),
	)

	// Fire webhook asynchronously (non-blocking)
	if h.scheduler != nil {
		h.scheduler.Schedule(ticket)
	}
	go h.FireWebhook(ticket)

	writeJSON(w, http.StatusCreated, TicketResponse{
		Ticket:   *ticket,
		Duration: ticket.Duration(),
	})
}

// handleListTickets returns tickets owned by the authenticated user.
func (h *Handler) handleListTickets(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Unauthorized"})
		return
	}

	status := r.URL.Query().Get("status")
	limit := parseIntParam(r.URL.Query().Get("limit"), 50)

	tickets, total, err := h.repo.ListTicketsByEmail(user.Email, status, limit)
	if err != nil {
		slog.Error("Failed to list tickets", "error", err, "user", user.Email)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "Internal Error", Message: "Failed to list tickets",
		})
		return
	}

	if tickets == nil {
		tickets = []Ticket{}
	}

	writeJSON(w, http.StatusOK, TicketListResponse{
		Tickets: tickets,
		Total:   total,
	})
}

// handleGetTicket returns a single ticket detail (own tickets only).
func (h *Handler) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Unauthorized"})
		return
	}

	id := extractPathParam(r.URL.Path, "/knox-api/tickets/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "Bad Request", Message: "Missing ticket ID",
		})
		return
	}

	ticket, err := h.repo.GetTicketByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "Internal Error", Message: "Failed to get ticket",
		})
		return
	}
	if ticket == nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "Not Found"})
		return
	}

	// Users can only view their own tickets
	if ticket.Email != user.Email {
		writeJSON(w, http.StatusForbidden, ErrorResponse{
			Error: "Forbidden", Message: "You can only view your own tickets",
		})
		return
	}

	writeJSON(w, http.StatusOK, TicketResponse{
		Ticket:   *ticket,
		Duration: ticket.Duration(),
	})
}

// handleCancelTicket allows a user to cancel their own pending ticket.
func (h *Handler) handleCancelTicket(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Unauthorized"})
		return
	}

	path := strings.TrimSuffix(r.URL.Path, "/cancel")
	id := extractPathParam(path, "/knox-api/tickets/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "Bad Request", Message: "Missing ticket ID",
		})
		return
	}

	ticket, err := h.repo.GetTicketByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "Internal Error", Message: "Failed to get ticket",
		})
		return
	}
	if ticket == nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "Not Found"})
		return
	}

	// Ownership check
	if ticket.Email != user.Email {
		writeJSON(w, http.StatusForbidden, ErrorResponse{
			Error: "Forbidden", Message: "You can only cancel your own tickets",
		})
		return
	}

	// Must be in "requested" status
	if ticket.Status != StatusRequested {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "Bad Request", Message: "Only 'requested' tickets can be canceled",
		})
		return
	}

	updatedTicket, err := h.repo.UpdateTicketStatus(id, StatusCanceled, "Canceled by user", user.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "Internal Error", Message: "Failed to cancel ticket",
		})
		return
	}

	slog.Info("Ticket canceled by user",
		"ticket_number", updatedTicket.TicketNumber,
		"user", user.Email,
	)
	audit.Log("JIT Ticket Canceled",
		"ticket_number", updatedTicket.TicketNumber,
		"user", user.Email,
	)

	// Fire webhook to notify external systems of the cancellation
	if h.scheduler != nil {
		h.scheduler.Cancel(id)
	}
	go h.FireWebhook(updatedTicket)

	writeJSON(w, http.StatusOK, TicketResponse{
		Ticket:   *updatedTicket,
		Duration: updatedTicket.Duration(),
	})
}

// handleUserInfo returns the current user's profile information.
func (h *Handler) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Unauthorized"})
		return
	}

	team := resolveTeam(user.Groups, h.groupPrefix)

	writeJSON(w, http.StatusOK, UserInfoResponse{
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Team:        team,
	})
}

// ═══════════════════════════════════════════════════════════════════════════════
// Admin / External Tool Handlers
// ═══════════════════════════════════════════════════════════════════════════════

// handleAdminListTickets lists all tickets with optional filters.
func (h *Handler) handleAdminListTickets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := TicketFilter{
		Email:      q.Get("email"),
		Team:       q.Get("team"),
		Status:     q.Get("status"),
		WorkflowID: q.Get("workflow_id"),
		Limit:      parseIntParam(q.Get("limit"), 50),
		Offset:     parseIntParam(q.Get("offset"), 0),
	}

	tickets, total, err := h.repo.ListAllTickets(filter)
	if err != nil {
		slog.Error("Admin: failed to list tickets", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "Internal Error", Message: "Failed to list tickets",
		})
		return
	}

	if tickets == nil {
		tickets = []Ticket{}
	}

	writeJSON(w, http.StatusOK, TicketListResponse{
		Tickets: tickets,
		Total:   total,
	})
}

// handleAdminGetTicket returns any ticket detail (no ownership check).
func (h *Handler) handleAdminGetTicket(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/knox-api/admin/tickets/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "Bad Request", Message: "Missing ticket ID",
		})
		return
	}

	ticket, err := h.repo.GetTicketByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "Internal Error", Message: "Failed to get ticket",
		})
		return
	}
	if ticket == nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "Not Found"})
		return
	}

	writeJSON(w, http.StatusOK, TicketResponse{
		Ticket:   *ticket,
		Duration: ticket.Duration(),
	})
}

// handleAdminUpdateStatus changes a ticket's status with validation.
func (h *Handler) handleAdminUpdateStatus(w http.ResponseWriter, r *http.Request) {
	// Extract ticket ID from: /knox-api/admin/tickets/{id}/status
	path := strings.TrimSuffix(r.URL.Path, "/status")
	id := extractPathParam(path, "/knox-api/admin/tickets/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "Bad Request", Message: "Missing ticket ID",
		})
		return
	}

	var req UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "Bad Request", Message: "Invalid JSON body",
		})
		return
	}

	// Validate target status
	validStatuses := map[string]bool{
		StatusApproved: true, StatusRejected: true, StatusRevoked: true,
	}
	if !validStatuses[req.Status] {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "Bad Request",
			Message: fmt.Sprintf("Invalid target status: %s. Allowed: approved, rejected, revoked", req.Status),
		})
		return
	}

	ticket, err := h.repo.UpdateTicketStatus(id, req.Status, req.Reason, req.UpdatedBy)
	if err != nil {
		slog.Error("Admin: failed to update ticket status",
			"id", id, "target_status", req.Status, "error", err)
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "Bad Request", Message: err.Error(),
		})
		return
	}

	slog.Info("Admin: ticket status updated",
		"ticket_number", ticket.TicketNumber,
		"new_status", req.Status,
		"updated_by", req.UpdatedBy,
	)
	audit.Log("JIT Ticket Status Updated (Admin)",
		"ticket_number", ticket.TicketNumber,
		"new_status", req.Status,
		"updated_by", req.UpdatedBy,
		"workflow_id", ticket.WorkflowID,
		"ticket_owner", ticket.Email,
	)

	if h.scheduler != nil {
		if req.Status == StatusApproved {
			h.scheduler.Schedule(ticket)
		} else if req.Status == StatusRejected || req.Status == StatusRevoked {
			h.scheduler.Cancel(ticket.ID)
		}
	}

	// Fire webhook to notify external systems of the manual admin status update
	go h.FireWebhook(ticket)

	writeJSON(w, http.StatusOK, TicketResponse{
		Ticket:   *ticket,
		Duration: ticket.Duration(),
	})
}

// ═══════════════════════════════════════════════════════════════════════════════
// Webhook
// ═══════════════════════════════════════════════════════════════════════════════

// FireWebhook sends the ticket payload to the configured webhook URL.
// This runs asynchronously and logs any errors — it does not block the API response.
func (h *Handler) FireWebhook(ticket *Ticket) {
	if h.webhookURL == "" {
		return
	}
	payload := map[string]interface{}{
		"ticket_id":        ticket.ID,
		"ticket_number":    ticket.TicketNumber,
		"user_requestor":   ticket.Email,
		"display_name":     ticket.DisplayName,
		"n8n_team":         ticket.Team,
		"workflow_id":      ticket.WorkflowID,
		"access_type":      ticket.AccessType,
		"description":      ticket.Description,
		"jit_period_start": ticket.PeriodStart.Format(time.RFC3339),
		"jit_period_end":   ticket.PeriodEnd.Format(time.RFC3339),
		"jit_duration":     ticket.Duration(),
		"requested_at":     ticket.CreatedAt.Format(time.RFC3339),
		"status":           ticket.Status,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Failed to marshal webhook payload", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, h.webhookURL, strings.NewReader(string(body)))
	if err != nil {
		slog.Error("Failed to create webhook request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Knox-IAM-Gateway/1.0")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		slog.Error("Webhook delivery failed",
			"url", h.webhookURL,
			"ticket", ticket.TicketNumber,
			"error", err,
		)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		slog.Info("Webhook delivered successfully",
			"ticket", ticket.TicketNumber,
			"status", resp.StatusCode,
		)
	} else {
		slog.Warn("Webhook returned non-success status",
			"ticket", ticket.TicketNumber,
			"status", resp.StatusCode,
			"url", h.webhookURL,
		)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Validation & Helpers
// ═══════════════════════════════════════════════════════════════════════════════

// validateCreateRequest checks the incoming request for completeness and correctness.
func (h *Handler) validateCreateRequest(req *CreateTicketRequest) []string {
	var errs []string

	// Workflow ID
	if req.WorkflowID == "" {
		errs = append(errs, "workflow_id is required")
	} else if !workflowIDRegex.MatchString(req.WorkflowID) {
		errs = append(errs, "workflow_id contains invalid characters")
	}

	// Access Type
	if len(req.AccessType) == 0 {
		errs = append(errs, "access_type is required (select at least one: run, edit)")
	} else {
		for _, at := range req.AccessType {
			if at != "run" && at != "edit" {
				errs = append(errs, fmt.Sprintf("invalid access_type: %s (must be 'run' or 'edit')", at))
			}
		}
	}

	// Period Start
	periodStart, err := time.Parse(time.RFC3339, req.PeriodStart)
	if err != nil {
		errs = append(errs, "period_start must be a valid RFC3339 timestamp")
	}

	// Period End
	periodEnd, err := time.Parse(time.RFC3339, req.PeriodEnd)
	if err != nil {
		errs = append(errs, "period_end must be a valid RFC3339 timestamp")
	}

	// Duration checks (only if both dates parsed successfully)
	if periodStart.IsZero() == false && periodEnd.IsZero() == false {
		if !periodEnd.After(periodStart) {
			errs = append(errs, "period_end must be after period_start")
		}
		duration := periodEnd.Sub(periodStart)
		if duration > h.maxDuration {
			errs = append(errs, fmt.Sprintf("request duration exceeds maximum allowed (%d days)", int(h.maxDuration.Hours()/24)))
		}
	}

	// Description
	if strings.TrimSpace(req.Description) == "" {
		errs = append(errs, "description is required")
	} else if len(strings.TrimSpace(req.Description)) < 10 {
		errs = append(errs, "description must be at least 10 characters")
	}

	return errs
}

// requireAPIKey wraps a handler with API key authentication.
func (h *Handler) requireAPIKey(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if h.apiKey == "" {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Service Unavailable",
			Message: "Admin API is not configured. Set KNOX_API_KEY environment variable.",
		})
		return
	}

	key := r.Header.Get("X-Knox-API-Key")
	if key == "" {
		key = r.Header.Get("Authorization")
		key = strings.TrimPrefix(key, "Bearer ")
	}

	if key != h.apiKey {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{
			Error: "Unauthorized", Message: "Invalid or missing API key",
		})
		return
	}

	next(w, r)
}

// resolveTeam extracts the team name from Keycloak groups using the configured prefix.
func resolveTeam(groups []string, prefix string) string {
	for _, group := range groups {
		if strings.HasPrefix(group, prefix) {
			teamName := strings.TrimPrefix(group, prefix)
			if teamName != "" {
				return teamName
			}
		}
	}
	return ""
}

// extractPathParam extracts the value after a prefix from a URL path.
// Example: extractPathParam("/knox-api/tickets/abc-123", "/knox-api/tickets/") → "abc-123"
func extractPathParam(urlPath, prefix string) string {
	if !strings.HasPrefix(urlPath, prefix) {
		return ""
	}
	return strings.TrimPrefix(urlPath, prefix)
}

// parseIntParam parses a string parameter to int with a default fallback.
func parseIntParam(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return defaultVal
	}
	return v
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
