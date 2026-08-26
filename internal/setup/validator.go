package setup

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// ConfigValidator validates PHP-FPM and Nginx configurations
type ConfigValidator struct {
	logger *slog.Logger
}

// NewConfigValidator creates a new config validator
func NewConfigValidator(log *slog.Logger) *ConfigValidator {
	return &ConfigValidator{logger: log}
}

// binaryExists checks if a binary exists in PATH
func (cv *ConfigValidator) binaryExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ValidateAll validates PHP-FPM and Nginx configurations
func (cv *ConfigValidator) ValidateAll() error {
	// These messages name the runtime service configs (php-fpm, nginx) explicitly:
	// a bare "Validating configurations..." / "All configurations valid" reads as
	// if the cbox-init config itself had been validated, which is misleading when
	// it appears right before a "Failed to load configuration" from the load below.
	cv.logger.Info("Validating runtime service configs (php-fpm, nginx)...")

	if err := cv.ValidatePHPFPM(); err != nil {
		return fmt.Errorf("php-fpm config invalid: %w", err)
	}
	cv.logger.Debug("PHP-FPM config valid")

	if err := cv.ValidateNginx(); err != nil {
		return fmt.Errorf("nginx config invalid: %w", err)
	}
	cv.logger.Debug("Nginx config valid")

	cv.logger.Info("Runtime service configs valid")
	return nil
}

// ValidatePHPFPM validates PHP-FPM configuration
func (cv *ConfigValidator) ValidatePHPFPM() error {
	// Check if php-fpm binary exists - skip validation if not installed
	if !cv.binaryExists("php-fpm") {
		cv.logger.Debug("PHP-FPM binary not found, skipping validation")
		return nil
	}

	cmd := exec.Command("php-fpm", "-t")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Check for success message or acceptable output
	// PHP-FPM's -t flag tests the configuration syntax
	if strings.Contains(outputStr, "test is successful") {
		return nil
	}

	// If test failed, check if it's due to config syntax or runtime issues
	// Only fail on actual configuration syntax errors
	if err != nil && (strings.Contains(outputStr, "ERROR") || strings.Contains(outputStr, "syntax error")) {
		return fmt.Errorf("%w: %s", err, outputStr)
	}

	// If we got here and there's output but no clear success/error, log it but don't fail
	if len(outputStr) > 0 {
		cv.logger.Debug("PHP-FPM test output", "output", outputStr)
	}

	return nil
}

// ValidateNginx validates Nginx configuration
func (cv *ConfigValidator) ValidateNginx() error {
	// Check if nginx binary exists - skip validation if not installed
	if !cv.binaryExists("nginx") {
		cv.logger.Debug("Nginx binary not found, skipping validation")
		return nil
	}

	cmd := exec.Command("nginx", "-t")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Check for syntax errors - this is what we care about
	if !strings.Contains(outputStr, "syntax is ok") {
		return fmt.Errorf("syntax error: %s", outputStr)
	}

	// If we got here, syntax is OK
	// Ignore runtime errors like permission denied - they don't indicate config problems
	if err != nil && !strings.Contains(outputStr, "syntax is ok") {
		return fmt.Errorf("%w: %s", err, outputStr)
	}

	return nil
}
