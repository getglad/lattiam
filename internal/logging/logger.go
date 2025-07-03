package logging

import (
	"fmt"
	"log"
	"os"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	DebugLevel LogLevel = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

// Logger provides structured logging
type Logger struct {
	level  LogLevel
	prefix string
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
		level:  level,
		prefix: prefix,
	}
}

// Debugf logs a debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	if l.level <= DebugLevel {
		l.logf("DEBUG", format, args...)
	}
}

// Infof logs an info message
func (l *Logger) Infof(format string, args ...interface{}) {
	if l.level <= InfoLevel {
		l.logf("INFO", format, args...)
	}
}

// Warnf logs a warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	if l.level <= WarnLevel {
		l.logf("WARN", format, args...)
	}
}

// Errorf logs an error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	if l.level <= ErrorLevel {
		l.logf("ERROR", format, args...)
	}
}

// InfoMsg logs a simple info message
func (l *Logger) InfoMsg(msg string) {
	l.Infof("%s", msg)
}

// DebugMsg logs a simple debug message
func (l *Logger) DebugMsg(msg string) {
	l.Debugf("%s", msg)
}

// WarnMsg logs a simple warning message
func (l *Logger) WarnMsg(msg string) {
	l.Warnf("%s", msg)
}

// ErrorMsg logs a simple error message
func (l *Logger) ErrorMsg(msg string) {
	l.Errorf("%s", msg)
}

// ResourceDeploymentStart logs the start of a resource deployment
func (l *Logger) ResourceDeploymentStart(resourceType, resourceName string, current, total int) {
	l.Infof("[%d/%d] Deploying %s/%s", current, total, resourceType, resourceName)
}

// ResourceDeploymentSuccess logs successful resource deployment
func (l *Logger) ResourceDeploymentSuccess(resourceType, resourceName string) {
	l.Infof("✓ Successfully created %s/%s", resourceType, resourceName)
}

// ResourceDeploymentFailed logs failed resource deployment
func (l *Logger) ResourceDeploymentFailed(resourceType, resourceName string, err error) {
	l.Errorf("✗ Failed to create %s/%s: %v", resourceType, resourceName, err)
}

// DeploymentSummary logs the deployment summary
func (l *Logger) DeploymentSummary(successful, total int) {
	if successful == total {
		l.Infof("✓ Deployment completed successfully: %d/%d resources created", successful, total)
	} else {
		l.Warnf("⚠ Deployment completed with errors: %d/%d resources created", successful, total)
	}
}

// ErrorWithAnalysis logs an error with detailed analysis
func (l *Logger) ErrorWithAnalysis(err error) {
	// For now, just log the error normally
	// This allows the interface to be implemented without importing the error analyzer
	l.Errorf("Error: %v", err)
}

func (l *Logger) logf(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if l.prefix != "" {
		log.Printf("[%s][%s] %s", l.prefix, level, msg)
	} else {
		log.Printf("[%s] %s", level, msg)
	}
}
