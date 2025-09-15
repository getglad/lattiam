// Package apiserver provides HTTP API endpoints and server functionality for Lattiam
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	"github.com/lattiam/lattiam/internal/apiserver/handlers"
	customMiddleware "github.com/lattiam/lattiam/internal/apiserver/middleware"
	"github.com/lattiam/lattiam/internal/apiserver/types"
	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/deployment"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/logging"
)

// APIServer provides HTTP API endpoints for deployment management
type APIServer struct {
	router            chi.Router
	server            *http.Server
	queue             interfaces.DeploymentQueue
	tracker           interfaces.DeploymentTracker
	workerPool        interfaces.WorkerPool
	stateStore        interfaces.StateStore
	deploymentService interfaces.DeploymentService
	requestConverter  *types.RequestConverter
	config            *config.ServerConfig
	logger            *logging.Logger
}

// DeploymentRequest represents a request to create or update a deployment
type DeploymentRequest struct {
	Name          string                 `json:"name"`
	TerraformJSON map[string]interface{} `json:"terraform_json"`
	Config        *DeploymentConfig      `json:"config,omitempty"`
}

// DeploymentConfig contains configuration overrides for deployments
type DeploymentConfig struct {
	AWSProfile   string `json:"aws_profile,omitempty"`
	AWSRegion    string `json:"aws_region,omitempty"`
	StateBackend string `json:"state_backend,omitempty"` // e.g., "s3://bucket/path/" or "gs://bucket/path/"
}

// DeploymentResponse represents the API response for deployment operations
type DeploymentResponse struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Status         string                `json:"status"`
	ResourceErrors []types.ResourceError `json:"resource_errors,omitempty"`
}

// Validation helpers
var (
	// validNamePattern allows alphanumeric, hyphens, underscores, dots
	// Must start and end with alphanumeric
	// validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]$`) // unused after handler refactor
	// validIDPattern for UUIDs or similar identifiers
	validIDPattern = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
)

// validateDeploymentID validates deployment IDs
func validateDeploymentID(id string) error {
	if id == "" {
		return fmt.Errorf("deployment ID is required")
	}
	if len(id) > 100 {
		return fmt.Errorf("deployment ID must be less than 100 characters")
	}
	if !validIDPattern.MatchString(id) {
		return fmt.Errorf("deployment ID contains invalid characters")
	}
	return nil
}

// NewAPIServerWithComponents creates a new API server with individual components
//
//nolint:funlen // Constructor function with many dependency injections - complexity is necessary
func NewAPIServerWithComponents(
	cfg *config.ServerConfig,
	queue interfaces.DeploymentQueue,
	tracker interfaces.DeploymentTracker,
	workerPool interfaces.WorkerPool,
	stateStore interfaces.StateStore,
	providerManager interfaces.ProviderLifecycleManager,
	interpolator interfaces.InterpolationResolver,
) (*APIServer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration is required")
	}
	if queue == nil {
		return nil, fmt.Errorf("deployment queue is required")
	}
	if tracker == nil {
		return nil, fmt.Errorf("deployment tracker is required")
	}
	if workerPool == nil {
		return nil, fmt.Errorf("worker pool is required")
	}
	if stateStore == nil {
		return nil, fmt.Errorf("state store is required")
	}
	// providerManager and interpolator can be nil for backward compatibility
	// but are required for full update functionality

	// Create deployment service with full configuration for update support
	deploymentServiceConfig := deployment.ServiceConfig{
		Queue:           queue,
		Tracker:         tracker,
		StateStore:      stateStore,
		ProviderManager: providerManager,
		Interpolator:    interpolator,
	}

	deploymentService, err := deployment.NewServiceWithConfig(deploymentServiceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment service: %w", err)
	}

	router := chi.NewRouter()

	// Middleware
	router.Use(middleware.RequestID) // Generate unique request ID for tracing
	router.Use(middleware.RealIP)    // Get real client IP for logging
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.StripSlashes) // Remove trailing slashes for consistent routing
	router.Use(middleware.Timeout(60 * time.Second))

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Create API server with components
	apiServer := &APIServer{
		router:            router,
		server:            server,
		queue:             queue,
		tracker:           tracker,
		workerPool:        workerPool,
		stateStore:        stateStore,
		deploymentService: deploymentService,
		requestConverter:  types.NewRequestConverterWithDefaults(),
		config:            cfg,
		logger:            logging.NewLogger("apiserver"),
	}

	// Setup routes with config
	if err := apiServer.setupRoutesWithConfig(); err != nil {
		return nil, err
	}

	// Add global 404 handler that returns JSON instead of HTML
	// Set after routes to ensure it's the fallback
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		apiServer.writeError(w, http.StatusNotFound, "not_found", "The requested endpoint was not found")
	})

	return apiServer, nil
}

