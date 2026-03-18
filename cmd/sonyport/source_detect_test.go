package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectCameraSourceFromRoot_AmbiguousBestCandidatesReturnsError(t *testing.T) {
	root := t.TempDir()
	makeVolume(t, root, "SONY_A", false, false)
	makeVolume(t, root, "SONY_B", false, false)

	got, err := detectCameraSourceFromRoot(root)
	if err == nil {
		t.Fatalf("detectCameraSourceFromRoot() = %q, want error for ambiguous best candidates", got)
	}
}

func TestDetectCameraSourceFromRoot_PicksSingleBestCandidate(t *testing.T) {
	root := t.TempDir()
	want := makeVolume(t, root, "SONY_CAM", true, false)
	makeVolume(t, root, "OTHER_CAM", false, false)

	got, err := detectCameraSourceFromRoot(root)
	if err != nil {
		t.Fatalf("detectCameraSourceFromRoot() error = %v", err)
	}
	if got != want {
		t.Fatalf("detectCameraSourceFromRoot() = %q, want %q", got, want)
	}
}

func makeVolume(t *testing.T, root, name string, hasM4Root, hasAvfInfo bool) string {
	t.Helper()

	volumePath := filepath.Join(root, name)
	dcimPath := filepath.Join(volumePath, "DCIM")
	if err := os.MkdirAll(dcimPath, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dcimPath, err)
	}
	if hasM4Root {
		if err := os.MkdirAll(filepath.Join(volumePath, "PRIVATE", "M4ROOT"), 0o755); err != nil {
			t.Fatalf("mkdir M4ROOT: %v", err)
		}
	}
	if hasAvfInfo {
		if err := os.MkdirAll(filepath.Join(volumePath, "AVF_INFO"), 0o755); err != nil {
			t.Fatalf("mkdir AVF_INFO: %v", err)
		}
	}

	if err := os.MkdirAll(filepath.Join(dcimPath, "100MSDCF"), 0o755); err != nil {
		t.Fatalf("mkdir MSDCF: %v", err)
	}

	return volumePath
}
