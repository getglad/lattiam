// Package handlers provides HTTP request handlers for the API server.
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/lattiam/lattiam/internal/apiserver/types"
	"github.com/lattiam/lattiam/internal/deployment"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/logging"
)

// Package-level logger for global functions
var logger = logging.NewLogger("deployment-handler")

// ErrorResponse represents a structured error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// writeJSON safely writes JSON response with proper error handling
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encoding_error", "Failed to encode response")
		logger.Errorf("JSON encoding error: %v, data: %+v", err, data)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if _, err := w.Write(jsonData); err != nil {
		logger.Errorf("Failed to write response body: %v", err)
	}
}

// writeError writes a structured error response
func writeError(w http.ResponseWriter, status int, code string, message string) {
	response := ErrorResponse{
		Error:   code,
		Message: message,
	}

	jsonData, err := json.Marshal(response)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal server error: failed to encode error response"))
		logger.Errorf("Failed to encode error response: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if _, err := w.Write(jsonData); err != nil {
		logger.Errorf("Failed to write error response: %v", err)
	}
}

// Error constants for deployment operations
var (
	ErrMissingName              = errors.New("deployment name is required")
	ErrMissingTerraformJSON     = errors.New("terraform JSON configuration is required")
	ErrDeploymentBeingDestroyed = errors.New("cannot update a deployment that is being or has been destroyed")
	ErrDeploymentInProgress     = errors.New("cannot update a deployment that is currently in progress")
	ErrInvalidDeploymentStatus  = errors.New("invalid deployment status")
)

// DeploymentHandler handles deployment-related HTTP requests
type DeploymentHandler struct {
	deploymentService interfaces.DeploymentService
	requestConverter  *types.RequestConverter
	logger            *logging.Logger
}

// NewDeploymentHandler creates a new deployment handler
func NewDeploymentHandler(
	deploymentService interfaces.DeploymentService,
	converter *types.RequestConverter,
) (*DeploymentHandler, error) {
	if deploymentService == nil {
		return nil, errors.New("deployment service is required")
	}
	return &DeploymentHandler{
		deploymentService: deploymentService,
		requestConverter:  converter,
		logger:            logging.NewLogger("deployment-handler"),
	}, nil
}

// CreateDeployment creates a new deployment
// @Summary Create new deployment
// @Description Submit a new infrastructure deployment using Terraform JSON
// @Tags deployments
// @Accept json
// @Produce json
// @Param deployment body types.DeploymentRequest true "Deployment configuration"
// @Success 201 {object} map[string]interface{} "Deployment created successfully"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /deployments [post]
func (h *DeploymentHandler) CreateDeployment(w http.ResponseWriter, r *http.Request) {
	var req types.DeploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON in request body")
		return
	}

	if err := h.validateDeploymentRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	// Parse Terraform JSON
	resources, dataSources, err := h.requestConverter.ParseTerraformJSON(req.TerraformJSON)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_terraform_json", err.Error())
		return
	}

	// Convert to deployment request
	deployRequest := h.requestConverter.ToDeploymentRequest(&req, resources, dataSources)

	// Add request ID to metadata for tracing
	requestID := middleware.GetReqID(r.Context())
	if deployRequest.Metadata == nil {
		deployRequest.Metadata = make(map[string]interface{})
	}
	deployRequest.Metadata[interfaces.MetadataKeyRequestID] = requestID

	// Submit to deployment service
	queuedDeployment, err := h.deploymentService.CreateDeployment(deployRequest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "deployment_failed", err.Error())
		return
	}

	// Build and return response
	response := h.buildQueuedDeploymentResponse(queuedDeployment)

	// Add queue information
	metrics := h.deploymentService.GetQueueMetrics()
	response["queue_info"] = map[string]interface{}{
		"queue_depth":       metrics.CurrentDepth,
		"average_wait_time": metrics.AverageWaitTime.Seconds(),
	}

	writeJSON(w, http.StatusCreated, response)
}

// validateDeploymentRequest validates the deployment request
func (h *DeploymentHandler) validateDeploymentRequest(req *types.DeploymentRequest) error {
	if req.Name == "" {
		return ErrMissingName
	}
	if req.TerraformJSON == nil {
		return ErrMissingTerraformJSON
	}
	return nil
}

