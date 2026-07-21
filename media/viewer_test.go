package media

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"study/deck"
)

func TestAudioArgs(t *testing.T) {
	path := []string{"/clip.mp3"}
	cases := []struct {
		name  string
		cmd   string
		paths []string
		speed float64
		want  []string
	}{
		{"mpv normal speed", "mpv", path, 1.0, []string{"--no-video", "--really-quiet", "/clip.mp3"}},
		{"mpv zero speed treated as default", "mpv", path, 0, []string{"--no-video", "--really-quiet", "/clip.mp3"}},
		{"mpv slowed gets speed + pitch correction", "mpv", path, 0.75,
			[]string{"--no-video", "--really-quiet", "--speed=0.75", "--audio-pitch-correction=yes", "/clip.mp3"}},
		{"mpv sped up", "mpv", path, 1.5,
			[]string{"--no-video", "--really-quiet", "--speed=1.5", "--audio-pitch-correction=yes", "/clip.mp3"}},
		// Several clips ride one invocation, in deck order, after the flags.
		{"mpv two clips", "mpv", []string{"/a.mp3", "/b.mp3"}, 1.0,
			[]string{"--no-video", "--really-quiet", "/a.mp3", "/b.mp3"}},
		// aplay has no speed control, so the multiplier is ignored entirely.
		{"aplay ignores speed", "aplay", path, 0.5, []string{"/clip.mp3"}},
		{"aplay normal", "aplay", path, 1.0, []string{"/clip.mp3"}},
		{"aplay two clips", "aplay", []string{"/a.wav", "/b.wav"}, 1.0, []string{"/a.wav", "/b.wav"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := audioArgs(c.cmd, c.paths, c.speed)
			if !equalStrings(got, c.want) {
				t.Errorf("audioArgs(%q, %g) = %v, want %v", c.cmd, c.speed, got, c.want)
			}
		})
	}
}

// TestShowMediaPlaysAllClips: a side with two @audio clips must hand both to a
// single player invocation, in deck order — a launch per clip would stop the
// previous one and only the last would sound.
func TestShowMediaPlaysAllClips(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-executable player assumes a POSIX shell")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$@\" > \"" + argsFile + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "aplay"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	v := NewViewer()
	media := []deck.Media{
		{Type: deck.Audio, Content: "/a.wav"},
		{Type: deck.Audio, Content: "/b.wav"},
	}
	if err := v.ShowMedia(media, 1.0); err != nil {
		t.Fatalf("ShowMedia: %v", err)
	}
	if len(v.audioProcs) != 1 {
		t.Fatalf("audio subprocesses = %d, want 1 (one invocation for the whole side)", len(v.audioProcs))
	}
	if _, err := v.audioProcs[0].Wait(); err != nil {
		t.Fatalf("waiting for fake player: %v", err)
	}
	v.audioProcs = nil // reaped above; StopAudio must not Wait again

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("fake player recorded no args: %v", err)
	}
	if want := "/a.wav /b.wav"; strings.TrimSpace(string(got)) != want {
		t.Errorf("player args = %q, want %q", strings.TrimSpace(string(got)), want)
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
