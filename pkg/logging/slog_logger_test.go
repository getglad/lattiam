package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestNewSlogLogger(t *testing.T) {
	t.Parallel()

	logger := NewSlogLogger("test-component")
	if logger == nil {
		t.Fatal("Expected non-nil logger")
	}
	if logger.component != "test-component" {
		t.Errorf("Expected component 'test-component', got %s", logger.component)
	}
	if logger.logger == nil {
		t.Fatal("Expected non-nil slog.Logger")
	}
}

//nolint:funlen // Comprehensive slog logger methods test covering all log levels
func TestSlogLoggerMethods(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		logFunc  func(*SlogLogger)
		expected string
	}{
		{
			name: "Debug",
			logFunc: func(logger *SlogLogger) {
				logger.Debug("debug message %s", "test")
			},
			expected: "DEBUG",
		},
		{
			name: "Info",
			logFunc: func(logger *SlogLogger) {
				logger.Info("info message %s", "test")
			},
			expected: "INFO",
		},
		{
			name: "Warn",
			logFunc: func(logger *SlogLogger) {
				logger.Warn("warn message %s", "test")
			},
			expected: "WARN",
		},
		{
			name: "Error",
			logFunc: func(logger *SlogLogger) {
				logger.Error("error message %s", "test")
			},
			expected: "ERROR",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Create separate buffer and logger for each subtest
			var buf bytes.Buffer
			handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})

			logger := &SlogLogger{
				logger:    slog.New(handler),
				component: "test",
			}

			tc.logFunc(logger)

			output := buf.String()
			if !strings.Contains(output, tc.expected) {
				t.Errorf("Expected log output to contain %s, got: %s", tc.expected, output)
			}
			if !strings.Contains(output, "component=test") {
				t.Errorf("Expected log output to contain component=test, got: %s", output)
			}
		})
	}
}

func TestSlogLoggerWithContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	logger := &SlogLogger{
		logger:    slog.New(handler),
		component: "test",
	}

	// Use typed context key for correlation ID
	ctx := context.WithValue(context.Background(), CorrelationIDKey, "test-123")
	contextLogger := logger.WithContext(ctx)

	contextLogger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "correlation_id=test-123") {
		t.Errorf("Expected log output to contain correlation_id=test-123, got: %s", output)
	}
}

func TestSlogLoggerWithCorrelation(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	logger := &SlogLogger{
		logger:    slog.New(handler),
		component: "test",
	}

	corrLogger := logger.WithCorrelation("corr-456")
	corrLogger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "correlation_id=corr-456") {
		t.Errorf("Expected log output to contain correlation_id=corr-456, got: %s", output)
	}
}

func TestSlogLoggerWithFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	logger := &SlogLogger{
		logger:    slog.New(handler),
		component: "test",
	}

	fields := map[string]interface{}{
		"user_id": "123",
		"action":  "create",
	}

	fieldLogger := logger.WithFields(fields)
	fieldLogger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "user_id=123") {
		t.Errorf("Expected log output to contain user_id=123, got: %s", output)
	}
	if !strings.Contains(output, "action=create") {
		t.Errorf("Expected log output to contain action=create, got: %s", output)
	}
}

func TestSlogLoggerLevelChecks(t *testing.T) {
	t.Parallel()

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	logger := &SlogLogger{
		logger:    slog.New(handler),
		component: "test",
	}

	if logger.IsDebugEnabled() {
		t.Error("Expected debug to be disabled with INFO level")
	}
	if logger.IsTraceEnabled() {
		t.Error("Expected trace to be disabled with INFO level")
	}
}