// GetDeploymentService returns the deployment service instance
func (s *APIServer) GetDeploymentService() interfaces.DeploymentService {
	return s.deploymentService
}

// setupRoutesWithConfig sets up routes including operational endpoints when config is available
func (s *APIServer) setupRoutesWithConfig() error {
	// Import the handlers package at the top of the file - need to add import
	deploymentHandler, err := s.createDeploymentHandler()
	if err != nil {
		return fmt.Errorf("failed to create deployment handler: %w", err)
	}

	s.router.Route("/api/v1", func(r chi.Router) {
		// Set 404 handler for this subrouter
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			s.writeError(w, http.StatusNotFound, "not_found", "The requested endpoint was not found")
		})

		// Apply content type validation to all endpoints
		r.Use(customMiddleware.ContentTypeValidator())

		// Deployment endpoints using handlers with validation
		r.Route("/deployments", func(r chi.Router) {
			// Endpoints that create/update deployments need additional validation
			r.With(customMiddleware.NameValidator(), customMiddleware.TerraformJSONValidator()).
				Post("/", deploymentHandler.CreateDeployment)

			r.Get("/", deploymentHandler.ListDeployments)

			r.Route("/{id}", func(r chi.Router) {
				// Apply ID validation to all endpoints with {id} parameter
				r.Use(customMiddleware.IDValidator("id"))

				r.Get("/", deploymentHandler.GetDeployment)
				r.With(customMiddleware.TerraformJSONValidator()).
					Put("/", deploymentHandler.UpdateDeployment)
				r.Delete("/", deploymentHandler.DeleteDeployment)

				// Worker system endpoints
				r.Post("/cancel", s.cancelDeployment)
			})
		})

		// Queue and system endpoints (no special validation needed)
		r.Get("/queue/metrics", s.getQueueMetrics)
		r.Get("/system/health", s.getSystemHealth)

		// Add operational endpoints if config is available
		if s.config != nil {
			// Create operations handler
			opsHandler := handlers.NewOperationsHandler(s.config, s.stateStore, s.workerPool, s.queue)

			// Register operations routes
			opsHandler.RegisterRoutes(r)
		}
	})

	// Add Swagger UI endpoint
	s.router.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("http://localhost:8084/swagger/doc.json"),
	))

	return nil
}

// createDeploymentHandler creates a new deployment handler with the required dependencies
func (s *APIServer) createDeploymentHandler() (*handlers.DeploymentHandler, error) {
	handler, err := handlers.NewDeploymentHandler(s.deploymentService, s.requestConverter)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment handler: %w", err)
	}
	return handler, nil
}

// writeError writes a structured error response
func (s *APIServer) writeError(w http.ResponseWriter, status int, code string, message string) {
	response := map[string]string{
		"error":   code,
		"message": message,
	}

	jsonData, err := json.Marshal(response)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal server error: failed to encode error response"))
		s.logger.Errorf("Failed to encode error response: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if _, err := w.Write(jsonData); err != nil {
		s.logger.Errorf("Failed to write error response: %v", err)
	}
}