// GetDeployment retrieves a deployment by ID
// @Summary Get deployment details
// @Description Retrieve detailed information about a specific deployment
// @Tags deployments
// @Accept json
// @Produce json
// @Param id path string true "Deployment ID"
// @Success 200 {object} map[string]interface{} "Deployment details"
// @Failure 404 {object} map[string]interface{} "Deployment not found"
// @Router /deployments/{id} [get]
func (h *DeploymentHandler) GetDeployment(w http.ResponseWriter, r *http.Request) {
	deploymentID := chi.URLParam(r, "id")

	// Get deployment from service
	dep, err := h.deploymentService.GetDeploymentByID(deploymentID)
	if err != nil {
		// Check if it's a custom deployment error
		if depErr, ok := deployment.IsDeploymentError(err); ok {
			writeError(w, depErr.HTTPStatus, depErr.Code, depErr.Message)
		} else {
			// Default to not found for backwards compatibility
			writeError(w, http.StatusNotFound, "not_found", "deployment not found")
		}
		return
	}

	// Always use detailed response to show resource status
	response := h.buildQueuedDeploymentResponseWithState(dep)
	writeJSON(w, http.StatusOK, response)
}

// ListDeployments retrieves all deployments
// @Summary List all deployments
// @Description Retrieve a list of all deployments with their current status
// @Tags deployments
// @Accept json
// @Produce json
// @Success 200 {array} map[string]interface{} "List of deployments"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /deployments [get]
func (h *DeploymentHandler) ListDeployments(w http.ResponseWriter, _ *http.Request) {
	// Use deployment service
	filter := interfaces.DeploymentFilter{}
	deployments, err := h.deploymentService.ListDeployments(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}

	// Convert to response format
	response := make([]map[string]interface{}, len(deployments))
	for i, qd := range deployments {
		// Use detailed response for completed/failed deployments
		if qd.Status == interfaces.DeploymentStatusCompleted || qd.Status == interfaces.DeploymentStatusFailed {
			response[i] = h.buildQueuedDeploymentResponseWithState(qd)
		} else {
			response[i] = h.buildQueuedDeploymentResponse(qd)
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// UpdateDeployment updates an existing deployment
// @Summary Update deployment
// @Description Update an existing deployment with new Terraform JSON configuration
// @Tags deployments
// @Accept json
// @Produce json
// @Param id path string true "Deployment ID"
// @Param force_update query bool false "Force update even with dangerous changes"
// @Param auto_approve query bool false "Automatically approve the update"
// @Param deployment body types.DeploymentRequest true "Updated deployment configuration"
// @Success 200 {object} map[string]interface{} "Update successful"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 404 {object} map[string]interface{} "Deployment not found"
// @Failure 409 {object} map[string]interface{} "Conflict - dangerous changes detected"
// @Router /deployments/{id} [put]
func (h *DeploymentHandler) UpdateDeployment(w http.ResponseWriter, r *http.Request) { //nolint:funlen // API handler with comprehensive validation and processing
	deploymentID := chi.URLParam(r, "id")

	// Check if deployment exists FIRST (REST best practice: resource existence before validation)
	status, err := h.deploymentService.GetDeploymentStatus(deploymentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "deployment not found")
		return
	}

	var req types.DeploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON in request body")
		return
	}

	// Validate request
	if err := h.validateDeploymentRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	// Validate deployment can be updated
	if err := h.validateDeploymentForUpdate(*status); err != nil {
		writeError(w, http.StatusConflict, "invalid_state", err.Error())
		return
	}

	// Generate plan first (safety-first approach)
	plan, err := h.deploymentService.GenerateUpdatePlan(deploymentID, req.TerraformJSON)
	if err != nil {
		writeError(w, http.StatusBadRequest, "plan_failed",
			"Failed to generate update plan: "+err.Error())
		return
	}

	// Check for dangerous changes (never force by default)
	forceUpdate := false // Default to safe
	autoApprove := false // Default to manual approval
	updateReason := ""

	// Parse additional options from query parameters or headers for safety
	if r.URL.Query().Get("force_update") == "true" {
		forceUpdate = true
	}
	if r.URL.Query().Get("auto_approve") == "true" {
		autoApprove = true
	}
	if reason := r.Header.Get("X-Update-Reason"); reason != "" {
		updateReason = reason
	}

	if len(plan.DangerousChanges) > 0 && !forceUpdate {
		response := map[string]interface{}{
			"error":             "dangerous_changes_detected",
			"message":           "Update contains potentially dangerous changes that could cause data loss or downtime",
			"plan":              plan,
			"dangerous_changes": plan.DangerousChanges,
			"mitigation":        "Review the changes carefully and add '?force_update=true' to proceed",
		}
		writeJSON(w, http.StatusConflict, response)
		return
	}

	// Create update request
	updateReq := &interfaces.UpdateRequest{
		DeploymentID:     deploymentID,
		RequestID:        middleware.GetReqID(r.Context()), // Add correlation ID
		NewTerraformJSON: req.TerraformJSON,
		PlanFirst:        true, // Always true for safety
		AutoApprove:      autoApprove,
		ForceUpdate:      forceUpdate,
		UpdatedBy:        r.Header.Get("X-User-ID"), // Get from auth header
		UpdateReason:     updateReason,
	}

	// Submit update to deployment service
	queuedUpdate, err := h.deploymentService.UpdateDeployment(deploymentID, updateReq)
	if err != nil {
		// Check if it's a custom deployment error
		if depErr, ok := deployment.IsDeploymentError(err); ok {
			writeError(w, depErr.HTTPStatus, depErr.Code, depErr.Message)
		} else {
			writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		}
		return
	}

	// Build response
	response := h.buildQueuedUpdateResponse(queuedUpdate, plan)

	// Return 200 OK for updates (not 202 Accepted) for API compatibility
	// Updates are processed synchronously in the current implementation
	writeJSON(w, http.StatusOK, response)
}

// DeleteDeployment deletes a deployment and its resources
// @Summary Delete deployment
// @Description Delete a deployment and destroy its resources
// @Tags deployments
// @Accept json
// @Produce json
// @Param id path string true "Deployment ID"
// @Success 202 {object} map[string]interface{} "Destruction initiated"
// @Success 204 "Deployment deleted (no resources to destroy)"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 404 {object} map[string]interface{} "Deployment not found"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /deployments/{id} [delete]
func (h *DeploymentHandler) DeleteDeployment(w http.ResponseWriter, r *http.Request) { //nolint:funlen // API handler with comprehensive validation and processing
	deploymentID := chi.URLParam(r, "id")

	// Get deployment status to determine delete behavior
	status, err := h.deploymentService.GetDeploymentStatus(deploymentID)
	if err != nil {
		// Check for "not found" in the error message to handle different error formats
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not_found", "deployment not found")
		} else {
			writeError(w, http.StatusInternalServerError, "status_check_failed", err.Error())
		}
		return
	}

	// Determine action based on deployment status
	switch *status {
	case interfaces.DeploymentStatusCompleted, interfaces.DeploymentStatusFailed:
		// Check if deployment has resources that need destruction
		needsDestruction, err := h.deploymentService.DeploymentNeedsDestruction(deploymentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "resource_check_failed", err.Error())
			return
		}

		if needsDestruction {
			// Delete will queue destruction job
			if err := h.deploymentService.DeleteDeployment(deploymentID); err != nil {
				writeError(w, http.StatusInternalServerError, "destruction_queue_failed", err.Error())
				return
			}
			// Return 202 Accepted to indicate destruction is in progress
			writeJSON(w, http.StatusAccepted, map[string]interface{}{
				"message": "Infrastructure destruction initiated, resources will be deleted",
				"status":  "destroying",
				"id":      deploymentID,
			})
		} else {
			// No resources to destroy, just delete
			if err := h.deploymentService.DeleteDeployment(deploymentID); err != nil {
				writeError(w, http.StatusInternalServerError, "remove_failed", err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}

	case interfaces.DeploymentStatusProcessing:
		// Delete will mark as canceling for graceful shutdown
		if err := h.deploymentService.DeleteDeployment(deploymentID); err != nil {
			writeError(w, http.StatusInternalServerError, "cancel_failed", err.Error())
			return
		}
		// Return 202 Accepted to indicate cancellation is in progress
		writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"message": "Deployment cancellation initiated, will complete after current resource operation",
			"status":  "canceling",
			"id":      deploymentID,
		})

	case interfaces.DeploymentStatusQueued:
		// Can cancel immediately since not yet processing
		if err := h.deploymentService.CancelDeployment(deploymentID); err != nil {
			writeError(w, http.StatusInternalServerError, "cancel_failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case interfaces.DeploymentStatusCanceled, interfaces.DeploymentStatusCanceling:
		// Already canceled or canceling, nothing to do
		w.WriteHeader(http.StatusNoContent)

	case interfaces.DeploymentStatusDestroying:
		// Already destroying, return appropriate response
		writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"message": "Deployment destruction already in progress",
			"status":  "destroying",
			"id":      deploymentID,
		})

	case interfaces.DeploymentStatusDestroyed, interfaces.DeploymentStatusDeleted:
		// Already deleted, nothing to do
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusBadRequest, "invalid_status", "invalid deployment status for deletion")
		return
	}
}

