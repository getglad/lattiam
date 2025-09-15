package logging

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// LogLevel represents the severity of a log message
type LogLevel int

// LogLevel constants represent the various log levels
const (
	TRACE LogLevel = iota
	DEBUG
	INFO
	WARN
	ERROR
)

const (
	logLevelTrace = "TRACE"
	logLevelDebug = "DEBUG"
	logLevelInfo  = "INFO"
	logLevelWarn  = "WARN"
	logLevelError = "ERROR"
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case TRACE:
		return logLevelTrace
	case DEBUG:
		return logLevelDebug
	case INFO:
		return logLevelInfo
	case WARN:
		return logLevelWarn
	case ERROR:
		return logLevelError
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging with context
type Logger struct {
	component  string
	level      LogLevel
	slogLogger *SlogLogger
}

// NewLogger creates a new logger for a specific component
func NewLogger(component string) *Logger {
	return &Logger{
		component:  component,
		level:      getLogLevel(),
		slogLogger: NewSlogLogger(component),
	}
}

// getLogLevel determines the current log level from environment
func getLogLevel() LogLevel {
	levelStr := strings.ToUpper(os.Getenv("LATTIAM_LOG_LEVEL"))
	switch levelStr {
	case logLevelTrace:
		return TRACE
	case logLevelDebug:
		return DEBUG
	case logLevelInfo:
		return INFO
	case logLevelWarn:
		return WARN
	case logLevelError:
		return ERROR
	default:
		return INFO // Default to INFO level
	}
}

// logf logs a message at the specified level
func (l *Logger) logf(level LogLevel, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	// Use slog backend for structured logging
	switch level {
	case TRACE, DEBUG:
		l.slogLogger.Debug(fmt.Sprintf(format, args...))
	case INFO:
		l.slogLogger.Info(fmt.Sprintf(format, args...))
	case WARN:
		l.slogLogger.Warn(fmt.Sprintf(format, args...))
	case ERROR:
		l.slogLogger.Error(fmt.Sprintf(format, args...))
	}
}

// Trace logs a trace-level message
func (l *Logger) Trace(format string, args ...interface{}) {
	l.logf(TRACE, format, args...)
}

// Debug logs a debug-level message
func (l *Logger) Debug(format string, args ...interface{}) {
	l.logf(DEBUG, format, args...)
}

// Info logs an info-level message
func (l *Logger) Info(format string, args ...interface{}) {
	l.logf(INFO, format, args...)
}

// Warn logs a warning-level message
func (l *Logger) Warn(format string, args ...interface{}) {
	l.logf(WARN, format, args...)
}

// Error logs an error-level message
func (l *Logger) Error(format string, args ...interface{}) {
	l.logf(ERROR, format, args...)
}

// WithContext logs with context information
func (l *Logger) WithContext(ctx context.Context, level LogLevel, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	// Use slog backend with context
	contextLogger := l.slogLogger.WithContext(ctx)
	switch level {
	case TRACE, DEBUG:
		contextLogger.Debug(fmt.Sprintf(format, args...))
	case INFO:
		contextLogger.Info(fmt.Sprintf(format, args...))
	case WARN:
		contextLogger.Warn(fmt.Sprintf(format, args...))
	case ERROR:
		contextLogger.Error(fmt.Sprintf(format, args...))
	}
}

// IsTraceEnabled returns true if trace logging is enabled
func (l *Logger) IsTraceEnabled() bool {
	return l.level <= TRACE
}

// IsDebugEnabled returns true if debug logging is enabled
func (l *Logger) IsDebugEnabled() bool {
	return l.level <= DEBUG
}

// Operation logs an operation with structured data
func (l *Logger) Operation(ctx context.Context, operation string, details map[string]interface{}) {
	l.slogLogger.Operation(ctx, operation, details)
}

// Success logs a successful operation
func (l *Logger) Success(ctx context.Context, operation string, details ...interface{}) {
	l.slogLogger.Success(ctx, operation, details...)
}

// Failure logs a failed operation
func (l *Logger) Failure(ctx context.Context, operation string, err error) {
	l.slogLogger.Failure(ctx, operation, err)
}

// Global logger instance for easy migration
var defaultLogger = NewLogger("lattiam")

// Trace logs a trace level message
func Trace(format string, args ...interface{}) {
	defaultLogger.Trace(format, args...)
}

// Debug logs a debug level message
func Debug(format string, args ...interface{}) {
	defaultLogger.Debug(format, args...)
}

// Info logs an info level message
func Info(format string, args ...interface{}) {
	defaultLogger.Info(format, args...)
}

// Warn logs a warning level message
func Warn(format string, args ...interface{}) {
	defaultLogger.Warn(format, args...)
}

// Error logs an error level message
func Error(format string, args ...interface{}) {
	defaultLogger.Error(format, args...)
}
