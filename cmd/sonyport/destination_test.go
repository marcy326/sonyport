package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectDestination_CountsOnlyDateDirsAndDirectMedia(t *testing.T) {
	root := t.TempDir()

	mustMkdirAll(t, filepath.Join(root, "2025-03-01"))
	mustMkdirAll(t, filepath.Join(root, "2025-03-02"))
	mustMkdirAll(t, filepath.Join(root, "2025-03-03"))
	mustMkdirAll(t, filepath.Join(root, "2025-03-04", "nested"))
	mustMkdirAll(t, filepath.Join(root, "notes"))
	mustMkdirAll(t, filepath.Join(root, "misc", "nested"))

	mustWriteFile(t, filepath.Join(root, "2025-03-01", "DSC0001.JPG"), "photo")
	mustWriteFile(t, filepath.Join(root, "2025-03-02", "C0001.MP4"), "video")
	mustWriteFile(t, filepath.Join(root, "2025-03-03", "README.txt"), "text")
	mustWriteFile(t, filepath.Join(root, "2025-03-04", "nested", "DSC0002.JPG"), "nested-photo")
	mustWriteFile(t, filepath.Join(root, "notes", "DSC0003.JPG"), "ignored")
	mustWriteFile(t, filepath.Join(root, "misc", "nested", "C0002.MP4"), "ignored")

	info, err := inspectDestination(root)
	if err != nil {
		t.Fatalf("inspectDestination() error = %v", err)
	}

	if got, want := info.Exists, true; got != want {
		t.Errorf("Exists = %v, want %v", got, want)
	}
	if got, want := info.DateDirs, 4; got != want {
		t.Errorf("DateDirs = %d, want %d", got, want)
	}
	if got, want := info.MediaFiles, 2; got != want {
		t.Errorf("MediaFiles = %d, want %d", got, want)
	}
	if t.Failed() {
		t.FailNow()
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