// cancelDeployment cancels a queued deployment
// @Summary Cancel deployment
// @Description Cancel a deployment that is currently queued or processing
// @Tags deployments
// @Accept json
// @Produce json
// @Param id path string true "Deployment ID"
// @Success 200 {object} map[string]string "Cancellation successful"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 501 {object} map[string]interface{} "Not implemented"
// @Router /deployments/{id}/cancel [post]
func (s *APIServer) cancelDeployment(w http.ResponseWriter, r *http.Request) {
	deploymentID := chi.URLParam(r, "id")

	// Validate deployment ID
	if err := validateDeploymentID(deploymentID); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}

	if s.queue == nil {
		s.writeError(w, http.StatusNotImplemented, "not_implemented", "Queue not configured")
		return
	}

	ctx := r.Context()
	if err := s.queue.Cancel(ctx, deploymentID); err != nil {
		s.writeError(w, http.StatusBadRequest, "cancel_failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"id":     deploymentID,
		"status": "canceled",
	}); err != nil {
		s.logger.Errorf("Failed to encode response: %v", err)
	}
}

// getQueueMetrics returns queue metrics
// @Summary Get queue metrics
// @Description Get metrics about the deployment queue
// @Tags system
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "Queue metrics"
// @Router /queue/metrics [get]
func (s *APIServer) getQueueMetrics(w http.ResponseWriter, _ *http.Request) {
	metrics := s.deploymentService.GetQueueMetrics()

	response := map[string]interface{}{
		"total_enqueued":    metrics.TotalEnqueued,
		"total_dequeued":    metrics.TotalDequeued,
		"current_depth":     metrics.CurrentDepth,
		"average_wait_time": metrics.AverageWaitTime.String(),
		"oldest_deployment": metrics.OldestDeployment.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorf("Failed to encode response: %v", err)
	}
}

// componentHealth represents the health status of a component
// componentHealth represents the health status of a system component
type componentHealth struct {
	Status  string
	Details map[string]interface{}
	Healthy bool
}

// getSystemHealth returns system health status
// @Summary Health check
// @Description Check if the API server is running and healthy
// @Tags system
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "Service is healthy"
// @Success 503 {object} map[string]interface{} "Service unhealthy"
// @Router /system/health [get]
func (s *APIServer) getSystemHealth(w http.ResponseWriter, _ *http.Request) {
	// Check health of all components
	queueHealth := s.checkQueueHealth()
	trackerHealth := s.checkTrackerHealth()
	workerPoolHealth := s.checkWorkerPoolHealth()
	stateStoreHealth := s.checkStateStoreHealth()

	// Determine overall health
	overallHealthy := queueHealth.Healthy && trackerHealth.Healthy &&
		workerPoolHealth.Healthy && stateStoreHealth.Healthy

	// Build component details
	componentDetails := map[string]interface{}{
		"queue":      queueHealth.Details,
		"tracker":    trackerHealth.Details,
		"workerPool": workerPoolHealth.Details,
		"stateStore": stateStoreHealth.Details,
	}

	// Get system metrics
	systemMetrics := s.getSystemMetrics()

	// Build and send response
	s.sendHealthResponse(w, overallHealthy, componentDetails, systemMetrics)
}

// checkQueueHealth checks the health of the queue component
func (s *APIServer) checkQueueHealth() componentHealth {
	if s.queue == nil {
		return componentHealth{
			Status: "unhealthy",
			Details: map[string]interface{}{
				"status":  "unhealthy",
				"message": "Queue not initialized",
			},
			Healthy: false,
		}
	}

	metrics := s.queue.GetMetrics()
	details := map[string]interface{}{
		"status":   "healthy",
		"depth":    metrics.CurrentDepth,
		"enqueued": metrics.TotalEnqueued,
		"dequeued": metrics.TotalDequeued,
	}

	// Check if queue depth is too high
	healthy := true
	if metrics.CurrentDepth > 1000 {
		details["status"] = "warning"
		details["message"] = "Queue depth is high"
		healthy = false
	}

	return componentHealth{
		Status:  details["status"].(string),
		Details: details,
		Healthy: healthy,
	}
}

