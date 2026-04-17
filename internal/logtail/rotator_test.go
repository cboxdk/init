package logtail

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileRotator_NoRotationNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	_ = os.WriteFile(path, []byte("small"), 0644)

	r := NewFileRotator(1024, 3)
	if err := r.CheckAndRotate(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "small" {
		t.Errorf("file was modified: %q", data)
	}
}

func TestFileRotator_RotatesWhenOverSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	_ = os.WriteFile(path, make([]byte, 2048), 0644)

	r := NewFileRotator(1024, 3)
	if err := r.CheckAndRotate(path); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}

	info, _ := os.Stat(path)
	if info.Size() != 0 {
		t.Errorf("expected truncated file, got size %d", info.Size())
	}

	rData, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	if len(rData) != 2048 {
		t.Errorf("rotated file size %d, expected 2048", len(rData))
	}
}

func TestFileRotator_ShiftsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	_ = os.WriteFile(path+".1", []byte("old-1"), 0644)
	_ = os.WriteFile(path+".2", []byte("old-2"), 0644)
	_ = os.WriteFile(path, make([]byte, 2048), 0644)

	r := NewFileRotator(1024, 3)
	if err := r.CheckAndRotate(path); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}

	info, _ := os.Stat(path + ".1")
	if info.Size() != 2048 {
		t.Errorf(".1 size %d, expected 2048", info.Size())
	}
	data, _ := os.ReadFile(path + ".2")
	if string(data) != "old-1" {
		t.Errorf(".2 content %q, expected 'old-1'", data)
	}
	data, _ = os.ReadFile(path + ".3")
	if string(data) != "old-2" {
		t.Errorf(".3 content %q, expected 'old-2'", data)
	}
}

func TestFileRotator_DeletesBeyondMaxFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	_ = os.WriteFile(path+".1", []byte("one"), 0644)
	_ = os.WriteFile(path+".2", []byte("two"), 0644)
	_ = os.WriteFile(path, make([]byte, 2048), 0644)

	r := NewFileRotator(1024, 2)
	if err := r.CheckAndRotate(path); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Error(".1 should exist")
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Error(".2 should exist")
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Error(".3 should have been deleted")
	}
}

func TestFileRotator_MissingFile(t *testing.T) {
	r := NewFileRotator(1024, 3)
	if err := r.CheckAndRotate("/tmp/nonexistent-logtail-test.log"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
