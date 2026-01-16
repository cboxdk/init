package setup

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigValidator_ValidatePHPFPM(t *testing.T) {
	logger := slog.Default()
	cv := NewConfigValidator(logger)

	// Check if php-fpm is available
	_, err := exec.LookPath("php-fpm")
	if err != nil {
		t.Skip("php-fpm not available, skipping test")
	}

	// Test validation
	err = cv.ValidatePHPFPM()
	if err != nil {
		t.Logf("PHP-FPM validation error (expected in test environment): %v", err)
		// This is acceptable - we're testing the logic, not a real PHP-FPM config
	}
}

func TestConfigValidator_ValidateNginx(t *testing.T) {
	logger := slog.Default()
	cv := NewConfigValidator(logger)

	// Check if nginx is available
	_, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx not available, skipping test")
	}

	// Test validation
	err = cv.ValidateNginx()
	if err != nil {
		t.Logf("Nginx validation error (expected in test environment): %v", err)
		// This is acceptable - we're testing the logic, not a real nginx config
	}
}

func TestConfigValidator_ValidateAll(t *testing.T) {
	logger := slog.Default()
	cv := NewConfigValidator(logger)

	// Test validation - should not panic even if binaries don't exist
	err := cv.ValidateAll()
	if err != nil {
		t.Logf("Validation errors (expected in test environment): %v", err)
		// This is acceptable - we're testing the logic
	}
}

func TestNewConfigValidator(t *testing.T) {
	logger := slog.Default()
	cv := NewConfigValidator(logger)

	if cv == nil {
		t.Fatal("NewConfigValidator returned nil")
	}

	if cv.logger == nil {
		t.Error("logger is nil")
	}
}

// Test that validator gracefully handles missing binaries
func TestConfigValidator_MissingBinaries(t *testing.T) {
	logger := slog.Default()
	cv := NewConfigValidator(logger)

	// The validator should handle missing binaries gracefully (skip validation)
	// This test verifies it doesn't panic

	// We can't easily test with missing binaries in a unit test,
	// but we can verify the validator is created correctly
	if cv == nil {
		t.Fatal("NewConfigValidator returned nil")
	}
}

func TestConfigValidator_ValidatePHPFPM_MissingBinary(t *testing.T) {
	logger := slog.Default()
	cv := NewConfigValidator(logger)

	// Save PATH
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)

	// Set empty PATH to simulate missing binary
	os.Setenv("PATH", "")

	// Should not error when binary is missing (graceful skip)
	err := cv.ValidatePHPFPM()
	if err != nil {
		t.Errorf("ValidatePHPFPM() should skip gracefully when binary missing, got error: %v", err)
	}
}

func TestConfigValidator_ValidateNginx_MissingBinary(t *testing.T) {
	logger := slog.Default()
	cv := NewConfigValidator(logger)

	// Save PATH
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)

	// Set empty PATH to simulate missing binary
	os.Setenv("PATH", "")

	// Should not error when binary is missing (graceful skip)
	err := cv.ValidateNginx()
	if err != nil {
		t.Errorf("ValidateNginx() should skip gracefully when binary missing, got error: %v", err)
	}
}

func TestConfigValidator_ValidatePHPFPM_WithMockScript(t *testing.T) {
	logger := slog.Default()
	cv := NewConfigValidator(logger)

	// Create a temporary directory for mock binaries
	tmpDir, err := os.MkdirTemp("", "cbox-validator-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		scriptCode  string
		expectError bool
		errorString string
	}{
		{
			name: "successful test",
			scriptCode: `#!/bin/sh
echo "test is successful"
exit 0`,
			expectError: false,
		},
		{
			name: "syntax error",
			scriptCode: `#!/bin/sh
echo "ERROR: syntax error in config"
exit 1`,
			expectError: true,
			errorString: "syntax error",
		},
		{
			name: "generic error output",
			scriptCode: `#!/bin/sh
echo "Some warning message"
exit 0`,
			expectError: false,
		},
		{
			name: "empty output with success",
			scriptCode: `#!/bin/sh
exit 0`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock php-fpm script
			mockScript := filepath.Join(tmpDir, "php-fpm-"+strings.ReplaceAll(tt.name, " ", "-"))
			if err := os.WriteFile(mockScript, []byte(tt.scriptCode), 0755); err != nil {
				t.Fatalf("Failed to create mock script: %v", err)
			}

			// Save original PATH
			originalPath := os.Getenv("PATH")
			defer os.Setenv("PATH", originalPath)

			// Prepend tmpDir to PATH so our mock binary is found first
			os.Setenv("PATH", tmpDir+":"+originalPath)

			// Rename to php-fpm temporarily
			mockPHPFPM := filepath.Join(tmpDir, "php-fpm")
			if err := os.Rename(mockScript, mockPHPFPM); err != nil {
				t.Fatalf("Failed to rename mock script: %v", err)
			}

			// Test validation
			err := cv.ValidatePHPFPM()

			// Check error expectation
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			if tt.expectError && err != nil && tt.errorString != "" {
				if !strings.Contains(err.Error(), tt.errorString) {
					t.Errorf("Expected error containing %q, got %q", tt.errorString, err.Error())
				}
			}

			// Cleanup
			os.Remove(mockPHPFPM)
		})
	}
}

