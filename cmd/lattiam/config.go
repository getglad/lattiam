//nolint:forbidigo // CLI command needs fmt.Print* for user output
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lattiam/lattiam/internal/config"
)

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Lattiam configuration",
		Long:  "View and manage Lattiam server configuration settings",
	}

	cmd.AddCommand(
		newConfigShowCommand(),
		newConfigPathsCommand(),
		newConfigValidateCommand(),
	)

	return cmd
}

func newConfigShowCommand() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		Long:  "Display the current Lattiam configuration including all settings from defaults, environment variables, and command-line flags",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Load standard configuration
			cfg, err := loadStandardConfig()
			if err != nil {
				return err
			}

			// Display based on format
			switch format {
			case "json":
				return displayConfigJSON(cfg)
			case "table":
				return displayConfigTable(cfg)
			default:
				return fmt.Errorf("unknown format: %s. Supported formats: table, json", format)
			}
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "table", "Output format (table, json)")

	return cmd
}

func newConfigPathsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "paths",
		Short: "Show all storage paths",
		Long:  "Display all configured storage paths and check if they exist",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Load standard configuration
			cfg, err := loadStandardConfig()
			if err != nil {
				return err
			}

			// Display paths with status
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "PATH TYPE\tLOCATION\tSTATUS") // Ignore error - output formatting
			_, _ = fmt.Fprintln(w, "---------\t--------\t------") // Ignore error - output formatting

			// Check each path
			checkPath(w, "State Directory", cfg.StateDir)
			checkPath(w, "State Store Path", cfg.StateStore.File.Path)
			checkPath(w, "Provider Directory", cfg.ProviderDir)

			// Log file is special - may be empty (stdout)
			if cfg.GetLogPath() != "" {
				checkPath(w, "Log File", cfg.GetLogPath())
			} else {
				_, _ = fmt.Fprintf(w, "Log File\tstdout\tN/A\n") // Ignore error - output formatting
			}

			checkPath(w, "PID File", cfg.PIDFile)

			// Also show temp files
			_, _ = fmt.Fprintf(w, "Config Info File\t%s\tTEMP\n", filepath.Join(os.TempDir(), "lattiam.info")) // Ignore error - output formatting

			_ = w.Flush() // Ignore error - output formatting

			return nil
		},
	}

	return cmd
}

func newConfigValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration",
		Long:  "Check if the current configuration is valid and all required directories are accessible",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Load standard configuration
			cfg, err := loadStandardConfig()
			if err != nil {
				return err
			}

			// Validate configuration
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("configuration validation failed: %w", err)
			}

			fmt.Println("✓ Configuration is valid")

			// Check directory permissions
			fmt.Println("\nChecking directory permissions...")

			errors := 0

			// Check state directory
			if err := checkDirExists(cfg.StateDir); err != nil {
				fmt.Printf("✗ State Directory (%s): %v\n", cfg.StateDir, err)
				errors++
			} else {
				fmt.Printf("✓ State Directory (%s): writable\n", cfg.StateDir)
			}

			// Check provider directory
			if err := checkDirExists(cfg.ProviderDir); err != nil {
				fmt.Printf("✗ Provider Directory (%s): %v\n", cfg.ProviderDir, err)
				errors++
			} else {
				fmt.Printf("✓ Provider Directory (%s): writable\n", cfg.ProviderDir)
			}

			// Check log file directory
			if cfg.GetLogPath() != "" {
				logDir := filepath.Dir(cfg.GetLogPath())
				if err := checkDirExists(logDir); err != nil {
					fmt.Printf("✗ Log Directory (%s): %v\n", logDir, err)
					errors++
				} else {
					fmt.Printf("✓ Log Directory (%s): writable\n", logDir)
				}
			}

			if errors > 0 {
				return fmt.Errorf("found %d configuration errors", errors)
			}

			fmt.Println("\n✓ All configuration checks passed")
			return nil
		},
	}

	return cmd
}

// Helper functions

func displayConfigJSON(cfg *config.ServerConfig) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	return nil
}

