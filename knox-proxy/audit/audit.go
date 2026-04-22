package audit

import (
	"log/slog"
	"os"
	"sync"
)

var (
	auditLogger *slog.Logger
	once        sync.Once
)

// Init initializes the audit logger to write strictly to the specified file (e.g., audit.log).
func Init(logFilePath string) error {
	var err error
	once.Do(func() {
		file, e := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if e != nil {
			err = e
			return
		}

		handler := slog.NewJSONHandler(file, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
		auditLogger = slog.New(handler)
	})
	return err
}

// Log writes an audit message to the audit log file.
// If the audit logger is not initialized, it falls back to the default logger.
func Log(msg string, args ...any) {
	if auditLogger != nil {
		auditLogger.Info(msg, args...)
	} else {
		// Fallback if Init wasn't called or failed
		slog.Info("[AUDIT] "+msg, args...)
	}
}

// GetLogger returns the underlying audit logger instance.
func GetLogger() *slog.Logger {
	return auditLogger
}
