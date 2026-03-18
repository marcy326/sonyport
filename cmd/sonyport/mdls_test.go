package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBuildPlan_CountsMdlsFallbacks(t *testing.T) {
	restore := stubMdlsOutput(t, func(path string) ([]byte, error) {
		return nil, errors.New("mdls failed")
	})
	defer restore()

	scan, err := buildPlan([]candidateMedia{
		{
			Path:     "/tmp/DSC0001.JPG",
			Kind:     mediaPhoto,
			Modified: time.Date(2025, time.March, 1, 10, 0, 0, 0, time.UTC),
		},
	}, t.TempDir(), "skip", "mdls", nil)
	if err != nil {
		t.Fatalf("buildPlan() error = %v", err)
	}

	if got, want := scan.MdlsFallbacks, 1; got != want {
		t.Fatalf("MdlsFallbacks = %d, want %d", got, want)
	}
}

func TestPrintSummary_ShowsMdlsFallbackNotice(t *testing.T) {
	var out bytes.Buffer

	printSummary(&out, "/source", "/dest", options{DateSource: "mdls"}, destinationInfo{}, nil, scanSummary{
		ByDate:        map[string]*dateSummary{},
		MdlsFallbacks: 2,
	})

	if !strings.Contains(out.String(), "Metadata fallbacks to filetime: 2") {
		t.Fatalf("summary output = %q, want mdls fallback notice", out.String())
	}
}

func stubMdlsOutput(t *testing.T, fn func(path string) ([]byte, error)) func() {
	t.Helper()

	previous := mdlsOutput
	mdlsOutput = fn
	return func() {
		mdlsOutput = previous
	}
}
