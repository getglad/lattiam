package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// contextKey is a type for context keys to avoid collisions
type contextKey string

// CorrelationIDKey is the context key for correlation IDs
const CorrelationIDKey contextKey = "correlationID"

// SlogLogger provides structured logging using slog
type SlogLogger struct {
	logger    *slog.Logger
	component string
}

// NewSlogLogger creates a new logger using slog backend
func NewSlogLogger(component string) *SlogLogger {
	handler := createHandler()
	logger := slog.New(handler)

	return &SlogLogger{
		logger:    logger,
		component: component,
	}
}

// createHandler creates an appropriate slog handler based on environment variables
func createHandler() slog.Handler {
	var output io.Writer = os.Stdout
	level := getLogLevelSlog()

	format := strings.ToUpper(os.Getenv("LATTIAM_LOG_FORMAT"))
	switch format {
	case "JSON":
		return slog.NewJSONHandler(output, &slog.HandlerOptions{
			Level:       level,
			AddSource:   false,
			ReplaceAttr: replaceAttr,
		})
	default:
		return slog.NewTextHandler(output, &slog.HandlerOptions{
			Level:       level,
			AddSource:   false,
			ReplaceAttr: replaceAttr,
		})
	}
}

// getLogLevelSlog determines the slog level from environment
func getLogLevelSlog() slog.Level {
	levelStr := strings.ToUpper(os.Getenv("LATTIAM_LOG_LEVEL"))
	switch levelStr {
	case "TRACE", "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// replaceAttr customizes attribute names and values
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	// Convert standard slog level names to match existing format
	if a.Key == slog.LevelKey {
		level := a.Value.Any().(slog.Level)
		switch level {
		case slog.LevelDebug:
			return slog.Attr{Key: a.Key, Value: slog.StringValue("DEBUG")}
		case slog.LevelInfo:
			return slog.Attr{Key: a.Key, Value: slog.StringValue("INFO")}
		case slog.LevelWarn:
			return slog.Attr{Key: a.Key, Value: slog.StringValue("WARN")}
		case slog.LevelError:
			return slog.Attr{Key: a.Key, Value: slog.StringValue("ERROR")}
		}
	}
	return a
}

// Trace logs a trace-level message (maps to debug in slog)
func (l *SlogLogger) Trace(format string, args ...interface{}) {
	l.logger.Debug(format, "component", l.component, "args", args)
}

// Debug logs a debug-level message
func (l *SlogLogger) Debug(format string, args ...interface{}) {
	l.logger.Debug(format, "component", l.component, "args", args)
}

// Info logs an info-level message
func (l *SlogLogger) Info(format string, args ...interface{}) {
	l.logger.Info(format, "component", l.component, "args", args)
}

// Warn logs a warning-level message
func (l *SlogLogger) Warn(format string, args ...interface{}) {
	l.logger.Warn(format, "component", l.component, "args", args)
}

// Error logs an error-level message
func (l *SlogLogger) Error(format string, args ...interface{}) {
	l.logger.Error(format, "component", l.component, "args", args)
}

// WithContext returns a logger with context information
func (l *SlogLogger) WithContext(ctx context.Context) *SlogLogger {
	// Extract correlation ID if available
	if corrID, ok := ctx.Value(CorrelationIDKey).(string); ok {
		contextLogger := l.logger.With("correlation_id", corrID)
		return &SlogLogger{
			logger:    contextLogger,
			component: l.component,
		}
	}
	return l
}

// WithCorrelation returns a logger with correlation ID
func (l *SlogLogger) WithCorrelation(correlationID string) *SlogLogger {
	contextLogger := l.logger.With("correlation_id", correlationID)
	return &SlogLogger{
		logger:    contextLogger,
		component: l.component,
	}
}

// WithFields returns a logger with additional fields
func (l *SlogLogger) WithFields(fields map[string]interface{}) *SlogLogger {
	args := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	contextLogger := l.logger.With(args...)
	return &SlogLogger{
		logger:    contextLogger,
		component: l.component,
	}
}

// IsTraceEnabled returns true if debug logging is enabled (slog doesn't have trace)
func (l *SlogLogger) IsTraceEnabled() bool {
	return l.logger.Enabled(context.Background(), slog.LevelDebug)
}

// IsDebugEnabled returns true if debug logging is enabled
func (l *SlogLogger) IsDebugEnabled() bool {
	return l.logger.Enabled(context.Background(), slog.LevelDebug)
}

// Operation logs an operation with structured data
func (l *SlogLogger) Operation(ctx context.Context, operation string, details map[string]interface{}) {
	if !l.IsDebugEnabled() {
		return
	}

	args := []interface{}{"component", l.component, "operation", operation}
	for k, v := range details {
		args = append(args, k, v)
	}

	l.logger.DebugContext(ctx, "Operation", args...)
}

// Success logs a successful operation
func (l *SlogLogger) Success(ctx context.Context, operation string, details ...interface{}) {
	args := []interface{}{"component", l.component, "operation", operation, "status", "success"}
	if len(details) > 0 {
		args = append(args, "details", details[0])
	}

	l.logger.InfoContext(ctx, "Operation completed successfully", args...)
}

// Failure logs a failed operation
func (l *SlogLogger) Failure(ctx context.Context, operation string, err error) {
	l.logger.ErrorContext(ctx, "Operation failed",
		"component", l.component,
		"operation", operation,
		"status", "failed",
		"error", err)
}

// Helper methods for common logging patterns

// InfoMsg logs a simple info message (compatibility with internal/logging)
func (l *SlogLogger) InfoMsg(msg string) {
	l.logger.Info(msg, "component", l.component)
}

// DebugMsg logs a simple debug message (compatibility with internal/logging)
func (l *SlogLogger) DebugMsg(msg string) {
	l.logger.Debug(msg, "component", l.component)
}

// WarnMsg logs a simple warning message (compatibility with internal/logging)
func (l *SlogLogger) WarnMsg(msg string) {
	l.logger.Warn(msg, "component", l.component)
}

// ErrorMsg logs a simple error message (compatibility with internal/logging)
func (l *SlogLogger) ErrorMsg(msg string) {
	l.logger.Error(msg, "component", l.component)
}

// Formatted logging methods for compatibility

// Infof logs a formatted info message
func (l *SlogLogger) Infof(format string, args ...interface{}) {
	l.logger.Info(format, "component", l.component, "args", args)
}

// Debugf logs a formatted debug message
func (l *SlogLogger) Debugf(format string, args ...interface{}) {
	l.logger.Debug(format, "component", l.component, "args", args)
}

// Warnf logs a formatted warning message
func (l *SlogLogger) Warnf(format string, args ...interface{}) {
	l.logger.Warn(format, "component", l.component, "args", args)
}

// Errorf logs a formatted error message
func (l *SlogLogger) Errorf(format string, args ...interface{}) {
	l.logger.Error(format, "component", l.component, "args", args)
}

// Specialized logging methods for resource deployment

// ResourceDeploymentStart logs the start of a resource deployment
func (l *SlogLogger) ResourceDeploymentStart(resourceType, resourceName string, current, total int) {
	l.logger.Info("Starting resource deployment",
		"component", l.component,
		"resource_type", resourceType,
		"resource_name", resourceName,
		"current", current,
		"total", total)
}

// ResourceDeploymentSuccess logs successful resource deployment
func (l *SlogLogger) ResourceDeploymentSuccess(resourceType, resourceName string) {
	l.logger.Info("Resource deployment successful",
		"component", l.component,
		"resource_type", resourceType,
		"resource_name", resourceName,
		"status", "success")
}

// ResourceDeploymentFailed logs failed resource deployment
func (l *SlogLogger) ResourceDeploymentFailed(resourceType, resourceName string, err error) {
	l.logger.Error("Resource deployment failed",
		"component", l.component,
		"resource_type", resourceType,
		"resource_name", resourceName,
		"status", "failed",
		"error", err)
}

// DeploymentSummary logs the deployment summary
func (l *SlogLogger) DeploymentSummary(successful, total int) {
	if successful == total {
		l.logger.Info("Deployment completed successfully",
			"component", l.component,
			"successful", successful,
			"total", total,
			"status", "completed")
	} else {
		l.logger.Warn("Deployment completed with errors",
			"component", l.component,
			"successful", successful,
			"total", total,
			"status", "completed_with_errors")
	}
}

// ErrorWithAnalysis logs an error with detailed analysis
func (l *SlogLogger) ErrorWithAnalysis(err error) {
	l.logger.Error("Error analysis",
		"component", l.component,
		"error", err,
		"type", "analysis")
}
