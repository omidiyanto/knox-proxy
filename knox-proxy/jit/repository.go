package jit

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Repository provides data access operations for JIT tickets.
type Repository struct {
	db *DB
}

// NewRepository creates a new Repository backed by the given database.
func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// ─── Create ────────────────────────────────────────────────────────────────────

// CreateTicket inserts a new ticket into the database and populates the
// auto-generated fields (id, ticket_number, created_at, updated_at).
func (r *Repository) CreateTicket(t *Ticket) error {
	ticketNumber, err := generateTicketNumber()
	if err != nil {
		return fmt.Errorf("failed to generate ticket number: %w", err)
	}
	t.TicketNumber = ticketNumber

	query := `
		INSERT INTO jit_tickets
			(ticket_number, email, display_name, team, workflow_id,
			 access_type, description, period_start, period_end, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at
	`

	err = r.db.Pool().QueryRow(query,
		t.TicketNumber,
		t.Email,
		t.DisplayName,
		t.Team,
		t.WorkflowID,
		t.AccessType,
		t.Description,
		t.PeriodStart,
		t.PeriodEnd,
		StatusRequested,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert ticket: %w", err)
	}

	t.Status = StatusRequested
	return nil
}

// ─── Read ──────────────────────────────────────────────────────────────────────

// GetTicketByID retrieves a single ticket by its UUID.
func (r *Repository) GetTicketByID(id string) (*Ticket, error) {
	query := `
		SELECT id, ticket_number, email, display_name, team, workflow_id,
		       access_type, description, period_start, period_end,
		       status, COALESCE(status_reason, ''), COALESCE(updated_by, ''),
		       created_at, updated_at
		FROM jit_tickets
		WHERE id = $1
	`

	t := &Ticket{}
	err := r.db.Pool().QueryRow(query, id).Scan(
		&t.ID, &t.TicketNumber, &t.Email, &t.DisplayName, &t.Team,
		&t.WorkflowID, &t.AccessType, &t.Description,
		&t.PeriodStart, &t.PeriodEnd, &t.Status,
		&t.StatusReason, &t.UpdatedBy,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get ticket: %w", err)
	}
	return t, nil
}

// ListTicketsByEmail returns tickets belonging to a specific user, filtered
// optionally by status and limited in count.
func (r *Repository) ListTicketsByEmail(email, status string, limit int) ([]Ticket, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	// Build WHERE clause dynamically
	conditions := []string{"email = $1"}
	args := []interface{}{email}
	argIdx := 2

	if status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, status)
		argIdx++
	}

	where := strings.Join(conditions, " AND ")

	// Count total matching tickets
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM jit_tickets WHERE %s", where)
	var total int
	if err := r.db.Pool().QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count tickets: %w", err)
	}

	// Fetch tickets
	dataQuery := fmt.Sprintf(`
		SELECT id, ticket_number, email, display_name, team, workflow_id,
		       access_type, description, period_start, period_end,
		       status, COALESCE(status_reason, ''), COALESCE(updated_by, ''),
		       created_at, updated_at
		FROM jit_tickets
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d
	`, where, argIdx)
	args = append(args, limit)

	rows, err := r.db.Pool().Query(dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list tickets: %w", err)
	}
	defer rows.Close()

	tickets, err := scanTickets(rows)
	if err != nil {
		return nil, 0, err
	}

	return tickets, total, nil
}

