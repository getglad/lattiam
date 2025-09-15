// Package main implements the Lattiam API server
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lattiam/lattiam/internal/apiserver"
	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/executor"
	"github.com/lattiam/lattiam/internal/state"
	"github.com/lattiam/lattiam/internal/system"
	"github.com/lattiam/lattiam/internal/utils/components"
	"github.com/lattiam/lattiam/pkg/logging"
)

// Version can be set at build time
var Version = "dev"

var logger = logging.NewLogger("api-server")

// @title           Lattiam API
// @version         1.0
// @description     REST API for infrastructure deployment using Terraform providers
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.url    https://github.com/lattiam/lattiam/issues
// @contact.email  support@lattiam.io

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      localhost:8084
// @BasePath  /api/v1

// @schemes http https

// @securityDefinitions.basic BasicAuth

// @externalDocs.description  OpenAPI
// @externalDocs.url          https://swagger.io/resources/open-api/
func main() {
	if err := run(); err != nil {
		logger.Error("Fatal error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	var port int
	var debug bool
	flag.IntVar(&port, "port", 8084, "Port to listen on")
	flag.BoolVar(&debug, "debug", false, "Enable debug mode")
	flag.Parse()

	// Set the version in config package
	config.AppVersion = Version

	// Create and configure server
	cfg, err := createServerConfig(port, debug)
	if err != nil {
		return err
	}

	// Log configuration
	logConfiguration(cfg)

	// Validate backend safety
	if err := validateBackendSafety(context.Background(), cfg); err != nil {
		return fmt.Errorf("backend validation failed: %w", err)
	}

	// Initialize components
	svcComponents, err := initializeComponents(cfg)
	if err != nil {
		return err
	}

	// Start server and handle shutdown
	return runServer(cfg, svcComponents)
}

func createServerConfig(port int, debug bool) (*config.ServerConfig, error) {
	cfg := config.NewServerConfig()
	cfg.Port = port
	cfg.Debug = debug

	// Load from environment
	if err := cfg.LoadFromEnv(); err != nil {
		return nil, fmt.Errorf("failed to load config from environment: %w", err)
	}

	// Expand paths
	if err := cfg.ExpandPaths(); err != nil {
		return nil, fmt.Errorf("failed to expand paths: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

func logConfiguration(cfg *config.ServerConfig) {
	logger.Info("Starting Lattiam API server v%s", Version)
	logger.Info("Configuration:")
	logger.Info("  Port: %d", cfg.Port)
	logger.Info("  Debug: %t", cfg.Debug)
	logger.Info("  State Store: %s", cfg.StateStore.Type)
}

func validateBackendSafety(ctx context.Context, cfg *config.ServerConfig) error {
	// Skip validation for memory backend (testing only)
	if cfg.StateStore.Type == "memory" {
		logger.Info("Skipping backend validation for memory backend")
		return nil
	}

	validator := state.NewBackendValidator(cfg)

	// Check for forced reinit flag from environment
	if os.Getenv("LATTIAM_FORCE_BACKEND_REINIT") == "true" {
		logger.Info("Forcing backend reinitialization due to LATTIAM_FORCE_BACKEND_REINIT=true")
		if err := validator.ForceReinitialize(ctx, cfg); err != nil {
			return fmt.Errorf("failed to force reinitialize backend: %w", err)
		}
		return nil
	}

	logger.Info("Validating backend configuration safety...")

	if err := state.ValidateOrInitializeBackend(ctx, cfg, validator); err != nil {
		// Check if this is a mismatch error - provide helpful guidance
		if errors.Is(err, state.ErrBackendMismatch) || errors.Is(err, state.ErrUnsafeBackendSwitch) {
			logger.Error("Backend validation failed: %v", err)
			logger.Error("")
			logger.Error("This error prevents accidental data loss from backend configuration changes.")
			logger.Error("")
			logger.Error("WARNING: State migration tools are not yet implemented.")
			logger.Error("To force reinitialization (DATA LOSS):")
			logger.Error("   LATTIAM_FORCE_BACKEND_REINIT=true lattiam server start")
			logger.Error("")
			return fmt.Errorf("backend validation failed: %w", err)
		}

		return fmt.Errorf("backend validation error: %w", err)
	}

	logger.Info("Backend validation successful")
	return nil
}

type serverComponents struct {
	stateStore         interface{}
	providerManager    interface{}
	bgComponents       *system.BackgroundSystemComponents
	server             *apiserver.APIServer
	deploymentExecutor *executor.DeploymentExecutor
}

func initializeComponents(cfg *config.ServerConfig) (*serverComponents, error) {
	// Create state store
	stateStore, err := components.CreateStateStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create state store: %w", err)
	}

	// Create provider manager
	providerManager, err := components.CreateProviderManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider manager: %w", err)
	}

	// Create deployment executor
	deploymentExecutor := executor.New(providerManager, stateStore, 10*time.Minute)
	executorFunc := executor.CreateExecutorFunc(deploymentExecutor)

	// Create background system using factory
	bgComponents, err := system.NewBackgroundSystem(cfg, executorFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to create background system: %w", err)
	}

	// Connect the executor's event bus to the tracker
	events.ConnectTrackerToEventBus(deploymentExecutor.GetEventBus(), bgComponents.Tracker)

	// Start the worker pool
	bgComponents.WorkerPool.Start()

	// Create interpolator
	interpolator, err := components.CreateInterpolator(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create interpolator: %w", err)
	}

	// Create server
	server, err := apiserver.NewAPIServerWithComponents(
		cfg, bgComponents.Queue, bgComponents.Tracker,
		bgComponents.WorkerPool, stateStore, providerManager, interpolator,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}

	return &serverComponents{
		stateStore:         stateStore,
		providerManager:    providerManager,
		bgComponents:       bgComponents,
		server:             server,
		deploymentExecutor: deploymentExecutor,
	}, nil
}

func runServer(_ *config.ServerConfig, svcComponents *serverComponents) error {
	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := svcComponents.server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	// Wait for signal or error
	select {
	case <-sigChan:
		logger.Info("Received shutdown signal")
		return gracefulShutdown(svcComponents)
	case err := <-errChan:
		// Ensure cleanup happens even on error
		shutdownErr := gracefulShutdown(svcComponents)
		if shutdownErr != nil {
			logger.Error("Shutdown error: %v", shutdownErr)
		}
		return fmt.Errorf("server failed: %w", err)
	}
}

func gracefulShutdown(svcComponents *serverComponents) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown server
	if err := svcComponents.server.Shutdown(ctx); err != nil {
		logger.Error("Failed to shutdown server: %v", err)
	}

	// Shutdown provider manager first to ensure all provider processes are terminated
	if shutdownable, ok := svcComponents.providerManager.(interface{ Shutdown() error }); ok {
		if err := shutdownable.Shutdown(); err != nil {
			logger.Error("Failed to shutdown provider manager: %v", err)
		} else {
			logger.Info("Provider manager shutdown completed")
		}
	}

	// Shutdown background components
	if err := svcComponents.bgComponents.Close(ctx); err != nil {
		logger.Error("Failed to shutdown components: %v", err)
		return fmt.Errorf("failed to close background components: %w", err)
	}

	return nil
}
