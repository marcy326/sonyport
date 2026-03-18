package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadState_IgnoresCorruptStateFile(t *testing.T) {
	restore := overrideUserConfigDir(t, t.TempDir())
	defer restore()

	statePath, err := stateFilePath()
	if err != nil {
		t.Fatalf("stateFilePath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(statePath), err)
	}
	if err := os.WriteFile(statePath, []byte("{not-valid-json"), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	state, err := loadState()
	if err != nil {
		t.Fatalf("loadState() error = %v, want nil; safest behavior is to ignore a corrupt state file and start fresh", err)
	}
	if state != nil {
		t.Fatalf("loadState() state = %#v, want nil when the state file is corrupt", state)
	}
}

func TestSaveState_ReturnsUserConfigDirError(t *testing.T) {
	restore := overrideUserConfigDir(t, "")
	defer restore()

	err := saveState(persistedState{LastDestination: "/tmp/destination"})
	if err == nil {
		t.Fatal("saveState() error = nil, want userConfigDir failure to be surfaced")
	}
}

func overrideUserConfigDir(t *testing.T, dir string) func() {
	t.Helper()

	previous := userConfigDir
	if dir == "" {
		userConfigDir = func() (string, error) {
			return "", errors.New("config dir unavailable")
		}
	} else {
		userConfigDir = func() (string, error) {
			return dir, nil
		}
	}

	return func() {
		userConfigDir = previous
	}
}
