package jit

import (
	"fmt"
	"time"
)

// ─── Ticket Status Constants ──────────────────────────────────────────────────

const (
	StatusRequested = "requested"
	StatusApproved  = "approved"
	StatusActive    = "active"
	StatusExpired   = "expired"
	StatusRejected  = "rejected"
	StatusRevoked   = "revoked"
	StatusCanceled  = "canceled"
)

// ValidTransitions defines the allowed status transitions for tickets.
// Map key = current status, value = set of allowed target statuses.
var ValidTransitions = map[string]map[string]bool{
	StatusRequested: {StatusApproved: true, StatusRejected: true, StatusCanceled: true},
	StatusApproved:  {StatusActive: true, StatusRevoked: true},
	StatusActive:    {StatusRevoked: true, StatusExpired: true},
}

// IsValidTransition checks if transitioning from `from` to `to` is allowed.
func IsValidTransition(from, to string) bool {
	targets, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	return targets[to]
}

// ─── Ticket Model ─────────────────────────────────────────────────────────────

// Ticket represents a JIT access request ticket stored in PostgreSQL.
type Ticket struct {
	ID           string    `json:"id"`
	TicketNumber string    `json:"ticket_number"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	Team         string    `json:"team"`
	WorkflowID   string    `json:"workflow_id"`
	AccessType   string    `json:"access_type"` // "run", "edit", or "run,edit"
	Description  string    `json:"description"`
	PeriodStart  time.Time `json:"period_start"`
	PeriodEnd    time.Time `json:"period_end"`
	Status       string    `json:"status"`
	StatusReason string    `json:"status_reason,omitempty"`
	UpdatedBy    string    `json:"updated_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Duration returns a human-readable duration string (e.g., "3d 2h 30m").
func (t *Ticket) Duration() string {
	d := t.PeriodEnd.Sub(t.PeriodStart)
	if d < 0 {
		return "0m"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// ─── Request / Response Types ─────────────────────────────────────────────────

// CreateTicketRequest is the JSON body submitted by the browser.
// User identity fields (email, display_name, team) are NOT included here —
// they are enriched server-side from the authenticated session.
type CreateTicketRequest struct {
	WorkflowID  string   `json:"workflow_id"`
	AccessType  []string `json:"access_type"` // ["run"], ["edit"], or ["run", "edit"]
	PeriodStart string   `json:"period_start"`
	PeriodEnd   string   `json:"period_end"`
	Description string   `json:"description"`
}

// UpdateStatusRequest is the JSON body for admin status changes.
type UpdateStatusRequest struct {
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	UpdatedBy string `json:"updated_by"`
}

// TicketFilter holds optional query parameters for listing tickets.
type TicketFilter struct {
	Email      string
	Team       string
	Status     string
	WorkflowID string
	Limit      int
	Offset     int
}

// TicketResponse wraps a single ticket for API responses.
type TicketResponse struct {
	Ticket   Ticket `json:"ticket"`
	Duration string `json:"duration"`
}

// TicketListResponse wraps a list of tickets for API responses.
type TicketListResponse struct {
	Tickets []Ticket `json:"tickets"`
	Total   int      `json:"total"`
}

// UserInfoResponse is returned by GET /knox-api/user-info.
type UserInfoResponse struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Team        string `json:"team"`
}

// ErrorResponse is a standard API error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}
