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
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/keybind"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xgraphics"
	"github.com/BurntSushi/xgbutil/xprop"
	"github.com/BurntSushi/xgbutil/xwindow"

	"github.com/go-text/render"
	gtfont "github.com/go-text/typesetting/font"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	imgdraw "golang.org/x/image/draw"

	"study/deck"
	"study/media"
	"study/progress"
	"study/quiz"
)

// ── Colors ──────────────────────────────────────────────────────────

type colorScheme struct {
	bg     color.RGBA
	text   color.RGBA
	dim    color.RGBA
	green  color.RGBA
	red    color.RGBA
	yellow color.RGBA
	accent color.RGBA
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

	// deadline is when the current question's timer expires.
	// Zero means the current card has no time limit.
	deadline time.Time

	// Fonts. The faces are rebuilt whenever the window is resized so text scales
	// with the window; dpi is the detected display resolution used to render
	// points at a physically sane size. The parsed *opentype.Font values are
	// cached so a resize only re-runs the cheap NewFace step, not a full re-parse
	// of the (potentially tens-of-MB) font file.
	fontRegular     font.Face
	fontBold        font.Face
	fontSmall       font.Face
	fontLarge       font.Face
	fontRegularFont *opentype.Font
	fontBoldFont    *opentype.Font
	dpi             float64
	baseFontPt      float64 // current base point size; adjustable with Ctrl+=/-
	initialFontPt   float64 // the starting base size; Ctrl+0 resets to this

	// Arabic-script rendering. Go's font.Drawer does no contextual shaping or
	// RTL layout, so Arabic/Persian text (which joins cursively and reads
	// right-to-left) is instead shaped and drawn through go-text. arabicFace is
	// nil when no Arabic-capable font is found, in which case such text falls
	// back to the plain drawer. measureImg is a scratch target used only to
	// measure a shaped line's width (for centering) before drawing it for real.
	arabicFace     *gtfont.Face
	arabicRenderer *render.Renderer
	measureImg     *image.RGBA

	// Cached question image (loaded from disk).
	questionImg image.Image

	// Text input buffer (type mode).
	inputBuf string

	// audioWarned suppresses repeat stderr warnings when audio playback fails
	// (e.g. no player installed) — we report it once rather than on every card.
	audioWarned bool

	// audioSpeed is the playback speed multiplier applied to question audio
	// (1.0 = normal). It persists across cards and is adjusted at runtime with
	// Ctrl+, / Ctrl+. (and reset with Ctrl+/), clamped to [minSpeed,maxSpeed].
	audioSpeed float64

	// Interned atoms for clipboard/primary paste. Zero until setupAtoms runs.
	clipboardAtom xproto.Atom // CLIPBOARD selection (Ctrl+V source)
	utf8Atom      xproto.Atom // UTF8_STRING conversion target
	selPropAtom   xproto.Atom // scratch property the selection is delivered to
}

const (
	defaultWidth  = 800
	defaultHeight = 600
	padding       = 40
)

// Type scale. All face sizes derive from a per-window base point size (the
// App.baseFontPt field) times a per-role multiplier, then scaled by window
// size and rendered at the detected DPI. The base size starts at defaultFontPt
// (or the deck's "# font-size:" directive) and the user nudges it at runtime
// with Ctrl+= / Ctrl+-, in fontStepPt increments, clamped to [min,max]FontPt.
const (
	defaultFontPt = 14.0 // base size when the deck doesn't set one
	minFontPt     = 8.0
	maxFontPt     = 40.0
	fontStepPt    = 2.0

	fontMulSmall   = 0.75 // hints, progress, timer
	fontMulRegular = 1.0  // prompts, answers, body
	fontMulLarge   = 1.5  // card content, headers
)

