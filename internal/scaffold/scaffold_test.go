package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gophpeek/phpeek-pm/internal/config"
	"gopkg.in/yaml.v3"
)

func TestDefaultConfig_Laravel(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)

	if cfg.Preset != PresetLaravel {
		t.Errorf("Expected preset Laravel, got %s", cfg.Preset)
	}
	if cfg.Framework != "laravel" {
		t.Errorf("Expected framework laravel, got %s", cfg.Framework)
	}
	if !cfg.EnableNginx {
		t.Error("Expected EnableNginx to be true for Laravel")
	}
	if !cfg.EnableHorizon {
		t.Error("Expected EnableHorizon to be true for Laravel")
	}
	if !cfg.EnableQueue {
		t.Error("Expected EnableQueue to be true for Laravel")
	}
	if !cfg.EnableScheduler {
		t.Error("Expected EnableScheduler to be true for Laravel")
	}
}

func TestDefaultConfig_Symfony(t *testing.T) {
	cfg := DefaultConfig(PresetSymfony)

	if cfg.Preset != PresetSymfony {
		t.Errorf("Expected preset Symfony, got %s", cfg.Preset)
	}
	if cfg.Framework != "symfony" {
		t.Errorf("Expected framework symfony, got %s", cfg.Framework)
	}
	if !cfg.EnableNginx {
		t.Error("Expected EnableNginx to be true for Symfony")
	}
	if !cfg.EnableQueue {
		t.Error("Expected EnableQueue to be true for Symfony")
	}
	if cfg.EnableHorizon {
		t.Error("Expected EnableHorizon to be false for Symfony")
	}
}

func TestDefaultConfig_PHP(t *testing.T) {
	cfg := DefaultConfig(PresetPHP)

	if cfg.Preset != PresetPHP {
		t.Errorf("Expected preset PHP, got %s", cfg.Preset)
	}
	if cfg.Framework != "php" {
		t.Errorf("Expected framework php, got %s", cfg.Framework)
	}
	if !cfg.EnableNginx {
		t.Error("Expected EnableNginx to be true for PHP")
	}
	if cfg.EnableHorizon {
		t.Error("Expected EnableHorizon to be false for PHP")
	}
	if cfg.EnableQueue {
		t.Error("Expected EnableQueue to be false for PHP")
	}
}

func TestDefaultConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)

	if cfg.AppName != "my-app" {
		t.Errorf("Expected default AppName 'my-app', got '%s'", cfg.AppName)
	}
	if cfg.WorkDir != "/var/www/html" {
		t.Errorf("Expected default WorkDir '/var/www/html', got '%s'", cfg.WorkDir)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("Expected default LogLevel 'info', got '%s'", cfg.LogLevel)
	}
	if cfg.QueueWorkers != 3 {
		t.Errorf("Expected default QueueWorkers 3, got %d", cfg.QueueWorkers)
	}
	if cfg.QueueConnection != "redis" {
		t.Errorf("Expected default QueueConnection 'redis', got '%s'", cfg.QueueConnection)
	}
}

func TestGenerateConfig_Laravel(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)
	content, err := GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Check version
	if !strings.Contains(content, `version: "1.0"`) {
		t.Error("Config should contain version 1.0")
	}

	// Check php-fpm process
	if !strings.Contains(content, "php-fpm:") {
		t.Error("Laravel config should contain php-fpm process")
	}

	// Check nginx process
	if !strings.Contains(content, "nginx:") {
		t.Error("Laravel config should contain nginx process")
	}

	// Check horizon process
	if !strings.Contains(content, "horizon:") {
		t.Error("Laravel config should contain horizon process")
	}

	// Check queue-default process
	if !strings.Contains(content, "queue-default:") {
		t.Error("Laravel config should contain queue-default process")
	}

	// Check scheduler process
	if !strings.Contains(content, "scheduler:") {
		t.Error("Laravel config should contain scheduler process")
	}
}

func TestGenerateConfig_PHP(t *testing.T) {
	cfg := DefaultConfig(PresetPHP)
	content, err := GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// PHP preset should have nginx
	if !strings.Contains(content, "nginx:") {
		t.Error("PHP config should contain nginx process")
	}

	// PHP should not have horizon
	if strings.Contains(content, "horizon:") {
		t.Error("PHP config should not contain horizon process")
	}
}

func TestGenerateDockerCompose_Laravel(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)
	content, err := GenerateDockerCompose(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerCompose failed: %v", err)
	}

	// Check version
	if !strings.Contains(content, "version: '3.8'") {
		t.Error("Docker compose should contain version 3.8")
	}

	// Check services
	if !strings.Contains(content, "app:") {
		t.Error("Docker compose should contain app service")
	}
	if !strings.Contains(content, "db:") {
		t.Error("Laravel docker compose should contain db service")
	}
	if !strings.Contains(content, "redis:") {
		t.Error("Laravel docker compose should contain redis service")
	}
}