// checkTrackerHealth checks the health of the tracker component
func (s *APIServer) checkTrackerHealth() componentHealth {
	if s.tracker == nil {
		return componentHealth{
			Status: "unhealthy",
			Details: map[string]interface{}{
				"status":  "unhealthy",
				"message": "Tracker not initialized",
			},
			Healthy: false,
		}
	}

	// Try to list recent deployments to verify tracker is working
	deployments, err := s.tracker.List(interfaces.DeploymentFilter{
		CreatedAfter: time.Now().Add(-1 * time.Minute),
	})
	if err != nil {
		return componentHealth{
			Status: "unhealthy",
			Details: map[string]interface{}{
				"status":  "unhealthy",
				"message": fmt.Sprintf("Failed to query tracker: %v", err),
			},
			Healthy: false,
		}
	}

	return componentHealth{
		Status: "healthy",
		Details: map[string]interface{}{
			"status":             "healthy",
			"recent_deployments": len(deployments),
		},
		Healthy: true,
	}
}

// checkWorkerPoolHealth checks the health of the worker pool
func (s *APIServer) checkWorkerPoolHealth() componentHealth {
	if s.workerPool == nil {
		return componentHealth{
			Status: "unhealthy",
			Details: map[string]interface{}{
				"status":  "unhealthy",
				"message": "Worker pool not initialized",
			},
			Healthy: false,
		}
	}

	// Worker pool is assumed healthy if it exists
	// Could be enhanced if worker pool exposes metrics
	return componentHealth{
		Status: "healthy",
		Details: map[string]interface{}{
			"status": "healthy",
		},
		Healthy: true,
	}
}

// checkStateStoreHealth checks the health of the state store
func (s *APIServer) checkStateStoreHealth() componentHealth {
	if s.stateStore == nil {
		return componentHealth{
			Status: "unhealthy",
			Details: map[string]interface{}{
				"status":  "unhealthy",
				"message": "State store not initialized",
			},
			Healthy: false,
		}
	}

	// Try a simple operation to verify connectivity
	err := s.stateStore.Ping(context.Background())
	if err != nil {
		return componentHealth{
			Status: "unhealthy",
			Details: map[string]interface{}{
				"status":  "unhealthy",
				"message": fmt.Sprintf("State store connectivity issue: %v", err),
			},
			Healthy: false,
		}
	}

	return componentHealth{
		Status: "healthy",
		Details: map[string]interface{}{
			"status": "healthy",
		},
		Healthy: true,
	}
}

// getSystemMetrics returns current system metrics
func (s *APIServer) getSystemMetrics() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"goroutines": runtime.NumGoroutine(),
		"memory": map[string]interface{}{
			"alloc_mb": m.Alloc / 1024 / 1024,
			"sys_mb":   m.Sys / 1024 / 1024,
			"gc_count": m.NumGC,
		},
	}
}

// sendHealthResponse sends the health check response
func (s *APIServer) sendHealthResponse(w http.ResponseWriter, healthy bool, components, system map[string]interface{}) {
	status := "healthy"
	if !healthy {
		status = "degraded"
	}

	response := map[string]interface{}{
		"status":     status,
		"time":       time.Now().Format(time.RFC3339),
		"components": components,
		"system":     system,
		"version": map[string]interface{}{
			"api": "v1",
		},
	}

	statusCode := http.StatusOK
	if !healthy {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorf("Failed to encode response: %v", err)
	}
}

// Start starts the API server
func (s *APIServer) Start() error {
	s.logger.Infof("Starting API server on %s", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}
	return nil
}

// Router returns the HTTP router for testing
func (s *APIServer) Router() http.Handler {
	return s.router
}

// Shutdown gracefully shuts down the API server
func (s *APIServer) Shutdown(ctx context.Context) error {
	s.logger.Infof("Shutting down API server...")

	// Stop worker pool if present
	if s.workerPool != nil {
		if err := s.workerPool.Stop(ctx); err != nil {
			s.logger.Warnf("Warning: failed to stop worker pool: %v", err)
		}
	}

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}
	return nil
}
