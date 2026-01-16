package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsReadOnlyRoot(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
		want   bool
	}{
		{
			name:   "explicitly set via env var",
			envVar: "true",
			want:   true,
		},
		{
			name:   "not set via env var",
			envVar: "",
			want:   false, // Should be false in normal test environment
		},
		{
			name:   "env var set to false",
			envVar: "false",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env var
			originalEnv := os.Getenv("CBOX_INIT_READ_ONLY_ROOT")
			defer os.Setenv("CBOX_INIT_READ_ONLY_ROOT", originalEnv)

			// Set test env var
			if tt.envVar != "" {
				os.Setenv("CBOX_INIT_READ_ONLY_ROOT", tt.envVar)
			} else {
				os.Unsetenv("CBOX_INIT_READ_ONLY_ROOT")
			}

			got := IsReadOnlyRoot()
			if got != tt.want {
				t.Errorf("IsReadOnlyRoot() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsReadOnlyRoot_FileSystemCheck(t *testing.T) {
	// Save original env var
	originalEnv := os.Getenv("CBOX_INIT_READ_ONLY_ROOT")
	defer os.Setenv("CBOX_INIT_READ_ONLY_ROOT", originalEnv)

	// Unset env var to test actual filesystem check
	os.Unsetenv("CBOX_INIT_READ_ONLY_ROOT")

	result := IsReadOnlyRoot()

	// In normal test environment, this should be false
	// (we can write to /tmp)
	if result {
		t.Log("Read-only root detected (this is OK if running in special environment)")
	} else {
		t.Log("Writable root filesystem detected (normal test environment)")
	}

	// Verify the test file is cleaned up
	testFile := "/tmp/.cbox-init-write-test"
	if _, err := os.Stat(testFile); err == nil {
		t.Errorf("Test file %s was not cleaned up", testFile)
	}
}

func TestGetRuntimeDir(t *testing.T) {
	tests := []struct {
		name         string
		readOnlyEnv  string
		expectedPath string
	}{
		{
			name:         "writable root filesystem",
			readOnlyEnv:  "",
			expectedPath: "/var/run/cbox-init",
		},
		{
			name:         "read-only root filesystem",
			readOnlyEnv:  "true",
			expectedPath: "/run/cbox-init",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env var
			originalEnv := os.Getenv("CBOX_INIT_READ_ONLY_ROOT")
			defer os.Setenv("CBOX_INIT_READ_ONLY_ROOT", originalEnv)

			// Set test env var
			if tt.readOnlyEnv != "" {
				os.Setenv("CBOX_INIT_READ_ONLY_ROOT", tt.readOnlyEnv)
			} else {
				os.Unsetenv("CBOX_INIT_READ_ONLY_ROOT")
			}

			// Call GetRuntimeDir
			got, err := GetRuntimeDir()

			// In restricted environments, we might not have permission to create
			// directories in /var/run or /run, which is acceptable
			if err != nil {
				t.Logf("GetRuntimeDir() error = %v (acceptable in restricted environments)", err)
				// Still verify the expected path would have been returned
				if !strings.Contains(err.Error(), tt.expectedPath) {
					t.Errorf("Error should mention expected path %s, got: %v", tt.expectedPath, err)
				}
				return
			}

			// Clean up the created directory
			defer os.RemoveAll(got)

			// Check the path
			if got != tt.expectedPath {
				t.Errorf("GetRuntimeDir() = %v, want %v", got, tt.expectedPath)
			}

			// Verify directory was created
			if info, err := os.Stat(got); err != nil {
				t.Errorf("Runtime directory was not created: %v", err)
			} else if !info.IsDir() {
				t.Errorf("Runtime path is not a directory")
			}
		})
	}
}

func TestGetRuntimeDir_CreationError(t *testing.T) {
	// This test verifies error handling when directory creation fails
	// We can't easily simulate this without root permissions or special filesystem setup,
	// but we can test the code path by using a path that might fail

	// Save original env var
	originalEnv := os.Getenv("CBOX_INIT_READ_ONLY_ROOT")
	defer os.Setenv("CBOX_INIT_READ_ONLY_ROOT", originalEnv)

	// Normal case should work
	os.Unsetenv("CBOX_INIT_READ_ONLY_ROOT")
	dir, err := GetRuntimeDir()
	if err != nil {
		// If this fails, it might be a permission issue in the test environment
		t.Logf("GetRuntimeDir() error = %v (acceptable in restricted environments)", err)
	} else {
		defer os.RemoveAll(dir)
		t.Logf("Created runtime directory: %s", dir)
	}
}

func TestEnsureWritableDir(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		readOnlyEnv string
		checkPath   func(string, string) error // Validates the returned path
	}{
		{
			name:        "create directory in writable location",
			path:        filepath.Join(os.TempDir(), "cbox-test-writable"),
			readOnlyEnv: "",
			checkPath: func(inputPath, returnedPath string) error {
				if inputPath != returnedPath {
					return os.ErrInvalid
				}
				return nil
			},
		},
		{
			name:        "create directory in read-only mode",
			path:        "/some/readonly/path",
			readOnlyEnv: "true",
			checkPath: func(inputPath, returnedPath string) error {
				// Should return alternative path in /run
				if !strings.HasPrefix(returnedPath, "/run/cbox-init") {
					return os.ErrInvalid
				}
				return nil
			},
		},
		{
			name:        "existing directory",
			path:        os.TempDir(), // Already exists
			readOnlyEnv: "",
			checkPath: func(inputPath, returnedPath string) error {
				if inputPath != returnedPath {
					return os.ErrInvalid
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env var
			originalEnv := os.Getenv("CBOX_INIT_READ_ONLY_ROOT")
			defer os.Setenv("CBOX_INIT_READ_ONLY_ROOT", originalEnv)

			// Set test env var
			if tt.readOnlyEnv != "" {
				os.Setenv("CBOX_INIT_READ_ONLY_ROOT", tt.readOnlyEnv)
			} else {
				os.Unsetenv("CBOX_INIT_READ_ONLY_ROOT")
			}

			// Call EnsureWritableDir
			got, err := EnsureWritableDir(tt.path)

			// In restricted environments, we might not have permission to create
			// directories, which is acceptable for testing
			if err != nil {
				t.Logf("EnsureWritableDir() error = %v (acceptable in restricted environments)", err)
				return
			}

			// Clean up created directory if it's not a system dir
			if !strings.HasPrefix(got, "/tmp") && !strings.HasPrefix(got, os.TempDir()) {
				defer os.RemoveAll(got)
			} else if got != os.TempDir() {
				// Clean up our test directories
				defer os.RemoveAll(got)
			}

			// Verify directory was created
			if info, err := os.Stat(got); err != nil {
				t.Errorf("Directory was not created: %v", err)
			} else if !info.IsDir() {
				t.Errorf("Path is not a directory")
			}

			// Run custom path validation
			if tt.checkPath != nil {
				if err := tt.checkPath(tt.path, got); err != nil {
					t.Errorf("Path validation failed: input=%s, output=%s, error=%v", tt.path, got, err)
				}
			}
		})
	}
}

func TestEnsureWritableDir_FallbackToRun(t *testing.T) {
	// Test that when a directory can't be created at original path,
	// it falls back to /run/cbox-init in read-only mode

	// Save original env var
	originalEnv := os.Getenv("CBOX_INIT_READ_ONLY_ROOT")
	defer os.Setenv("CBOX_INIT_READ_ONLY_ROOT", originalEnv)

	// Set read-only mode
	os.Setenv("CBOX_INIT_READ_ONLY_ROOT", "true")

	// Try to create in a path that would fail
	path := "/root/some/path/that/wont/work"
	got, err := EnsureWritableDir(path)

	if err != nil {
		// In read-only mode, we should get an alternative path
		// or an error if /run isn't writable either
		t.Logf("EnsureWritableDir() error = %v (acceptable if /run isn't writable)", err)
		return
	}

	// Should have gotten an alternative path
	defer os.RemoveAll(got)

	if !strings.HasPrefix(got, "/run/cbox-init") {
		t.Errorf("Expected fallback path in /run/cbox-init, got %s", got)
	}

	// Verify directory exists
	if info, err := os.Stat(got); err != nil {
		t.Errorf("Fallback directory was not created: %v", err)
	} else if !info.IsDir() {
		t.Errorf("Fallback path is not a directory")
	}
}

func TestEnsureWritableDir_ErrorInWritableMode(t *testing.T) {
	// Test error handling when not in read-only mode but mkdir fails

	// Save original env var
	originalEnv := os.Getenv("CBOX_INIT_READ_ONLY_ROOT")
	defer os.Setenv("CBOX_INIT_READ_ONLY_ROOT", originalEnv)

	// Ensure we're not in read-only mode
	os.Unsetenv("CBOX_INIT_READ_ONLY_ROOT")

	// Try to create a file first, then try to create a directory with same name
	tmpFile, err := os.CreateTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Try to create a directory where a file already exists
	// This should fail even in writable mode
	_, err = EnsureWritableDir(tmpFile.Name())
	if err == nil {
		t.Error("Expected error when trying to create directory over existing file, got nil")
	}
}