// Helper methods

func (h *DeploymentHandler) buildQueuedDeploymentResponse(qd *interfaces.QueuedDeployment) map[string]interface{} {
	response := map[string]interface{}{
		"id":         qd.ID,
		"status":     string(qd.Status),
		"created_at": qd.CreatedAt.Format(time.RFC3339),
	}

	// Add metadata if present
	if qd.Request != nil && qd.Request.Metadata != nil {
		if name, ok := qd.Request.Metadata[interfaces.MetadataKeyName].(string); ok {
			response["name"] = name
		}
	}

	// Queue information would come from service if needed

	if qd.StartedAt != nil {
		response["started_at"] = qd.StartedAt.Format(time.RFC3339)
	}

	if qd.CompletedAt != nil {
		response["completed_at"] = qd.CompletedAt.Format(time.RFC3339)
	}

	if qd.LastError != nil {
		response["error"] = qd.LastError.Error()
	}

	return response
}

func (h *DeploymentHandler) buildQueuedDeploymentResponseWithState(qd *interfaces.QueuedDeployment) map[string]interface{} { //nolint:gocognit,gocyclo // Complex response building with state integration
	// Start with basic response
	response := h.buildQueuedDeploymentResponse(qd)

	// Add deletion information if deployment is destroyed
	if qd.Status == interfaces.DeploymentStatusDestroyed || qd.Status == interfaces.DeploymentStatusDeleted {
		if qd.DeletedAt != nil {
			response["deleted_at"] = qd.DeletedAt.Format(time.RFC3339)
		}
		if qd.DeletedBy != "" {
			response["deleted_by"] = qd.DeletedBy
		}
		if qd.DeleteReason != "" {
			response["delete_reason"] = qd.DeleteReason
		}
		response["message"] = "This deployment has been destroyed and is kept for audit purposes only"
	}

	// Try to get deployment state for any status (including in-progress)
	// This allows showing partial results for deployments in progress
	deploymentState, err := h.getDeploymentStateFromWorkerSystem(qd.ID)
	switch {
	case err != nil:
		h.logger.Warnf("Failed to get deployment state for %s: %v", qd.ID, err)
	case len(deploymentState) > 0:
		// deploymentState IS the resources map, not a wrapper
		h.logger.Debugf("DeploymentState for %s: %+v", qd.ID, deploymentState)
		resourceArray := h.ConvertResourceMapToArray(deploymentState)
		h.logger.Debugf("Converted resource array for %s: %+v", qd.ID, resourceArray)
		response["resources"] = resourceArray

		// Calculate resource summary
		summary := h.calculateResourceSummary(resourceArray)
		response["resource_summary"] = summary

		// Add other state information
		if success, ok := deploymentState["success"].(bool); ok {
			response["success"] = success
		}
		if startTime, ok := deploymentState["start_time"]; ok {
			response["start_time"] = startTime
		}
		if endTime, ok := deploymentState["end_time"]; ok {
			response["end_time"] = endTime
		}
	case qd.Status == interfaces.DeploymentStatusProcessing || qd.Status == interfaces.DeploymentStatusQueued:
		// For in-progress deployments without state yet, show resources as pending
		if qd.Request != nil && len(qd.Request.Resources) > 0 {
			resourceArray := make([]interface{}, 0, len(qd.Request.Resources))
			for _, resource := range qd.Request.Resources {
				resourceArray = append(resourceArray, map[string]interface{}{
					"type":   resource.Type,
					"name":   resource.Name,
					"status": "pending",
					"state":  map[string]interface{}{},
				})
			}
			response["resources"] = resourceArray
			response["resource_summary"] = h.calculateResourceSummary(resourceArray)
		}
	}

	return response
}

