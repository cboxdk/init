package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gophpeek/phpeek-pm/internal/config"
)

// TestResolveAutotuneThreshold tests the threshold resolution logic
func TestResolveAutotuneThreshold(t *testing.T) {
	tests := []struct {
		name            string
		cliThreshold    float64
		envThreshold    string
		configThreshold float64
		wantThreshold   float64
		wantSource      string
	}{
		{
			name:            "CLI takes priority over all",
			cliThreshold:    0.75,
			envThreshold:    "0.85",
			configThreshold: 0.90,
			wantThreshold:   0.75,
			wantSource:      "CLI flag",
		},
		{
			name:            "ENV takes priority when CLI is 0",
			cliThreshold:    0,
			envThreshold:    "0.85",
			configThreshold: 0.90,
			wantThreshold:   0.85,
			wantSource:      "ENV variable",
		},
		{
			name:            "Config used when CLI and ENV empty",
			cliThreshold:    0,
			envThreshold:    "",
			configThreshold: 0.90,
			wantThreshold:   0.90,
			wantSource:      "global config",
		},
		{
			name:            "Profile default when all empty",
			cliThreshold:    0,
			envThreshold:    "",
			configThreshold: 0,
			wantThreshold:   0,
			wantSource:      "profile default",
		},
		{
			name:            "Invalid ENV is skipped",
			cliThreshold:    0,
			envThreshold:    "not-a-number",
			configThreshold: 0.70,
			wantThreshold:   0.70,
			wantSource:      "global config",
		},
		{
			name:            "Zero ENV is skipped",
			cliThreshold:    0,
			envThreshold:    "0",
			configThreshold: 0.70,
			wantThreshold:   0.70,
			wantSource:      "global config",
		},
		{
			name:            "Negative ENV is skipped",
			cliThreshold:    0,
			envThreshold:    "-0.5",
			configThreshold: 0.70,
			wantThreshold:   0.70,
			wantSource:      "global config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveAutotuneThreshold(tt.cliThreshold, tt.envThreshold, tt.configThreshold)
			if result.Threshold != tt.wantThreshold {
				t.Errorf("Threshold = %v, want %v", result.Threshold, tt.wantThreshold)
			}
			if result.Source != tt.wantSource {
				t.Errorf("Source = %v, want %v", result.Source, tt.wantSource)
			}
		})
	}
}

// TestGetAutotuneProfileSource tests profile source determination
func TestGetAutotuneProfileSource(t *testing.T) {
	tests := []struct {
		name       string
		cliProfile string
		wantSource string
	}{
		{
			name:       "CLI profile",
			cliProfile: "dev",
			wantSource: "CLI flag",
		},
		{
			name:       "ENV profile (CLI empty)",
			cliProfile: "",
			wantSource: "ENV var",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetAutotuneProfileSource(tt.cliProfile)
			if result != tt.wantSource {
				t.Errorf("GetAutotuneProfileSource() = %v, want %v", result, tt.wantSource)
			}
		})
	}
}

// TestValidatePreset tests preset validation
func TestValidatePreset(t *testing.T) {
	tests := []struct {
		name      string
		preset    string
		wantValid bool
	}{
		{name: "valid laravel", preset: "laravel", wantValid: true},
		{name: "valid symfony", preset: "symfony", wantValid: true},
		{name: "valid php", preset: "php", wantValid: true},
		{name: "valid wordpress", preset: "wordpress", wantValid: true},
		{name: "valid magento", preset: "magento", wantValid: true},
		{name: "valid drupal", preset: "drupal", wantValid: true},
		{name: "valid nextjs", preset: "nextjs", wantValid: true},
		{name: "valid nuxt", preset: "nuxt", wantValid: true},
		{name: "valid nodejs", preset: "nodejs", wantValid: true},
		{name: "invalid preset", preset: "invalid", wantValid: false},
		{name: "empty preset", preset: "", wantValid: false},
		{name: "case sensitive", preset: "Laravel", wantValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, presets := ValidatePreset(tt.preset)
			if valid != tt.wantValid {
				t.Errorf("ValidatePreset(%q) = %v, want %v", tt.preset, valid, tt.wantValid)
			}
			if len(presets) == 0 {
				t.Error("ValidatePreset should return list of valid presets")
			}
		})
	}
}

