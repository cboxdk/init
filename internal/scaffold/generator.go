package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
)

// Generator generates project scaffolding
type Generator struct {
	config *Config
	outDir string
}

// NewGenerator creates a new scaffold generator
func NewGenerator(preset Preset, outDir string) *Generator {
	return &Generator{
		config: DefaultConfig(preset),
		outDir: outDir,
	}
}

// NewGeneratorWithConfig creates generator with custom config
func NewGeneratorWithConfig(config *Config, outDir string) *Generator {
	return &Generator{
		config: config,
		outDir: outDir,
	}
}

// SetAppName sets the application name
func (g *Generator) SetAppName(name string) {
	g.config.AppName = name
}

// SetLogLevel sets the log level
func (g *Generator) SetLogLevel(level string) {
	g.config.LogLevel = level
}

// EnableFeature enables a specific feature
func (g *Generator) EnableFeature(feature string, enabled bool) {
	switch feature {
	case "nginx":
		g.config.EnableNginx = enabled
	case "horizon":
		g.config.EnableHorizon = enabled
	case "queue":
		g.config.EnableQueue = enabled
	case "scheduler":
		g.config.EnableScheduler = enabled
	case "metrics":
		g.config.EnableMetrics = enabled
	case "api":
		g.config.EnableAPI = enabled
	case "tracing":
		g.config.EnableTracing = enabled
	}
}

// SetQueueWorkers sets the number of queue workers
func (g *Generator) SetQueueWorkers(count int) {
	g.config.QueueWorkers = count
}

// SetQueueConnection sets the queue connection type
func (g *Generator) SetQueueConnection(conn string) {
	g.config.QueueConnection = conn
}

// Generate generates all scaffolding files
func (g *Generator) Generate(files []string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(g.outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	for _, file := range files {
		switch file {
		case "config":
			if err := g.generateConfig(); err != nil {
				return fmt.Errorf("failed to generate config: %w", err)
			}
		case "docker-compose":
			if err := g.generateDockerCompose(); err != nil {
				return fmt.Errorf("failed to generate docker-compose: %w", err)
			}
		case "dockerfile":
			if err := g.generateDockerfile(); err != nil {
				return fmt.Errorf("failed to generate Dockerfile: %w", err)
			}
		case "nginx":
			if err := g.generateNginxConfig(); err != nil {
				return fmt.Errorf("failed to generate nginx.conf: %w", err)
			}
		}
	}

	return nil
}

// generateConfig generates phpeek-pm.yaml
func (g *Generator) generateConfig() error {
	content, err := GenerateConfig(g.config)
	if err != nil {
		return err
	}

	path := filepath.Join(g.outDir, "phpeek-pm.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// generateDockerCompose generates docker-compose.yml
func (g *Generator) generateDockerCompose() error {
	content, err := GenerateDockerCompose(g.config)
	if err != nil {
		return err
	}

	path := filepath.Join(g.outDir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose file: %w", err)
	}

	return nil
}

// generateDockerfile generates Dockerfile
func (g *Generator) generateDockerfile() error {
	content, err := GenerateDockerfile(g.config)
	if err != nil {
		return err
	}

	path := filepath.Join(g.outDir, "Dockerfile")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	return nil
}

// generateNginxConfig generates nginx.conf
func (g *Generator) generateNginxConfig() error {
	content, err := GenerateNginxConfig(g.config)
	if err != nil {
		return err
	}

	path := filepath.Join(g.outDir, "nginx.conf")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write nginx.conf: %w", err)
	}

	return nil
}

// GetConfig returns the current configuration
func (g *Generator) GetConfig() *Config {
	return g.config
}

// PreviewConfig returns the generated config content for preview
func (g *Generator) PreviewConfig() (string, error) {
	return GenerateConfig(g.config)
}
