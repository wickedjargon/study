package gui

import (
	"image"
	"image/color"
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
	f, err := parseFont(data)
	if err != nil {
		t.Fatalf("parseFont: %v", err)
	}
	for _, pt := range []float64{minFontPt, 14, 27, maxFontPt, maxFontPt * fontMulLarge * 2} {
		face, err := newFace(f, pt, 158) // 158 DPI = a HiDPI panel
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

// TestControlsBoxAnchorsBottomRight confirms the controls box hugs the
// bottom-right corner — its border lands exactly at the padding inset — and
// stays within the canvas.
func TestControlsBoxAnchorsBottomRight(t *testing.T) {
	data := loadTestFont(t)
	f, err := parseFont(data)
	if err != nil {
		t.Fatalf("parseFont: %v", err)
	}
	face, err := newFace(f, 10, 96)
	if err != nil {
		t.Fatalf("newFace: %v", err)
	}

	a := &App{width: 800, height: 600, baseFontPt: 10, dpi: 96, fontSmall: face}
	oldDim := dimColor
	dimColor = color.RGBA{9, 9, 9, 255}
	defer func() { dimColor = oldDim }()

	canvas := image.NewRGBA(image.Rect(0, 0, a.width, a.height))
	a.drawControlsBox(canvas, []string{"enter: continue", "esc: end"})

	corner := image.Pt(a.width-padding-1, a.height-padding-1)
	if got := canvas.RGBAAt(corner.X, corner.Y); got != dimColor {
		t.Errorf("border pixel at %v = %v, want %v", corner, got, dimColor)
	}
	// Nothing may spill past the inset into the window edge.
	for x := a.width - padding; x < a.width; x++ {
		if got := canvas.RGBAAt(x, corner.Y); got != (color.RGBA{}) {
			t.Fatalf("pixel right of the inset at x=%d is drawn: %v", x, got)
		}
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
