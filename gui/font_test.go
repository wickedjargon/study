package gui

import (
	"os"
	"testing"
)

// loadTestFont returns a system CJK font, or skips if none is installed.
func loadTestFont(t *testing.T) []byte {
	t.Helper()
	for _, path := range systemFontPaths {
		if d, err := os.ReadFile(path); err == nil {
			return d
		}
	}
	t.Skip("no system font available")
	return nil
}

// TestLineHeightClearsGlyphExtent guards the no-overlap invariant: the line
// advance must always exceed the font's ascent+descent so successive lines
// cannot collide, at every size (including very large zoom levels).
func TestLineHeightClearsGlyphExtent(t *testing.T) {
	data := loadTestFont(t)
	for _, pt := range []float64{minFontPt, 14, 27, maxFontPt, maxFontPt * fontMulLarge * 2} {
		face, err := parseFontFace(data, pt, 158) // 158 DPI = a HiDPI panel
		if err != nil {
			t.Fatalf("size %.0f: %v", pt, err)
		}
		m := face.Metrics()
		extent := (m.Ascent + m.Descent).Ceil()
		if adv := lineHeight(face); adv <= extent {
			t.Errorf("size %.0f: lineHeight=%d does not clear glyph extent=%d", pt, adv, extent)
		}
	}
}

// TestFooterY confirms the footer pins to the bottom with room, but flows below
// content (never overlaps) once the content grows past the pin line.
func TestFooterY(t *testing.T) {
	a := &App{height: 600}
	pinned := a.height - padding

	if got := a.footerY(100); got != pinned {
		t.Errorf("short content: footerY=%d, want pinned %d", got, pinned)
	}
	if got := a.footerY(pinned + 50); got != pinned+50 {
		t.Errorf("tall content: footerY=%d, want flowed %d", got, pinned+50)
	}
}

// TestClampFontPt checks the size bounds.
func TestClampFontPt(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{minFontPt - 5, minFontPt},
		{maxFontPt + 5, maxFontPt},
		{14, 14},
	}
	for _, c := range cases {
		if got := clampFontPt(c.in); got != c.want {
			t.Errorf("clampFontPt(%.0f)=%.0f, want %.0f", c.in, got, c.want)
		}
	}
}
