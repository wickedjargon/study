// Package gui provides an X11 window for the quiz, inspired by suckless sent.
package gui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/keybind"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xgraphics"
	"github.com/BurntSushi/xgbutil/xwindow"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"

	imgdraw "golang.org/x/image/draw"

	"study/deck"
	"study/media"
	"study/progress"
	"study/quiz"
)

// ── Colors ──────────────────────────────────────────────────────────

type colorScheme struct {
	bg      color.RGBA
	text    color.RGBA
	dim     color.RGBA
	green   color.RGBA
	red     color.RGBA
	yellow  color.RGBA
	accent  color.RGBA
}

var darkScheme = colorScheme{
	bg:     color.RGBA{0x11, 0x18, 0x27, 0xff},
	text:   color.RGBA{0xf9, 0xfa, 0xfb, 0xff},
	dim:    color.RGBA{0x6b, 0x72, 0x80, 0xff},
	green:  color.RGBA{0x22, 0xc5, 0x5e, 0xff},
	red:    color.RGBA{0xef, 0x44, 0x44, 0xff},
	yellow: color.RGBA{0xea, 0xb3, 0x08, 0xff},
	accent: color.RGBA{0x8b, 0x5c, 0xf6, 0xff},
}

var lightScheme = colorScheme{
	bg:     color.RGBA{0xfb, 0xf1, 0xc7, 0xff},
	text:   color.RGBA{0x3c, 0x38, 0x36, 0xff},
	dim:    color.RGBA{0x92, 0x83, 0x74, 0xff},
	green:  color.RGBA{0x79, 0x74, 0x0e, 0xff},
	red:    color.RGBA{0x9d, 0x00, 0x06, 0xff},
	yellow: color.RGBA{0xb5, 0x76, 0x14, 0xff},
	accent: color.RGBA{0x07, 0x66, 0x78, 0xff},
}

// Active colors — set at startup based on detected theme.
var (
	bgColor     color.RGBA
	textColor   color.RGBA
	dimColor    color.RGBA
	greenColor  color.RGBA
	redColor    color.RGBA
	yellowColor color.RGBA
	accentColor color.RGBA
)

func applyScheme(s colorScheme) {
	bgColor = s.bg
	textColor = s.text
	dimColor = s.dim
	greenColor = s.green
	redColor = s.red
	yellowColor = s.yellow
	accentColor = s.accent
}

// detectTheme determines dark/light preference.
// Priority: gsettings → ~/.config/theme-mode → dark.
func detectTheme() string {
	// 1. Try gsettings (FreeDesktop standard).
	if out, err := exec.Command("gsettings", "get",
		"org.gnome.desktop.interface", "color-scheme").Output(); err == nil {
		val := strings.TrimSpace(string(out))
		if strings.Contains(val, "prefer-light") {
			return "light"
		}
		if strings.Contains(val, "prefer-dark") {
			return "dark"
		}
	}

	// 2. Try ~/.config/theme-mode file.
	home, _ := os.UserHomeDir()
	if data, err := os.ReadFile(home + "/.config/theme-mode"); err == nil {
		mode := strings.TrimSpace(string(data))
		if mode == "light" {
			return "light"
		}
		if mode == "dark" {
			return "dark"
		}
	}

	// 3. Default to dark.
	return "dark"
}

// ── App ─────────────────────────────────────────────────────────────

// App holds the GUI state.
type App struct {
	xu     *xgbutil.XUtil
	win    *xwindow.Window
	engine *quiz.Engine
	viewer *media.Viewer
	store  *progress.Store
	result *quiz.Result
	start  time.Time
	width  int
	height int

	// Fonts.
	fontRegular font.Face
	fontBold    font.Face
	fontSmall   font.Face
	fontLarge   font.Face

	// Cached question image (loaded from disk).
	questionImg image.Image

	// Text input buffer (type mode).
	inputBuf string
}

const (
	defaultWidth  = 800
	defaultHeight = 600
	padding       = 40
)

