package jit

import (
	"log/slog"
	"sync"
	"time"
)

// Scheduler manages in-memory timers for ticket state transitions.
type Scheduler struct {
	mu           sync.Mutex
	timers       map[string]*time.Timer
	repo         *Repository
	onTransition func(t Ticket)
}

// NewScheduler creates a new event-driven ticket scheduler.
func NewScheduler(repo *Repository, onTransition func(t Ticket)) *Scheduler {
	return &Scheduler{
		timers:       make(map[string]*time.Timer),
		repo:         repo,
		onTransition: onTransition,
	}
}

// InitFromDB loads all active and approved tickets from the DB and schedules their timers.
func (s *Scheduler) InitFromDB() error {
	tickets, err := s.repo.GetPendingTransitions()
	if err != nil {
		return err
	}

	for _, t := range tickets {
		// Use a local copy of the pointer inside the loop
		ticket := t
		s.Schedule(&ticket)
	}

	slog.Info("Scheduler initialized", "loaded_timers", len(tickets))
	return nil
}

// Cancel stops and removes any pending timer for the given ticket ID.
func (s *Scheduler) Cancel(ticketID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if timer, ok := s.timers[ticketID]; ok {
		timer.Stop()
		delete(s.timers, ticketID)
		slog.Debug("Scheduler: canceled timer", "ticket_id", ticketID)
	}
}

// Schedule sets up a timer to transition the ticket at the required time.
func (s *Scheduler) Schedule(t *Ticket) {
	// Cancel any existing timer first
	s.Cancel(t.ID)

	now := time.Now()
	var targetTime time.Time
	var targetStatus string

	if t.Status == StatusApproved {
		targetTime = t.PeriodStart
		targetStatus = StatusActive
	} else if t.Status == StatusActive || t.Status == StatusRequested {
		targetTime = t.PeriodEnd
		targetStatus = StatusExpired
	} else {
		// No time-based transition needed for other statuses
		return
	}

	duration := targetTime.Sub(now)
	if duration < 0 {
		// If the time has already passed, trigger immediately (e.g., missed during downtime)
		duration = 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Need a copy of the ticket ID to avoid closure capture issues
	ticketID := t.ID
	
	s.timers[ticketID] = time.AfterFunc(duration, func() {
		// This runs in a new goroutine when the timer fires
		s.executeTransition(ticketID, targetStatus)
	})

	slog.Debug("Scheduler: scheduled timer",
		"ticket", t.TicketNumber,
		"target_status", targetStatus,
		"in", duration.Round(time.Second),
	)
}

// executeTransition actually applies the transition in the DB and triggers the callback
func (s *Scheduler) executeTransition(ticketID, targetStatus string) {
	// First remove it from the map so it's clean
	s.mu.Lock()
	delete(s.timers, ticketID)
	s.mu.Unlock()

	updatedTicket, err := s.repo.systemUpdateStatus(ticketID, targetStatus)
	if err != nil {
		slog.Error("Scheduler: failed to apply time-based transition",
			"ticket_id", ticketID,
			"target_status", targetStatus,
			"error", err,
		)
		return
	}

	if updatedTicket != nil {
		slog.Info("Scheduler: automatic transition applied",
			"ticket", updatedTicket.TicketNumber,
			"new_status", targetStatus,
		)
		if s.onTransition != nil {
			s.onTransition(*updatedTicket)
		}

		// If it transitioned to Active, we need to schedule the next timer for Expired!
		if targetStatus == StatusActive {
			s.Schedule(updatedTicket)
		}
	}
}
