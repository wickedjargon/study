// Package media manages external viewers for images and audio.
package media

import (
	"fmt"
	"os"
	"os/exec"
	"study/deck"
)

// Viewer manages external media viewer processes.
type Viewer struct {
	imageCmd  string   // image viewer command (sxiv, feh)
	audioCmd  string   // audio player command (mpv, aplay)
	processes []*os.Process
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

// HasImageViewer returns true if an image viewer was found.
func (v *Viewer) HasImageViewer() bool {
	return v.imageCmd != ""
}

// HasAudioPlayer returns true if an audio player was found.
func (v *Viewer) HasAudioPlayer() bool {
	return v.audioCmd != ""
}

// ShowMedia displays all media elements for a card side.
// Returns an error if a required viewer is not available.
func (v *Viewer) ShowMedia(media []deck.Media) error {
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
			if err := v.playAudio(m.Content); err != nil {
				return err
			}
		}
	}
	return nil
}

// CloseAll terminates all running media processes.
func (v *Viewer) CloseAll() {
	for _, p := range v.processes {
		if p != nil {
			p.Kill()
			p.Wait()
		}
	}
	v.processes = nil
}

// showImage launches the image viewer as a subprocess.
func (v *Viewer) showImage(path string) error {
	cmd := exec.Command(v.imageCmd, path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching %s: %w", v.imageCmd, err)
	}
	v.processes = append(v.processes, cmd.Process)
	return nil
}

// playAudio launches the audio player as a subprocess.
func (v *Viewer) playAudio(path string) error {
	var args []string
	for _, p := range audioPlayers {
		if p.cmd == v.audioCmd {
			args = append(args, p.args...)
			break
		}
	}
	args = append(args, path)

	cmd := exec.Command(v.audioCmd, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching %s: %w", v.audioCmd, err)
	}
	v.processes = append(v.processes, cmd.Process)
	return nil
}

// CheckRequiredViewers validates that viewers are available for the
// media types used in a deck.
func (v *Viewer) CheckRequiredViewers(d *deck.Deck) error {
	needsImage := false
	needsAudio := false

	for _, card := range d.Cards {
		for _, m := range card.Question {
			switch m.Type {
			case deck.Image:
				needsImage = true
			case deck.Audio:
				needsAudio = true
			}
		}
		for _, m := range card.Answer {
			switch m.Type {
			case deck.Image:
				needsImage = true
			case deck.Audio:
				needsAudio = true
			}
		}
	}

	if needsImage && !v.HasImageViewer() {
		return fmt.Errorf("deck uses images but no viewer found (install sxiv or feh)")
	}
	if needsAudio && !v.HasAudioPlayer() {
		return fmt.Errorf("deck uses audio but no player found (install mpv or aplay)")
	}

	return nil
}