func (h *DeploymentHandler) getDeploymentStateFromWorkerSystem(deploymentID string) (map[string]interface{}, error) {
	// Get deployment state from service
	state, err := h.deploymentService.GetDeploymentState(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment state: %w", err)
	}
	return state, nil
}

// ConvertResourceMapToArray converts resource map to array format for API responses (exported for testing)
func (h *DeploymentHandler) ConvertResourceMapToArray(resourceMap map[string]interface{}) []interface{} {
	resources := make([]interface{}, 0, len(resourceMap))

	for resourceKey, resourceData := range resourceMap {
		// Parse resourceKey using centralized function (handles dot notation)
		resourceType, resourceName := interfaces.ParseResourceKey(interfaces.ResourceKey(resourceKey))
		if resourceType == "" || resourceName == "" {
			continue
		}

		// Convert resource data to expected format
		resource := map[string]interface{}{
			"type": resourceType,
			"name": resourceName,
		}

		// Extract status and state from resource data
		status := "completed" // Default for backwards compatibility
		var state map[string]interface{}
		var resourceError string

		if stateData, ok := resourceData.(map[string]interface{}); ok {
			// Check for explicit status field
			if s, ok := stateData["status"].(string); ok {
				status = s
			}
			// Check for error field
			if err, ok := stateData["error"].(string); ok {
				resourceError = err
			}
			// Store the full state (may include status field)
			state = stateData
		} else {
			// Handle simple values (backwards compatibility)
			state = map[string]interface{}{
				"id": resourceData,
			}
		}

		resource["status"] = status
		resource["state"] = state
		if resourceError != "" {
			resource["error"] = resourceError
		}

		resources = append(resources, resource)
	}

	return resources
}