// TestDetermineScaffoldFiles tests file list generation
func TestDetermineScaffoldFiles(t *testing.T) {
	tests := []struct {
		name            string
		generateCompose bool
		generateDocker  bool
		wantFiles       []string
	}{
		{
			name:            "config only",
			generateCompose: false,
			generateDocker:  false,
			wantFiles:       []string{"config"},
		},
		{
			name:            "with docker-compose",
			generateCompose: true,
			generateDocker:  false,
			wantFiles:       []string{"config", "docker-compose"},
		},
		{
			name:            "with dockerfile",
			generateCompose: false,
			generateDocker:  true,
			wantFiles:       []string{"config", "dockerfile"},
		},
		{
			name:            "with both docker files",
			generateCompose: true,
			generateDocker:  true,
			wantFiles:       []string{"config", "docker-compose", "dockerfile"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := DetermineScaffoldFiles(tt.generateCompose, tt.generateDocker)
			if len(files) != len(tt.wantFiles) {
				t.Errorf("DetermineScaffoldFiles() returned %d files, want %d", len(files), len(tt.wantFiles))
				return
			}
			for i, file := range files {
				if file != tt.wantFiles[i] {
					t.Errorf("DetermineScaffoldFiles()[%d] = %v, want %v", i, file, tt.wantFiles[i])
				}
			}
		})
	}
}

// TestCheckExistingFiles tests file existence checking
func TestCheckExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some test files
	if err := os.WriteFile(filepath.Join(tmpDir, "phpeek-pm.yaml"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		dir        string
		files      []string
		wantCount  int
		wantFiles  []string
	}{
		{
			name:      "find existing config",
			dir:       tmpDir,
			files:     []string{"config"},
			wantCount: 1,
			wantFiles: []string{"phpeek-pm.yaml"},
		},
		{
			name:      "find existing dockerfile",
			dir:       tmpDir,
			files:     []string{"dockerfile"},
			wantCount: 1,
			wantFiles: []string{"Dockerfile"},
		},
		{
			name:      "docker-compose not found",
			dir:       tmpDir,
			files:     []string{"docker-compose"},
			wantCount: 0,
			wantFiles: []string{},
		},
		{
			name:      "find multiple existing",
			dir:       tmpDir,
			files:     []string{"config", "dockerfile", "docker-compose"},
			wantCount: 2,
			wantFiles: []string{"phpeek-pm.yaml", "Dockerfile"},
		},
		{
			name:      "non-existent directory",
			dir:       "/nonexistent/path",
			files:     []string{"config"},
			wantCount: 0,
			wantFiles: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := CheckExistingFiles(tt.dir, tt.files)
			if len(existing) != tt.wantCount {
				t.Errorf("CheckExistingFiles() returned %d files, want %d: %v", len(existing), tt.wantCount, existing)
			}
		})
	}
}

// TestDetermineWorkdir tests workdir resolution
func TestDetermineWorkdir(t *testing.T) {
	// Save original env
	origWorkdir := os.Getenv("WORKDIR")
	defer os.Setenv("WORKDIR", origWorkdir)

	tests := []struct {
		name        string
		envValue    string
		wantWorkdir string
	}{
		{
			name:        "default when env empty",
			envValue:    "",
			wantWorkdir: "/var/www/html",
		},
		{
			name:        "custom workdir from env",
			envValue:    "/app",
			wantWorkdir: "/app",
		},
		{
			name:        "custom path with spaces",
			envValue:    "/my app/html",
			wantWorkdir: "/my app/html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("WORKDIR", tt.envValue)
			result := DetermineWorkdir()
			if result != tt.wantWorkdir {
				t.Errorf("DetermineWorkdir() = %v, want %v", result, tt.wantWorkdir)
			}
		})
	}
}

// TestResolveAutotuneProfile tests profile resolution from CLI/ENV
func TestResolveAutotuneProfile(t *testing.T) {
	// Save original env
	origProfile := os.Getenv("PHP_FPM_AUTOTUNE_PROFILE")
	defer os.Setenv("PHP_FPM_AUTOTUNE_PROFILE", origProfile)

	tests := []struct {
		name        string
		cliProfile  string
		envProfile  string
		wantProfile string
	}{
		{
			name:        "CLI takes priority",
			cliProfile:  "dev",
			envProfile:  "heavy",
			wantProfile: "dev",
		},
		{
			name:        "ENV used when CLI empty",
			cliProfile:  "",
			envProfile:  "heavy",
			wantProfile: "heavy",
		},
		{
			name:        "empty when both empty",
			cliProfile:  "",
			envProfile:  "",
			wantProfile: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("PHP_FPM_AUTOTUNE_PROFILE", tt.envProfile)
			result := ResolveAutotuneProfile(tt.cliProfile)
			if result != tt.wantProfile {
				t.Errorf("ResolveAutotuneProfile(%q) = %v, want %v", tt.cliProfile, result, tt.wantProfile)
			}
		})
	}
}