// Run launches the X11 quiz window.
func Run(engine *quiz.Engine, viewer *media.Viewer, store *progress.Store) error {
	// Detect and apply theme.
	if detectTheme() == "light" {
		applyScheme(lightScheme)
	} else {
		applyScheme(darkScheme)
	}

	xu, err := xgbutil.NewConn()
	if err != nil {
		return fmt.Errorf("connecting to X: %w", err)
	}
	defer xu.Conn().Close()

	app := &App{
		xu:     xu,
		engine: engine,
		viewer: viewer,
		store:  store,
		start:  time.Now(),
		width:  defaultWidth,
		height: defaultHeight,
	}

	if err := app.loadFonts(); err != nil {
		return fmt.Errorf("loading fonts: %w", err)
	}

	// Create window.
	win, err := xwindow.Generate(xu)
	if err != nil {
		return fmt.Errorf("creating window: %w", err)
	}
	app.win = win

	// Convert bgColor to X11 pixel value.
	bgPixel := uint32(bgColor.R)<<16 | uint32(bgColor.G)<<8 | uint32(bgColor.B)

	win.Create(xu.RootWin(), 0, 0, app.width, app.height,
		xproto.CwBackPixel|xproto.CwEventMask,
		bgPixel,
		xproto.EventMaskExposure|
			xproto.EventMaskKeyPress|
			xproto.EventMaskStructureNotify)

	// Set window title.
	err = xproto.ChangePropertyChecked(xu.Conn(), xproto.PropModeReplace,
		win.Id, xproto.AtomWmName, xproto.AtomString, 8,
		uint32(len("study")), []byte("study")).Check()
	if err != nil {
		return fmt.Errorf("setting title: %w", err)
	}

	// Handle graceful close.
	win.WMGracefulClose(func(w *xwindow.Window) {
		app.quit()
	})

	win.Map()

	// Load first card's media.
	app.loadQuestionImage()

	// Play audio if any.
	app.playQuestionAudio()

	// Set up event handlers.
	keybind.Initialize(xu)

	xevent.KeyPressFun(func(xu *xgbutil.XUtil, ev xevent.KeyPressEvent) {
		app.handleKey(ev)
	}).Connect(xu, win.Id)

	xevent.ExposeFun(func(xu *xgbutil.XUtil, ev xevent.ExposeEvent) {
		app.render()
	}).Connect(xu, win.Id)

	xevent.ConfigureNotifyFun(func(xu *xgbutil.XUtil, ev xevent.ConfigureNotifyEvent) {
		app.width = int(ev.Width)
		app.height = int(ev.Height)
		app.render()
	}).Connect(xu, win.Id)

	// Initial render.
	app.render()

	// Main event loop.
	xevent.Main(xu)
	return nil
}

// ── Event Handling ──────────────────────────────────────────────────

func (a *App) handleKey(ev xevent.KeyPressEvent) {
	key := keybind.LookupString(a.xu, ev.State, ev.Detail)

	switch a.engine.State() {
	case quiz.ShowQuestion:
		if a.engine.Mode() == deck.ModeType {
			a.handleTypeKey(key)
		} else {
			a.handleChoiceKey(key)
		}

	case quiz.ShowResult:
		switch key {
		case "Return", "space":
			a.viewer.CloseAll()
			a.engine.Next()
			a.result = nil
			a.inputBuf = ""

			if a.engine.State() == quiz.ShowQuestion {
				a.loadQuestionImage()
				a.playQuestionAudio()
			} else if a.engine.State() == quiz.Done {
				a.viewer.CloseAll()
				a.saveProgress()
			}
			a.render()
		case "Escape":
			a.quit()
		}

	case quiz.Done:
		switch key {
		case "Escape", "Return":
			a.quit()
		}
	}
}

func (a *App) handleChoiceKey(key string) {
	switch key {
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key[0]-'0') - 1
		opts := a.engine.Options()
		if idx >= len(opts) {
			return
		}
		a.result = a.engine.Answer(idx)
		if a.result != nil {
			if a.result.Correct {
				a.store.RecordCorrect(a.result.Card.ID)
			} else {
				a.store.RecordWrong(a.result.Card.ID)
			}
		}
		a.render()
	case "Escape":
		a.quit()
	}
}

