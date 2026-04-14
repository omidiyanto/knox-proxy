package jit

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// migrationSQL is executed on startup to create the schema if it doesn't exist.
const migrationSQL = `
CREATE TABLE IF NOT EXISTS jit_tickets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_number VARCHAR(20) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL,
    display_name VARCHAR(255) NOT NULL,
    team VARCHAR(100) NOT NULL,
    workflow_id VARCHAR(100) NOT NULL,
    access_type VARCHAR(20) NOT NULL,
    description TEXT,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'requested',
    status_reason TEXT,
    updated_by VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tickets_email ON jit_tickets(email);
CREATE INDEX IF NOT EXISTS idx_tickets_status ON jit_tickets(status);
CREATE INDEX IF NOT EXISTS idx_tickets_team ON jit_tickets(team);
CREATE INDEX IF NOT EXISTS idx_tickets_workflow ON jit_tickets(workflow_id);
CREATE INDEX IF NOT EXISTS idx_tickets_period ON jit_tickets(period_start, period_end);
`

// DB wraps a PostgreSQL connection pool for JIT ticket storage.
type DB struct {
	pool *sql.DB
}

// NewDB creates a new database connection with retry logic for cold-start
// scenarios where PostgreSQL may not be ready yet.
func NewDB(dsn string) (*DB, error) {
	var pool *sql.DB
	var err error

	for i := 0; i < 10; i++ {
		pool, err = sql.Open("postgres", dsn)
		if err != nil {
			slog.Warn("Failed to open database connection, retrying...",
				"attempt", i+1, "error", err)
			time.Sleep(3 * time.Second)
			continue
		}

		// Verify the connection is alive
		if err = pool.Ping(); err != nil {
			slog.Warn("Database ping failed, retrying...",
				"attempt", i+1, "error", err)
			pool.Close()
			time.Sleep(3 * time.Second)
			continue
		}

		break
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database after retries: %w", err)
	}

	// Connection pool settings
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(5 * time.Minute)

	slog.Info("Database connection established")
	return &DB{pool: pool}, nil
}

// Migrate runs the schema migration to create the jit_tickets table
// and indexes if they don't already exist.
func (db *DB) Migrate() error {
	_, err := db.pool.Exec(migrationSQL)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	slog.Info("Database migration completed successfully")
	return nil
}

// Pool returns the underlying sql.DB connection pool.
func (db *DB) Pool() *sql.DB {
	return db.pool
}

// Close closes the database connection pool.
func (db *DB) Close() error {
	slog.Info("Closing database connection")
	return db.pool.Close()
}
