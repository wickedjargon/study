package gui

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	"study/deck"
	"study/progress"
	"study/quiz"
)

// newTestApp builds an App with real font faces from the embedded Go fonts, so
// rendering/layout helpers can be exercised without an X connection.
func newTestApp(t *testing.T) *App {
	t.Helper()
	reg, err := parseFont(goregular.TTF)
	if err != nil {
		t.Fatalf("parse regular: %v", err)
	}
	bold, err := parseFont(gobold.TTF)
	if err != nil {
		t.Fatalf("parse bold: %v", err)
	}
	a := &App{
		width:           defaultWidth,
		height:          defaultHeight,
		dpi:             96,
		baseFontPt:      defaultFontPt,
		fontRegularFont: reg,
		fontBoldFont:    bold,
	}
	if err := a.buildFonts(); err != nil {
		t.Fatalf("buildFonts: %v", err)
	}
	return a
}

// fakeStore is a progressStore whose Save fails for the first failTimes calls,
// recording how many times it was invoked.
type fakeStore struct {
	saveCalls int
	failTimes int
}

func (f *fakeStore) Save() error {
	f.saveCalls++
	if f.saveCalls <= f.failTimes {
		return errors.New("disk on fire")
	}
	return nil
}

func (f *fakeStore) SummaryFor([]string) (int, int, int) { return 0, 0, 0 }

func TestSaveProgress(t *testing.T) {
	cases := []struct {
		name          string
		failTimes     int
		wantCalls     int
		wantFailedSet bool
	}{
		{"succeeds first try", 0, 1, false},
		{"transient failure recovered by retry", 1, 2, false},
		{"persistent failure flags the warning", 99, 2, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := &fakeStore{failTimes: c.failTimes}
			a := &App{store: fs}
			a.saveProgress()
			if fs.saveCalls != c.wantCalls {
				t.Errorf("Save called %d times, want %d", fs.saveCalls, c.wantCalls)
			}
			if a.saveFailed != c.wantFailedSet {
				t.Errorf("saveFailed = %v, want %v", a.saveFailed, c.wantFailedSet)
			}
		})
	}
}

// TestSaveProgressClearsFlag verifies a later success resets a prior failure, so
// the warning doesn't stick around after the problem clears.
func TestSaveProgressClearsFlag(t *testing.T) {
	a := &App{store: &fakeStore{failTimes: 99}, saveFailed: true}
	a.store = &fakeStore{failTimes: 0} // now healthy
	a.saveProgress()
	if a.saveFailed {
		t.Error("saveFailed should be cleared after a successful save")
	}
}

func TestLoadImageDecodeError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.png")
	if err := os.WriteFile(bad, []byte("this is not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadImage(bad); err == nil {
		t.Error("expected decode error for non-image data")
	}
	if _, err := loadImage(filepath.Join(dir, "missing.png")); err == nil {
		t.Error("expected error for missing file")
	}

	// A valid image decodes cleanly.
	good := writePNG(t, dir, "good.png")
	if _, err := loadImage(good); err != nil {
		t.Errorf("valid PNG failed to load: %v", err)
	}
}

// TestLoadQuestionImageFailureFlag drives loadQuestionImage end-to-end: a card
// names an image that exists at parse time but can't be decoded, so the flag is
// set (which is what makes the question show a placeholder).
func TestLoadQuestionImageFailureFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep progress out of the real home dir
	dir := t.TempDir()

	badImg := filepath.Join(dir, "bad.png")
	if err := os.WriteFile(badImg, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := appForDeck(t, dir, "@img "+badImg+"\n---\nanswer\n")
	a.loadQuestionImage()
	if !a.questionImgFailed {
		t.Error("expected questionImgFailed=true for an undecodable image")
	}
	if a.questionImg != nil {
		t.Error("expected questionImg=nil when decode fails")
	}

	// A valid image clears the flag and loads.
	goodImg := writePNG(t, dir, "good.png")
	b := appForDeck(t, dir, "@img "+goodImg+"\n---\nanswer\n")
	b.loadQuestionImage()
	if b.questionImgFailed {
		t.Error("expected questionImgFailed=false for a valid image")
	}
	if b.questionImg == nil {
		t.Error("expected questionImg to be loaded for a valid image")
	}
}

func TestRenderQuestionImageBlock(t *testing.T) {
	a := newTestApp(t)
	canvas := image.NewRGBA(image.Rect(0, 0, a.width, a.height))
	const startY = 100

	// No image on the card: y is returned unchanged.
	if got := a.renderQuestionImageBlock(canvas, startY); got != startY {
		t.Errorf("no image: y = %d, want unchanged %d", got, startY)
	}

	// Failed decode: a placeholder is drawn, so y advances past the start.
	a.questionImg, a.questionImgFailed = nil, true
	if got := a.renderQuestionImageBlock(canvas, startY); got <= startY {
		t.Errorf("failed image: y = %d, want > %d (placeholder reserves space)", got, startY)
	}

	// Present image: y advances by the scaled image height plus the gap.
	a.questionImgFailed = false
	a.questionImg = image.NewRGBA(image.Rect(0, 0, 40, 30))
	if got := a.renderQuestionImageBlock(canvas, startY); got <= startY {
		t.Errorf("present image: y = %d, want > %d", got, startY)
	}
}

// writePNG encodes a tiny opaque PNG into dir/name and returns its path.
func writePNG(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return p
}

// appForDeck writes a one-card deck under dir, parses it, and returns an App
// whose engine is positioned on the first card.
func appForDeck(t *testing.T, dir, body string) *App {
	t.Helper()
	deckPath := filepath.Join(dir, "t.deck")
	if err := os.WriteFile(deckPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := deck.Parse(deckPath)
	if err != nil {
		t.Fatalf("parse deck: %v", err)
	}
	store, err := progress.NewStore(d.Path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return &App{engine: quiz.NewEngine(d, store)}
}
