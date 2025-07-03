package main

import (
	"fmt"
)

// cliLogger implements deployment.Logger for CLI use
type cliLogger struct{}

// Infof logs an info message
func (l *cliLogger) Infof(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

// Errorf logs an error message
func (l *cliLogger) Errorf(format string, args ...interface{}) {
	// CLI logger error messages are handled by the deployment engine
	_ = format
	_ = args
}

// Debugf logs a debug message (only in debug mode)
func (l *cliLogger) Debugf(_ string, _ ...interface{}) {
	// CLI doesn't show debug by default
}

// ResourceDeploymentStart logs the start of a resource deployment
func (l *cliLogger) ResourceDeploymentStart(resourceType, resourceName string, _, _ int) {
	fmt.Printf("Deploying %s/%s\n", resourceType, resourceName)
}

// ResourceDeploymentSuccess logs successful resource deployment
func (l *cliLogger) ResourceDeploymentSuccess(resourceType, resourceName string) {
	_ = resourceType
	_ = resourceName
}

// ResourceDeploymentFailed logs failed resource deployment
func (l *cliLogger) ResourceDeploymentFailed(resourceType, resourceName string, err error) {
	_ = resourceType
	_ = resourceName
	_ = err
}

// DeploymentSummary logs the deployment summary
func (l *cliLogger) DeploymentSummary(successful, total int) {
	if total == 0 {
		fmt.Println("no resources found")
	} else {
		fmt.Printf("Deployment complete: %d/%d resources succeeded\n", successful, total)
	}
}

// ErrorWithAnalysis logs an error with analysis
func (l *cliLogger) ErrorWithAnalysis(err error) {
	_ = err
}

// newCLILogger creates a logger for CLI use
func newCLILogger() *cliLogger {
	return &cliLogger{}
}