// Audio playback speed. The multiplier starts at defaultSpeed (or the deck's
// "# speed:" directive) and the user nudges it at runtime with Ctrl+, / Ctrl+.
// in speedStep increments, clamped to [minSpeed,maxSpeed]; Ctrl+/ resets it.
const (
	defaultSpeed = 1.0
	minSpeed     = 0.25
	maxSpeed     = 4.0
	speedStep    = 0.25
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

	// Base font size: the deck's "# font-size:" directive if set, else default.
	app.baseFontPt = defaultFontPt
	if pt := engine.FontSize(); pt > 0 {
		app.baseFontPt = clampFontPt(float64(pt))
	}
	app.initialFontPt = app.baseFontPt // Ctrl+0 returns here

	// Audio speed: the deck's "# speed:" directive if set, else default.
	app.audioSpeed = defaultSpeed
	if x := engine.Speed(); x > 0 {
		app.audioSpeed = clampSpeed(x)
	}

	if err := app.loadFonts(); err != nil {
		return fmt.Errorf("loading fonts: %w", err)
	}

	// Load an Arabic-capable font for shaping RTL scripts (best-effort; decks
	// without Arabic text are unaffected if none is found).
	app.loadArabicFont()

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
			xproto.EventMaskButtonPress|
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

	// Start the countdown for the first card (if it has a time limit).
	app.startTimer()

	// Set up event handlers.
	keybind.Initialize(xu)
	app.setupAtoms()

	xevent.KeyPressFun(func(xu *xgbutil.XUtil, ev xevent.KeyPressEvent) {
		app.handleKey(ev)
	}).Connect(xu, win.Id)

	xevent.ButtonPressFun(func(xu *xgbutil.XUtil, ev xevent.ButtonPressEvent) {
		app.handleButton(ev)
	}).Connect(xu, win.Id)

	xevent.SelectionNotifyFun(func(xu *xgbutil.XUtil, ev xevent.SelectionNotifyEvent) {
		app.handleSelectionNotify(ev)
	}).Connect(xu, win.Id)

	xevent.ExposeFun(func(xu *xgbutil.XUtil, ev xevent.ExposeEvent) {
		app.render()
	}).Connect(xu, win.Id)

	xevent.ConfigureNotifyFun(func(xu *xgbutil.XUtil, ev xevent.ConfigureNotifyEvent) {
		if int(ev.Width) == app.width && int(ev.Height) == app.height {
			return // position-only change; nothing to redraw
		}
		app.width = int(ev.Width)
		app.height = int(ev.Height)
		// Rescale fonts to the new window size. Keep the old faces on error.
		if err := app.buildFonts(); err != nil {
			fmt.Fprintf(os.Stderr, "rescaling fonts: %v\n", err)
		}
		app.render()
	}).Connect(xu, win.Id)

	// Initial render.
	app.render()

	// Main event loop. We use MainPing instead of the simpler xevent.Main so
	// we can interleave a periodic timer tick that drives the per-question
	// countdown. The pingBefore/pingAfter handshake guarantees that X event
	// callbacks (key presses, etc.) never run concurrently with a timer tick.
	pingBefore, pingAfter, pingQuit := xevent.MainPing(xu)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-pingBefore:
			<-pingAfter
		case <-ticker.C:
			app.tick()
		case <-pingQuit:
			return nil
		}
	}
}

// startTimer arms the countdown for the current question. If the current card
// has no time limit, the deadline is cleared.
func (a *App) startTimer() {
	secs := a.engine.TimeLimit()
	if secs > 0 {
		a.deadline = time.Now().Add(time.Duration(secs) * time.Second)
	} else {
		a.deadline = time.Time{}
	}
}

// tick is called periodically from the event loop. While a question with a
// time limit is showing, it re-renders the countdown and, once the deadline
// passes, records the card as wrong.
func (a *App) tick() {
	if a.deadline.IsZero() || a.engine.State() != quiz.ShowQuestion {
		return
	}
	if time.Now().Before(a.deadline) {
		a.render() // update the on-screen countdown
		return
	}
	// Time's up — count the card as wrong, just like an incorrect answer.
	a.deadline = time.Time{}
	a.result = a.engine.AnswerTimeout()
	// The engine records the outcome to the store; persist it now so an
	// ungraceful exit can't lose this answer.
	a.saveProgress()
	a.render()
}