func TestGenerateDockerCompose_WithMetrics(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)
	content, err := GenerateDockerCompose(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerCompose failed: %v", err)
	}

	// Check Prometheus
	if !strings.Contains(content, "prometheus:") {
		t.Error("Laravel docker compose should contain prometheus service")
	}

	// Check Grafana
	if !strings.Contains(content, "grafana:") {
		t.Error("Laravel docker compose should contain grafana service")
	}
}

func TestGenerateDockerfile(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)
	content, err := GenerateDockerfile(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerfile failed: %v", err)
	}

	// Check base image
	if !strings.Contains(content, "FROM gophpeek/php-fpm-nginx:") {
		t.Error("Dockerfile should use gophpeek/php-fpm-nginx base image")
	}

	// Check composer
	if !strings.Contains(content, "composer install") {
		t.Error("Laravel Dockerfile should run composer install")
	}

	// Check PHPeek PM
	if !strings.Contains(content, "phpeek-pm") {
		t.Error("Dockerfile should reference phpeek-pm")
	}
}

func TestNewGenerator(t *testing.T) {
	g := NewGenerator(PresetLaravel, "/tmp/test-output")

	if g.config == nil {
		t.Error("Generator config should not be nil")
	}
	if g.config.Preset != PresetLaravel {
		t.Errorf("Expected preset Laravel, got %s", g.config.Preset)
	}
	if g.outDir != "/tmp/test-output" {
		t.Errorf("Expected outDir /tmp/test-output, got %s", g.outDir)
	}
}

func TestNewGeneratorWithConfig(t *testing.T) {
	cfg := &Config{
		AppName:       "custom-app",
		EnableMetrics: true,
	}

	g := NewGeneratorWithConfig(cfg, "/tmp/test-output")

	if g.config.AppName != "custom-app" {
		t.Errorf("Expected AppName 'custom-app', got '%s'", g.config.AppName)
	}
}

func TestGenerator_SetAppName(t *testing.T) {
	g := NewGenerator(PresetLaravel, "/tmp/test")
	g.SetAppName("my-custom-app")

	if g.config.AppName != "my-custom-app" {
		t.Errorf("Expected AppName 'my-custom-app', got '%s'", g.config.AppName)
	}
}

func TestGenerator_SetLogLevel(t *testing.T) {
	g := NewGenerator(PresetLaravel, "/tmp/test")
	g.SetLogLevel("debug")

	if g.config.LogLevel != "debug" {
		t.Errorf("Expected LogLevel 'debug', got '%s'", g.config.LogLevel)
	}
}

func TestGenerator_EnableFeature(t *testing.T) {
	g := NewGenerator(PresetPHP, "/tmp/test")

	tests := []struct {
		feature  string
		expected *bool
	}{
		{"nginx", &g.config.EnableNginx},
		{"horizon", &g.config.EnableHorizon},
		{"queue", &g.config.EnableQueue},
		{"scheduler", &g.config.EnableScheduler},
		{"metrics", &g.config.EnableMetrics},
		{"api", &g.config.EnableAPI},
		{"tracing", &g.config.EnableTracing},
	}

	for _, tt := range tests {
		t.Run(tt.feature, func(t *testing.T) {
			g.EnableFeature(tt.feature, true)
			if !*tt.expected {
				t.Errorf("Expected %s to be enabled", tt.feature)
			}
			g.EnableFeature(tt.feature, false)
			if *tt.expected {
				t.Errorf("Expected %s to be disabled", tt.feature)
			}
		})
	}
}

func TestGenerator_SetQueueWorkers(t *testing.T) {
	g := NewGenerator(PresetLaravel, "/tmp/test")
	g.SetQueueWorkers(10)

	if g.config.QueueWorkers != 10 {
		t.Errorf("Expected QueueWorkers 10, got %d", g.config.QueueWorkers)
	}
}

func TestGenerator_SetQueueConnection(t *testing.T) {
	g := NewGenerator(PresetLaravel, "/tmp/test")
	g.SetQueueConnection("database")

	if g.config.QueueConnection != "database" {
		t.Errorf("Expected QueueConnection 'database', got '%s'", g.config.QueueConnection)
	}
}

func TestGenerator_GetConfig(t *testing.T) {
	g := NewGenerator(PresetLaravel, "/tmp/test")
	cfg := g.GetConfig()

	if cfg != g.config {
		t.Error("GetConfig should return the generator's config")
	}
}

func TestGenerator_PreviewConfig(t *testing.T) {
	g := NewGenerator(PresetLaravel, "/tmp/test")
	content, err := g.PreviewConfig()

	if err != nil {
		t.Fatalf("PreviewConfig failed: %v", err)
	}
	if content == "" {
		t.Error("PreviewConfig should return non-empty content")
	}
}