func (a *App) handleTypeKey(key string) {
	switch key {
	case "Return":
		a.result = a.engine.AnswerTyped(a.inputBuf)
		if a.result != nil {
			if a.result.Correct {
				a.store.RecordCorrect(a.result.Card.ID)
			} else {
				a.store.RecordWrong(a.result.Card.ID)
			}
		}
		a.render()
	case "BackSpace":
		if len(a.inputBuf) > 0 {
			// Remove last rune (handles multibyte).
			runes := []rune(a.inputBuf)
			a.inputBuf = string(runes[:len(runes)-1])
			a.render()
		}
	case "Escape":
		a.quit()
	case "space":
		a.inputBuf += " "
		a.render()
	default:
		// Single printable character.
		if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
			a.inputBuf += key
			a.render()
		}
	}
}

func (a *App) quit() {
	a.viewer.CloseAll()
	a.saveProgress()
	xevent.Quit(a.xu)
}

func (a *App) saveProgress() {
	_ = a.store.Save()
}

// ── Media Loading ───────────────────────────────────────────────────

func (a *App) loadQuestionImage() {
	a.questionImg = nil
	card := a.engine.Current()
	if card == nil {
		return
	}
	for _, m := range card.Question {
		if m.Type == deck.Image {
			if img, err := loadImage(m.Content); err == nil {
				a.questionImg = img
			}
			return
		}
	}
}

func (a *App) playQuestionAudio() {
	card := a.engine.Current()
	if card == nil {
		return
	}
	var audioMedia []deck.Media
	for _, m := range card.Question {
		if m.Type == deck.Audio {
			audioMedia = append(audioMedia, m)
		}
	}
	if len(audioMedia) > 0 {
		_ = a.viewer.ShowMedia(audioMedia)
	}
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// ── Rendering ───────────────────────────────────────────────────────

func (a *App) render() {
	// Create a standard RGBA image, draw everything, then convert for X11.
	canvas := image.NewRGBA(image.Rect(0, 0, a.width, a.height))

	// Fill background.
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(bgColor), image.Point{}, draw.Src)

	switch a.engine.State() {
	case quiz.ShowQuestion:
		a.renderQuestion(canvas)
	case quiz.ShowResult:
		a.renderResult(canvas)
	case quiz.Done:
		a.renderSummary(canvas)
	}

	// Convert and paint to X11 window.
	ximg := xgraphics.NewConvert(a.xu, canvas)
	ximg.XSurfaceSet(a.win.Id)
	ximg.XDraw()
	ximg.XPaint(a.win.Id)
	ximg.Destroy()
}

func (a *App) renderQuestion(canvas *image.RGBA) {
	card := a.engine.Current()
	if card == nil {
		return
	}

	y := padding

	// Progress indicator.
	seen := a.engine.TotalSeen
	remaining := a.engine.Remaining()
	prog := fmt.Sprintf("[%d/%d]", seen+1, seen+remaining)
	if a.engine.IsRetry() {
		prog += "  ↻ retry"
	}
	a.drawText(canvas, prog, padding, y, a.fontSmall, dimColor)
	y += 30

	// Question image.
	if a.questionImg != nil {
		imgH := a.renderImage(canvas, a.questionImg, y)
		y += imgH + 20
	}

	// Question text.
	for _, m := range card.Question {
		if m.Type == deck.Text {
			a.drawTextCentered(canvas, m.Content, y, a.fontLarge, textColor)
			y += 40
		}
	}

	y += 10

	if a.engine.Mode() == deck.ModeType {
		// Text input field.
		prompt := "> " + a.inputBuf + "_"
		a.drawText(canvas, prompt, padding+20, y, a.fontRegular, accentColor)
		y += 32

		// Help.
		a.drawTextCentered(canvas, "type answer + enter  |  esc: quit", a.height-padding, a.fontSmall, dimColor)
	} else {
		// Choices.
		opts := a.engine.Options()
		for i, opt := range opts {
			line := fmt.Sprintf("%d)  %s", i+1, opt)
			a.drawText(canvas, line, padding+20, y, a.fontRegular, textColor)
			y += 32
		}

		// Help.
		help := fmt.Sprintf("1-%d: answer  |  esc: quit", len(opts))
		a.drawTextCentered(canvas, help, a.height-padding, a.fontSmall, dimColor)
	}
}