// ListAllTickets returns tickets matching the given filter (for admin use).
func (r *Repository) ListAllTickets(filter TicketFilter) ([]Ticket, int, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	conditions := []string{"1=1"}
	args := []interface{}{}
	argIdx := 1

	if filter.Email != "" {
		conditions = append(conditions, fmt.Sprintf("email = $%d", argIdx))
		args = append(args, filter.Email)
		argIdx++
	}
	if filter.Team != "" {
		conditions = append(conditions, fmt.Sprintf("team = $%d", argIdx))
		args = append(args, filter.Team)
		argIdx++
	}
	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.WorkflowID != "" {
		conditions = append(conditions, fmt.Sprintf("workflow_id = $%d", argIdx))
		args = append(args, filter.WorkflowID)
		argIdx++
	}

	where := strings.Join(conditions, " AND ")

	// Count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM jit_tickets WHERE %s", where)
	var total int
	if err := r.db.Pool().QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count tickets: %w", err)
	}

	// Fetch
	dataQuery := fmt.Sprintf(`
		SELECT id, ticket_number, email, display_name, team, workflow_id,
		       access_type, description, period_start, period_end,
		       status, COALESCE(status_reason, ''), COALESCE(updated_by, ''),
		       created_at, updated_at
		FROM jit_tickets
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, filter.Limit, filter.Offset)

	rows, err := r.db.Pool().Query(dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list tickets: %w", err)
	}
	defer rows.Close()

	tickets, err := scanTickets(rows)
	if err != nil {
		return nil, 0, err
	}

	return tickets, total, nil
}

// ─── Update ────────────────────────────────────────────────────────────────────

// UpdateTicketStatus transitions a ticket to a new status with an optional
// reason and the identity of the person making the change.
// Returns an error if the transition is not valid.
func (r *Repository) UpdateTicketStatus(id, newStatus, reason, updatedBy string) (*Ticket, error) {
	// Read current status
	var currentStatus string
	err := r.db.Pool().QueryRow(
		"SELECT status FROM jit_tickets WHERE id = $1", id,
	).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("ticket not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read ticket status: %w", err)
	}

	// Validate transition
	if !IsValidTransition(currentStatus, newStatus) {
		return nil, fmt.Errorf("invalid status transition: %s → %s", currentStatus, newStatus)
	}

	// Update
	query := `
		UPDATE jit_tickets
		SET status = $2, status_reason = $3, updated_by = $4, updated_at = NOW()
		WHERE id = $1
	`
	_, err = r.db.Pool().Exec(query, id, newStatus, nullableString(reason), nullableString(updatedBy))
	if err != nil {
		return nil, fmt.Errorf("failed to update ticket status: %w", err)
	}

	return r.GetTicketByID(id)
}

// systemUpdateStatus is an internal function used by the scheduler to transition
// statuses strictly based on time constraints (avoids standard transition validation mapping
// so the system can forcefully expire things if rules change).
func (r *Repository) systemUpdateStatus(id, newStatus string) (*Ticket, error) {
	query := `
		UPDATE jit_tickets
		SET status = $2, updated_by = 'system', updated_at = NOW()
		WHERE id = $1 AND status != $2
		RETURNING id
	`
	var returnedID string
	err := r.db.Pool().QueryRow(query, id, newStatus).Scan(&returnedID)
	if err == sql.ErrNoRows {
		// Possibly already transitioned or deleted
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("system update failed: %w", err)
	}
	return r.GetTicketByID(returnedID)
}

// GetPendingTransitions retrieves all tickets that might need time-based timers.
func (r *Repository) GetPendingTransitions() ([]Ticket, error) {
	query := `
		SELECT id, ticket_number, email, display_name, team, workflow_id,
		       access_type, description, period_start, period_end,
		       status, COALESCE(status_reason, ''), COALESCE(updated_by, ''),
		       created_at, updated_at
		FROM jit_tickets
		WHERE status IN ($1, $2, $3)
	`
	rows, err := r.db.Pool().Query(query, StatusRequested, StatusApproved, StatusActive)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending transitions: %w", err)
	}
	defer rows.Close()

	return scanTickets(rows)
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

// scanTickets reads multiple ticket rows from a query result set.
func scanTickets(rows *sql.Rows) ([]Ticket, error) {
	var tickets []Ticket
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(
			&t.ID, &t.TicketNumber, &t.Email, &t.DisplayName, &t.Team,
			&t.WorkflowID, &t.AccessType, &t.Description,
			&t.PeriodStart, &t.PeriodEnd, &t.Status,
			&t.StatusReason, &t.UpdatedBy,
			&t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan ticket row: %w", err)
		}
		tickets = append(tickets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return tickets, nil
}

// generateTicketNumber creates a human-readable ticket number: JIT-YYYYMMDD-XXXX
func generateTicketNumber() (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	suffix := strings.ToUpper(hex.EncodeToString(b))
	date := time.Now().Format("20060102")
	return fmt.Sprintf("JIT-%s-%s", date, suffix), nil
}

// nullableString returns nil for empty strings (for nullable TEXT columns).
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