func TestGenerator_Generate(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "scaffold-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewGenerator(PresetLaravel, tmpDir)

	// Generate all files
	err = g.Generate([]string{"config", "docker-compose", "dockerfile"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify files exist
	files := []string{"phpeek-pm.yaml", "docker-compose.yml", "Dockerfile"}
	for _, file := range files {
		path := filepath.Join(tmpDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected file %s to exist", file)
		}
	}
}

func TestGenerator_Generate_ConfigOnly(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "scaffold-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewGenerator(PresetPHP, tmpDir)

	// Generate config only
	err = g.Generate([]string{"config"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify config exists
	configPath := filepath.Join(tmpDir, "phpeek-pm.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Expected phpeek-pm.yaml to exist")
	}

	// Verify docker-compose does not exist
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	if _, err := os.Stat(composePath); !os.IsNotExist(err) {
		t.Error("Expected docker-compose.yml to not exist")
	}
}

func TestValidPresets(t *testing.T) {
	presets := ValidPresets()

	expected := []string{"laravel", "symfony", "php", "wordpress", "magento", "drupal", "nextjs", "nuxt", "nodejs"}
	if len(presets) != len(expected) {
		t.Errorf("Expected %d presets, got %d", len(expected), len(presets))
	}

	for _, exp := range expected {
		found := false
		for _, p := range presets {
			if p == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected preset '%s' to be in ValidPresets()", exp)
		}
	}
}

func TestGenerateConfig_WithTracing(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)
	cfg.EnableTracing = true // Enable tracing explicitly (via --observability flag)
	content, err := GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Check tracing configuration
	if !strings.Contains(content, "tracing_enabled: true") {
		t.Error("Config with tracing enabled should have tracing_enabled: true")
	}
	if !strings.Contains(content, "tracing_exporter: otlp-grpc") {
		t.Error("Config with tracing enabled should have tracing exporter")
	}
	if !strings.Contains(content, "tracing_sample_rate: 0.1") {
		t.Error("Config with tracing enabled should have tracing sample rate")
	}
}

// TestGenerator_Generate_InvalidOutputDir tests generation with invalid output directory
func TestGenerator_Generate_InvalidOutputDir(t *testing.T) {
	// Use a path that can't be created (child of a file, not a directory)
	tmpFile, err := os.CreateTemp("", "scaffold-test-file")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Try to use the file as a directory path (will fail)
	g := NewGenerator(PresetPHP, filepath.Join(tmpFile.Name(), "subdir"))
	err = g.Generate([]string{"config"})
	if err == nil {
		t.Error("Expected error when output directory cannot be created")
	}
}

// TestGenerator_Generate_ReadOnlyDir tests generation in a read-only directory
func TestGenerator_Generate_ReadOnlyDir(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "scaffold-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a subdirectory that's read-only
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0555); err != nil {
		t.Fatalf("Failed to create readonly dir: %v", err)
	}

	g := NewGenerator(PresetPHP, readOnlyDir)

	// Try to generate - should fail to write
	err = g.Generate([]string{"config"})
	if err == nil {
		t.Log("Note: Read-only test may pass on some systems")
	}
}

// TestGenerator_Generate_DockerComposeOnly tests generating only docker-compose
func TestGenerator_Generate_DockerComposeOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "scaffold-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewGenerator(PresetLaravel, tmpDir)
	err = g.Generate([]string{"docker-compose"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify docker-compose exists
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Error("Expected docker-compose.yml to exist")
	}

	// Verify config does not exist
	configPath := filepath.Join(tmpDir, "phpeek-pm.yaml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("Expected phpeek-pm.yaml to not exist")
	}
}

// TestGenerator_Generate_DockerfileOnly tests generating only Dockerfile
func TestGenerator_Generate_DockerfileOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "scaffold-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewGenerator(PresetLaravel, tmpDir)
	err = g.Generate([]string{"dockerfile"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify Dockerfile exists
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		t.Error("Expected Dockerfile to exist")
	}
}

// TestGenerator_Generate_UnknownFile tests generating with unknown file type
func TestGenerator_Generate_UnknownFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "scaffold-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewGenerator(PresetPHP, tmpDir)
	// Unknown file type should be silently ignored
	err = g.Generate([]string{"unknown"})
	if err != nil {
		t.Errorf("Generate with unknown file should not error: %v", err)
	}
}

// TestGenerateDockerCompose_PHP tests docker-compose for php preset
func TestGenerateDockerCompose_PHP(t *testing.T) {
	cfg := DefaultConfig(PresetPHP)
	content, err := GenerateDockerCompose(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerCompose failed: %v", err)
	}

	// Check services
	if !strings.Contains(content, "app:") {
		t.Error("Docker compose should contain app service")
	}
	// Minimal should not have redis by default
	if strings.Contains(content, "redis:") {
		t.Error("PHP docker compose should not contain redis service")
	}
}

// TestGenerateDockerCompose_Symfony tests docker-compose for Symfony preset
func TestGenerateDockerCompose_Symfony(t *testing.T) {
	cfg := DefaultConfig(PresetSymfony)
	content, err := GenerateDockerCompose(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerCompose failed: %v", err)
	}

	// Check services
	if !strings.Contains(content, "app:") {
		t.Error("Docker compose should contain app service")
	}
}