func (a *App) renderResult(canvas *image.RGBA) {
	if a.result == nil {
		return
	}
	card := a.result.Card
	y := padding

	// Progress.
	seen := a.engine.TotalSeen
	remaining := a.engine.Remaining()
	prog := fmt.Sprintf("[%d/%d]", seen, seen+remaining)
	a.drawText(canvas, prog, padding, y, a.fontSmall, dimColor)
	y += 30

	// Question image.
	if a.questionImg != nil {
		imgH := a.renderImage(canvas, a.questionImg, y)
		y += imgH + 20
	}

	// Question text.
	for _, m := range card.Question {
		if m.Type == deck.Text {
			a.drawTextCentered(canvas, m.Content, y, a.fontLarge, textColor)
			y += 40
		}
	}

	y += 10

	if a.engine.Mode() == deck.ModeType {
		// Show what was typed.
		if a.result.Correct {
			a.drawText(canvas, "> "+a.result.Typed+"  ✓", padding+20, y, a.fontRegular, greenColor)
		} else {
			a.drawText(canvas, "> "+a.result.Typed+"  X", padding+20, y, a.fontRegular, redColor)
			y += 32
			a.drawText(canvas, "= "+a.result.Answer, padding+20, y, a.fontRegular, greenColor)
		}
		y += 32
	} else {
		// Choices with highlighting.
		opts := a.engine.Options()
		for i, opt := range opts {
			line := fmt.Sprintf("%d)  %s", i+1, opt)
			c := dimColor
			if opt == a.result.Answer {
				line += "  ✓"
				c = greenColor
			} else if i == a.result.Chosen && !a.result.Correct {
				line += "  X"
				c = redColor
			}
			a.drawText(canvas, line, padding+20, y, a.fontRegular, c)
			y += 32
		}
	}

	y += 16

	// Result message.
	if a.result.Correct {
		a.drawTextCentered(canvas, "✓  Correct!", y, a.fontBold, greenColor)
	} else {
		msg := "X  Wrong — answer: " + a.result.Answer
		a.drawTextCentered(canvas, msg, y, a.fontBold, redColor)
	}

	// Help.
	a.drawTextCentered(canvas, "enter: continue  •  esc: quit", a.height-padding, a.fontSmall, dimColor)
}

func (a *App) renderSummary(canvas *image.RGBA) {
	elapsed := time.Since(a.start).Round(time.Second)
	y := a.height/4

	a.drawTextCentered(canvas, "Session Complete", y, a.fontLarge, accentColor)
	y += 60

	stats := []struct {
		label string
		value string
		color color.RGBA
	}{
		{"Cards studied", fmt.Sprintf("%d", a.engine.TotalSeen), textColor},
		{"Correct", fmt.Sprintf("%d", a.engine.TotalCorrect), greenColor},
		{"Wrong", fmt.Sprintf("%d", a.engine.TotalWrong), redColor},
	}

	// Accuracy.
	acc := float64(0)
	if a.engine.TotalSeen > 0 {
		acc = float64(a.engine.TotalCorrect) / float64(a.engine.TotalSeen) * 100
	}
	accColor := greenColor
	if acc < 80 {
		accColor = redColor
	}
	stats = append(stats, struct {
		label string
		value string
		color color.RGBA
	}{"Accuracy", fmt.Sprintf("%.0f%%", acc), accColor})

	stats = append(stats, struct {
		label string
		value string
		color color.RGBA
	}{"Time", elapsed.String(), textColor})

	for _, s := range stats {
		line := fmt.Sprintf("%-16s %s", s.label, s.value)
		a.drawTextCentered(canvas, line, y, a.fontRegular, s.color)
		y += 32
	}

	// All-time stats.
	totalCorrect, totalWrong, cardsStudied := a.store.Summary()
	if cardsStudied > 0 {
		y += 20
		a.drawTextCentered(canvas, "── All-time ──", y, a.fontSmall, dimColor)
		y += 32
		totalAcc := float64(0)
		if totalCorrect+totalWrong > 0 {
			totalAcc = float64(totalCorrect) / float64(totalCorrect+totalWrong) * 100
		}
		a.drawTextCentered(canvas, fmt.Sprintf("Cards seen: %d  •  Accuracy: %.0f%%", cardsStudied, totalAcc), y, a.fontRegular, dimColor)
		y += 32
	}

	a.drawTextCentered(canvas, "esc: exit", a.height-padding, a.fontSmall, dimColor)
}