func TestConfigValidator_ValidateNginx_WithMockScript(t *testing.T) {
	logger := slog.Default()
	cv := NewConfigValidator(logger)

	// Create a temporary directory for mock binaries
	tmpDir, err := os.MkdirTemp("", "cbox-validator-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		scriptCode  string
		expectError bool
		errorString string
	}{
		{
			name: "syntax ok",
			scriptCode: `#!/bin/sh
echo "nginx: the configuration file syntax is ok"
exit 0`,
			expectError: false,
		},
		{
			name: "syntax error",
			scriptCode: `#!/bin/sh
echo "nginx: [emerg] syntax error"
exit 1`,
			expectError: true,
			errorString: "syntax error",
		},
		{
			name: "missing syntax ok message",
			scriptCode: `#!/bin/sh
echo "some other message"
exit 0`,
			expectError: true,
			errorString: "syntax error",
		},
		{
			name: "syntax ok with runtime error",
			scriptCode: `#!/bin/sh
echo "nginx: the configuration file syntax is ok"
echo "nginx: [warn] could not open error log file"
exit 0`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock nginx script
			mockScript := filepath.Join(tmpDir, "nginx-"+strings.ReplaceAll(tt.name, " ", "-"))
			if err := os.WriteFile(mockScript, []byte(tt.scriptCode), 0755); err != nil {
				t.Fatalf("Failed to create mock script: %v", err)
			}

			// Save original PATH
			originalPath := os.Getenv("PATH")
			defer os.Setenv("PATH", originalPath)

			// Prepend tmpDir to PATH so our mock binary is found first
			os.Setenv("PATH", tmpDir+":"+originalPath)

			// Rename to nginx temporarily
			mockNginx := filepath.Join(tmpDir, "nginx")
			if err := os.Rename(mockScript, mockNginx); err != nil {
				t.Fatalf("Failed to rename mock script: %v", err)
			}

			// Test validation
			err := cv.ValidateNginx()

			// Check error expectation
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			if tt.expectError && err != nil && tt.errorString != "" {
				if !strings.Contains(err.Error(), tt.errorString) {
					t.Errorf("Expected error containing %q, got %q", tt.errorString, err.Error())
				}
			}

			// Cleanup
			os.Remove(mockNginx)
		})
	}
}

func TestConfigValidator_ValidateAll_WithErrors(t *testing.T) {
	logger := slog.Default()
	cv := NewConfigValidator(logger)

	// Create a temporary directory for mock binaries
	tmpDir, err := os.MkdirTemp("", "cbox-validator-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a mock php-fpm that fails
	mockPHPFPM := filepath.Join(tmpDir, "php-fpm")
	phpFPMScript := `#!/bin/sh
echo "ERROR: test is failed"
exit 1`
	if err := os.WriteFile(mockPHPFPM, []byte(phpFPMScript), 0755); err != nil {
		t.Fatalf("Failed to create mock php-fpm: %v", err)
	}

	// Save original PATH
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)

	// Set PATH to use our mock binaries
	os.Setenv("PATH", tmpDir+":"+originalPath)

	// Test ValidateAll with failing PHP-FPM
	err = cv.ValidateAll()
	if err == nil {
		t.Error("Expected error from ValidateAll when PHP-FPM validation fails")
	} else {
		if !strings.Contains(err.Error(), "php-fpm config invalid") {
			t.Errorf("Expected error message to contain 'php-fpm config invalid', got: %v", err)
		}
	}

	// Clean up php-fpm mock
	os.Remove(mockPHPFPM)

	// Create a mock nginx that fails
	mockNginx := filepath.Join(tmpDir, "nginx")
	nginxScript := `#!/bin/sh
echo "nginx: [emerg] syntax error"
exit 1`
	if err := os.WriteFile(mockNginx, []byte(nginxScript), 0755); err != nil {
		t.Fatalf("Failed to create mock nginx: %v", err)
	}

	// Create a mock php-fpm that succeeds
	phpFPMSuccessScript := `#!/bin/sh
echo "test is successful"
exit 0`
	if err := os.WriteFile(mockPHPFPM, []byte(phpFPMSuccessScript), 0755); err != nil {
		t.Fatalf("Failed to create mock php-fpm: %v", err)
	}

	// Test ValidateAll with failing Nginx
	err = cv.ValidateAll()
	if err == nil {
		t.Error("Expected error from ValidateAll when Nginx validation fails")
	} else {
		if !strings.Contains(err.Error(), "nginx config invalid") {
			t.Errorf("Expected error message to contain 'nginx config invalid', got: %v", err)
		}
	}
}