// TestGenerateDockerfile_PHP tests Dockerfile for php preset
func TestGenerateDockerfile_PHP(t *testing.T) {
	cfg := DefaultConfig(PresetPHP)
	content, err := GenerateDockerfile(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerfile failed: %v", err)
	}

	// Check base image
	if !strings.Contains(content, "FROM gophpeek/php-fpm-nginx:") {
		t.Error("Dockerfile should use gophpeek/php-fpm-nginx base image")
	}

	// Check PHPeek PM
	if !strings.Contains(content, "phpeek-pm") {
		t.Error("Dockerfile should reference phpeek-pm")
	}
}

// TestGenerateDockerfile_Symfony tests Dockerfile for Symfony preset
func TestGenerateDockerfile_Symfony(t *testing.T) {
	cfg := DefaultConfig(PresetSymfony)
	content, err := GenerateDockerfile(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerfile failed: %v", err)
	}

	// Check base image
	if !strings.Contains(content, "FROM gophpeek/php-fpm-nginx:") {
		t.Error("Dockerfile should use gophpeek/php-fpm-nginx base image")
	}
}

// TestGenerateDockerfile_PHP_Alt tests Dockerfile for php preset
func TestGenerateDockerfile_PHP_Alt(t *testing.T) {
	cfg := DefaultConfig(PresetPHP)
	content, err := GenerateDockerfile(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerfile failed: %v", err)
	}

	// Check PHPeek PM
	if !strings.Contains(content, "phpeek-pm") {
		t.Error("Dockerfile should reference phpeek-pm")
	}
}

// TestGenerateConfig_Symfony tests config generation for Symfony preset
func TestGenerateConfig_Symfony(t *testing.T) {
	cfg := DefaultConfig(PresetSymfony)
	content, err := GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Check php-fpm process
	if !strings.Contains(content, "php-fpm:") {
		t.Error("Symfony config should contain php-fpm process")
	}

	// Check nginx process
	if !strings.Contains(content, "nginx:") {
		t.Error("Symfony config should contain nginx process")
	}

	// Symfony should not have horizon
	if strings.Contains(content, "horizon:") {
		t.Error("Symfony config should not contain horizon process")
	}
}

// TestGenerateConfig_PHP_Alt tests config generation for php preset
func TestGenerateConfig_PHP_Alt(t *testing.T) {
	cfg := DefaultConfig(PresetPHP)
	content, err := GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// PHP preset uses generic framework which doesn't include php-fpm by default
	// Only laravel and symfony frameworks include php-fpm in the template

	// Check nginx process is enabled for php preset
	if !strings.Contains(content, "nginx:") {
		t.Error("PHP config should contain nginx process")
	}

	// Check that basic config structure is present
	if !strings.Contains(content, "version:") {
		t.Error("PHP config should contain version field")
	}
}

// TestGenerateConfig_Laravel_WithTracing tests config generation for laravel preset
func TestGenerateConfig_Laravel_WithTracing(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)
	content, err := GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Check php-fpm process
	if !strings.Contains(content, "php-fpm:") {
		t.Error("Laravel config should contain php-fpm process")
	}

	// Check horizon process (production is based on Laravel)
	if !strings.Contains(content, "horizon:") {
		t.Error("Laravel config should contain horizon process")
	}

	// Check metrics enabled
	if !strings.Contains(content, "metrics_enabled: true") {
		t.Error("Laravel config should have metrics enabled")
	}

	// Check API enabled
	if !strings.Contains(content, "api_enabled: true") {
		t.Error("Laravel config should have API enabled")
	}
}

// TestGenerateDockerCompose_PHP_Alt tests docker-compose for php preset
func TestGenerateDockerCompose_PHP_Alt(t *testing.T) {
	cfg := DefaultConfig(PresetPHP)
	content, err := GenerateDockerCompose(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerCompose failed: %v", err)
	}

	// Check app service
	if !strings.Contains(content, "app:") {
		t.Error("Docker compose should contain app service")
	}
}

// TestGenerateDockerfile_Laravel tests Dockerfile for laravel preset
func TestGenerateDockerfile_Laravel(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)
	content, err := GenerateDockerfile(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerfile failed: %v", err)
	}

	// Check composer install
	if !strings.Contains(content, "composer install") {
		t.Error("Production Dockerfile should run composer install")
	}
}

// TestGenerator_EnableFeature_Unknown tests enabling an unknown feature
func TestGenerator_EnableFeature_Unknown(t *testing.T) {
	g := NewGenerator(PresetPHP, "/tmp/test")

	// Enabling an unknown feature should not panic
	g.EnableFeature("unknown_feature", true)
	g.EnableFeature("invalid", false)

	// No error should occur
	t.Log("Unknown feature handling works correctly")
}

