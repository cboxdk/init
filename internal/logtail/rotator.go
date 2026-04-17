package logtail

import (
	"fmt"
	"os"
)

// FileRotator performs size-based log file rotation.
// When a file exceeds MaxSize, it is renamed with a numeric suffix
// (app.log -> app.log.1, app.log.1 -> app.log.2, etc.) and the
// original is truncated. Files beyond MaxFiles are deleted.
type FileRotator struct {
	MaxSize  int64
	MaxFiles int
}

// NewFileRotator creates a new FileRotator.
func NewFileRotator(maxSize int64, maxFiles int) *FileRotator {
	return &FileRotator{MaxSize: maxSize, MaxFiles: maxFiles}
}

// CheckAndRotate checks if the file exceeds MaxSize and rotates if needed.
// Returns nil if file doesn't exist (file may not have been created yet).
func (r *FileRotator) CheckAndRotate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if info.Size() < r.MaxSize {
		return nil
	}

	// Shift existing rotated files: .N -> .N+1, starting from highest
	for i := r.MaxFiles; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)

		if i == r.MaxFiles {
			os.Remove(dst)
		}

		if _, err := os.Stat(src); err == nil {
			if i >= r.MaxFiles {
				_ = os.Remove(src)
			} else {
				_ = os.Rename(src, dst)
			}
		}
	}

	// Rename current file to .1
	if err := os.Rename(path, fmt.Sprintf("%s.1", path)); err != nil {
		return fmt.Errorf("rename %s: %w", path, err)
	}

	// Create new empty file
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	f.Close()

	return nil
}