// secondsLeft returns the whole seconds remaining on the current question's
// timer, or -1 if there is no active time limit.
func (a *App) secondsLeft() int {
	if a.deadline.IsZero() {
		return -1
	}
	d := time.Until(a.deadline)
	if d < 0 {
		return 0
	}
	// Round up so the display counts N..1 rather than N-1..0.
	return int((d + time.Second - 1) / time.Second)
}

// ── Event Handling ──────────────────────────────────────────────────

func (a *App) handleKey(ev xevent.KeyPressEvent) {
	key := keybind.LookupString(a.xu, ev.State, ev.Detail)

	// Ctrl+= / Ctrl+- adjust the font size in any state and either mode.
	// Handled before dispatch so the keys never land in the type-mode buffer.
	// LookupString collapses these keysyms to their characters ("=", "+", "-"),
	// including the keypad variants, so we match on those rather than names.
	if ev.State&xproto.ModMaskControl > 0 {
		switch key {
		case "=", "+":
			a.changeFontSize(fontStepPt)
			return
		case "-":
			a.changeFontSize(-fontStepPt)
			return
		case "0":
			a.setFontSize(a.initialFontPt)
			return
		}
	}

	switch a.engine.State() {
	case quiz.ShowQuestion:
		// Audio controls work in either mode and are handled before mode dispatch
		// so they never land in the type-mode answer buffer. Ctrl+R replays at the
		// current speed; Ctrl+, / Ctrl+. slow down / speed up (and replay, so the
		// change is heard immediately); Ctrl+/ resets to normal speed.
		// LookupString collapses the keysyms to their characters, including the
		// shifted variants (< > ?), so we match on those rather than names.
		if ev.State&xproto.ModMaskControl > 0 {
			switch {
			case key == "r" || key == "R":
				a.playQuestionAudio()
				return
			case key == "," || key == "<":
				a.changeSpeed(-speedStep)
				return
			case key == "." || key == ">":
				a.changeSpeed(speedStep)
				return
			case key == "/" || key == "?":
				a.setSpeed(defaultSpeed)
				return
			}
		}
		if a.engine.Mode() == deck.ModeType {
			a.handleTypeKey(key, ev)
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
				a.startTimer()
			} else if a.engine.State() == quiz.Done {
				a.deadline = time.Time{}
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
		// The engine records the outcome; persist immediately so an ungraceful
		// exit can't lose this answer.
		a.saveProgress()
		a.render()
	case "Escape":
		a.quit()
	}
}

func (a *App) handleTypeKey(key string, ev xevent.KeyPressEvent) {
	switch key {
	case "Return":
		a.result = a.engine.AnswerTyped(a.inputBuf)
		// The engine records the outcome; persist immediately so an ungraceful
		// exit can't lose this answer.
		a.saveProgress()
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
	default:
		// Ctrl combos are commands, not text. Ctrl+V pastes the CLIPBOARD
		// selection; every other Ctrl combo is swallowed so it can't leak a
		// stray character into the buffer (LookupString ignores Ctrl, so e.g.
		// Ctrl+V would otherwise insert a literal "v").
		if ev.State&xproto.ModMaskControl > 0 {
			if key == "v" || key == "V" {
				a.requestPaste(a.clipboardAtom, ev.Time)
			}
			return
		}
		if r, ok := a.typedRune(key, ev); ok {
			a.inputBuf += string(r)
			a.render()
		}
	}
}

// typedRune resolves a printable character from a key press for text input.
//
// keybind.LookupString already returns the correct character for ASCII keys
// (and space/punctuation), honouring shift and caps-lock — so a single
// printable rune is used as-is. For non-ASCII keys it instead returns the
// keysym *name* (e.g. "aacute" for á) or an empty string (for Unicode
// keysyms), neither of which is the character the user typed. In that case we
// resolve the rune from the keysym directly.
func (a *App) typedRune(key string, ev xevent.KeyPressEvent) (rune, bool) {
	if r, n := utf8.DecodeRuneInString(key); n == len(key) && unicode.IsPrint(r) {
		return r, true
	}

	col := byte(0)
	if ev.State&xproto.ModMaskShift > 0 {
		col = 1
	}
	return keysymToRune(keybind.KeysymGet(a.xu, ev.Detail, col))
}

// keysymToRune converts an X11 keysym to its Unicode rune using the standard
// keysym→UCS rules: Latin-1 keysyms (which include ASCII) map directly to the
// same code point, and "Unicode keysyms" carry the code point in their low 24
// bits (0x01000000 | cp). The latter is how non-Latin layouts and tools like
// `xdotool type` deliver characters, so this covers most real input. Legacy
// national keysyms (Cyrillic_*, Greek_*, …) are not mapped here, and IME
// composition (XIM) is not received by this X stack at all.
func keysymToRune(ks xproto.Keysym) (rune, bool) {
	k := uint32(ks)
	switch {
	case k&0xff000000 == 0x01000000:
		if r := rune(k & 0x00ffffff); unicode.IsPrint(r) {
			return r, true
		}
	case (k >= 0x20 && k <= 0x7e) || (k >= 0xa0 && k <= 0xff):
		if r := rune(k); unicode.IsPrint(r) {
			return r, true
		}
	}
	return 0, false
}

// ── Clipboard paste ─────────────────────────────────────────────────
//
// Type mode supports pasting so that text which can't be entered directly —
// notably CJK composed via an IME, which this X stack never receives — can
// still be brought in: compose it elsewhere, then paste. Ctrl+V pastes the
// CLIPBOARD selection and middle-click pastes the PRIMARY selection, per X11
// convention.

const selPropName = "STUDY_SELECTION"

// setupAtoms interns the atoms used for selection paste. Failures are left as
// zero atoms; requestPaste then no-ops, so paste is simply unavailable rather
// than fatal.
func (a *App) setupAtoms() {
	if at, err := xprop.Atm(a.xu, "CLIPBOARD"); err == nil {
		a.clipboardAtom = at
	}
	if at, err := xprop.Atm(a.xu, "UTF8_STRING"); err == nil {
		a.utf8Atom = at
	}
	if at, err := xprop.Atm(a.xu, selPropName); err == nil {
		a.selPropAtom = at
	}
}

// handleButton pastes the PRIMARY selection on a middle-click while a typed
// answer is being entered.
func (a *App) handleButton(ev xevent.ButtonPressEvent) {
	const middleButton = 2
	if ev.Detail != middleButton {
		return
	}
	if a.engine.State() != quiz.ShowQuestion || a.engine.Mode() != deck.ModeType {
		return
	}
	a.requestPaste(xproto.AtomPrimary, ev.Time)
}

// requestPaste asks the owner of the given selection to convert it to UTF-8
// text into our scratch property. The reply arrives later as a SelectionNotify
// event (handleSelectionNotify).
func (a *App) requestPaste(selection xproto.Atom, t xproto.Timestamp) {
	if selection == 0 || a.utf8Atom == 0 || a.selPropAtom == 0 {
		return
	}
	xproto.ConvertSelection(a.xu.Conn(), a.win.Id, selection, a.utf8Atom, a.selPropAtom, t)
}

// handleSelectionNotify reads the pasted text delivered by a ConvertSelection
// request and appends its printable characters to the input buffer.
func (a *App) handleSelectionNotify(ev xevent.SelectionNotifyEvent) {
	// Property is None (0) when no owner held the selection or the conversion
	// was refused.
	if ev.Property == 0 {
		return
	}
	if a.engine.State() != quiz.ShowQuestion || a.engine.Mode() != deck.ModeType {
		return
	}

	text, err := xprop.PropValStr(xprop.GetProperty(a.xu, a.win.Id, selPropName))
	// Clear the scratch property regardless, per ICCCM.
	xproto.DeleteProperty(a.xu.Conn(), a.win.Id, a.selPropAtom)
	if err != nil || text == "" {
		return
	}

	var b strings.Builder
	b.WriteString(a.inputBuf)
	for _, r := range text {
		// Keep printable runes only; this drops newlines, tabs, and other
		// control characters so a multi-line paste collapses into the field.
		if unicode.IsPrint(r) {
			b.WriteRune(r)
		}
	}
	a.inputBuf = b.String()
	a.render()
}

func (a *App) quit() {
	a.viewer.CloseAll()
	a.saveProgress()
	xevent.Quit(a.xu)
}

func (a *App) saveProgress() {
	// Sessions are endless by design and the only way to stop is to quit, so
	// progress is flushed after every answer (not just at exit) — an ungraceful
	// kill then loses at most nothing. Report a failed write instead of
	// silently dropping the session's results.
	if err := a.store.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "study: failed to save progress: %v\n", err)
	}
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
		if err := a.viewer.ShowMedia(audioMedia, a.audioSpeed); err != nil && !a.audioWarned {
			fmt.Fprintf(os.Stderr, "study: %v\n", err)
			a.audioWarned = true
		}
	}
}

