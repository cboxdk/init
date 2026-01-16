package main

import (
	"fmt"
	"os"

	"github.com/cboxdk/init/internal/autotune"
	"github.com/cboxdk/init/internal/config"
	"github.com/spf13/cobra"
)

var checkConfigCmd = &cobra.Command{
	Use:   "check-config",
	Short: "Validate configuration file",
	Long:  `Validate the Cbox Init configuration file and report any errors or warnings`,
	Run:   runCheckConfig,
}

func init() {
	checkConfigCmd.Flags().Bool("strict", false, "Fail on warnings (not just errors)")
	checkConfigCmd.Flags().Bool("json", false, "Output validation results as JSON")
	checkConfigCmd.Flags().Bool("quiet", false, "Show only summary (no detailed report)")
}

func runCheckConfig(cmd *cobra.Command, args []string) {
	strict, _ := cmd.Flags().GetBool("strict")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	quiet, _ := cmd.Flags().GetBool("quiet")

	// Get config path from persistent flag or default
	cfgPath := getConfigPath()

	// Load configuration
	cfg, err := config.LoadWithEnvExpansion(cfgPath)
	if err != nil {
		if jsonOutput {
			fmt.Fprintf(os.Stderr, `{"error":"Configuration load failed: %v"}`+"\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "❌ Configuration load failed: %v\n", err)
		}
		os.Exit(1)
	}

	// Apply defaults
	cfg.SetDefaults()

	// Run comprehensive validation
	result, err := cfg.ValidateComprehensive()
	if err != nil {
		// Validation found blocking errors
		if jsonOutput {
			jsonData := config.FormatValidationJSON(result)
			jsonData["config_path"] = cfgPath
			fmt.Println(formatJSONOutput(jsonData))
		} else if quiet {
			fmt.Printf("❌ %s\n", config.FormatValidationSummary(result))
		} else {
			fmt.Print(config.FormatValidationReport(result))
		}
		os.Exit(1)
	}

	// Check for auto-tuning profile if specified
	autotuneProfile := os.Getenv("PHP_FPM_AUTOTUNE_PROFILE")
	if cfg.Global.AutotuneMemoryThreshold > 0 && autotuneProfile != "" {
		profile := autotune.Profile(autotuneProfile)
		if err := profile.Validate(); err != nil {
			if jsonOutput {
				fmt.Fprintf(os.Stderr, `{"error":"Invalid PHP-FPM profile: %v"}`+"\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "❌ Invalid PHP-FPM profile: %v\n", err)
			}
			os.Exit(1)
		}
	}

	// Output results based on format
	if jsonOutput {
		// JSON output
		jsonData := config.FormatValidationJSON(result)
		jsonData["config_path"] = cfgPath
		jsonData["version"] = cfg.Version
		jsonData["process_count"] = len(cfg.Processes)
		if autotuneProfile != "" {
			jsonData["php_fpm_profile"] = autotuneProfile
		}
		fmt.Println(formatJSONOutput(jsonData))
	} else if quiet {
		// Quiet mode - just summary
		if result.TotalIssues() == 0 {
			fmt.Println("✅ Configuration is valid")
		} else {
			fmt.Printf("✅ Configuration is valid (with issues): %s\n", config.FormatValidationSummary(result))
		}
	} else {
		// Full report mode
		if result.TotalIssues() > 0 {
			fmt.Print(config.FormatValidationReport(result))
		}

		// Print configuration summary
		fmt.Printf("\n📋 Configuration Summary:\n")
		fmt.Printf("   Path: %s\n", cfgPath)
		fmt.Printf("   Version: %s\n", cfg.Version)
		fmt.Printf("   Processes: %d\n", len(cfg.Processes))
		fmt.Printf("   Log Level: %s\n", cfg.Global.LogLevel)
		fmt.Printf("   Shutdown Timeout: %ds\n", cfg.Global.ShutdownTimeout)

		if autotuneProfile != "" {
			fmt.Printf("   PHP-FPM Profile: %s (auto-tuned)\n", autotuneProfile)
		}

		if result.TotalIssues() == 0 {
			fmt.Println("\n✅ Configuration ready for use")
		} else {
			fmt.Println("\n✅ Configuration is valid but has warnings/suggestions")
		}
	}

	// Strict mode: fail if warnings exist
	if strict && result.HasWarnings() {
		if !jsonOutput {
			fmt.Println("\n❌ Validation failed in strict mode (warnings present)")
		}
		os.Exit(1)
	}
}

// formatJSONOutput formats data as JSON string
func formatJSONOutput(data map[string]interface{}) string {
	// Simple JSON formatting (could use json.Marshal for production)
	var parts []string
	parts = append(parts, "{")
	i := 0
	for k, v := range data {
		if i > 0 {
			parts[len(parts)-1] += ","
		}
		switch val := v.(type) {
		case string:
			parts = append(parts, fmt.Sprintf(`  "%s": "%s"`, k, val))
		case int:
			parts = append(parts, fmt.Sprintf(`  "%s": %d`, k, val))
		case bool:
			parts = append(parts, fmt.Sprintf(`  "%s": %v`, k, val))
		default:
			// For complex types, use fmt.Sprint (not production-grade)
			parts = append(parts, fmt.Sprintf(`  "%s": %v`, k, val))
		}
		i++
	}
	parts = append(parts, "}")
	return fmt.Sprintf("%s\n", fmt.Sprint(parts))
}

// getConfigPath determines configuration file path with priority order
func getConfigPath() string {
	// 1. Try persistent flag (explicit, highest priority)
	if cfgFile != "" {
		return cfgFile
	}

	// 2. Try environment variable
	if envPath := os.Getenv("CBOX_INIT_CONFIG"); envPath != "" {
		return envPath
	}

	// 3. Try default paths in priority order
	defaultPaths := []string{
		// User-specific config (highest priority for defaults)
		os.ExpandEnv("$HOME/.cbox/init/config.yaml"),
		os.ExpandEnv("$HOME/.cbox/init/config.yml"),

		// System-wide configs
		"/etc/cbox/init/config.yaml",
		"/etc/cbox/init/config.yml",

		// Legacy paths (backward compatibility)
		"/etc/cbox-init/cbox-init.yaml",
		"/etc/cbox-init/cbox-init.yml",

		// Current directory (lowest priority)
		"cbox-init.yaml",
		"cbox-init.yml",
	}

	for _, path := range defaultPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Fallback to local yaml (will error later if doesn't exist)
	return "cbox-init.yaml"
}