// ── Drawing Helpers ─────────────────────────────────────────────────

func (a *App) drawText(canvas *image.RGBA, text string, x, y int, face font.Face, c color.RGBA) {
	d := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

func (a *App) drawTextCentered(canvas *image.RGBA, text string, y int, face font.Face, c color.RGBA) {
	d := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(c),
		Face: face,
	}
	width := d.MeasureString(text)
	x := (a.width - width.Round()) / 2
	if x < padding {
		x = padding
	}
	d.Dot = fixed.P(x, y)
	d.DrawString(text)
}

func (a *App) renderImage(canvas *image.RGBA, img image.Image, y int) int {
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()

	// Scale to fit within the available width and max 40% of window height.
	maxW := a.width - padding*2
	maxH := int(float64(a.height) * 0.4)

	scale := 1.0
	if imgW > maxW {
		scale = float64(maxW) / float64(imgW)
	}
	if int(float64(imgH)*scale) > maxH {
		scale = float64(maxH) / float64(imgH)
	}

	dstW := int(float64(imgW) * scale)
	dstH := int(float64(imgH) * scale)

	// Center horizontally.
	x := (a.width - dstW) / 2

	dstRect := image.Rect(x, y, x+dstW, y+dstH)
	imgdraw.BiLinear.Scale(canvas, dstRect, img, bounds, imgdraw.Over, nil)

	return dstH
}

// ── Font Loading ────────────────────────────────────────────────────

// System fonts with CJK support, searched in preference order.
// Noto Sans CJK covers both Latin and CJK in one font.
var systemFontPaths = []string{
	// Noto Sans CJK — best option, full Latin + CJK.
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/OTF/NotoSansCJK-Regular.ttc",
	// WenQuanYi — good CJK + Latin coverage.
	"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
	"/usr/share/fonts/wenquanyi/wqy-zenhei/wqy-zenhei.ttc",
	// Droid Fallback — CJK only, poor Latin. Last resort.
	"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
}

func (a *App) loadFonts() error {
	// Try system font with CJK support first.
	var sysFontData []byte
	for _, path := range systemFontPaths {
		if data, err := os.ReadFile(path); err == nil {
			sysFontData = data
			break
		}
	}

	// Choose font data: system CJK font or embedded Go font as fallback.
	regularData := goregular.TTF
	boldData := gobold.TTF
	if sysFontData != nil {
		regularData = sysFontData
		boldData = sysFontData // CJK fonts typically have one weight
	}

	regular, err := parseFontFace(regularData, 18)
	if err != nil {
		return err
	}
	a.fontRegular = regular

	bold, err := parseFontFace(boldData, 18)
	if err != nil {
		return err
	}
	a.fontBold = bold

	small, err := parseFontFace(regularData, 14)
	if err != nil {
		return err
	}
	a.fontSmall = small

	large, err := parseFontFace(boldData, 28)
	if err != nil {
		return err
	}
	a.fontLarge = large

	return nil
}

func parseFontFace(ttfData []byte, size float64) (font.Face, error) {
	// Try parsing as TTC (font collection) first, then as single font.
	collection, err := opentype.ParseCollection(ttfData)
	if err == nil {
		f, err := collection.Font(0)
		if err != nil {
			return nil, err
		}
		return opentype.NewFace(f, &opentype.FaceOptions{
			Size:    size,
			DPI:     72,
			Hinting: font.HintingFull,
		})
	}

	f, err := opentype.Parse(ttfData)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	return face, nil
}

// suppress unused import for strings