// audioHelp returns the footer hint for the audio controls — replay and the
// current speed — or "" when the current question has no audio (so decks
// without audio show no irrelevant hint).
func (a *App) audioHelp() string {
	card := a.engine.Current()
	if card == nil {
		return ""
	}
	for _, m := range card.Question {
		if m.Type == deck.Audio {
			return fmt.Sprintf("  |  ^R replay · ^,/. speed %.2fx", a.audioSpeed)
		}
	}
	return ""
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
		prog += "  retry"
	}
	a.drawText(canvas, prog, padding, y, a.fontSmall, dimColor)

	// Countdown timer (top-right), if the card has a time limit.
	if secs := a.secondsLeft(); secs >= 0 {
		timerColor := yellowColor
		if secs <= 3 {
			timerColor = redColor
		}
		a.drawTextRight(canvas, fmt.Sprintf("%ds", secs), a.width-padding, y, a.fontSmall, timerColor)
	}
	y += lineHeight(a.fontSmall)

	// Question image.
	if a.questionImg != nil {
		imgH := a.renderImage(canvas, a.questionImg, y)
		y += imgH + a.scaled(20)
	}

	// Question text.
	for _, m := range card.Question {
		if m.Type == deck.Text {
			a.drawCardText(canvas, m.Content, y, textColor)
			y += lineHeight(a.fontLarge)
		}
	}

	y += a.scaled(10)

	if a.engine.Mode() == deck.ModeType {
		// Text input field.
		prompt := "> " + a.inputBuf + "_"
		a.drawText(canvas, prompt, padding+20, y, a.fontRegular, accentColor)
		y += lineHeight(a.fontRegular)

		// Help.
		a.drawTextCentered(canvas, "type answer + enter"+a.audioHelp()+"  |  esc: quit", a.footerY(y), a.fontSmall, dimColor)
	} else {
		// Choices.
		opts := a.engine.Options()
		for i, opt := range opts {
			line := fmt.Sprintf("%d)  %s", i+1, opt)
			a.drawText(canvas, line, padding+20, y, a.fontRegular, textColor)
			y += lineHeight(a.fontRegular)
		}

		// Help.
		help := fmt.Sprintf("1-%d: answer%s  |  esc: quit", len(opts), a.audioHelp())
		a.drawTextCentered(canvas, help, a.footerY(y), a.fontSmall, dimColor)
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
	y += lineHeight(a.fontSmall)

	// Question image.
	if a.questionImg != nil {
		imgH := a.renderImage(canvas, a.questionImg, y)
		y += imgH + a.scaled(20)
	}

	// Question text.
	for _, m := range card.Question {
		if m.Type == deck.Text {
			a.drawCardText(canvas, m.Content, y, textColor)
			y += lineHeight(a.fontLarge)
		}
	}

	y += a.scaled(10)

	if a.engine.Mode() == deck.ModeType {
		// Show what was typed.
		if a.result.Correct {
			a.drawText(canvas, "> "+a.result.Typed+"  ✓", padding+20, y, a.fontRegular, greenColor)
		} else {
			a.drawText(canvas, "> "+a.result.Typed+"  X", padding+20, y, a.fontRegular, redColor)
			y += lineHeight(a.fontRegular)
			a.drawText(canvas, "= "+a.result.Answer, padding+20, y, a.fontRegular, greenColor)
		}
		y += lineHeight(a.fontRegular)
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
			y += lineHeight(a.fontRegular)
		}
	}

	y += a.scaled(16)

	// Result message.
	if a.result.Correct {
		a.drawTextCentered(canvas, "✓  Correct!", y, a.fontBold, greenColor)
	} else if a.result.TimedOut {
		msg := "Time's up! — answer: " + a.result.Answer
		a.drawTextCentered(canvas, msg, y, a.fontBold, redColor)
	} else {
		msg := "X  Wrong — answer: " + a.result.Answer
		a.drawTextCentered(canvas, msg, y, a.fontBold, redColor)
	}
	y += lineHeight(a.fontBold)

	// Help.
	a.drawTextCentered(canvas, "enter: continue  •  esc: quit", a.footerY(y), a.fontSmall, dimColor)
}

