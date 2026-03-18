package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_DryRunDoesNotPersistState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	source := makeTestSource(t)
	destination := filepath.Join(t.TempDir(), "destination")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := run([]string{"--dry-run", "--source", source, destination}, bytes.NewBuffer(nil), stdout, stderr); err != nil {
		t.Fatalf("run() returned error: %v", err)
	}

	assertNoStateFile(t)
}

func TestRun_CancelDoesNotPersistState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	source := makeTestSource(t)
	destination := filepath.Join(t.TempDir(), "destination")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := run([]string{"--source", source, destination}, bytes.NewBufferString("n\n"), stdout, stderr)
	if !errors.Is(err, errCancelled) {
		t.Fatalf("run() error = %v, want errCancelled", err)
	}

	assertNoStateFile(t)
}

func makeTestSource(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	mediaDir := filepath.Join(root, "DCIM", "100MSDCF")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", mediaDir, err)
	}

	mediaPath := filepath.Join(mediaDir, "DSC0001.JPG")
	if err := os.WriteFile(mediaPath, []byte("photo-data"), 0o644); err != nil {
		t.Fatalf("write %s: %v", mediaPath, err)
	}

	return root
}

func assertNoStateFile(t *testing.T) {
	t.Helper()

	statePath, err := stateFilePath()
	if err != nil {
		t.Fatalf("stateFilePath() error: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected no state file at %s, got err=%v", statePath, err)
	}
}