// calculateResourceSummary calculates a summary of resource statuses
func (h *DeploymentHandler) calculateResourceSummary(resources []interface{}) map[string]interface{} {
	summary := map[string]interface{}{
		"total":      len(resources),
		"completed":  0,
		"failed":     0,
		"pending":    0,
		"dry_run":    0,
		"deleted":    0,
		"destroyed":  0,
		"processing": 0,
	}

	for _, r := range resources {
		if resource, ok := r.(map[string]interface{}); ok {
			if status, ok := resource["status"].(string); ok {
				switch status {
				case "completed", "created", "updated":
					summary["completed"] = summary["completed"].(int) + 1
				case "failed":
					summary["failed"] = summary["failed"].(int) + 1
				case "pending":
					summary["pending"] = summary["pending"].(int) + 1
				case "dry-run", "dry-run-create", "dry-run-update", "dry-run-delete":
					summary["dry_run"] = summary["dry_run"].(int) + 1
				case "deleted":
					summary["deleted"] = summary["deleted"].(int) + 1
				case "destroyed":
					summary["destroyed"] = summary["destroyed"].(int) + 1
				case "processing":
					summary["processing"] = summary["processing"].(int) + 1
				default:
					// Count unknown statuses as completed for backwards compatibility
					summary["completed"] = summary["completed"].(int) + 1
				}
			}
		}
	}

	return summary
}

// validateDeploymentForUpdate checks if a deployment can be updated
func (h *DeploymentHandler) validateDeploymentForUpdate(status interfaces.DeploymentStatus) error {
	switch status {
	case interfaces.DeploymentStatusProcessing:
		return ErrDeploymentInProgress
	case interfaces.DeploymentStatusQueued:
		return errors.New("cannot update a deployment that is still queued")
	case interfaces.DeploymentStatusCanceled:
		return errors.New("cannot update a canceled deployment")
	case interfaces.DeploymentStatusCanceling:
		return errors.New("cannot update a deployment that is being canceled")
	case interfaces.DeploymentStatusDestroying:
		return errors.New("cannot update a deployment that is being destroyed")
	case interfaces.DeploymentStatusDestroyed:
		return errors.New("cannot update a destroyed deployment")
	case interfaces.DeploymentStatusDeleted:
		return errors.New("cannot update a deleted deployment")
	case interfaces.DeploymentStatusCompleted, interfaces.DeploymentStatusFailed:
		return nil // These can be updated
	default:
		return ErrInvalidDeploymentStatus
	}
}

// buildQueuedUpdateResponse builds response for queued update
func (h *DeploymentHandler) buildQueuedUpdateResponse(qu *interfaces.QueuedUpdate, plan *interfaces.UpdatePlan) map[string]interface{} {
	response := map[string]interface{}{
		"update_id":     qu.ID,
		"deployment_id": qu.DeploymentID,
		"status":        string(qu.Status),
		"created_at":    qu.CreatedAt.Format(time.RFC3339),
	}

	// Add plan information
	if plan != nil {
		response["plan"] = map[string]interface{}{
			"plan_id":           plan.PlanID,
			"summary":           plan.Summary,
			"requires_approval": plan.RequiresApproval,
			"dangerous_changes": len(plan.DangerousChanges),
		}
	}

	// Add timestamps if available
	if qu.StartedAt != nil {
		response["started_at"] = qu.StartedAt.Format(time.RFC3339)
	}

	if qu.CompletedAt != nil {
		response["completed_at"] = qu.CompletedAt.Format(time.RFC3339)
	}

	if qu.LastError != nil {
		response["error"] = qu.LastError.Error()
	}

	return response
}
