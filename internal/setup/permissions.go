package setup

import (
	"log/slog"
	"os"
	"path/filepath"
)

// Framework represents a detected PHP framework
type Framework string

const (
	FrameworkLaravel   Framework = "laravel"
	FrameworkSymfony   Framework = "symfony"
	FrameworkWordPress Framework = "wordpress"
	FrameworkGeneric   Framework = "generic"
)

// PermissionManager handles directory creation and permission setup
type PermissionManager struct {
	logger  *slog.Logger
	workdir string
}

// NewPermissionManager creates a new permission manager
func NewPermissionManager(workdir string, log *slog.Logger) *PermissionManager {
	return &PermissionManager{
		logger:  log,
		workdir: workdir,
	}
}

// detectFramework identifies the PHP framework in the working directory
func (pm *PermissionManager) detectFramework() Framework {
	// Laravel: check for artisan file
	if fileExists(filepath.Join(pm.workdir, "artisan")) {
		return FrameworkLaravel
	}
	// Symfony: check for bin/console and var/cache
	if fileExists(filepath.Join(pm.workdir, "bin", "console")) &&
		dirExists(filepath.Join(pm.workdir, "var", "cache")) {
		return FrameworkSymfony
	}
	// WordPress: check for wp-config.php
	if fileExists(filepath.Join(pm.workdir, "wp-config.php")) {
		return FrameworkWordPress
	}
	return FrameworkGeneric
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Setup creates necessary directories and sets permissions
func (pm *PermissionManager) Setup() error {
	fw := pm.detectFramework()
	pm.logger.Info("Setting up permissions", "framework", fw)

	// Detect read-only root filesystem
	if IsReadOnlyRoot() {
		pm.logger.Info("Read-only root filesystem detected, skipping permission setup",
			"info", "Runtime state will use /run/cbox-init (tmpfs)")
		return nil
	}

	switch fw {
	case FrameworkLaravel:
		return pm.setupLaravel()
	case FrameworkSymfony:
		return pm.setupSymfony()
	case FrameworkWordPress:
		return pm.setupWordPress()
	default:
		pm.logger.Debug("Generic framework, skipping permission setup")
		return nil
	}
}

func (pm *PermissionManager) setupLaravel() error {
	dirs := []string{
		filepath.Join(pm.workdir, "storage", "framework", "sessions"),
		filepath.Join(pm.workdir, "storage", "framework", "views"),
		filepath.Join(pm.workdir, "storage", "framework", "cache"),
		filepath.Join(pm.workdir, "storage", "logs"),
		filepath.Join(pm.workdir, "bootstrap", "cache"),
	}

	for _, dir := range dirs {
		if err := pm.createDir(dir, 0775); err != nil {
			pm.logger.Warn("Failed to create directory", "dir", dir, "error", err)
		}
	}

	// Set ownership (www-data UID/GID typically 82 on Alpine, 33 on Debian)
	// Note: This will fail silently if not running as root
	pm.chownRecursive(filepath.Join(pm.workdir, "storage"), 82, 82)
	pm.chownRecursive(filepath.Join(pm.workdir, "bootstrap", "cache"), 82, 82)

	return nil
}

func (pm *PermissionManager) setupSymfony() error {
	dirs := []string{
		filepath.Join(pm.workdir, "var", "cache"),
		filepath.Join(pm.workdir, "var", "log"),
	}

	for _, dir := range dirs {
		if err := pm.createDir(dir, 0775); err != nil {
			pm.logger.Warn("Failed to create directory", "dir", dir, "error", err)
		}
	}

	pm.chownRecursive(filepath.Join(pm.workdir, "var"), 82, 82)
	return nil
}

func (pm *PermissionManager) setupWordPress() error {
	dir := filepath.Join(pm.workdir, "wp-content", "uploads")
	if err := pm.createDir(dir, 0775); err != nil {
		pm.logger.Warn("Failed to create uploads directory", "error", err)
	}

	pm.chownRecursive(filepath.Join(pm.workdir, "wp-content"), 82, 82)
	return nil
}

func (pm *PermissionManager) createDir(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (pm *PermissionManager) chownRecursive(path string, uid, gid int) {
	// Note: This will fail silently if not running as root
	// That's acceptable - in dev environments permissions may not matter
	_ = filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err == nil {
			_ = os.Chown(name, uid, gid)
		}
		return nil
	})
}
