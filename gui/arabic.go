package gui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"strings"

	"github.com/go-text/render"
	gtfont "github.com/go-text/typesetting/font"
	"golang.org/x/image/font"
)

// Arabic-script support.
//
// The rest of the GUI draws text with golang.org/x/image's font.Drawer, which
// maps each rune to a glyph and advances left-to-right. That is fine for Latin
// and CJK (whose glyphs are self-contained and laid out left-to-right) but
// wrong for Arabic-script languages such as Persian: their letters join
// cursively (each takes an isolated/initial/medial/final form by context) and
// run right-to-left. Those joined forms live in the font's OpenType GSUB tables
// and are only reachable through a shaping engine. So Arabic lines are detected
// here and rendered with go-text, which shapes (HarfBuzz-derived) and lays out
// the run; everything else keeps the original, cheaper path untouched.

// arabicFontPaths lists Arabic-capable fonts in preference order. The GUI's
// usual font (Noto Sans CJK) contains no Arabic glyphs, so a separate face is
// needed for these scripts. Vazirmatn leads: it's Persian-first (its letter
// forms and digits are Persian, not Arabic) and, being a modern sans, it
// harmonizes with the Sans UI font where Naskh reads ornate next to it.
var arabicFontPaths = []string{
	"/usr/share/fonts/truetype/vazirmatn/Vazirmatn-Regular.ttf",
	"/usr/share/fonts/vazirmatn/Vazirmatn-Regular.ttf",
	"/usr/local/share/fonts/truetype/vazirmatn/Vazirmatn-Regular.ttf",
	"/usr/share/fonts/truetype/noto/NotoNaskhArabic-Regular.ttf",
	"/usr/local/share/fonts/truetype/noto/NotoNaskhArabic-Regular.ttf",
	"/usr/share/fonts/noto/NotoNaskhArabic-Regular.ttf",
	"/usr/share/fonts/truetype/noto/NotoSansArabic-Regular.ttf",
	"/usr/local/share/fonts/truetype/noto/NotoSansArabic-Regular.ttf",
	"/usr/share/fonts/noto/NotoSansArabic-Regular.ttf",
	"/usr/share/fonts/TTF/Amiri-Regular.ttf",
}

// loadArabicFont finds and parses an Arabic-capable font and prepares the
// shaping renderer. It is best-effort: if no such font is available, arabicFace
// stays nil and Arabic text falls back to the plain (unshaped) drawer. Always
// initializes the renderer and scratch measure image so the draw path is safe.
func (a *App) loadArabicFont() {
	a.arabicRenderer = &render.Renderer{}
	a.measureImg = image.NewRGBA(image.Rect(0, 0, 1, 1))

	paths := append([]string{}, arabicFontPaths...)
	if p := fcMatchArabic(); p != "" {
		paths = append(paths, p)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		// bytes.Reader satisfies go-text's Resource (Read/ReadAt/Seek) and keeps
		// the data alive for the lifetime of the face, which reads tables lazily.
		if face, err := gtfont.ParseTTF(bytes.NewReader(data)); err == nil {
			a.arabicFace = face
			return
		}
	}
}

// fcMatchArabic asks fontconfig for an Arabic-script font file, trying known
// families. fc-match always returns *some* font, so the result is only accepted
// when its path looks like an Arabic font — otherwise we'd load a Latin
// fallback with no Arabic glyphs.
func fcMatchArabic() string {
	for _, fam := range []string{"Vazirmatn", "Noto Naskh Arabic", "Noto Sans Arabic", "Amiri", "Scheherazade"} {
		out, err := exec.Command("fc-match", "--format=%{file}", fam).Output()
		if err != nil {
			continue
		}
		p := strings.TrimSpace(string(out))
		lp := strings.ToLower(p)
		if p == "" {
			continue
		}
		for _, hint := range []string{"arab", "naskh", "vazir", "amiri", "scheher"} {
			if strings.Contains(lp, hint) {
				return p
			}
		}
	}
	return ""
}

