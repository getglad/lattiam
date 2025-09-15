package helpers

import (
	"testing"
)

// TestLogger implements deployment.Logger for testing
type TestLogger struct {
	t *testing.T
}

// NewTestLogger creates a test logger
func NewTestLogger(t *testing.T) *TestLogger {
	t.Helper()
	return &TestLogger{t: t}
}

// Infof logs an info message
func (l *TestLogger) Infof(format string, args ...interface{}) {
	l.t.Logf("[INFO] "+format, args...)
}

// Errorf logs an error message
func (l *TestLogger) Errorf(format string, args ...interface{}) {
	l.t.Logf("[ERROR] "+format, args...)
}

// Debugf logs a debug message
func (l *TestLogger) Debugf(format string, args ...interface{}) {
	l.t.Logf("[DEBUG] "+format, args...)
}

// ResourceDeploymentStart logs the start of a resource deployment
func (l *TestLogger) ResourceDeploymentStart(resourceType, resourceName string, current, total int) {
	l.t.Logf("[DEPLOY] Starting %s/%s (%d/%d)", resourceType, resourceName, current, total)
}

// ResourceDeploymentSuccess logs successful resource deployment
func (l *TestLogger) ResourceDeploymentSuccess(resourceType, resourceName string) {
	l.t.Logf("[SUCCESS] %s/%s", resourceType, resourceName)
}

// ResourceDeploymentFailed logs failed resource deployment
func (l *TestLogger) ResourceDeploymentFailed(resourceType, resourceName string, err error) {
	l.t.Logf("[FAILED] %s/%s: %v", resourceType, resourceName, err)
}

// DeploymentSummary logs a deployment summary
func (l *TestLogger) DeploymentSummary(successful, total int) {
	l.t.Logf("[SUMMARY] %d/%d resources deployed successfully", successful, total)
}

// ErrorWithAnalysis logs an error with analysis
func (l *TestLogger) ErrorWithAnalysis(err error) {
	l.t.Logf("[ERROR ANALYSIS] %v", err)
}

// NullLogger implements deployment.Logger but discards all output
type NullLogger struct{}

// Infof discards info messages
func (l *NullLogger) Infof(_ string, _ ...interface{}) {}

// Errorf discards error messages
func (l *NullLogger) Errorf(_ string, _ ...interface{}) {}

// Debugf discards debug messages
func (l *NullLogger) Debugf(_ string, _ ...interface{}) {}

// ResourceDeploymentStart discards deployment start messages
func (l *NullLogger) ResourceDeploymentStart(_, _ string, _, _ int) {}

// ResourceDeploymentSuccess discards deployment success messages
func (l *NullLogger) ResourceDeploymentSuccess(_, _ string) {}

// ResourceDeploymentFailed discards deployment failure messages
func (l *NullLogger) ResourceDeploymentFailed(_, _ string, _ error) {}

// DeploymentSummary discards deployment summary messages
func (l *NullLogger) DeploymentSummary(_, _ int) {}

// ErrorWithAnalysis discards error analysis messages
func (l *NullLogger) ErrorWithAnalysis(_ error) {}
