package setup

import (
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestPermissionManager_Setup(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name      string
		setupFunc func(string) error
		checkDirs []string
	}{
		{
			name: "Laravel setup",
			setupFunc: func(dir string) error {
				// Create artisan to make it a Laravel project
				return os.WriteFile(filepath.Join(dir, "artisan"), []byte("#!/usr/bin/env php"), 0644)
			},
			checkDirs: []string{
				"storage/framework/sessions",
				"storage/framework/views",
				"storage/framework/cache",
				"storage/logs",
				"bootstrap/cache",
			},
		},
		{
			name: "Symfony setup",
			setupFunc: func(dir string) error {
				// Create bin/console and var/cache to make it a Symfony project
				if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(dir, "bin", "console"), []byte("#!/usr/bin/env php"), 0644); err != nil {
					return err
				}
				return os.MkdirAll(filepath.Join(dir, "var", "cache"), 0755)
			},
			checkDirs: []string{
				"var/cache",
				"var/log",
			},
		},
		{
			name: "WordPress setup",
			setupFunc: func(dir string) error {
				// Create wp-config.php to make it a WordPress project
				return os.WriteFile(filepath.Join(dir, "wp-config.php"), []byte("<?php"), 0644)
			},
			checkDirs: []string{
				"wp-content/uploads",
			},
		},
		{
			name: "Generic framework (no-op)",
			setupFunc: func(dir string) error {
				return nil
			},
			checkDirs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir, err := os.MkdirTemp("", "cbox-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Setup test files if needed
			if err := tt.setupFunc(tmpDir); err != nil {
				t.Fatalf("Setup func failed: %v", err)
			}

			// Create permission manager and run setup
			pm := NewPermissionManager(tmpDir, logger)
			if err := pm.Setup(); err != nil {
				t.Errorf("Setup() error = %v", err)
			}

			// Verify directories were created
			for _, dir := range tt.checkDirs {
				fullPath := filepath.Join(tmpDir, dir)
				if _, err := os.Stat(fullPath); os.IsNotExist(err) {
					t.Errorf("Expected directory %s was not created", dir)
				}
			}
		})
	}
}

func TestPermissionManager_CreateDir(t *testing.T) {
	logger := slog.Default()
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pm := NewPermissionManager(tmpDir, logger)

	testPath := filepath.Join(tmpDir, "nested", "dir", "structure")
	if err := pm.createDir(testPath, 0755); err != nil {
		t.Errorf("createDir() error = %v", err)
	}

	// Verify directory exists
	if _, err := os.Stat(testPath); os.IsNotExist(err) {
		t.Error("Directory was not created")
	}
}

func TestNewPermissionManager(t *testing.T) {
	logger := slog.Default()
	workdir := "/var/www/html"

	pm := NewPermissionManager(workdir, logger)

	if pm == nil {
		t.Fatal("NewPermissionManager returned nil")
	}

	if pm.workdir != workdir {
		t.Errorf("workdir = %v, want %v", pm.workdir, workdir)
	}

	if pm.logger == nil {
		t.Error("logger is nil")
	}
}

func TestDetectFramework(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name     string
		setup    func(string) error
		expected Framework
	}{
		{
			name: "Laravel detection",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "artisan"), []byte("#!/usr/bin/env php"), 0644)
			},
			expected: FrameworkLaravel,
		},
		{
			name: "Symfony detection",
			setup: func(dir string) error {
				if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(dir, "bin", "console"), []byte("#!/usr/bin/env php"), 0644); err != nil {
					return err
				}
				return os.MkdirAll(filepath.Join(dir, "var", "cache"), 0755)
			},
			expected: FrameworkSymfony,
		},
		{
			name: "WordPress detection",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "wp-config.php"), []byte("<?php"), 0644)
			},
			expected: FrameworkWordPress,
		},
		{
			name: "Generic (no markers)",
			setup: func(dir string) error {
				return nil
			},
			expected: FrameworkGeneric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "cbox-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			if err := tt.setup(tmpDir); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			pm := NewPermissionManager(tmpDir, logger)
			detected := pm.detectFramework()

			if detected != tt.expected {
				t.Errorf("detectFramework() = %v, want %v", detected, tt.expected)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() (string, error)
		expected bool
	}{
		{
			name: "file exists",
			setup: func() (string, error) {
				f, err := os.CreateTemp("", "cbox-test-*")
				if err != nil {
					return "", err
				}
				f.Close()
				return f.Name(), nil
			},
			expected: true,
		},
		{
			name: "file does not exist",
			setup: func() (string, error) {
				return "/nonexistent/file/path", nil
			},
			expected: false,
		},
		{
			name: "directory instead of file",
			setup: func() (string, error) {
				return os.MkdirTemp("", "cbox-test-*")
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := tt.setup()
			if err != nil {
				t.Fatalf("Setup failed: %v", err)
			}
			if tt.expected {
				defer os.Remove(path)
			} else if tt.name == "directory instead of file" {
				defer os.RemoveAll(path)
			}

			result := fileExists(path)
			if result != tt.expected {
				t.Errorf("fileExists(%s) = %v, want %v", path, result, tt.expected)
			}
		})
	}
}

