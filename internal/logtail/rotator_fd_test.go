package logtail

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Rotation must not orphan a writer that holds the file open — php-fpm, nginx
// and Monolog all keep a descriptor. Renaming the live file and creating a new
// one leaves their fd pointing at the rotated inode: everything they write after
// the first rotation disappears from the tailed file (and the rotated file grows
// past the size cap forever). Copy-then-truncate keeps the descriptor valid.
func TestRotationKeepsWriterDescriptorValid(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")
	w, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	r := &FileRotator{MaxSize: 64, MaxFiles: 2}
	for i := 0; i < 12; i++ {
		fmt.Fprintf(w, "line-%02d aaaaaaaaaaaaaaaaaaaa\n", i)
		if err := r.CheckAndRotate(p); err != nil {
			t.Fatalf("rotate: %v", err)
		}
	}
	// The decisive part: a write AFTER a rotation must land in the live file.
	fmt.Fprintln(w, "AFTER-ROTATION-MARKER")

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("live file after post-rotation write: %d bytes", len(b))
	if !strings.Contains(string(b), "AFTER-ROTATION-MARKER") {
		t.Error("post-rotation write did not land in the live file: the writer's fd was orphaned")
	}
}