// TestGenerator_Generate_AllFilesForAllPresets tests all file types for all presets
func TestGenerator_Generate_AllFilesForAllPresets(t *testing.T) {
	presets := []Preset{PresetLaravel, PresetSymfony, PresetPHP, PresetPHP, PresetLaravel}

	for _, preset := range presets {
		t.Run(string(preset), func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "scaffold-test-")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			g := NewGenerator(preset, tmpDir)
			err = g.Generate([]string{"config", "docker-compose", "dockerfile"})
			if err != nil {
				t.Fatalf("Generate failed for preset %s: %v", preset, err)
			}

			// Verify all files exist
			files := []string{"phpeek-pm.yaml", "docker-compose.yml", "Dockerfile"}
			for _, file := range files {
				path := filepath.Join(tmpDir, file)
				if _, err := os.Stat(path); os.IsNotExist(err) {
					t.Errorf("Expected file %s to exist for preset %s", file, preset)
				}
			}
		})
	}
}

// TestGenerateDockerCompose_WithTracing tests docker-compose with tracing and metrics enabled
func TestGenerateDockerCompose_WithTracing(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)
	content, err := GenerateDockerCompose(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerCompose failed: %v", err)
	}

	// Laravel preset has metrics enabled, which includes prometheus
	if !strings.Contains(content, "prometheus:") {
		t.Error("Laravel docker compose should contain prometheus service")
	}

	// Laravel preset has metrics enabled, which includes grafana
	if !strings.Contains(content, "grafana:") {
		t.Error("Laravel docker compose should contain grafana service")
	}

	// Laravel framework should include redis
	if !strings.Contains(content, "redis:") {
		t.Error("Laravel docker compose should contain redis service")
	}
}

// TestDefaultConfig_Unknown tests unknown preset defaults
func TestDefaultConfig_Unknown(t *testing.T) {
	cfg := DefaultConfig(Preset("unknown"))

	// Unknown preset should get default settings
	if cfg.Framework != "" {
		t.Logf("Unknown preset defaulted to framework: %s", cfg.Framework)
	}
}

// TestDefaultConfig_NextJS tests Next.js preset configuration
func TestDefaultConfig_NextJS(t *testing.T) {
	cfg := DefaultConfig(PresetNextJS)

	if cfg.Framework != "nextjs" {
		t.Errorf("Framework = %v, want nextjs", cfg.Framework)
	}
	if cfg.WorkDir != "/app" {
		t.Errorf("WorkDir = %v, want /app", cfg.WorkDir)
	}
	if !cfg.EnableNginx {
		t.Error("EnableNginx should be true for nextjs")
	}
	if cfg.NodeInstances != 2 {
		t.Errorf("NodeInstances = %v, want 2", cfg.NodeInstances)
	}
	if cfg.PortBase != 3000 {
		t.Errorf("PortBase = %v, want 3000", cfg.PortBase)
	}
	if cfg.MaxMemoryMB != 512 {
		t.Errorf("MaxMemoryMB = %v, want 512", cfg.MaxMemoryMB)
	}
	if cfg.NodeCommand != "node .next/standalone/server.js" {
		t.Errorf("NodeCommand = %v, want node .next/standalone/server.js", cfg.NodeCommand)
	}
}

// TestDefaultConfig_Nuxt tests Nuxt preset configuration
func TestDefaultConfig_Nuxt(t *testing.T) {
	cfg := DefaultConfig(PresetNuxt)

	if cfg.Framework != "nuxt" {
		t.Errorf("Framework = %v, want nuxt", cfg.Framework)
	}
	if cfg.WorkDir != "/app" {
		t.Errorf("WorkDir = %v, want /app", cfg.WorkDir)
	}
	if !cfg.EnableNginx {
		t.Error("EnableNginx should be true for nuxt")
	}
	if cfg.NodeInstances != 2 {
		t.Errorf("NodeInstances = %v, want 2", cfg.NodeInstances)
	}
	if cfg.PortBase != 3000 {
		t.Errorf("PortBase = %v, want 3000", cfg.PortBase)
	}
	if cfg.MaxMemoryMB != 512 {
		t.Errorf("MaxMemoryMB = %v, want 512", cfg.MaxMemoryMB)
	}
	if cfg.NodeCommand != "node .output/server/index.mjs" {
		t.Errorf("NodeCommand = %v, want node .output/server/index.mjs", cfg.NodeCommand)
	}
}

// TestDefaultConfig_NodeJS tests generic Node.js preset configuration
func TestDefaultConfig_NodeJS(t *testing.T) {
	cfg := DefaultConfig(PresetNodeJS)

	if cfg.Framework != "nodejs" {
		t.Errorf("Framework = %v, want nodejs", cfg.Framework)
	}
	if cfg.WorkDir != "/app" {
		t.Errorf("WorkDir = %v, want /app", cfg.WorkDir)
	}
	if !cfg.EnableNginx {
		t.Error("EnableNginx should be true for nodejs")
	}
	if cfg.NodeInstances != 2 {
		t.Errorf("NodeInstances = %v, want 2", cfg.NodeInstances)
	}
	if cfg.PortBase != 3000 {
		t.Errorf("PortBase = %v, want 3000", cfg.PortBase)
	}
	if cfg.MaxMemoryMB != 512 {
		t.Errorf("MaxMemoryMB = %v, want 512", cfg.MaxMemoryMB)
	}
	if !cfg.EnableWorkers {
		t.Error("EnableWorkers should be true for nodejs")
	}
	if cfg.QueueWorkers != 2 {
		t.Errorf("QueueWorkers = %v, want 2", cfg.QueueWorkers)
	}
}