func TestDirExists(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() (string, error)
		expected bool
	}{
		{
			name: "directory exists",
			setup: func() (string, error) {
				return os.MkdirTemp("", "cbox-test-*")
			},
			expected: true,
		},
		{
			name: "directory does not exist",
			setup: func() (string, error) {
				return "/nonexistent/directory/path", nil
			},
			expected: false,
		},
		{
			name: "file instead of directory",
			setup: func() (string, error) {
				f, err := os.CreateTemp("", "cbox-test-*")
				if err != nil {
					return "", err
				}
				f.Close()
				return f.Name(), nil
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := tt.setup()
			if err != nil {
				t.Fatalf("Setup failed: %v", err)
			}
			if tt.expected {
				defer os.RemoveAll(path)
			} else if tt.name == "file instead of directory" {
				defer os.Remove(path)
			}

			result := dirExists(path)
			if result != tt.expected {
				t.Errorf("dirExists(%s) = %v, want %v", path, result, tt.expected)
			}
		})
	}
}

func TestPermissionManager_Setup_ReadOnlyRoot(t *testing.T) {
	logger := slog.Default()

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create artisan to make it a Laravel project
	if err := os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644); err != nil {
		t.Fatalf("Failed to create artisan: %v", err)
	}

	// Save original env var
	originalEnv := os.Getenv("CBOX_INIT_READ_ONLY_ROOT")
	defer os.Setenv("CBOX_INIT_READ_ONLY_ROOT", originalEnv)

	// Set read-only root
	os.Setenv("CBOX_INIT_READ_ONLY_ROOT", "true")

	// Create permission manager and run setup
	pm := NewPermissionManager(tmpDir, logger)
	if err := pm.Setup(); err != nil {
		t.Errorf("Setup() error = %v", err)
	}

	// Verify that directories were NOT created (read-only mode)
	checkDirs := []string{
		"storage/framework/sessions",
		"storage/framework/views",
	}
	for _, dir := range checkDirs {
		fullPath := filepath.Join(tmpDir, dir)
		if _, err := os.Stat(fullPath); err == nil {
			t.Errorf("Directory %s should not be created in read-only mode", dir)
		}
	}
}

func TestPermissionManager_SetupLaravel_DirectoryCreationFailure(t *testing.T) {
	logger := slog.Default()

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create artisan to make it a Laravel project
	if err := os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644); err != nil {
		t.Fatalf("Failed to create artisan: %v", err)
	}

	// Create a file where a directory should be (to cause mkdir failure)
	storagePath := filepath.Join(tmpDir, "storage")
	if err := os.WriteFile(storagePath, []byte("blocker"), 0644); err != nil {
		t.Fatalf("Failed to create blocker file: %v", err)
	}

	pm := NewPermissionManager(tmpDir, logger)

	// This should not return an error (errors are logged as warnings)
	if err := pm.Setup(); err != nil {
		t.Errorf("Setup() should not return error for directory creation failures, got: %v", err)
	}
}

