package main

import "testing"

func TestBuildPlanDetectsSameTargetCollisionUnderSkip(t *testing.T) {
	t.Parallel()

	scan, err := buildPlan([]candidateMedia{
		{
			Path: "/Volumes/CamA/DCIM/100MSDCF/DSC0001.JPG",
			Kind: mediaPhoto,
			Date: "2025-03-01",
		},
		{
			Path: "/Volumes/CamB/DCIM/200MSDCF/DSC0001.JPG",
			Kind: mediaPhoto,
			Date: "2025-03-01",
		},
	}, t.TempDir(), "skip", "filetime", nil)
	if err != nil {
		t.Fatalf("buildPlan() error = %v", err)
	}

	if got, want := scan.FoundDuplicates, 1; got != want {
		t.Fatalf("FoundDuplicates = %d, want %d", got, want)
	}
	if got, want := len(scan.Items), 1; got != want {
		t.Fatalf("len(Items) = %d, want %d", got, want)
	}
	if got, want := scan.PlannedPhotos, 1; got != want {
		t.Fatalf("PlannedPhotos = %d, want %d", got, want)
	}
	if got, want := scan.ByDate["2025-03-01"].Photos, 1; got != want {
		t.Fatalf("ByDate[2025-03-01].Photos = %d, want %d", got, want)
	}
}

func TestBuildPlanRejectsSameTargetCollisionUnderOverwrite(t *testing.T) {
	t.Parallel()

	_, err := buildPlan([]candidateMedia{
		{
			Path: "/Volumes/CamA/DCIM/100MSDCF/DSC0001.JPG",
			Kind: mediaPhoto,
			Date: "2025-03-01",
		},
		{
			Path: "/Volumes/CamB/DCIM/200MSDCF/DSC0001.JPG",
			Kind: mediaPhoto,
			Date: "2025-03-01",
		},
	}, t.TempDir(), "overwrite", "filetime", nil)
	if err == nil {
		t.Fatal("buildPlan() error = nil, want collision error")
	}
}
