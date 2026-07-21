// Package media manages external viewers for images and audio.
package media

import (
	"fmt"
	"os"
	"os/exec"
	"study/deck"
)

// Viewer manages external media viewer processes. Image and audio processes
// are tracked separately so audio can be replayed (stopping any in-flight clip)
// without disturbing a displayed image.
type Viewer struct {
	imageCmd   string // image viewer command (sxiv, feh)
	audioCmd   string // audio player command (mpv, aplay)
	imageProcs []*os.Process
	audioProcs []*os.Process
}

// imageViewers lists image viewers in preference order.
var imageViewers = []string{"sxiv", "nsxiv", "feh"}

// audioPlayers lists audio players in preference order.
var audioPlayers = []struct {
	cmd  string
	args []string // extra args before the file path
}{
	{"mpv", []string{"--no-video", "--really-quiet"}},
	{"aplay", nil},
}

// NewViewer creates a media viewer, detecting available tools.
func NewViewer() *Viewer {
	v := &Viewer{}

	for _, cmd := range imageViewers {
		if _, err := exec.LookPath(cmd); err == nil {
			v.imageCmd = cmd
			break
		}
	}

	for _, p := range audioPlayers {
		if _, err := exec.LookPath(p.cmd); err == nil {
			v.audioCmd = p.cmd
			break
		}
	}

	return v
}

// ShowMedia displays all media elements for a card side. Audio is played at the
// given speed multiplier (1.0 = normal); images ignore it.
// Returns an error if a required viewer is not available.
func (v *Viewer) ShowMedia(media []deck.Media, speed float64) error {
	var clips []string
	for _, m := range media {
		switch m.Type {
		case deck.Image:
			if v.imageCmd == "" {
				return fmt.Errorf("no image viewer found (install sxiv or feh)")
			}
			if err := v.showImage(m.Content); err != nil {
				return err
			}
		case deck.Audio:
			if v.audioCmd == "" {
				return fmt.Errorf("no audio player found (install mpv or aplay)")
			}
			clips = append(clips, m.Content)
		}
	}
	// One player invocation for the whole side: a launch per clip would stop
	// the previous one (playAudio kills in-flight audio), leaving only the
	// last clip audible.
	if len(clips) > 0 {
		return v.playAudio(clips, speed)
	}
	return nil
}

// CloseAll terminates all running media processes (image and audio).
func (v *Viewer) CloseAll() {
	killProcs(v.imageProcs)
	v.imageProcs = nil
	killProcs(v.audioProcs)
	v.audioProcs = nil
}

// StopAudio terminates any in-flight audio clip, leaving images untouched.
func (v *Viewer) StopAudio() {
	killProcs(v.audioProcs)
	v.audioProcs = nil
}

// killProcs terminates and reaps a set of subprocesses.
func killProcs(procs []*os.Process) {
	for _, p := range procs {
		if p != nil {
			p.Kill()
			p.Wait()
		}
	}
}

// showImage launches the image viewer as a subprocess.
func (v *Viewer) showImage(path string) error {
	cmd := exec.Command(v.imageCmd, path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching %s: %w", v.imageCmd, err)
	}
	v.imageProcs = append(v.imageProcs, cmd.Process)
	return nil
}

// playAudio launches the audio player as a single subprocess over all clips —
// both mpv and aplay play their file arguments back to back, so every clip on
// a side is heard in order. Any clip already playing is stopped first, so a
// replay restarts cleanly rather than overlapping.
//
// speed is a playback multiplier (1.0 = normal). It is honoured only by mpv,
// via --speed with pitch correction so slowed-down speech stays intelligible;
// aplay has no speed control, so the clips play at normal speed there.
func (v *Viewer) playAudio(paths []string, speed float64) error {
	v.StopAudio()

	cmd := exec.Command(v.audioCmd, audioArgs(v.audioCmd, paths, speed)...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching %s: %w", v.audioCmd, err)
	}
	v.audioProcs = append(v.audioProcs, cmd.Process)
	return nil
}

// audioArgs builds the argument list (excluding the command name itself) for
// playing paths, in order, on audioCmd at the given speed multiplier. The
// player's fixed flags come first; then, for mpv only and a non-default speed,
// --speed with pitch correction so slowed speech stays intelligible. aplay has
// no speed control, so speed is ignored there. The file paths are always last.
func audioArgs(audioCmd string, paths []string, speed float64) []string {
	var args []string
	for _, p := range audioPlayers {
		if p.cmd == audioCmd {
			args = append(args, p.args...)
			break
		}
	}
	if audioCmd == "mpv" && speed > 0 && speed != 1.0 {
		args = append(args, fmt.Sprintf("--speed=%g", speed), "--audio-pitch-correction=yes")
	}
	return append(args, paths...)
}