func (a *App) renderSummary(canvas *image.RGBA) {
	elapsed := time.Since(a.start).Round(time.Second)
	y := a.height / 4

	a.drawTextCentered(canvas, "Session Complete", y, a.fontLarge, accentColor)
	y += lineHeight(a.fontLarge) + a.scaled(20)

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
		y += lineHeight(a.fontRegular)
	}

	// All-time stats.
	totalCorrect, totalWrong, cardsStudied := a.store.Summary()
	if cardsStudied > 0 {
		y += a.scaled(20)
		a.drawTextCentered(canvas, "── All-time ──", y, a.fontSmall, dimColor)
		y += lineHeight(a.fontSmall)
		totalAcc := float64(0)
		if totalCorrect+totalWrong > 0 {
			totalAcc = float64(totalCorrect) / float64(totalCorrect+totalWrong) * 100
		}
		a.drawTextCentered(canvas, fmt.Sprintf("Cards seen: %d  •  Accuracy: %.0f%%", cardsStudied, totalAcc), y, a.fontRegular, dimColor)
		y += lineHeight(a.fontRegular)
	}

	a.drawTextCentered(canvas, "esc: exit", a.footerY(y), a.fontSmall, dimColor)
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

// drawTextRight draws text so that its right edge sits at xRight.
func (a *App) drawTextRight(canvas *image.RGBA, text string, xRight, y int, face font.Face, c color.RGBA) {
	d := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(c),
		Face: face,
	}
	width := d.MeasureString(text)
	x := xRight - width.Round()
	if x < padding {
		x = padding
	}
	d.Dot = fixed.P(x, y)
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
	// Same, but installed under /usr/local (e.g. manual installs).
	"/usr/local/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/local/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
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

	// Fall back to asking fontconfig where a CJK font lives, so a font in a
	// non-standard prefix (e.g. /usr/local) is still found. Try specific
	// families: the generic "Noto Sans CJK" often resolves to a Latin-only
	// fallback, whereas the per-language names match the real CJK font.
	if sysFontData == nil {
		for _, fam := range []string{"Noto Sans CJK JP", "Noto Sans CJK SC", "WenQuanYi Zen Hei"} {
			if path := fcMatchFile(fam); path != "" {
				if data, err := os.ReadFile(path); err == nil {
					sysFontData = data
					break
				}
			}
		}
	}

	// Choose font data: system CJK font or embedded Go font as fallback.
	regularData := goregular.TTF
	boldData := gobold.TTF
	if sysFontData != nil {
		regularData = sysFontData
		boldData = sysFontData // CJK fonts typically have one weight
	}

	// Parse the font data once here; buildFonts (called on every resize) then
	// only builds faces from these cached fonts.
	regularFont, err := parseFont(regularData)
	if err != nil {
		return err
	}
	boldFont, err := parseFont(boldData)
	if err != nil {
		return err
	}
	a.fontRegularFont = regularFont
	a.fontBoldFont = boldFont
	a.dpi = detectDPI(a.xu)

	return a.buildFonts()
}

