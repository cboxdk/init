package setup

import (
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// Framework represents a detected PHP framework
type Framework string

const (
	FrameworkLaravel   Framework = "laravel"
	FrameworkSymfony   Framework = "symfony"
	FrameworkWordPress Framework = "wordpress"
	FrameworkGeneric   Framework = "generic"
)

// Fallback uid/gid when neither env override nor /etc/passwd lookup
// resolves an app user. 82 is www-data on Alpine, but on Debian-based
// images this constant is wrong — preferred path is the lookup below.
const (
	fallbackUID = 82
	fallbackGID = 82
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

// resolveAppUser returns the uid/gid the framework directories should be
// chowned to. Resolution order:
//
//  1. `PUID` / `PGID` environment variables — explicit operator override.
//     Either both must be valid integers or both are ignored.
//  2. `/etc/passwd` lookup of `www-data` — handles Debian (uid 33) and
//     Alpine (uid 82) without hardcoding either, so the same binary
//     produces correct ownership in both baseimages.
//  3. Fallback to `fallbackUID/fallbackGID` (Alpine convention).
//
// The chosen source is logged so the operator can see which path won.
func (pm *PermissionManager) resolveAppUser() (uid, gid int) {
	if envUID, envGID, ok := readPuidPgidEnv(); ok {
		pm.logger.Info("App user from PUID/PGID env", "uid", envUID, "gid", envGID)
		return envUID, envGID
	}

	if u, err := user.Lookup("www-data"); err == nil {
		uidI, errU := strconv.Atoi(u.Uid)
		gidI, errG := strconv.Atoi(u.Gid)
		if errU == nil && errG == nil {
			pm.logger.Info("App user from /etc/passwd lookup", "user", "www-data", "uid", uidI, "gid", gidI)
			return uidI, gidI
		}
	}

	pm.logger.Warn("Could not resolve app user — falling back to Alpine convention",
		"uid", fallbackUID, "gid", fallbackGID,
		"hint", "Set PUID/PGID env vars or ensure /etc/passwd has a www-data entry",
	)
	return fallbackUID, fallbackGID
}

// readPuidPgidEnv reads PUID and PGID. Both must be present and parse
// as non-negative integers; otherwise the override is ignored.
func readPuidPgidEnv() (uid, gid int, ok bool) {
	puidStr, pgidStr := os.Getenv("PUID"), os.Getenv("PGID")
	if puidStr == "" || pgidStr == "" {
		return 0, 0, false
	}
	puid, errU := strconv.Atoi(puidStr)
	pgid, errG := strconv.Atoi(pgidStr)
	if errU != nil || errG != nil || puid < 0 || pgid < 0 {
		return 0, 0, false
	}
	return puid, pgid, true
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

	uid, gid := pm.resolveAppUser()
	pm.chownRecursive(filepath.Join(pm.workdir, "storage"), uid, gid)
	pm.chownRecursive(filepath.Join(pm.workdir, "bootstrap", "cache"), uid, gid)

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

	uid, gid := pm.resolveAppUser()
	pm.chownRecursive(filepath.Join(pm.workdir, "var"), uid, gid)
	return nil
}

func (pm *PermissionManager) setupWordPress() error {
	dir := filepath.Join(pm.workdir, "wp-content", "uploads")
	if err := pm.createDir(dir, 0775); err != nil {
		pm.logger.Warn("Failed to create uploads directory", "error", err)
	}

	uid, gid := pm.resolveAppUser()
	pm.chownRecursive(filepath.Join(pm.workdir, "wp-content"), uid, gid)
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
