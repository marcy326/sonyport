package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFile_PreservesExistingTargetOnCopyError(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.jpg")
	if err := os.WriteFile(sourcePath, []byte("new-data"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	targetPath := filepath.Join(t.TempDir(), "target.jpg")
	if err := os.WriteFile(targetPath, []byte("old-data"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	restore := stubCopyData(t, func(dst io.Writer, src io.Reader) (int64, error) {
		if _, err := dst.Write([]byte("partial")); err != nil {
			return 0, err
		}
		return int64(len("partial")), errors.New("copy failed")
	})
	defer restore()

	if err := copyFile(sourcePath, targetPath); err == nil {
		t.Fatal("copyFile() error = nil, want copy failure")
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if got, want := string(data), "old-data"; got != want {
		t.Fatalf("target content = %q, want %q", got, want)
	}
}

func TestCopyFile_RemovesPartialTargetOnCopyError(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.jpg")
	if err := os.WriteFile(sourcePath, []byte("new-data"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	targetPath := filepath.Join(t.TempDir(), "target.jpg")

	restore := stubCopyData(t, func(dst io.Writer, src io.Reader) (int64, error) {
		if _, err := dst.Write([]byte("partial")); err != nil {
			return 0, err
		}
		return int64(len("partial")), errors.New("copy failed")
	})
	defer restore()

	if err := copyFile(sourcePath, targetPath); err == nil {
		t.Fatal("copyFile() error = nil, want copy failure")
	}

	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("expected target to be absent, stat err = %v", err)
	}
}

func stubCopyData(t *testing.T, fn func(dst io.Writer, src io.Reader) (int64, error)) func() {
	t.Helper()

	previous := copyData
	copyData = fn
	return func() {
		copyData = previous
	}
}