// TestResolveConfigPath tests config path resolution
func TestResolveConfigPath(t *testing.T) {
	// Save original env
	origConfig := os.Getenv("PHPEEK_PM_CONFIG")
	defer os.Setenv("PHPEEK_PM_CONFIG", origConfig)

	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		cliPath    string
		envPath    string
		wantSource string
	}{
		{
			name:       "CLI takes priority",
			cliPath:    "/custom/config.yaml",
			envPath:    "/env/config.yaml",
			wantSource: "CLI flag",
		},
		{
			name:       "ENV used when CLI empty",
			cliPath:    "",
			envPath:    filepath.Join(tmpDir, "config.yaml"),
			wantSource: "ENV variable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("PHPEEK_PM_CONFIG", tt.envPath)
			result := ResolveConfigPath(tt.cliPath)
			if result.Source != tt.wantSource {
				t.Errorf("ResolveConfigPath(%q).Source = %v, want %v", tt.cliPath, result.Source, tt.wantSource)
			}
		})
	}

	// Test fallback separately - result depends on whether user config exists
	t.Run("fallback when CLI and ENV empty", func(t *testing.T) {
		os.Setenv("PHPEEK_PM_CONFIG", "")
		result := ResolveConfigPath("")
		// Result should be either "user config", "system config", or "local config"
		validSources := []string{"user config", "system config", "local config"}
		found := false
		for _, valid := range validSources {
			if result.Source == valid {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ResolveConfigPath(\"\").Source = %v, want one of %v", result.Source, validSources)
		}
	})

	// Test that CLI path is returned as-is
	t.Run("CLI path is returned unchanged", func(t *testing.T) {
		os.Setenv("PHPEEK_PM_CONFIG", "")
		testPath := "/custom/path/to/config.yaml"
		result := ResolveConfigPath(testPath)
		if result.Path != testPath {
			t.Errorf("ResolveConfigPath(%q).Path = %v, want %v", testPath, result.Path, testPath)
		}
	})

	// Test that ENV path is returned when set
	t.Run("ENV path is returned when set", func(t *testing.T) {
		envPath := filepath.Join(tmpDir, "env-config.yaml")
		os.Setenv("PHPEEK_PM_CONFIG", envPath)
		result := ResolveConfigPath("")
		if result.Path != envPath {
			t.Errorf("ResolveConfigPath(\"\").Path = %v, want %v", result.Path, envPath)
		}
	})
}

// TestFormatAutotuneOutput tests output formatting
func TestFormatAutotuneOutput(t *testing.T) {
	tests := []struct {
		name            string
		profile         string
		profileSource   string
		threshold       float64
		thresholdSource string
		showThreshold   bool
		wantLineCount   int
	}{
		{
			name:            "without threshold",
			profile:         "dev",
			profileSource:   "CLI flag",
			threshold:       0,
			thresholdSource: "profile default",
			showThreshold:   false,
			wantLineCount:   1,
		},
		{
			name:            "with threshold",
			profile:         "heavy",
			profileSource:   "ENV var",
			threshold:       0.85,
			thresholdSource: "CLI flag",
			showThreshold:   true,
			wantLineCount:   2,
		},
		{
			name:            "show threshold but zero value",
			profile:         "dev",
			profileSource:   "CLI flag",
			threshold:       0,
			thresholdSource: "profile default",
			showThreshold:   true,
			wantLineCount:   1, // Zero threshold not displayed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := FormatAutotuneOutput(tt.profile, tt.profileSource, tt.threshold, tt.thresholdSource, tt.showThreshold)
			if len(lines) != tt.wantLineCount {
				t.Errorf("FormatAutotuneOutput() returned %d lines, want %d: %v", len(lines), tt.wantLineCount, lines)
			}
		})
	}
}

// TestExtractGlobalConfig tests config extraction
func TestExtractGlobalConfig(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			LogLevel:        "debug",
			LogFormat:       "json",
			ShutdownTimeout: 60,
			TracingEnabled:  true,
		},
	}
	// Set metrics and API enabled via pointers
	metricsEnabled := true
	apiEnabled := true
	cfg.Global.MetricsEnabled = &metricsEnabled
	cfg.Global.APIEnabled = &apiEnabled

	result := ExtractGlobalConfig(cfg)

	if result.LogLevel != "debug" {
		t.Errorf("LogLevel = %v, want debug", result.LogLevel)
	}
	if result.LogFormat != "json" {
		t.Errorf("LogFormat = %v, want json", result.LogFormat)
	}
	if result.ShutdownTimeout != 60 {
		t.Errorf("ShutdownTimeout = %v, want 60", result.ShutdownTimeout)
	}
	if !result.MetricsEnabled {
		t.Error("MetricsEnabled = false, want true")
	}
	if !result.APIEnabled {
		t.Error("APIEnabled = false, want true")
	}
	if !result.TracingEnabled {
		t.Error("TracingEnabled = false, want true")
	}
}

// TestGetFilenameHelper tests filename mapping via helper test
func TestGetFilenameHelper(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"config", "phpeek-pm.yaml"},
		{"docker-compose", "docker-compose.yml"},
		{"dockerfile", "Dockerfile"},
		{"unknown", "unknown"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := getFilename(tt.input)
			if result != tt.expected {
				t.Errorf("getFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