func TestPermissionManager_SetupSymfony_DirectoryCreationFailure(t *testing.T) {
	logger := slog.Default()

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create bin/console and var/cache to make it a Symfony project
	if err := os.MkdirAll(filepath.Join(tmpDir, "bin"), 0755); err != nil {
		t.Fatalf("Failed to create bin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "bin", "console"), []byte("#!/usr/bin/env php"), 0644); err != nil {
		t.Fatalf("Failed to create console: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "var", "cache"), 0755); err != nil {
		t.Fatalf("Failed to create cache dir: %v", err)
	}

	// Create a file where a directory should be (to cause mkdir failure)
	varPath := filepath.Join(tmpDir, "var", "log")
	if err := os.MkdirAll(filepath.Join(tmpDir, "var"), 0755); err != nil {
		t.Fatalf("Failed to create var dir: %v", err)
	}
	if err := os.WriteFile(varPath, []byte("blocker"), 0644); err != nil {
		t.Fatalf("Failed to create blocker file: %v", err)
	}

	pm := NewPermissionManager(tmpDir, logger)

	// This should not return an error (errors are logged as warnings)
	if err := pm.Setup(); err != nil {
		t.Errorf("Setup() should not return error for directory creation failures, got: %v", err)
	}
}

func TestPermissionManager_SetupWordPress_DirectoryCreationFailure(t *testing.T) {
	logger := slog.Default()

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create wp-config.php to make it a WordPress project
	if err := os.WriteFile(filepath.Join(tmpDir, "wp-config.php"), []byte("<?php"), 0644); err != nil {
		t.Fatalf("Failed to create wp-config.php: %v", err)
	}

	// Create a file where a directory should be (to cause mkdir failure)
	wpContentPath := filepath.Join(tmpDir, "wp-content")
	if err := os.WriteFile(wpContentPath, []byte("blocker"), 0644); err != nil {
		t.Fatalf("Failed to create blocker file: %v", err)
	}

	pm := NewPermissionManager(tmpDir, logger)

	// This should not return an error (errors are logged as warnings)
	if err := pm.Setup(); err != nil {
		t.Errorf("Setup() should not return error for directory creation failures, got: %v", err)
	}
}

func TestPermissionManager_ChownRecursive(t *testing.T) {
	logger := slog.Default()

	// Create temp dir with nested structure
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create nested directories and files
	nestedDir := filepath.Join(tmpDir, "nested", "structure")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("Failed to create nested dir: %v", err)
	}

	testFile := filepath.Join(nestedDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	pm := NewPermissionManager(tmpDir, logger)

	// Call chownRecursive - should not panic even if not running as root
	pm.chownRecursive(tmpDir, 82, 82)

	// Verify files still exist (function should not fail)
	if _, err := os.Stat(testFile); err != nil {
		t.Errorf("Test file disappeared after chownRecursive: %v", err)
	}
}

func TestPermissionManager_ChownRecursive_WithError(t *testing.T) {
	logger := slog.Default()

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "cbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pm := NewPermissionManager(tmpDir, logger)

	// Call chownRecursive on non-existent path - should not panic
	pm.chownRecursive("/nonexistent/path", 82, 82)

	// Test completes successfully if no panic
}

func TestReadPuidPgidEnv(t *testing.T) {
	tests := []struct {
		name     string
		puid     string
		pgid     string
		wantUID  int
		wantGID  int
		wantOk   bool
	}{
		{name: "both unset", puid: "", pgid: "", wantOk: false},
		{name: "only PUID set", puid: "33", pgid: "", wantOk: false},
		{name: "only PGID set", puid: "", pgid: "33", wantOk: false},
		{name: "valid debian uids", puid: "33", pgid: "33", wantUID: 33, wantGID: 33, wantOk: true},
		{name: "valid alpine uids", puid: "82", pgid: "82", wantUID: 82, wantGID: 82, wantOk: true},
		{name: "PUID non-numeric", puid: "abc", pgid: "33", wantOk: false},
		{name: "PGID non-numeric", puid: "33", pgid: "xyz", wantOk: false},
		{name: "negative PUID rejected", puid: "-1", pgid: "33", wantOk: false},
		{name: "negative PGID rejected", puid: "33", pgid: "-1", wantOk: false},
		{name: "uid 0 (root) is valid", puid: "0", pgid: "0", wantUID: 0, wantGID: 0, wantOk: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PUID", tt.puid)
			t.Setenv("PGID", tt.pgid)

			uid, gid, ok := readPuidPgidEnv()
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && (uid != tt.wantUID || gid != tt.wantGID) {
				t.Errorf("uid,gid = %d,%d, want %d,%d", uid, gid, tt.wantUID, tt.wantGID)
			}
		})
	}
}

func TestResolveAppUser_PUIDOverridesPasswdLookup(t *testing.T) {
	logger := slog.Default()
	pm := NewPermissionManager("/tmp", logger)

	// PUID/PGID set: even on a system where www-data resolves to a
	// different uid, the env override wins.
	t.Setenv("PUID", "1234")
	t.Setenv("PGID", "5678")

	uid, gid := pm.resolveAppUser()
	if uid != 1234 || gid != 5678 {
		t.Errorf("uid,gid = %d,%d, want 1234,5678 (env override should win)", uid, gid)
	}
}

func TestResolveAppUser_FallsBackWhenLookupFails(t *testing.T) {
	logger := slog.Default()
	pm := NewPermissionManager("/tmp", logger)

	// Clear env so we don't take the override path.
	t.Setenv("PUID", "")
	t.Setenv("PGID", "")

	uid, gid := pm.resolveAppUser()

	// Either www-data was on the test host and resolved to a real uid,
	// or it wasn't and we fell back to the Alpine convention. Both are
	// acceptable; the function never returns invalid values.
	if uid < 0 || gid < 0 {
		t.Errorf("got negative uid/gid: %d,%d", uid, gid)
	}

	// If www-data does not resolve on the test host, the fallback must hit.
	if _, err := user.Lookup("www-data"); err != nil {
		if uid != fallbackUID || gid != fallbackGID {
			t.Errorf("expected fallback %d/%d, got %d/%d", fallbackUID, fallbackGID, uid, gid)
		}
	}
}