// hasArabic reports whether s contains any Arabic-script character, i.e. text
// that needs shaping and RTL layout. Covers the Arabic block and its
// supplements plus the presentation-form blocks.
func hasArabic(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x0600 && r <= 0x06FF, // Arabic
			r >= 0x0750 && r <= 0x077F, // Arabic Supplement
			r >= 0x08A0 && r <= 0x08FF, // Arabic Extended-A
			r >= 0xFB50 && r <= 0xFDFF, // Arabic Presentation Forms-A
			r >= 0xFE70 && r <= 0xFEFF: // Arabic Presentation Forms-B
			return true
		}
	}
	return false
}

// drawCardText draws a centered card-content line (the large question/answer
// role).
func (a *App) drawCardText(canvas *image.RGBA, text string, y int, c color.RGBA) {
	a.drawShapedCentered(canvas, text, y, fontMulLarge, a.fontLarge, c)
}

// drawShapedCentered draws a centered line at the given size (multiplier plus
// the matching plain face). Arabic-script lines are shaped and laid out RTL via
// go-text; all other text uses the original drawer. Falls back to the plain
// drawer if no Arabic font loaded, so the text still appears (unshaped) rather
// than vanishing.
func (a *App) drawShapedCentered(canvas *image.RGBA, text string, y int, mul float64, plain font.Face, c color.RGBA) {
	if hasArabic(text) {
		if a.arabicFace != nil {
			a.drawArabicCentered(canvas, text, y, mul, c)
			return
		}
		// No Arabic font: the plain drawer below still shows the text, but
		// unshaped and left-to-right. Note it once so the degradation isn't a
		// silent mystery.
		if !a.arabicWarned {
			fmt.Fprintln(os.Stderr, "study: no Arabic-capable font found; Arabic text shown unshaped (try installing fonts-noto-naskh-arabic)")
			a.arabicWarned = true
		}
	}
	a.drawTextCentered(canvas, text, y, plain, c)
}

// drawArabicCentered shapes text and draws it centered on the canvas with its
// baseline at y, at the given size multiplier. Everything is drawn with the
// Arabic face; the per-script Noto fonts carry no Latin glyphs, so Latin text
// mixed into the line comes out as tofu — callers keep scripts on separate
// lines instead. The line is measured first (by drawing onto a 1×1 scratch
// target, whose return value is the line's advance width) so it can be
// horizontally centered like the other content.
func (a *App) drawArabicCentered(canvas *image.RGBA, text string, y int, mul float64, c color.RGBA) {
	r := a.arabicRenderer
	// Match the corresponding opentype face: point size × multiplier × window
	// scale, with DPI applied via PixScale (px = FontSize × PixScale = pt × dpi/72).
	r.FontSize = float32(a.baseFontPt * mul * a.windowScale())
	r.PixScale = float32(a.dpi / 72.0)
	r.Color = c

	width := a.measureArabic(text, mul)
	x := (a.width - width) / 2
	if x < padding {
		x = padding
	}
	r.DrawStringAt(text, canvas, x, y, a.arabicFace)
}

// measureArabic returns the shaped pixel width of text at the given size
// multiplier: the advance DrawStringAt reports against the 1×1 scratch
// target. It's how shaped lines are centered, and how they're wrapped.
func (a *App) measureArabic(text string, mul float64) int {
	r := a.arabicRenderer
	r.FontSize = float32(a.baseFontPt * mul * a.windowScale())
	r.PixScale = float32(a.dpi / 72.0)
	return r.DrawStringAt(text, a.measureImg, 0, 0, a.arabicFace)
}

// wrapCardText soft-wraps one authored line of card content to fit maxW,
// measuring with whichever pipeline will draw it — the shaped Arabic renderer
// or the plain large face — so the wrap points match what lands on screen.
func (a *App) wrapCardText(text string, maxW int) []string {
	if hasArabic(text) && a.arabicFace != nil {
		return wrapLines(text, maxW, func(s string) int { return a.measureArabic(s, fontMulLarge) })
	}
	return wrapLines(text, maxW, func(s string) int { return font.MeasureString(a.fontLarge, s).Round() })
}