// buildFonts rebuilds the four faces from the cached fonts at the current
// window size and DPI. Faces are assigned together at the end so a build
// failure leaves the previous faces intact. The previous faces are closed
// before the swap so resizing/zooming doesn't leak a face set per event.
func (a *App) buildFonts() error {
	scale := a.windowScale()

	regular, err := newFace(a.fontRegularFont, a.baseFontPt*fontMulRegular*scale, a.dpi)
	if err != nil {
		return err
	}
	bold, err := newFace(a.fontBoldFont, a.baseFontPt*fontMulRegular*scale, a.dpi)
	if err != nil {
		regular.Close()
		return err
	}
	small, err := newFace(a.fontRegularFont, a.baseFontPt*fontMulSmall*scale, a.dpi)
	if err != nil {
		regular.Close()
		bold.Close()
		return err
	}
	large, err := newFace(a.fontBoldFont, a.baseFontPt*fontMulLarge*scale, a.dpi)
	if err != nil {
		regular.Close()
		bold.Close()
		small.Close()
		return err
	}

	a.closeFonts()
	a.fontRegular, a.fontBold, a.fontSmall, a.fontLarge = regular, bold, small, large
	return nil
}

// closeFonts releases the current face set. Safe to call before the first build
// (the faces are nil) and after each rebuild.
func (a *App) closeFonts() {
	for _, f := range []font.Face{a.fontRegular, a.fontBold, a.fontSmall, a.fontLarge} {
		if f != nil {
			f.Close()
		}
	}
}