//nolint:funlen // Comprehensive slog logger operations test with context and structured logging
func TestSlogLoggerOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("Operation", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})

		logger := &SlogLogger{
			logger:    slog.New(handler),
			component: "test",
		}

		details := map[string]interface{}{
			"resource": "test-resource",
			"count":    5,
		}
		logger.Operation(ctx, "deploy", details)

		output := buf.String()
		if !strings.Contains(output, "operation=deploy") {
			t.Errorf("Expected log output to contain operation=deploy, got: %s", output)
		}
		if !strings.Contains(output, "resource=test-resource") {
			t.Errorf("Expected log output to contain resource=test-resource, got: %s", output)
		}
	})

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})

		logger := &SlogLogger{
			logger:    slog.New(handler),
			component: "test",
		}

		logger.Success(ctx, "deploy", "all good")

		output := buf.String()
		if !strings.Contains(output, "operation=deploy") {
			t.Errorf("Expected log output to contain operation=deploy, got: %s", output)
		}
		if !strings.Contains(output, "status=success") {
			t.Errorf("Expected log output to contain status=success, got: %s", output)
		}
	})

	t.Run("Failure", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})

		logger := &SlogLogger{
			logger:    slog.New(handler),
			component: "test",
		}

		err := errors.New("test error")
		logger.Failure(ctx, "deploy", err)

		output := buf.String()
		if !strings.Contains(output, "operation=deploy") {
			t.Errorf("Expected log output to contain operation=deploy, got: %s", output)
		}
		if !strings.Contains(output, "status=failed") {
			t.Errorf("Expected log output to contain status=failed, got: %s", output)
		}
		if !strings.Contains(output, "test error") {
			t.Errorf("Expected log output to contain error message, got: %s", output)
		}
	})
}

//nolint:funlen // Comprehensive resource deployment logging test
func TestSlogLoggerResourceDeployment(t *testing.T) {
	t.Parallel()

	t.Run("ResourceDeploymentStart", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})

		logger := &SlogLogger{
			logger:    slog.New(handler),
			component: "test",
		}

		logger.ResourceDeploymentStart("aws_instance", "web-server", 1, 3)

		output := buf.String()
		if !strings.Contains(output, "resource_type=aws_instance") {
			t.Errorf("Expected log output to contain resource_type=aws_instance, got: %s", output)
		}
		if !strings.Contains(output, "resource_name=web-server") {
			t.Errorf("Expected log output to contain resource_name=web-server, got: %s", output)
		}
		if !strings.Contains(output, "current=1") {
			t.Errorf("Expected log output to contain current=1, got: %s", output)
		}
	})

	t.Run("ResourceDeploymentSuccess", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})

		logger := &SlogLogger{
			logger:    slog.New(handler),
			component: "test",
		}

		logger.ResourceDeploymentSuccess("aws_instance", "web-server")

		output := buf.String()
		if !strings.Contains(output, "resource_type=aws_instance") {
			t.Errorf("Expected log output to contain resource_type=aws_instance, got: %s", output)
		}
		if !strings.Contains(output, "status=success") {
			t.Errorf("Expected log output to contain status=success, got: %s", output)
		}
	})

	t.Run("ResourceDeploymentFailed", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})

		logger := &SlogLogger{
			logger:    slog.New(handler),
			component: "test",
		}

		err := errors.New("deployment failed")
		logger.ResourceDeploymentFailed("aws_instance", "web-server", err)

		output := buf.String()
		if !strings.Contains(output, "resource_type=aws_instance") {
			t.Errorf("Expected log output to contain resource_type=aws_instance, got: %s", output)
		}
		if !strings.Contains(output, "status=failed") {
			t.Errorf("Expected log output to contain status=failed, got: %s", output)
		}
		if !strings.Contains(output, "deployment failed") {
			t.Errorf("Expected log output to contain error message, got: %s", output)
		}
	})
}

func TestSlogLoggerEnvironmentConfig(t *testing.T) {
	// Cannot use t.Parallel() when using t.Setenv

	// Test JSON format
	t.Run("JSONFormat", func(t *testing.T) {
		t.Setenv("LATTIAM_LOG_FORMAT", "JSON")

		handler := createHandler()
		// Just verify it doesn't panic and returns a handler
		if handler == nil {
			t.Error("Expected non-nil handler for JSON format")
		}
	})

	// Test different log levels
	t.Run("LogLevels", func(t *testing.T) {
		testCases := []struct {
			env      string
			expected slog.Level
		}{
			{"DEBUG", slog.LevelDebug},
			{"INFO", slog.LevelInfo},
			{"WARN", slog.LevelWarn},
			{"ERROR", slog.LevelError},
			{"INVALID", slog.LevelInfo}, // Default
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.env, func(t *testing.T) {
				t.Setenv("LATTIAM_LOG_LEVEL", tc.env)

				level := getLogLevelSlog()
				if level != tc.expected {
					t.Errorf("Expected level %v for env %s, got %v", tc.expected, tc.env, level)
				}
			})
		}
	})
}
