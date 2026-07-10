package setup

import (
	"fmt"
	"os"
	"path/filepath"
)

// IsReadOnlyRoot detects if the root filesystem is mounted read-only
func IsReadOnlyRoot() bool {
	// Check if explicitly set via environment variable
	if os.Getenv("CBOX_INIT_READ_ONLY_ROOT") == "true" {
		return true
	}

	// Try to create a test file in /tmp (should always work if not read-only)
	// Using /tmp instead of / to avoid permission issues
	testFile := "/tmp/.cbox-init-write-test"
	f, err := os.OpenFile(testFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		// If we can't write to /tmp, filesystem is likely read-only
		return true
	}

	// Successfully created file - filesystem is writable
	f.Close()
	_ = os.Remove(testFile) // best-effort cleanup of the writability probe

	// Additional check: try writing to root if we're running as root
	if os.Getuid() == 0 {
		rootTestFile := "/.cbox-init-ro-check"
		if f, err := os.OpenFile(rootTestFile, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644); err != nil {
			// Can't write to root even as root - read-only
			return true
		} else {
			f.Close()
			_ = os.Remove(rootTestFile) // best-effort cleanup of the writability probe
		}
	}

	return false
}

// GetRuntimeDir returns the appropriate directory for runtime state
// Uses /run for read-only root, /var/run otherwise
func GetRuntimeDir() (string, error) {
	var runtimeDir string

	if IsReadOnlyRoot() {
		// Use /run (typically tmpfs) for read-only root
		runtimeDir = "/run/cbox-init"
	} else {
		// Use /var/run for writable root
		runtimeDir = "/var/run/cbox-init"
	}

	// Create runtime directory if it doesn't exist
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create runtime directory %s: %w", runtimeDir, err)
	}

	return runtimeDir, nil
}

// EnsureWritableDir ensures a directory exists and is writable
// For read-only root, creates in /run instead of original path
func EnsureWritableDir(path string) (string, error) {
	// Try to create directory at original path
	err := os.MkdirAll(path, 0755)
	if err == nil {
		// Successfully created - path is writable
		return path, nil
	}

	// Failed to create - check if read-only root
	if !IsReadOnlyRoot() {
		// Not read-only, actual error
		return "", fmt.Errorf("failed to create directory %s: %w", path, err)
	}

	// Read-only root - create alternative in /run
	baseName := filepath.Base(path)
	altPath := filepath.Join("/run/cbox-init", baseName)

	if err := os.MkdirAll(altPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create alternative directory %s: %w", altPath, err)
	}

	return altPath, nil
}