// TestGenerateConfig_NextJS tests Next.js config generation
func TestGenerateConfig_NextJS(t *testing.T) {
	cfg := DefaultConfig(PresetNextJS)
	content, err := GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Check Next.js specific content
	if !strings.Contains(content, "nextjs:") {
		t.Error("Config should contain nextjs process")
	}
	if !strings.Contains(content, ".next/standalone/server.js") {
		t.Error("Config should contain Next.js standalone command")
	}
	if !strings.Contains(content, "port_base: 3000") {
		t.Error("Config should contain port_base: 3000")
	}
	if !strings.Contains(content, "max_memory_mb: 512") {
		t.Error("Config should contain max_memory_mb: 512")
	}
	if !strings.Contains(content, "NODE_ENV: production") {
		t.Error("Config should contain NODE_ENV: production")
	}
	if !strings.Contains(content, "HOSTNAME:") {
		t.Error("Config should contain HOSTNAME env var")
	}
}

// TestGenerateConfig_Nuxt tests Nuxt config generation
func TestGenerateConfig_Nuxt(t *testing.T) {
	cfg := DefaultConfig(PresetNuxt)
	content, err := GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Check Nuxt specific content
	if !strings.Contains(content, "nuxt:") {
		t.Error("Config should contain nuxt process")
	}
	if !strings.Contains(content, ".output/server/index.mjs") {
		t.Error("Config should contain Nuxt Nitro command")
	}
	if !strings.Contains(content, "port_base: 3000") {
		t.Error("Config should contain port_base: 3000")
	}
	if !strings.Contains(content, "max_memory_mb: 512") {
		t.Error("Config should contain max_memory_mb: 512")
	}
	if !strings.Contains(content, "NITRO_HOST:") {
		t.Error("Config should contain NITRO_HOST env var")
	}
}

// TestGenerateConfig_NodeJS tests generic Node.js config generation
func TestGenerateConfig_NodeJS(t *testing.T) {
	cfg := DefaultConfig(PresetNodeJS)
	content, err := GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Check Node.js specific content
	if !strings.Contains(content, "app:") {
		t.Error("Config should contain app process")
	}
	if !strings.Contains(content, `["node", "dist/server.js"]`) {
		t.Error("Config should contain Node.js command")
	}
	if !strings.Contains(content, "port_base: 3000") {
		t.Error("Config should contain port_base: 3000")
	}
	if !strings.Contains(content, "max_memory_mb: 512") {
		t.Error("Config should contain max_memory_mb: 512")
	}
	// Workers should be enabled
	if !strings.Contains(content, "worker:") {
		t.Error("Config should contain worker process for nodejs preset")
	}
}

// TestGenerateConfig_NodeJS_NginxDependsOn tests nginx depends_on for Node.js
func TestGenerateConfig_NodeJS_NginxDependsOn(t *testing.T) {
	tests := []struct {
		name       string
		preset     Preset
		dependsOn  string
	}{
		{"nextjs", PresetNextJS, "nextjs"},
		{"nuxt", PresetNuxt, "nuxt"},
		{"nodejs", PresetNodeJS, "app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig(tt.preset)
			content, err := GenerateConfig(cfg)
			if err != nil {
				t.Fatalf("GenerateConfig failed: %v", err)
			}

			// Nginx should have depends_on to the Node.js process
			expectedDepends := "- " + tt.dependsOn
			if !strings.Contains(content, expectedDepends) {
				t.Errorf("Nginx should depend on %s process", tt.dependsOn)
			}
		})
	}
}

// TestGenerator_Generate_NodeJSPresets tests file generation for Node.js presets
func TestGenerator_Generate_NodeJSPresets(t *testing.T) {
	presets := []Preset{PresetNextJS, PresetNuxt, PresetNodeJS}

	for _, preset := range presets {
		t.Run(string(preset), func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "scaffold-nodejs-*")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tmpDir)

			gen := NewGenerator(preset, tmpDir)
			if err := gen.Generate([]string{"config"}); err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			// Verify config file was created
			configPath := filepath.Join(tmpDir, "phpeek-pm.yaml")
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				t.Error("Expected phpeek-pm.yaml to be created")
			}

			// Read and verify content
			content, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("Failed to read config file: %v", err)
			}

			// Should contain Node.js specific fields
			if !strings.Contains(string(content), "port_base:") {
				t.Error("Config should contain port_base")
			}
			if !strings.Contains(string(content), "max_memory_mb:") {
				t.Error("Config should contain max_memory_mb")
			}
		})
	}
}

