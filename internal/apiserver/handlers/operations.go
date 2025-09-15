package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"

	"github.com/go-chi/chi/v5"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/logging"
	"github.com/lattiam/lattiam/internal/utils/fsutil"
)

// OperationsHandler handles operational endpoints like config, metrics, etc.
type OperationsHandler struct {
	config     *config.ServerConfig
	stateStore interfaces.StateStore
	workerPool interfaces.WorkerPool
	queue      interfaces.DeploymentQueue
	logger     *logging.Logger
}

// NewOperationsHandler creates a new operations handler
func NewOperationsHandler(
	cfg *config.ServerConfig,
	stateStore interfaces.StateStore,
	workerPool interfaces.WorkerPool,
	queue interfaces.DeploymentQueue,
) *OperationsHandler {
	return &OperationsHandler{
		config:     cfg,
		stateStore: stateStore,
		workerPool: workerPool,
		queue:      queue,
		logger:     logging.NewLogger("operations-handler"),
	}
}

// RegisterRoutes registers all operational routes
func (h *OperationsHandler) RegisterRoutes(r chi.Router) {
	r.Get("/system/config", h.GetConfig)
	r.Get("/system/paths", h.GetPaths)
	r.Get("/system/storage", h.GetStorageInfo)
	r.Get("/system/runtime", h.GetRuntimeInfo)
	r.Get("/system/disk-usage", h.GetDiskUsage)
}

// GetConfig returns the current server configuration
// @Summary Get server configuration
// @Description Get the current server configuration (sanitized)
// @Tags system
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "Server configuration"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /system/config [get]
func (h *OperationsHandler) GetConfig(w http.ResponseWriter, _ *http.Request) {
	// Return sanitized configuration
	safeCfg := h.config.GetSanitized()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(safeCfg); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to encode configuration")
	}
}

// GetPaths returns path health status without exposing sensitive information
// @Summary Get system paths
// @Description Get configured system paths and their health status
// @Tags system
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "Path information"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /system/paths [get]
func (h *OperationsHandler) GetPaths(w http.ResponseWriter, _ *http.Request) {
	// Only expose health status, not actual paths
	status := map[string]interface{}{
		"state_storage": map[string]interface{}{
			"configured": h.config.StateDir != "",
			"healthy":    fsutil.DirExists(h.config.StateDir) && fsutil.IsWritable(h.config.StateDir),
		},
		"provider_storage": map[string]interface{}{
			"configured": h.config.ProviderDir != "",
			"healthy":    fsutil.DirExists(h.config.ProviderDir) && fsutil.IsWritable(h.config.ProviderDir),
		},
		"logging": map[string]interface{}{
			"configured": h.config.GetLogPath() != "",
			"type":       "file",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to encode path status")
	}
}

// GetStorageInfo returns information about the storage backend
// @Summary Get storage information
// @Description Get storage backend information
// @Tags system
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "Storage information"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /system/storage [get]
func (h *OperationsHandler) GetStorageInfo(w http.ResponseWriter, _ *http.Request) {
	info := map[string]interface{}{
		"state_store": map[string]interface{}{
			"type": h.config.StateStore.Type,
		},
	}

	// If state store exists, get detailed information
	if h.stateStore != nil {
		// Test connectivity with a ping
		ctx := context.Background()
		pingErr := h.stateStore.Ping(ctx)

		// Try to get detailed storage info if the store supports it
		if storageInfoProvider, ok := h.stateStore.(interface {
			GetStorageInfo() *interfaces.StorageInfo
		}); ok {
			storageInfo := storageInfoProvider.GetStorageInfo()
			if storageInfo != nil {
				// Build response with detailed storage information
				safeInfo := map[string]interface{}{
					"type":             storageInfo.Type,
					"exists":           storageInfo.Exists && pingErr == nil,
					"writable":         storageInfo.Writable,
					"deployment_count": storageInfo.DeploymentCount,
				}
				info["state_store"] = safeInfo
			} else {
				// Fall back to basic info
				safeInfo := map[string]interface{}{
					"type":   h.config.StateStore.Type,
					"exists": pingErr == nil,
				}
				info["state_store"] = safeInfo
			}
		} else {
			// Build response with basic information only
			safeInfo := map[string]interface{}{
				"type":   h.config.StateStore.Type,
				"exists": pingErr == nil,
			}
			info["state_store"] = safeInfo
		}
	}

	// Add disk space percentages only (no paths)
	diskSpace := make(map[string]interface{})
	if stateUsage, err := fsutil.GetDiskUsageMap(h.config.StateDir); err == nil {
		if percent, ok := stateUsage["used_percent"].(float64); ok {
			diskSpace["state_storage"] = map[string]interface{}{
				"used_percent": percent,
			}
		}
	}
	if providerUsage, err := fsutil.GetDiskUsageMap(h.config.ProviderDir); err == nil {
		if percent, ok := providerUsage["used_percent"].(float64); ok {
			diskSpace["provider_storage"] = map[string]interface{}{
				"used_percent": percent,
			}
		}
	}
	info["disk_space"] = diskSpace

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to encode storage info")
	}
}

