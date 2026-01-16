package main

import (
	"os"
	"strconv"

	"github.com/cboxdk/init/internal/config"
	"github.com/cboxdk/init/internal/scaffold"
)

// ThresholdResult contains the resolved memory threshold and its source
type ThresholdResult struct {
	Threshold float64
	Source    string
}

// ResolveAutotuneThreshold determines the memory threshold from CLI, ENV, or config
// Priority: CLI flag > ENV variable > config file > profile default
func ResolveAutotuneThreshold(cliThreshold float64, envThreshold string, configThreshold float64) ThresholdResult {
	// CLI flag takes highest priority
	if cliThreshold > 0 {
		return ThresholdResult{Threshold: cliThreshold, Source: "CLI flag"}
	}

	// ENV variable is next
	if envThreshold != "" {
		if parsed, err := strconv.ParseFloat(envThreshold, 64); err == nil && parsed > 0 {
			return ThresholdResult{Threshold: parsed, Source: "ENV variable"}
		}
	}

	// Config file is next
	if configThreshold > 0 {
		return ThresholdResult{Threshold: configThreshold, Source: "global config"}
	}

	// Default to profile default (0 means use profile's built-in default)
	return ThresholdResult{Threshold: 0, Source: "profile default"}
}

// GetAutotuneProfileSource determines whether profile came from CLI or ENV
func GetAutotuneProfileSource(cliProfile string) string {
	if cliProfile != "" {
		return "CLI flag"
	}
	return "ENV var"
}

// ValidatePreset checks if a preset is valid and returns an error message if not
func ValidatePreset(preset string) (valid bool, validPresets []string) {
	validPresets = scaffold.ValidPresets()
	for _, v := range validPresets {
		if preset == v {
			return true, validPresets
		}
	}
	return false, validPresets
}

// DetermineScaffoldFiles returns the list of files to generate based on flags
func DetermineScaffoldFiles(generateCompose, generateDocker bool) []string {
	files := []string{"config"}
	if generateCompose {
		files = append(files, "docker-compose")
	}
	if generateDocker {
		files = append(files, "dockerfile")
	}
	return files
}

// CheckExistingFiles checks which files already exist in the output directory
func CheckExistingFiles(dir string, files []string) []string {
	existing := []string{}
	for _, file := range files {
		filename := getFilename(file)
		path := dir + "/" + filename
		if _, err := os.Stat(path); err == nil {
			existing = append(existing, filename)
		}
	}
	return existing
}

// DetermineWorkdir returns the working directory from env or default
func DetermineWorkdir() string {
	if workdir := os.Getenv("WORKDIR"); workdir != "" {
		return workdir
	}
	return "/var/www/html"
}

// ResolveAutotuneProfile returns the profile name from CLI flag or ENV
func ResolveAutotuneProfile(cliProfile string) string {
	if cliProfile != "" {
		return cliProfile
	}
	return os.Getenv("PHP_FPM_AUTOTUNE_PROFILE")
}

// ConfigPathResult contains the resolved config path and its source
type ConfigPathResult struct {
	Path   string
	Source string
}

// ResolveConfigPath determines the config file path from various sources
// Priority: CLI flag > ENV variable > user config > system config > local config
func ResolveConfigPath(cliPath string) ConfigPathResult {
	// CLI flag takes highest priority
	if cliPath != "" {
		return ConfigPathResult{Path: cliPath, Source: "CLI flag"}
	}

	// ENV variable
	if envPath := os.Getenv("CBOX_INIT_CONFIG"); envPath != "" {
		return ConfigPathResult{Path: envPath, Source: "ENV variable"}
	}

	// User config directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		userConfig := homeDir + "/.cbox/init/config.yaml"
		if _, err := os.Stat(userConfig); err == nil {
			return ConfigPathResult{Path: userConfig, Source: "user config"}
		}
	}

	// System config
	systemConfig := "/etc/cbox/init/config.yaml"
	if _, err := os.Stat(systemConfig); err == nil {
		return ConfigPathResult{Path: systemConfig, Source: "system config"}
	}

	// Local config (default)
	return ConfigPathResult{Path: "cbox-init.yaml", Source: "local config"}
}

// FormatAutotuneOutput formats the autotune results for display
func FormatAutotuneOutput(profile string, profileSource string, threshold float64, thresholdSource string, showThreshold bool) []string {
	var lines []string
	lines = append(lines, "PHP-FPM auto-tuned ("+profile+" profile via "+profileSource+"):")

	if showThreshold && threshold > 0 {
		// Format threshold as percentage
		thresholdPct := threshold * 100
		lines = append(lines, "   Memory threshold: "+strconv.FormatFloat(thresholdPct, 'f', 1, 64)+"% (via "+thresholdSource+")")
	}

	return lines
}

// ProcessConfigGlobal extracts global settings for process management
type ProcessConfigGlobal struct {
	LogLevel         string
	LogFormat        string
	ShutdownTimeout  int
	MetricsEnabled   bool
	APIEnabled       bool
	TracingEnabled   bool
}

// ExtractGlobalConfig extracts global configuration values
func ExtractGlobalConfig(cfg *config.Config) ProcessConfigGlobal {
	return ProcessConfigGlobal{
		LogLevel:         cfg.Global.LogLevel,
		LogFormat:        cfg.Global.LogFormat,
		ShutdownTimeout:  cfg.Global.ShutdownTimeout,
		MetricsEnabled:   cfg.Global.MetricsEnabledValue(),
		APIEnabled:       cfg.Global.APIEnabledValue(),
		TracingEnabled:   cfg.Global.TracingEnabled,
	}
}