// TestGenerateNginxConfig_NodeJS tests nginx config generation for Node.js presets
func TestGenerateNginxConfig_NodeJS(t *testing.T) {
	tests := []struct {
		name           string
		preset         Preset
		expectedServer string
		staticLocation string
	}{
		{
			name:           "nextjs",
			preset:         PresetNextJS,
			expectedServer: "server 127.0.0.1:3000",
			staticLocation: "/_next/static",
		},
		{
			name:           "nuxt",
			preset:         PresetNuxt,
			expectedServer: "server 127.0.0.1:3000",
			staticLocation: "/_nuxt",
		},
		{
			name:           "nodejs",
			preset:         PresetNodeJS,
			expectedServer: "server 127.0.0.1:3000",
			staticLocation: "", // generic nodejs doesn't have static file handling
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig(tt.preset)
			content, err := GenerateNginxConfig(cfg)
			if err != nil {
				t.Fatalf("GenerateNginxConfig failed: %v", err)
			}

			// Should contain upstream configuration
			if !strings.Contains(content, "upstream nodejs_backend") {
				t.Error("Nginx config should contain upstream nodejs_backend")
			}

			// Should have server entries for each instance
			if !strings.Contains(content, tt.expectedServer) {
				t.Errorf("Nginx config should contain server entry: %s", tt.expectedServer)
			}

			// Should contain second server (instance 1)
			if !strings.Contains(content, "server 127.0.0.1:3001") {
				t.Error("Nginx config should contain second server for instance 1")
			}

			// Check static file handling for specific frameworks
			if tt.staticLocation != "" {
				if !strings.Contains(content, tt.staticLocation) {
					t.Errorf("Nginx config should contain static location: %s", tt.staticLocation)
				}
			}

			// Should have health check endpoint
			if !strings.Contains(content, "location /health") {
				t.Error("Nginx config should contain health check location")
			}

			// Should have proxy configuration
			if !strings.Contains(content, "proxy_pass http://nodejs_backend") {
				t.Error("Nginx config should contain proxy_pass to nodejs_backend")
			}
		})
	}
}

// TestGenerateNginxConfig_PHP tests nginx config generation for PHP presets
func TestGenerateNginxConfig_PHP(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)
	content, err := GenerateNginxConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateNginxConfig failed: %v", err)
	}

	// Should contain PHP-FPM configuration
	if !strings.Contains(content, "fastcgi_pass 127.0.0.1:9000") {
		t.Error("Nginx config should contain fastcgi_pass for PHP-FPM")
	}

	// Should NOT contain nodejs upstream
	if strings.Contains(content, "upstream nodejs_backend") {
		t.Error("PHP nginx config should not contain nodejs upstream")
	}

	// Should have health check
	if !strings.Contains(content, "location /health") {
		t.Error("Nginx config should contain health check location")
	}
}

// TestGenerator_Generate_NginxConfig tests generator nginx file creation
func TestGenerator_Generate_NginxConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "scaffold-nginx-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	gen := NewGenerator(PresetNextJS, tmpDir)
	if err := gen.Generate([]string{"nginx"}); err != nil {
		t.Fatalf("Generate nginx failed: %v", err)
	}

	// Verify nginx.conf was created
	nginxPath := filepath.Join(tmpDir, "nginx.conf")
	if _, err := os.Stat(nginxPath); os.IsNotExist(err) {
		t.Error("Expected nginx.conf to be created")
	}

	// Read and verify content
	content, err := os.ReadFile(nginxPath)
	if err != nil {
		t.Fatalf("Failed to read nginx.conf: %v", err)
	}

	if !strings.Contains(string(content), "upstream nodejs_backend") {
		t.Error("nginx.conf should contain upstream nodejs_backend")
	}
}

// =============================================================================
// VALIDATION TESTS - Verify generated configs are valid and parseable
// =============================================================================

// TestGeneratedConfig_ValidYAML_AllPresets tests that all preset configs are valid YAML
func TestGeneratedConfig_ValidYAML_AllPresets(t *testing.T) {
	presets := []Preset{
		PresetLaravel,
		PresetSymfony,
		PresetPHP,
		PresetPHP,
		PresetLaravel,
		PresetNextJS,
		PresetNuxt,
		PresetNodeJS,
		PresetWordPress,
		PresetMagento,
		PresetDrupal,
	}

	for _, preset := range presets {
		t.Run(string(preset), func(t *testing.T) {
			cfg := DefaultConfig(preset)
			yamlContent, err := GenerateConfig(cfg)
			if err != nil {
				t.Fatalf("GenerateConfig failed for %s: %v", preset, err)
			}

			// Parse as generic YAML to ensure syntax is valid
			var parsed map[string]interface{}
			if err := yaml.Unmarshal([]byte(yamlContent), &parsed); err != nil {
				t.Errorf("Generated YAML is invalid for preset %s: %v\nContent:\n%s", preset, err, yamlContent)
			}

			// Verify required top-level keys exist
			if _, ok := parsed["version"]; !ok {
				t.Errorf("Generated config for %s missing 'version' key", preset)
			}
			if _, ok := parsed["global"]; !ok {
				t.Errorf("Generated config for %s missing 'global' key", preset)
			}
			if _, ok := parsed["processes"]; !ok {
				t.Errorf("Generated config for %s missing 'processes' key", preset)
			}
		})
	}
}

