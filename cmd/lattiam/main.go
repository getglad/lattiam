package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

// loadStandardConfig creates a new config, loads from environment, and expands paths.
// This is the standard pattern used by most commands.
func loadStandardConfig() (*config.ServerConfig, error) {
	cfg := config.NewServerConfig()
	if err := cfg.LoadFromEnv(); err != nil {
		return nil, fmt.Errorf("failed to load config from environment: %w", err)
	}
	if err := cfg.ExpandPaths(); err != nil {
		return nil, fmt.Errorf("failed to expand paths: %w", err)
	}
	return cfg, nil
}

var (
	version = "dev"
	commit  = "none"    //nolint:gochecknoglobals // Build-time commit info
	date    = "unknown" //nolint:gochecknoglobals // Build-time date info

	// Global debug flag
	debugMode bool   //nolint:gochecknoglobals // CLI global flag
	debugDir  string //nolint:gochecknoglobals // CLI global flag
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "lattiam",
		Short: "Universal infrastructure management with Terraform JSON",
		Long: `Lattiam deploys infrastructure using Terraform JSON format and providers.
		
It communicates directly with providers via gRPC - no HCL files or terraform CLI needed.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			// Set up debug environment if debug flag is set
			if debugMode {
				_ = os.Setenv("LATTIAM_DEBUG", "1") // os.Setenv always returns nil
				if debugDir != "" {
					_ = os.Setenv("LATTIAM_DEBUG_DIR", debugDir)
				}
				// Also enable Terraform debug logging
				_ = os.Setenv("TF_LOG", "DEBUG")

				// Reinitialize the debug logger to pick up the environment variables
				protocol.ReinitializeDebugLogger()

				fmt.Fprintf(os.Stderr, "Debug mode enabled. Logs will be written to: %s\n", getDebugDir())
			}
		},
		PersistentPostRun: func(_ *cobra.Command, _ []string) {
			// Show debug info after command completes
			if debugMode {
				showDebugInfo()
			}
		},
	}

	// Add global debug flags
	rootCmd.PersistentFlags().BoolVar(&debugMode, "debug", false,
		"Enable debug mode and save all provider communication to disk")
	rootCmd.PersistentFlags().StringVar(&debugDir, "debug-dir", "",
		"Directory for debug output (default: ./tmp/lattiam-debug)")

	rootCmd.AddCommand(
		newApplyCommand(),
		newServerCommand(),
		newConfigCommand(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newApplyCommand() *cobra.Command {
	var dryRun bool
	var providerVersion string

	cmd := &cobra.Command{
		Use:   "apply [file]",
		Short: "Apply infrastructure from Terraform JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runApply(args[0], dryRun, providerVersion)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be created without actually creating resources")
	cmd.Flags().StringVar(&providerVersion, "provider-version", "", "Override provider version (e.g., 5.70.0, 5.31.0)")
	return cmd
}

func newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the Lattiam API server",
		Long:  "Start, stop, and manage the Lattiam API server for REST API access",
	}

	cmd.AddCommand(
		newServerStartCommand(),
		newServerStopCommand(),
		newServerStatusCommand(),
	)

	return cmd
}

func newServerStartCommand() *cobra.Command {
	var port int
	var daemon bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the API server",
		RunE: func(_ *cobra.Command, _ []string) error {
			if daemon {
				return runServerDaemon(port, debugMode)
			}
			return runServerForeground(port, debugMode)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8084, "Port to listen on")
	cmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "Run server in background")
	return cmd
}

func newServerStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the API server",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Use lightweight function to get just the PID file path
			pidFile := config.GetPIDPath()
			return stopServer(pidFile)
		},
	}
	return cmd
}

func newServerStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check API server status",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Use lightweight functions to get just what we need
			pidFile := config.GetPIDPath()
			port := config.GetPort()
			return checkServerStatus(pidFile, port)
		},
	}
	return cmd
}

func getDebugDir() string {
	if debugDir != "" {
		return debugDir
	}
	if dir := os.Getenv("LATTIAM_DEBUG_DIR"); dir != "" {
		return dir
	}
	return "./tmp/lattiam-debug"
}

func showDebugInfo() { //nolint:funlen,gocognit // Debug output function with detailed formatting
	dir := getDebugDir()

	// Check if debug directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}

	fmt.Fprintf(os.Stderr, "\n=== Debug Output Summary ===\n")
	fmt.Fprintf(os.Stderr, "Debug directory: %s\n", dir)

	// List all debug files
	files, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading debug directory: %v\n", err)
		return
	}

	// Group files by type
	type fileInfo struct {
		name string
		size int64
		time time.Time
	}

	var logFiles, binFiles []fileInfo
	for _, f := range files {
		info, err := f.Info()
		if err != nil {
			continue
		}
		fi := fileInfo{
			name: f.Name(),
			size: info.Size(),
			time: info.ModTime(),
		}
		if strings.HasSuffix(f.Name(), ".log") {
			logFiles = append(logFiles, fi)
		} else if strings.HasSuffix(f.Name(), ".bin") {
			binFiles = append(binFiles, fi)
		}
	}

	// Sort by modification time (most recent first)
	sort.Slice(logFiles, func(i, j int) bool {
		return logFiles[i].time.After(logFiles[j].time)
	})
	sort.Slice(binFiles, func(i, j int) bool {
		return binFiles[i].time.After(binFiles[j].time)
	})

	// Show log files
	if len(logFiles) > 0 {
		fmt.Fprintf(os.Stderr, "\nLog files (newest first):\n")
		for i, f := range logFiles {
			if i >= 5 { // Show only 5 most recent
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(logFiles)-5)
				break
			}
			fmt.Fprintf(os.Stderr, "  - %s (%d bytes)\n", filepath.Join(dir, f.name), f.size)
		}
	}

	// Show binary files
	if len(binFiles) > 0 {
		fmt.Fprintf(os.Stderr, "\nBinary/msgpack files:\n")
		for i, f := range binFiles {
			if i >= 3 { // Show only 3 most recent
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(binFiles)-3)
				break
			}
			fmt.Fprintf(os.Stderr, "  - %s (%d bytes)\n", filepath.Join(dir, f.name), f.size)
		}
	}

	// Suggest debug commands
	fmt.Fprintf(os.Stderr, "\nUseful debug commands:\n")
	fmt.Fprintf(os.Stderr, "  # View latest log:\n")
	if len(logFiles) > 0 {
		fmt.Fprintf(os.Stderr, "  tail -f %s\n", filepath.Join(dir, logFiles[0].name))
	}
	fmt.Fprintf(os.Stderr, "\n  # View all logs:\n")
	fmt.Fprintf(os.Stderr, "  ls -la %s/\n", dir)
	fmt.Fprintf(os.Stderr, "\n")
}
