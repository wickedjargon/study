package media

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAudioArgs(t *testing.T) {
	const path = "/clip.mp3"
	cases := []struct {
		name  string
		cmd   string
		speed float64
		want  []string
	}{
		{"mpv normal speed", "mpv", 1.0, []string{"--no-video", "--really-quiet", path}},
		{"mpv zero speed treated as default", "mpv", 0, []string{"--no-video", "--really-quiet", path}},
		{"mpv slowed gets speed + pitch correction", "mpv", 0.75,
			[]string{"--no-video", "--really-quiet", "--speed=0.75", "--audio-pitch-correction=yes", path}},
		{"mpv sped up", "mpv", 1.5,
			[]string{"--no-video", "--really-quiet", "--speed=1.5", "--audio-pitch-correction=yes", path}},
		// aplay has no speed control, so the multiplier is ignored entirely.
		{"aplay ignores speed", "aplay", 0.5, []string{path}},
		{"aplay normal", "aplay", 1.0, []string{path}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := audioArgs(c.cmd, path, c.speed)
			if !equalStrings(got, c.want) {
				t.Errorf("audioArgs(%q, %g) = %v, want %v", c.cmd, c.speed, got, c.want)
			}
		})
	}
}

// TestNewViewerDetection points PATH at a directory holding fake executables and
// checks that NewViewer picks the first available tool in each preference list.
func TestNewViewerDetection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-executable detection assumes a POSIX exec bit")
	}
	dir := t.TempDir()
	// Provide the second-choice image viewer and the second-choice audio player,
	// so detection has to skip the first preference (sxiv/mpv) and still resolve.
	writeFakeExec(t, dir, "nsxiv")
	writeFakeExec(t, dir, "aplay")
	t.Setenv("PATH", dir)

	v := NewViewer()
	if v.imageCmd != "nsxiv" {
		t.Errorf("imageCmd = %q, want nsxiv", v.imageCmd)
	}
	if v.audioCmd != "aplay" {
		t.Errorf("audioCmd = %q, want aplay", v.audioCmd)
	}
}

// TestNewViewerNoTools confirms an empty PATH leaves both commands unset, which
// is what makes ShowMedia report a missing-viewer error rather than crash.
func TestNewViewerNoTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	v := NewViewer()
	if v.imageCmd != "" || v.audioCmd != "" {
		t.Errorf("expected no tools detected, got image=%q audio=%q", v.imageCmd, v.audioCmd)
	}
}

func writeFakeExec(t *testing.T, dir, name string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