// GetRuntimeInfo returns runtime information about the server
// @Summary Get runtime information
// @Description Get runtime statistics and metrics
// @Tags system
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "Runtime information"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /system/runtime [get]
func (h *OperationsHandler) GetRuntimeInfo(w http.ResponseWriter, _ *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	info := map[string]interface{}{
		"go_version":     runtime.Version(),
		"num_goroutines": runtime.NumGoroutine(),
		"num_cpu":        runtime.NumCPU(),
		"memory": map[string]interface{}{
			"alloc_mb":       m.Alloc / 1024 / 1024,
			"total_alloc_mb": m.TotalAlloc / 1024 / 1024,
			"sys_mb":         m.Sys / 1024 / 1024,
			"gc_runs":        m.NumGC,
		},
		"config": map[string]interface{}{
			"port":        h.config.Port,
			"debug":       h.config.Debug,
			"daemon_mode": h.config.DaemonMode,
		},
	}

	// Add worker pool info if available
	if h.workerPool != nil {
		// Try to cast to the detailed Pool interface for statistics
		if pool, ok := h.workerPool.(interface {
			GetActiveWorkers() int
			GetIdleWorkers() int
			GetBusyWorkers() int
			GetQueueDepth() int
		}); ok {
			info["worker_pool"] = map[string]interface{}{
				"active_workers": pool.GetActiveWorkers(),
				"idle_workers":   pool.GetIdleWorkers(),
				"busy_workers":   pool.GetBusyWorkers(),
				"queue_depth":    pool.GetQueueDepth(),
			}
		}
	}

	// Add queue info if available
	if h.queue != nil {
		// Try to cast to interface with Size method
		if queue, ok := h.queue.(interface {
			Size() int
		}); ok {
			info["queue"] = map[string]interface{}{
				"size": queue.Size(),
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to encode runtime info")
	}
}

// GetDiskUsage returns current disk usage without exposing paths
// @Summary Get disk usage
// @Description Get disk usage statistics
// @Tags system
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "Disk usage information"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /system/disk-usage [get]
func (h *OperationsHandler) GetDiskUsage(w http.ResponseWriter, _ *http.Request) {
	usage := map[string]interface{}{
		"storage": make(map[string]interface{}),
		"thresholds": map[string]float64{
			"warning":  80.0,
			"critical": 90.0,
		},
	}

	// Check each configured storage area without exposing paths
	storageAreas := map[string]string{
		"state":    h.config.StateDir,
		"provider": h.config.ProviderDir,
	}

	// Get usage for each storage area
	alerts := []map[string]interface{}{}
	for name, path := range storageAreas {
		if path == "" {
			continue
		}

		if diskUsage, err := fsutil.GetDiskUsageMap(path); err == nil {
			// Only expose percentage and status, not actual paths
			percentUsed, ok := diskUsage["used_percent"].(float64)
			if ok {
				storageInfo := map[string]interface{}{
					"used_percent": percentUsed,
				}

				switch {
				case percentUsed >= 90.0:
					storageInfo["status"] = "critical"
					alerts = append(alerts, map[string]interface{}{
						"storage": name,
						"level":   "critical",
						"percent": percentUsed,
						"message": fmt.Sprintf("%s storage is %.1f%% full", name, percentUsed),
					})
				case percentUsed >= 80.0:
					storageInfo["status"] = "warning"
					alerts = append(alerts, map[string]interface{}{
						"storage": name,
						"level":   "warning",
						"percent": percentUsed,
						"message": fmt.Sprintf("%s storage is %.1f%% full", name, percentUsed),
					})
				default:
					storageInfo["status"] = "healthy"
				}

				usage["storage"].(map[string]interface{})[name] = storageInfo
			}
		}
	}

	usage["alerts"] = alerts
	usage["alert_count"] = len(alerts)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(usage); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to encode disk usage")
	}
}

// Helper method to write errors
func (h *OperationsHandler) writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	}); err != nil {
		// Log encoding error but don't write again to avoid double response
		h.logger.Errorf("Failed to encode error response: %v", err)
	}
}
