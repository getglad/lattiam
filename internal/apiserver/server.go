package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/lattiam/lattiam/internal/provider"
)

type APIServer struct {
	router            chi.Router
	server            *http.Server
	deploymentService *DeploymentService
}

type DeploymentRequest struct {
	Name          string                 `json:"name"`
	TerraformJSON map[string]interface{} `json:"terraform_json"`
	Config        *DeploymentConfig      `json:"config,omitempty"`
}

type DeploymentConfig struct {
	AWSProfile   string `json:"aws_profile,omitempty"`
	AWSRegion    string `json:"aws_region,omitempty"`
	StateBackend string `json:"state_backend,omitempty"` // e.g., "s3://bucket/path/" or "gs://bucket/path/"
}

type DeploymentResponse struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Status         string          `json:"status"`
	ResourceErrors []ResourceError `json:"resource_errors,omitempty"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type PlanRequest struct {
	TerraformJSON map[string]interface{} `json:"terraform_json,omitempty"`
}

type PlanResponse struct {
	DeploymentID string      `json:"deployment_id"`
	Plan         interface{} `json:"plan"`
	CreatedAt    string      `json:"created_at"`
}

func NewAPIServer(port int) (*APIServer, error) {
	router := chi.NewRouter()

	// Middleware
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(60 * time.Second))

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Create deployment service with persistent cache
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	providerDir := filepath.Join(homeDir, ".lattiam", "providers")

	// Ensure cache directory exists
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create provider cache directory: %w", err)
	}

	deploymentService, err := NewDeploymentService(providerDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment service: %w", err)
	}

	apiServer := &APIServer{
		router:            router,
		server:            server,
		deploymentService: deploymentService,
	}

	apiServer.setupRoutes()
	return apiServer, nil
}

func (s *APIServer) setupRoutes() {
	s.router.Route("/api/v1", func(r chi.Router) {
		// Health check
		r.Get("/health", s.healthCheck)

		// Deployment endpoints
		r.Post("/deployments", s.createDeployment)
		r.Get("/deployments", s.listDeployments)
		r.Get("/deployments/{id}", s.getDeployment)
		r.Put("/deployments/{id}", s.updateDeployment)
		r.Delete("/deployments/{id}", s.deleteDeployment)
		r.Post("/deployments/{id}/plan", s.planDeployment)
	})
}

func (s *APIServer) createDeployment(w http.ResponseWriter, r *http.Request) {
	var req DeploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON in request body")
		return
	}

	if req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "missing_name", "Deployment name is required")
		return
	}

	if req.TerraformJSON == nil {
		s.writeError(w, http.StatusBadRequest, "missing_terraform_json", "Terraform JSON configuration is required")
		return
	}

	// Create deployment using the deployment service
	ctx := r.Context()
	deployment, err := s.deploymentService.CreateDeployment(ctx, req.Name, req.TerraformJSON, req.Config)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "deployment_failed", err.Error())
		return
	}

	// Return enhanced response with resource details
	response := BuildEnhancedResponse(deployment)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

func (s *APIServer) listDeployments(w http.ResponseWriter, r *http.Request) {
	deployments := s.deploymentService.ListDeployments()

	// Check if user wants detailed view
	detailed := r.URL.Query().Get("detailed") == "true"

	if detailed {
		response := make([]EnhancedDeploymentResponse, len(deployments))
		for i, d := range deployments {
			response[i] = BuildEnhancedResponse(d)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode response: %v", err)
		}
	} else {
		// Simple response for list view
		response := make([]DeploymentResponse, len(deployments))
		for i, d := range deployments {
			response[i] = DeploymentResponse{
				ID:             d.ID,
				Name:           d.Name,
				Status:         string(d.Status),
				ResourceErrors: d.ResourceErrors,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode response: %v", err)
		}
	}
}

func (s *APIServer) getDeployment(w http.ResponseWriter, r *http.Request) {
	deploymentID := chi.URLParam(r, "id")

	deployment, err := s.deploymentService.GetDeployment(deploymentID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	// Use enhanced response for detailed information
	response := BuildEnhancedResponse(deployment)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

func (s *APIServer) updateDeployment(w http.ResponseWriter, r *http.Request) {
	deploymentID := chi.URLParam(r, "id")

	// First check if deployment exists
	deployment, err := s.deploymentService.GetDeployment(deploymentID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	// Check deployment status - only allow updates for certain statuses
	switch deployment.Status {
	case StatusDestroying, StatusDestroyed:
		s.writeError(w, http.StatusConflict, "invalid_state", "Cannot update a deployment that is being or has been destroyed")
		return
	case StatusApplying, StatusPlanning:
		s.writeError(w, http.StatusConflict, "deployment_in_progress", "Cannot update a deployment that is currently in progress")
		return
	case StatusPending, StatusCompleted, StatusFailed:
		// These statuses allow updates
	}

	var req DeploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON in request body")
		return
	}

	// Update fields if provided
	if req.Name != "" {
		deployment.Name = req.Name
	}

	// Update deployment configuration if provided
	if req.Config != nil {
		deployment.Config = req.Config
	}

	// If new Terraform JSON is provided, we need to re-deploy
	if req.TerraformJSON != nil {
		// Parse the new resources
		resources, err := s.deploymentService.ParseTerraformJSON(req.TerraformJSON)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid_terraform_json", err.Error())
			return
		}

		// Update resources and reset status
		deployment.Resources = resources
		deployment.Status = StatusPending
		// Don't clear Results here - we want to preserve them in case the update fails
		// The deployment service will handle result management
		deployment.ResourceErrors = nil
		deployment.TerraformJSON = req.TerraformJSON
	}

	// Update the deployment
	ctx := r.Context()
	updatedDeployment, err := s.deploymentService.UpdateDeployment(ctx, deployment)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}

	// Return enhanced response
	response := BuildEnhancedResponse(updatedDeployment)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

func (s *APIServer) planDeployment(w http.ResponseWriter, r *http.Request) {
	deploymentID := chi.URLParam(r, "id")

	// Parse request body for plan configuration
	var req PlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty body for simple plan
		req = PlanRequest{}
	}

	// Get the deployment
	deployment, err := s.deploymentService.GetDeployment(deploymentID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	// Check deployment status - can only plan certain statuses
	switch deployment.Status {
	case StatusDestroying, StatusDestroyed:
		s.writeError(w, http.StatusConflict, "invalid_state", "Cannot plan a deployment that is being or has been destroyed")
		return
	case StatusPlanning, StatusApplying:
		s.writeError(w, http.StatusConflict, "deployment_in_progress", "Deployment is already in progress")
		return
	case StatusPending, StatusCompleted, StatusFailed:
		// These statuses allow planning
	}

	// Generate plan
	ctx := r.Context()
	plan, err := s.deploymentService.PlanDeployment(ctx, deploymentID, req.TerraformJSON)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "plan_failed", err.Error())
		return
	}

	// Return plan response
	response := PlanResponse{
		DeploymentID: deploymentID,
		Plan:         plan,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode plan response: %v", err)
	}
}

func (s *APIServer) deleteDeployment(w http.ResponseWriter, r *http.Request) {
	deploymentID := chi.URLParam(r, "id")

	ctx := r.Context()
	if err := s.deploymentService.DeleteDeployment(ctx, deploymentID); err != nil {
		s.writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *APIServer) healthCheck(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"version":   "0.1.0",
	}

	// Test LocalStack connectivity
	localstackHealth := s.checkLocalStack(r.Context())
	health["localstack"] = localstackHealth

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(health); err != nil {
		log.Printf("Failed to encode health response: %v", err)
	}
}

func (s *APIServer) checkLocalStack(ctx context.Context) map[string]interface{} {
	// Get the endpoint strategy
	strategy := provider.GetEndpointStrategy()

	// Get health check URL from strategy
	healthURL := strategy.GetHealthCheckURL()
	if healthURL == "" {
		// No health check available for this strategy
		return map[string]interface{}{"status": "not_applicable", "message": "No health check for this endpoint type"}
	}

	// Use NO_PROXY to bypass proxy restrictions
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, http.NoBody)
	if err != nil {
		return map[string]interface{}{"status": "error", "message": "failed to create request"}
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: nil, // Bypass proxy
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return map[string]interface{}{"status": "error", "message": err.Error()}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusOK {
		return map[string]interface{}{"status": "healthy"}
	}

	return map[string]interface{}{"status": "unhealthy", "code": resp.StatusCode}
}

func (s *APIServer) writeError(w http.ResponseWriter, status int, errorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(ErrorResponse{
		Error:   errorCode,
		Message: message,
	}); err != nil {
		log.Printf("Failed to encode error response: %v", err)
	}
}

func (s *APIServer) Start() error {
	log.Printf("Starting API server on %s", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}
	return nil
}

func (s *APIServer) Shutdown(ctx context.Context) error {
	log.Println("Shutting down API server...")

	// Close deployment service first to ensure provider processes are cleaned up
	if s.deploymentService != nil {
		if err := s.deploymentService.Close(); err != nil {
			log.Printf("Warning: failed to close deployment service: %v", err)
		}
	}

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}
	return nil
}