// lineHeight returns the baseline-to-baseline advance for face, derived from
// the font's own metrics so line spacing tracks the (DPI- and window-scaled)
// font size. Metrics().Height is the font's recommended advance; the extra
// quarter adds comfortable leading. Using metrics instead of fixed pixels is
// what keeps successive lines from overlapping as the fonts grow.
func lineHeight(face font.Face) int {
	h := face.Metrics().Height
	return (h + h/4).Ceil()
}

// textScale reports how much larger the current regular font renders than the
// app's original 18px design. Fixed spacer gaps are multiplied by it (via
// scaled) so the vertical layout keeps its proportions as fonts scale.
func (a *App) textScale() float64 {
	return a.baseFontPt * fontMulRegular * a.windowScale() * a.dpi / 72.0 / 18.0
}

// clampFontPt keeps a base point size within the legible range.
func clampFontPt(pt float64) float64 {
	if pt < minFontPt {
		return minFontPt
	}
	if pt > maxFontPt {
		return maxFontPt
	}
	return pt
}

// setFontSize sets the base font size (clamped), rebuilds the faces, and
// redraws. A no-op if the size is unchanged or the rebuild fails.
func (a *App) setFontSize(pt float64) {
	pt = clampFontPt(pt)
	if pt == a.baseFontPt {
		return
	}
	a.baseFontPt = pt
	if err := a.buildFonts(); err != nil {
		return
	}
	a.render()
}

// changeFontSize nudges the base font size by delta points (one increment is
// fontStepPt). Ctrl+= / Ctrl+- call this; Ctrl+0 calls setFontSize directly.
func (a *App) changeFontSize(delta float64) {
	a.setFontSize(a.baseFontPt + delta)
}

// clampSpeed keeps an audio speed multiplier within the playable range.
func clampSpeed(x float64) float64 {
	if x < minSpeed {
		return minSpeed
	}
	if x > maxSpeed {
		return maxSpeed
	}
	return x
}

// setSpeed sets the audio playback speed (clamped) and replays the current
// question's audio at the new speed so the change is heard immediately. The
// replay happens even when the value is unchanged (e.g. already at a clamp
// bound), so pressing the key always gives audible feedback. Ctrl+/ resets to
// defaultSpeed via this; the +/- keys go through changeSpeed.
func (a *App) setSpeed(x float64) {
	a.audioSpeed = clampSpeed(x)
	if a.engine.State() == quiz.ShowQuestion {
		a.playQuestionAudio()
	}
	a.render()
}

