// Package logging provides structured logging with configurable levels
package logging

import (
	"os"

	mainlogging "github.com/lattiam/lattiam/pkg/logging"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	// DebugLevel is for detailed debugging information
	DebugLevel LogLevel = iota
	// InfoLevel is for general informational messages
	InfoLevel
	// WarnLevel is for warning messages that indicate potential problems
	WarnLevel
	// ErrorLevel is for error messages that indicate serious problems
	ErrorLevel
)

// Logger provides structured logging
type Logger struct {
	level      LogLevel
	prefix     string
	slogLogger *mainlogging.SlogLogger
}

// NewLogger creates a logger with the given prefix
func NewLogger(prefix string) *Logger {
	level := InfoLevel
	if os.Getenv("LATTIAM_DEBUG") == "true" {
		level = DebugLevel
	}
	// Reduce verbosity during tests
	if os.Getenv("LATTIAM_TEST_MODE") == "true" {
		level = ErrorLevel
	}
	return &Logger{
		level:      level,
		prefix:     prefix,
		slogLogger: mainlogging.NewSlogLogger(prefix),
	}
}

// Debugf logs a debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	if l.level <= DebugLevel {
		l.slogLogger.Debugf(format, args...)
	}
}

// Infof logs an info message
func (l *Logger) Infof(format string, args ...interface{}) {
	if l.level <= InfoLevel {
		l.slogLogger.Infof(format, args...)
	}
}

// Warnf logs a warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	if l.level <= WarnLevel {
		l.slogLogger.Warnf(format, args...)
	}
}

// Errorf logs an error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	if l.level <= ErrorLevel {
		l.slogLogger.Errorf(format, args...)
	}
}

// InfoMsg logs a simple info message
func (l *Logger) InfoMsg(msg string) {
	l.slogLogger.InfoMsg(msg)
}

// DebugMsg logs a simple debug message
func (l *Logger) DebugMsg(msg string) {
	l.slogLogger.DebugMsg(msg)
}

// WarnMsg logs a simple warning message
func (l *Logger) WarnMsg(msg string) {
	l.slogLogger.WarnMsg(msg)
}

// ErrorMsg logs a simple error message
func (l *Logger) ErrorMsg(msg string) {
	l.slogLogger.ErrorMsg(msg)
}

// ResourceDeploymentStart logs the start of a resource deployment
func (l *Logger) ResourceDeploymentStart(resourceType, resourceName string, current, total int) {
	l.slogLogger.ResourceDeploymentStart(resourceType, resourceName, current, total)
}

// ResourceDeploymentSuccess logs successful resource deployment
func (l *Logger) ResourceDeploymentSuccess(resourceType, resourceName string) {
	l.slogLogger.ResourceDeploymentSuccess(resourceType, resourceName)
}

// ResourceDeploymentFailed logs failed resource deployment
func (l *Logger) ResourceDeploymentFailed(resourceType, resourceName string, err error) {
	l.slogLogger.ResourceDeploymentFailed(resourceType, resourceName, err)
}

// DeploymentSummary logs the deployment summary
func (l *Logger) DeploymentSummary(successful, total int) {
	l.slogLogger.DeploymentSummary(successful, total)
}

// ErrorWithAnalysis logs an error with detailed analysis
func (l *Logger) ErrorWithAnalysis(err error) {
	l.slogLogger.ErrorWithAnalysis(err)
}