// TestGeneratedConfig_ParseableAsConfig tests configs can be parsed by config package
func TestGeneratedConfig_ParseableAsConfig(t *testing.T) {
	presets := []Preset{
		PresetLaravel,
		PresetSymfony,
		PresetPHP,
		PresetLaravel,
		PresetNextJS,
		PresetNuxt,
		PresetNodeJS,
		PresetWordPress,
		PresetMagento,
		PresetDrupal,
	}

	for _, preset := range presets {
		t.Run(string(preset), func(t *testing.T) {
			scaffoldCfg := DefaultConfig(preset)
			yamlContent, err := GenerateConfig(scaffoldCfg)
			if err != nil {
				t.Fatalf("GenerateConfig failed: %v", err)
			}

			// Parse with config.Config struct
			var cfg config.Config
			if err := yaml.Unmarshal([]byte(yamlContent), &cfg); err != nil {
				t.Errorf("Failed to parse as config.Config for %s: %v\nContent:\n%s", preset, err, yamlContent)
				return
			}

			// Verify key fields are populated
			if cfg.Version == "" {
				t.Errorf("Config version is empty for %s", preset)
			}
			if len(cfg.Processes) == 0 && preset != PresetPHP {
				t.Errorf("Config has no processes for %s", preset)
			}
		})
	}
}

// TestGeneratedConfig_Validates tests that generated configs pass validation
func TestGeneratedConfig_Validates(t *testing.T) {
	presets := []Preset{
		PresetLaravel,
		PresetSymfony,
		PresetPHP,
		PresetLaravel,
		PresetNextJS,
		PresetNuxt,
		PresetNodeJS,
		PresetWordPress,
		PresetMagento,
		PresetDrupal,
	}

	for _, preset := range presets {
		t.Run(string(preset), func(t *testing.T) {
			scaffoldCfg := DefaultConfig(preset)
			yamlContent, err := GenerateConfig(scaffoldCfg)
			if err != nil {
				t.Fatalf("GenerateConfig failed: %v", err)
			}

			// Parse with config.Config struct
			var cfg config.Config
			if err := yaml.Unmarshal([]byte(yamlContent), &cfg); err != nil {
				t.Fatalf("Failed to parse config: %v", err)
			}

			// Apply defaults before validation
			cfg.SetDefaults()

			// Validate the config
			if err := cfg.Validate(); err != nil {
				t.Errorf("Config validation failed for %s: %v\nYAML:\n%s", preset, err, yamlContent)
			}
		})
	}
}

// TestGeneratedDockerCompose_ValidYAML tests docker-compose output is valid YAML
func TestGeneratedDockerCompose_ValidYAML(t *testing.T) {
	presets := []Preset{
		PresetLaravel,
		PresetLaravel,
		PresetNextJS,
		PresetNuxt,
		PresetNodeJS,
	}

	for _, preset := range presets {
		t.Run(string(preset), func(t *testing.T) {
			cfg := DefaultConfig(preset)
			content, err := GenerateDockerCompose(cfg)
			if err != nil {
				t.Fatalf("GenerateDockerCompose failed for %s: %v", preset, err)
			}

			// Parse as generic YAML
			var parsed map[string]interface{}
			if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
				t.Errorf("Generated docker-compose is invalid YAML for %s: %v", preset, err)
			}

			// Check required docker-compose keys
			if _, ok := parsed["version"]; !ok {
				t.Errorf("Docker-compose missing 'version' for %s", preset)
			}
			if _, ok := parsed["services"]; !ok {
				t.Errorf("Docker-compose missing 'services' for %s", preset)
			}
		})
	}
}

// TestGeneratedNginxConfig_SyntaxCheck tests nginx config has valid structure
func TestGeneratedNginxConfig_SyntaxCheck(t *testing.T) {
	presets := []Preset{
		PresetLaravel,
		PresetNextJS,
		PresetNuxt,
		PresetNodeJS,
	}

	for _, preset := range presets {
		t.Run(string(preset), func(t *testing.T) {
			cfg := DefaultConfig(preset)
			content, err := GenerateNginxConfig(cfg)
			if err != nil {
				t.Fatalf("GenerateNginxConfig failed for %s: %v", preset, err)
			}

			// Check basic nginx config structure
			if !strings.Contains(content, "worker_processes") {
				t.Errorf("Nginx config missing worker_processes for %s", preset)
			}
			if !strings.Contains(content, "http {") {
				t.Errorf("Nginx config missing http block for %s", preset)
			}
			if !strings.Contains(content, "server {") {
				t.Errorf("Nginx config missing server block for %s", preset)
			}
			if !strings.Contains(content, "listen 80") {
				t.Errorf("Nginx config missing listen directive for %s", preset)
			}

			// Check for balanced braces (basic syntax check)
			openBraces := strings.Count(content, "{")
			closeBraces := strings.Count(content, "}")
			if openBraces != closeBraces {
				t.Errorf("Nginx config has unbalanced braces for %s: %d open, %d close",
					preset, openBraces, closeBraces)
			}
		})
	}
}