func displayConfigTable(cfg *config.ServerConfig) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(w, "SETTING\tVALUE\tSOURCE") // Ignore error - output formatting
	_, _ = fmt.Fprintln(w, "-------\t-----\t------") // Ignore error - output formatting

	// Server settings
	_, _ = fmt.Fprintf(w, "Port\t%d\tconfig\n", cfg.Port)   // Ignore error - output formatting
	_, _ = fmt.Fprintf(w, "Debug\t%t\tconfig\n", cfg.Debug) // Ignore error - output formatting

	// Storage paths
	_, _ = fmt.Fprintf(w, "State Directory\t%s\tconfig\n", cfg.StateDir)       // Ignore error - output formatting
	_, _ = fmt.Fprintf(w, "Log File\t%s\tconfig\n", cfg.GetLogPath())          // Ignore error - output formatting
	_, _ = fmt.Fprintf(w, "Provider Directory\t%s\tconfig\n", cfg.ProviderDir) // Ignore error - output formatting

	// State store
	_, _ = fmt.Fprintf(w, "State Store Type\t%s\tconfig\n", cfg.StateStore.Type)      // Ignore error - output formatting
	_, _ = fmt.Fprintf(w, "State Store Path\t%s\tconfig\n", cfg.StateStore.File.Path) // Ignore error - output formatting

	// Daemon settings
	_, _ = fmt.Fprintf(w, "Daemon Mode\t%t\tconfig\n", cfg.DaemonMode) // Ignore error - output formatting
	_, _ = fmt.Fprintf(w, "PID File\t%s\tconfig\n", cfg.PIDFile)       // Ignore error - output formatting

	_ = w.Flush() // Ignore error - output formatting

	fmt.Println("\nEnvironment Variables:")
	printEnvironmentVariables(cfg)

	return nil
}

// printEnvironmentVariables dynamically prints environment variables from struct tags
func printEnvironmentVariables(cfg *config.ServerConfig) {
	// Collect all environment variables
	vars := collectEnvVars(reflect.TypeOf(*cfg), reflect.ValueOf(*cfg), "")

	// Find the longest env var name for alignment
	maxLen := 0
	for _, v := range vars {
		if len(v.name) > maxLen {
			maxLen = len(v.name)
		}
	}

	// Print with proper alignment
	for _, v := range vars {
		fmt.Printf("  %-*s - %s\n", maxLen, v.name, v.description)
	}
}

// collectEnvVars recursively collects environment variables from struct tags
func collectEnvVars(t reflect.Type, v reflect.Value, _ string) []struct{ name, description string } {
	var vars []struct{ name, description string }

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// Check for env tag
		if envTag := field.Tag.Get("env"); envTag != "" {
			// Get description from desc tag or generate from field name
			desc := field.Tag.Get("desc")
			if desc == "" {
				desc = generateDescription(field.Name, field.Type.String())
			}
			vars = append(vars, struct{ name, description string }{
				name:        envTag,
				description: desc,
			})
		}

		// Recursively check embedded structs
		if field.Type.Kind() == reflect.Struct {
			subVars := collectEnvVars(field.Type, fieldValue, field.Name)
			vars = append(vars, subVars...)
		}
	}

	return vars
}

// generateDescription creates a human-readable description from field name
func generateDescription(fieldName string, _ string) string {
	// Convert CamelCase to words as fallback
	words := camelCaseToWords(fieldName)
	return strings.Join(words, " ")
}

// camelCaseToWords converts CamelCase to space-separated words
func camelCaseToWords(s string) []string {
	var words []string
	var currentWord []rune

	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			// Start new word at uppercase letter
			if len(currentWord) > 0 {
				words = append(words, string(currentWord))
			}
			currentWord = []rune{r}
		} else {
			currentWord = append(currentWord, r)
		}
	}

	if len(currentWord) > 0 {
		words = append(words, string(currentWord))
	}

	return words
}

func checkPath(w *tabwriter.Writer, name, path string) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			_, _ = fmt.Fprintf(w, "%s\t%s\tNOT FOUND\n", name, path) // Ignore error - output formatting
		} else {
			_, _ = fmt.Fprintf(w, "%s\t%s\tERROR: %v\n", name, path, err) // Ignore error - output formatting
		}
		return
	}

	if info.IsDir() {
		_, _ = fmt.Fprintf(w, "%s\t%s\tEXISTS (dir)\n", name, path) // Ignore error - output formatting
	} else {
		_, _ = fmt.Fprintf(w, "%s\t%s\tEXISTS (file, %d bytes)\n", name, path, info.Size()) // Ignore error - output formatting
	}
}

// checkDirExists performs a read-only check on a directory for validation
func checkDirExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("does not exist")
		}
		return fmt.Errorf("failed to check directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("exists but is not a directory")
	}

	// Check if writable by creating a temp file
	tempFile := filepath.Join(path, ".write_test")
	file, err := os.Create(tempFile) // #nosec G304 -- tempFile is constructed from safe path components
	if err != nil {
		return fmt.Errorf("not writable: %w", err)
	}
	_ = file.Close()        // Ignore error - cleanup operation
	_ = os.Remove(tempFile) // Ignore error - cleanup operation

	return nil
}