// changeSpeed nudges the audio speed by delta (one increment is speedStep).
func (a *App) changeSpeed(delta float64) {
	a.setSpeed(a.audioSpeed + delta)
}

// footerY returns the baseline for a bottom-anchored footer line. It pins the
// footer near the window bottom, but pushes it down to clear contentY when the
// content has grown tall enough to reach it — so the footer never overlaps the
// body, at any font size. (At extreme zoom the footer simply scrolls off the
// bottom edge, which is the expected consequence of zooming in.)
func (a *App) footerY(contentY int) int {
	pinned := a.height - padding
	if contentY > pinned {
		return contentY
	}
	return pinned
}

// scaled converts a spacer gap from the original design (in pixels) to the
// current text scale.
func (a *App) scaled(px int) int {
	return int(float64(px) * a.textScale())
}

// windowScale returns the font scale factor for the current window size,
// relative to the default. Text grows with the window, clamped so it stays
// legible when small and doesn't overflow when large.
func (a *App) windowScale() float64 {
	s := float64(a.height) / float64(defaultHeight)
	if s < 0.8 {
		s = 0.8
	}
	if s > 2.0 {
		s = 2.0
	}
	return s
}

// detectDPI returns the display DPI to render fonts at.
// Priority: Xft.dpi (the desktop's configured value) → the X screen's
// 96 (the conventional default).
//
// Deliberately NOT derived from the panel's physical size: that yields the
// monitor's true DPI (e.g. ~158 on a 1080p laptop panel), but the rest of the
// desktop only renders at that scale when the user has configured it — in which
// case Xft.dpi is set and we use it above. With Xft.dpi unset the desktop is
// effectively 96 DPI, so rendering fonts at the physical DPI makes this app's
// text ~1.6× larger than every other window. Matching 96 keeps us consistent.
func detectDPI(xu *xgbutil.XUtil) float64 {
	const minDPI, maxDPI = 50.0, 400.0

	// Xft.dpi from the X resource database — the source of truth when set
	// (it reflects the user's actual scaling choice, HiDPI included).
	if out, err := exec.Command("xrdb", "-query").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.HasPrefix(line, "Xft.dpi:") {
				continue
			}
			v := strings.TrimSpace(strings.TrimPrefix(line, "Xft.dpi:"))
			if dpi, err := strconv.ParseFloat(v, 64); err == nil && dpi >= minDPI && dpi <= maxDPI {
				return dpi
			}
		}
	}

	// Conventional default — matches an unscaled desktop.
	return 96
}

// fcMatchFile asks fontconfig for the file path of the best match for the
// given family pattern, e.g. "Noto Sans CJK". Returns "" if fontconfig is
// unavailable or finds nothing usable.
func fcMatchFile(pattern string) string {
	out, err := exec.Command("fc-match", "--format=%{file}", pattern).Output()
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(out))
	// fc-match always returns *some* font; only accept a CJK match so we don't
	// pick a Latin-only fallback that lacks the symbol glyphs.
	if path == "" || !strings.Contains(strings.ToLower(path), "cjk") {
		return ""
	}
	return path
}

// parseFont parses raw font data into a reusable *opentype.Font, trying a TTC
// (font collection) first, then a single font. Parsing is the expensive step
// and is size-independent, so callers do it once and build many faces from the
// result with newFace.
func parseFont(ttfData []byte) (*opentype.Font, error) {
	if collection, err := opentype.ParseCollection(ttfData); err == nil {
		return collection.Font(0)
	}
	return opentype.Parse(ttfData)
}

// newFace builds a face at the given size and DPI from an already-parsed font.
// This is the cheap, per-size step run on every resize.
func newFace(f *opentype.Font, size, dpi float64) (font.Face, error) {
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     dpi,
		Hinting: font.HintingFull,
	})
}
