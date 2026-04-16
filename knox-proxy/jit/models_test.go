package jit

import (
	"testing"
)

func TestIsValidTransition_Valid(t *testing.T) {
	valid := []struct {
		from string
		to   string
	}{
		{StatusRequested, StatusApproved},
		{StatusRequested, StatusRejected},
		{StatusRequested, StatusCanceled},
		{StatusApproved, StatusActive},
		{StatusApproved, StatusRevoked},
		{StatusActive, StatusRevoked},
		{StatusActive, StatusExpired},
	}

	for _, tt := range valid {
		t.Run(tt.from+"→"+tt.to, func(t *testing.T) {
			if !IsValidTransition(tt.from, tt.to) {
				t.Errorf("expected %s→%s to be valid", tt.from, tt.to)
			}
		})
	}
}

func TestIsValidTransition_Invalid(t *testing.T) {
	invalid := []struct {
		from string
		to   string
	}{
		{StatusRejected, StatusApproved},
		{StatusExpired, StatusActive},
		{StatusCanceled, StatusApproved},
		{StatusActive, StatusApproved},
		{StatusRejected, StatusActive},
		{StatusRevoked, StatusActive},
		{StatusExpired, StatusApproved},
		{StatusCanceled, StatusActive},
		{StatusActive, StatusRequested},
		{StatusApproved, StatusRequested},
		{StatusRequested, StatusExpired},    // not in transitions
		{StatusApproved, StatusExpired},     // not in transitions
		// Same status
		{StatusRequested, StatusRequested},
		{StatusActive, StatusActive},
	}

	for _, tt := range invalid {
		t.Run(tt.from+"→"+tt.to, func(t *testing.T) {
			if IsValidTransition(tt.from, tt.to) {
				t.Errorf("expected %s→%s to be invalid", tt.from, tt.to)
			}
		})
	}
}

func TestTicket_Duration(t *testing.T) {
	tests := []struct {
		name     string
		ticket   Ticket
		expected string
	}{
		{
			"1 hour",
			Ticket{
				PeriodStart: mustParseTime("2024-01-01T10:00:00Z"),
				PeriodEnd:   mustParseTime("2024-01-01T11:00:00Z"),
			},
			"1h 0m",
		},
		{
			"1 day",
			Ticket{
				PeriodStart: mustParseTime("2024-01-01T10:00:00Z"),
				PeriodEnd:   mustParseTime("2024-01-02T10:00:00Z"),
			},
			"1d 0h 0m",
		},
		{
			"2 days 3 hours 30 minutes",
			Ticket{
				PeriodStart: mustParseTime("2024-01-01T00:00:00Z"),
				PeriodEnd:   mustParseTime("2024-01-03T03:30:00Z"),
			},
			"2d 3h 30m",
		},
		{
			"30 minutes",
			Ticket{
				PeriodStart: mustParseTime("2024-01-01T10:00:00Z"),
				PeriodEnd:   mustParseTime("2024-01-01T10:30:00Z"),
			},
			"30m",
		},
		{
			"negative duration",
			Ticket{
				PeriodStart: mustParseTime("2024-01-02T10:00:00Z"),
				PeriodEnd:   mustParseTime("2024-01-01T10:00:00Z"),
			},
			"0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ticket.Duration()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestStatusConstants(t *testing.T) {
	expected := map[string]string{
		"requested": StatusRequested,
		"approved":  StatusApproved,
		"rejected":  StatusRejected,
		"active":    StatusActive,
		"expired":   StatusExpired,
		"canceled":  StatusCanceled,
		"revoked":   StatusRevoked,
	}

	for value, constant := range expected {
		if constant != value {
			t.Errorf("expected status constant %q to equal %q", constant, value)
		}
	}
}

func TestTicket_DefaultValues(t *testing.T) {
	ticket := Ticket{}

	if ticket.Status != "" {
		t.Errorf("expected empty status on zero value, got %q", ticket.Status)
	}
	if ticket.ID != "" {
		t.Errorf("expected empty ID on zero value, got %q", ticket.ID)
	}
	if ticket.TicketNumber != "" {
		t.Errorf("expected empty ticket number, got %q", ticket.TicketNumber)
	}
}
