package logtail

import (
	"fmt"
	"io"
	"os"
)

// FileRotator performs size-based log file rotation.
// When a file exceeds MaxSize, it is renamed with a numeric suffix
// (app.log -> app.log.1, app.log.1 -> app.log.2, etc.) and the
// original path is recreated as an empty file. Files beyond MaxFiles
// are deleted.
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

	// MaxSize 0 would rotate on every single write; treat it as "no rotation"
	// rather than shredding the log.
	if r.MaxSize <= 0 || info.Size() < r.MaxSize {
		return nil
	}

	// Shift existing rotated files: .N -> .N+1, starting from highest
	for i := r.MaxFiles; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)

		if i == r.MaxFiles {
			_ = os.Remove(dst) // best-effort: drop the oldest rotated file
		}

		if _, err := os.Stat(src); err == nil {
			if i >= r.MaxFiles {
				_ = os.Remove(src)
			} else {
				_ = os.Rename(src, dst)
			}
		}
	}
	// Copy-then-truncate rather than rename-then-create.
	//
	// Renaming the live file and creating a new one at the same path breaks
	// every writer that holds an open descriptor — php-fpm, nginx, Monolog's
	// StreamHandler — because their fd follows the renamed inode. They keep
	// appending to the rotated file (which then grows without bound, defeating
	// the size cap) while the file being tailed stays empty forever. Truncating
	// in place keeps their descriptors valid.
	//
	// The cost is a small window in which lines written between the copy and
	// the truncate are lost; that is the same trade-off logrotate's copytruncate
	// makes, and it is far cheaper than silently losing all future output.
	if err := copyFile(path, fmt.Sprintf("%s.1", path)); err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}
	if err := os.Truncate(path, 0); err != nil {
		return fmt.Errorf("truncate %s: %w", path, err)
	}

	return nil
}

// copyFile copies src to dst, replacing dst if it exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
