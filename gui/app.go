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
	"golang.org/x/image/font/sfnt"
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

// progressStore is the subset of *progress.Store the GUI depends on. Declaring
// it as an interface (rather than the concrete type) lets tests substitute a
// fake — e.g. one whose Save fails — to exercise the save-failure handling.
type progressStore interface {
	Save() error
	SummaryFor(ids []string) (totalCorrect, totalWrong, cardsStudied int)
}

// App holds the GUI state.
type App struct {
	xu     *xgbutil.XUtil
	win    *xwindow.Window
	engine *quiz.Engine
	viewer *media.Viewer
	store  progressStore
	result *quiz.Result
	start  time.Time
	width  int
	height int

	// deadline is when the current question's timer expires.
	// Zero means the current card has no time limit.
	deadline time.Time

	// resultLock is when the result screen of a wrong answer may be advanced.
	// Until then enter/space are ignored, so a reflexive keypress can't skip
	// past an unnoticed miss. Zero means no lock (correct answers don't set
	// one).
	resultLock time.Time

	// Fonts. The faces are rebuilt whenever the window is resized so text scales
	// with the window; dpi is the detected display resolution used to render
	// points at a physically sane size. The parsed *opentype.Font values are
	// cached so a resize only re-runs the cheap NewFace step, not a full re-parse
	// of the (potentially tens-of-MB) font file.
	fontRegular     font.Face
	fontBold        font.Face
	fontSmall       font.Face
	fontLarge       font.Face
	// Faces from fontSymbolsFont: regular size for the ✔/✘ verdict marks,
	// small size for the 🔊 audio badge. Nil without a symbols font.
	fontSymbols      font.Face
	fontSymbolsSmall font.Face
	fontRegularFont *opentype.Font
	fontBoldFont    *opentype.Font
	// fontSymbolsFont carries the ✔/✘ verdict marks and the 🔊 audio badge: no
	// CJK font covers them, so a symbols-capable font (Noto Sans Symbols2 or
	// DejaVu Sans) is loaded separately. Nil when the system has none — the
	// marks then degrade to ✓/╳ and the badge to ♪, which the CJK face covers.
	fontSymbolsFont *opentype.Font
	// hasSpeaker reports 🔊 coverage in fontSymbolsFont (DejaVu carries the
	// marks but not the speaker), decided once at load time.
	hasSpeaker bool
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

	// Cached question image (loaded from disk). questionImgFailed is set when the
	// current card names an image that exists but couldn't be decoded (e.g. an
	// unsupported format or a corrupt file), so the question can show a
	// placeholder instead of silently dropping the image.
	questionImg       image.Image
	questionImgFailed bool

	// revealImg is the reversed card's answer-side image, loaded when the
	// reveal is shown (reverse mode only) and cleared on the next card.
	revealImg image.Image

	// confusedImg is the confused-with card's question-side image, loaded with
	// the result so the contrast note shows that card as it appears when
	// prompted. Cleared on the next card.
	confusedImg image.Image

	// One-shot warning guards so a degraded-feature notice is printed to stderr
	// once rather than on every use: pasteWarned when clipboard atoms are
	// unavailable, arabicWarned when Arabic text is shown without an Arabic font.
	pasteWarned  bool
	arabicWarned bool

	// saveFailed is set when a progress save did not succeed (after a retry); the
	// GUI then shows a persistent warning so the user knows results aren't being
	// written. Cleared on the next successful save.
	saveFailed bool

	// Text input buffer (type mode).
	inputBuf string

	// reverse is set when the session runs a flipped deck (--reverse): the prompt
	// is the English and the user produces the target language, so the native
	// script and audio are held back until the result screen, which renders the
	// reveal (card.Answer) and speaks it.
	reverse bool

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
	defaultFontPt = 10.0 // base size when the deck doesn't set one
	minFontPt     = 8.0
	maxFontPt     = 40.0
	fontStepPt    = 2.0

	fontMulSmall   = 0.75 // hints, progress, timer
	fontMulRegular = 1.0  // prompts, answers, body
	fontMulLarge   = 1.5  // card content, headers

	// Image height caps, as fractions of the window height.
	imageHFrac     = 0.4 // question and reveal images
	noteImageHFrac = 0.2 // the confusion note's prompt reproduction
)

// Audio playback speed. The multiplier starts at defaultSpeed (or the deck's
// "# audio-speed:" directive) and the user nudges it at runtime with Ctrl+, / Ctrl+.
// in speedStep increments, clamped to [minSpeed,maxSpeed]; Ctrl+/ resets it.
const (
	defaultSpeed = 1.0
	minSpeed     = 0.25
	maxSpeed     = 4.0
	speedStep    = 0.25
)

// Run launches the X11 quiz window. reverse is true when the deck was flipped
// for production practice (--reverse), which changes how the result screen
// reveals the answer (native script + audio) rather than the quiz mechanics.
func Run(engine *quiz.Engine, viewer *media.Viewer, store *progress.Store, reverse bool) error {
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
		xu:      xu,
		engine:  engine,
		viewer:  viewer,
		store:   store,
		start:   time.Now(),
		width:   defaultWidth,
		height:  defaultHeight,
		reverse: reverse,
	}

	// Base font size: the deck's "# font-size:" directive if set, else default.
	app.baseFontPt = defaultFontPt
	if pt := engine.FontSize(); pt > 0 {
		app.baseFontPt = clampFontPt(float64(pt))
	}
	app.initialFontPt = app.baseFontPt // Ctrl+0 returns here

	// Audio speed: the deck's "# audio-speed:" directive if set, else default.
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

	// Load the first card's media, play its audio, and arm its countdown (or,
	// on a first-viewing preview, reveal the answer side instead).
	app.presentCard()

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

// presentCard loads and plays the media for a freshly served card. On a
// first-viewing preview the answer side is revealed too (its image and audio)
// and the countdown stays disarmed — the timer belongs to recall, which only
// begins once the reveal is confirmed.
func (a *App) presentCard() {
	a.loadQuestionImage()
	a.playQuestionAudio()
	if a.engine.State() == quiz.ShowPreview {
		a.showReveal()
		a.deadline = time.Time{}
		return
	}
	a.startTimer()
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
	// Wrong-answer pause: keep the result screen's countdown moving, and
	// redraw once more when it expires so it disappears.
	if a.engine.State() == quiz.ShowResult && !a.resultLock.IsZero() {
		if !time.Now().Before(a.resultLock) {
			a.resultLock = time.Time{}
		}
		a.render()
		return
	}

	if a.deadline.IsZero() || a.engine.State() != quiz.ShowQuestion {
		return
	}
	if time.Now().Before(a.deadline) {
		a.render() // update the on-screen countdown
		return
	}
	// Time's up — count the card as wrong, just like an incorrect answer.
	a.deadline = time.Time{}
	a.setResult(a.engine.AnswerTimeout())
	// Reveal (and, in reverse mode, speak) the answer just as a typed miss does.
	if a.reverse {
		a.showReveal()
	}
	// The engine records the outcome to the store; persist it now so an
	// ungraceful exit can't lose this answer.
	a.saveProgress()
	a.render()
}

// secondsLeft returns the whole seconds remaining on the current question's
// timer, or -1 if there is no active time limit.
func (a *App) secondsLeft() int {
	return secondsUntil(a.deadline)
}

// lockSecondsLeft returns the whole seconds remaining on the wrong-answer
// pause, or -1 if none is active.
func (a *App) lockSecondsLeft() int {
	return secondsUntil(a.resultLock)
}

// secondsUntil converts a deadline to whole seconds remaining (-1 for the
// zero time, meaning "not armed"). Rounded up so the display counts N..1
// rather than N-1..0.
func secondsUntil(t time.Time) int {
	if t.IsZero() {
		return -1
	}
	d := time.Until(t)
	if d < 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

// setResult installs a just-submitted result: it arms the wrong-answer pause
// and loads the confused-with card's question image for the contrast note.
func (a *App) setResult(r *quiz.Result) {
	a.result = r
	a.loadConfusedImage()
	a.lockResult()
}

// lockResult arms the wrong-answer pause when the just-submitted result is a
// miss (including a timeout). Called right after a.result is set. The length
// comes from the deck's "# wrong-pause:" setting (or the --wrong-pause flag);
// 0 disables the pause.
func (a *App) lockResult() {
	secs := a.engine.WrongPause()
	if secs <= 0 || a.result == nil || a.result.Correct {
		return
	}
	a.resultLock = time.Now().Add(time.Duration(secs) * time.Second)
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
		// Audio controls stay live on the result screen: Ctrl+R replays the
		// clip that matters here (in reverse mode, the reveal — the one moment
		// replay is most wanted), and the speed keys adjust + replay it.
		if ev.State&xproto.ModMaskControl > 0 {
			switch {
			case key == "r" || key == "R":
				a.replayAudio()
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
		switch key {
		case "Return", "space":
			// Wrong-answer pause: ignore continue until the countdown ends,
			// so the miss registers before the card disappears.
			if time.Now().Before(a.resultLock) {
				return
			}
			a.resultLock = time.Time{}
			a.viewer.CloseAll()
			a.engine.Next()
			a.result = nil
			a.revealImg = nil
			a.confusedImg = nil
			a.inputBuf = ""

			if s := a.engine.State(); s == quiz.ShowQuestion || s == quiz.ShowPreview {
				a.presentCard()
			}
			a.render()
		case "Escape":
			a.endSession()
		}

	case quiz.ShowPreview:
		// Audio controls stay live so the revealed clip can be re-heard while
		// studying the new card.
		if ev.State&xproto.ModMaskControl > 0 {
			switch {
			case key == "r" || key == "R":
				a.replayAudio()
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
		switch key {
		case "Return", "space":
			a.engine.ConfirmPreview()
			if a.engine.State() == quiz.ShowPreview {
				// Flip-through: a new card was served, still answer-visible.
				a.viewer.CloseAll()
				a.presentCard()
			} else {
				// First-viewing reveal: study time is over — quiz the very same
				// card. The countdown (if any) starts now, when recall begins.
				a.startTimer()
			}
			a.render()
		case "Escape":
			a.endSession()
		}

	case quiz.Done:
		switch key {
		case "Escape", "Return", "q":
			a.quit()
		}
	}
}

// endSession stops the quiz early at the user's request and shows the summary
// screen (a second Escape there exits). Progress was already persisted after
// every answer, so this only has to close media and switch state.
func (a *App) endSession() {
	a.viewer.CloseAll()
	a.deadline = time.Time{}
	a.resultLock = time.Time{}
	a.result = nil
	a.revealImg = nil
	a.confusedImg = nil
	a.engine.End()
	a.render()
}

func (a *App) handleChoiceKey(key string) {
	switch key {
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key[0]-'0') - 1
		opts := a.engine.Options()
		if idx >= len(opts) {
			return
		}
		a.setResult(a.engine.Answer(idx))
		// The engine records the outcome; persist immediately so an ungraceful
		// exit can't lose this answer.
		a.saveProgress()
		a.render()
	case "Escape":
		a.endSession()
	}
}

func (a *App) handleTypeKey(key string, ev xevent.KeyPressEvent) {
	switch key {
	case "Return":
		a.setResult(a.engine.AnswerTyped(a.inputBuf))
		// In reverse mode the prompt was silent; reveal the target language now
		// that the answer is in — speak its clip and load its image.
		if a.reverse {
			a.showReveal()
		}
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
		a.endSession()
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
		if !a.pasteWarned {
			fmt.Fprintln(os.Stderr, "study: clipboard paste unavailable (could not intern X selection atoms)")
			a.pasteWarned = true
		}
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
	// Progress is flushed after every answer (not just at session end) — an
	// ungraceful kill then loses at most nothing. Most save failures are transient (a brief
	// filesystem hiccup), so retry once before giving up. A persistent failure
	// is both logged and surfaced in-window (saveFailed) so the user knows
	// results aren't being written, rather than silently dropping them.
	err := a.store.Save()
	if err != nil {
		err = a.store.Save()
	}
	if err != nil {
		a.saveFailed = true
		fmt.Fprintf(os.Stderr, "study: failed to save progress: %v\n", err)
		return
	}
	a.saveFailed = false
}

// ── Media Loading ───────────────────────────────────────────────────

func (a *App) loadQuestionImage() {
	a.questionImg = nil
	a.questionImgFailed = false
	card := a.engine.Current()
	if card == nil {
		return
	}
	for _, m := range card.Question {
		if m.Type == deck.Image {
			if img, err := loadImage(m.Content); err == nil {
				a.questionImg = img
			} else {
				// The file existed at parse time (it's stat-checked then) but
				// won't decode now — unsupported format or corrupt/removed. Flag
				// it so the question shows a placeholder instead of nothing.
				a.questionImgFailed = true
				fmt.Fprintf(os.Stderr, "study: could not load image %s: %v\n", m.Content, err)
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

// hasAudio reports whether a card side carries an audio clip.
func hasAudio(side []deck.Media) bool {
	for _, m := range side {
		if m.Type == deck.Audio {
			return true
		}
	}
	return false
}

// audioLines returns the controls-box key bindings for the audio controls, or
// nil when the given card side has no audio (so decks without audio show no
// irrelevant hint). The result screen passes the reveal (answer) side in
// reverse mode, where the audio lives. The current speed is not repeated
// here — the audio badge on the progress line carries it.
func (a *App) audioLines(side []deck.Media) []string {
	if !hasAudio(side) {
		return nil
	}
	return []string{
		"ctrl+r: replay audio",
		"ctrl+,: slower",
		"ctrl+.: faster",
	}
}

// speakerFace returns the audio badge's speaker glyph and its face: 🔊 from
// the symbols font when covered, else the CJK-covered ♪.
func (a *App) speakerFace() (string, font.Face) {
	if a.hasSpeaker && a.fontSymbolsSmall != nil {
		return speakerGlyph, a.fontSymbolsSmall
	}
	return speakerFallback, a.fontSmall
}

// statusItem is one piece of the top status row's left cluster: a text run in
// its own color, and optionally its own face (nil means the small face) —
// which is how the tally's ✔/✘ come from the symbols font while everything
// else stays in the UI font.
type statusItem struct {
	text  string
	color color.RGBA
	face  font.Face
}

// leftStatusItems builds the left-aligned session-context cluster: the
// counter, the audio badge, and tags for non-default session shapes (a
// non-adaptive order, reverse direction). Read once per session; the live
// data lives in the centered tally instead.
func (a *App) leftStatusItems(prog string, progColor color.RGBA, audioSide []deck.Media) [][]statusItem {
	groups := [][]statusItem{{{text: prog, color: progColor}}}
	if hasAudio(audioSide) {
		glyph, face := a.speakerFace()
		groups = append(groups, []statusItem{
			{text: glyph, color: dimColor, face: face},
			{text: fmt.Sprintf(": %.2fx", a.audioSpeed), color: dimColor},
		})
	}
	if o := a.engine.Order(); o != deck.OrderAdaptive {
		groups = append(groups, []statusItem{{text: o.String(), color: dimColor}})
	}
	if a.reverse {
		groups = append(groups, []statusItem{{text: "reverse", color: dimColor}})
	}
	return groups
}

// tallyItems builds the running ✔/✘ tally, centered above the card by
// drawTopStatus. Nil until there is something to tally.
func (a *App) tallyItems() [][]statusItem {
	if a.engine.TotalCorrect+a.engine.TotalWrong == 0 {
		return nil
	}
	check, cross, face := markCorrect, markWrong, a.fontSymbolsSmall
	if face == nil {
		check, cross = markCorrectFallback, markWrongFallback
	}
	return [][]statusItem{
		{
			{text: check, color: dimColor, face: face},
			{text: fmt.Sprintf(" %d", a.engine.TotalCorrect), color: dimColor},
		},
		{
			{text: cross, color: dimColor, face: face},
			{text: fmt.Sprintf(" %d", a.engine.TotalWrong), color: dimColor},
		},
	}
}

// tagItems wraps a per-card tag ("retry", "new") for drawTopStatus; nil for
// no tag.
func tagItems(tag string, c color.RGBA) []statusItem {
	if tag == "" {
		return nil
	}
	return []statusItem{{text: tag, color: c}}
}

// statusBarHeight is the vertical space the top status bar reserves: card
// content starts below it. Unlike the bottom controls box (an overlay),
// the bar owns its strip — the centered tally sits exactly where centered
// card content would otherwise arrive, so sharing pixels is not an option.
func (a *App) statusBarHeight() int {
	return lineHeight(a.fontSmall)
}

// drawTopStatus draws the top status bar in three zones: the session-context
// cluster left-aligned, the live tally centered on the window with the
// per-card tag hanging off its right — the tally is the anchor, so the tag's
// coming and going never moves it (a tag with no tally yet takes the center
// itself) — and an optional right-aligned item (question timer or
// wrong-answer countdown). The bar owns its strip (statusBarHeight): card
// content starts below it, so nothing can ever underlap the centered zone.
func (a *App) drawTopStatus(canvas *image.RGBA, left, tally [][]statusItem, tag []statusItem, right string, rightColor color.RGBA) {
	gap := a.scaled(18) // between groups — a glyph-independent separator

	itemFace := func(it statusItem) font.Face {
		if it.face != nil {
			return it.face
		}
		return a.fontSmall
	}
	groupsWidth := func(groups [][]statusItem) int {
		w := 0
		for gi, g := range groups {
			if gi > 0 {
				w += gap
			}
			for _, it := range g {
				w += font.MeasureString(itemFace(it), it.text).Round()
			}
		}
		return w
	}
	drawGroups := func(groups [][]statusItem, x int) int {
		for gi, g := range groups {
			if gi > 0 {
				x += gap
			}
			for _, it := range g {
				a.drawText(canvas, it.text, x, padding, itemFace(it), it.color)
				x += font.MeasureString(itemFace(it), it.text).Round()
			}
		}
		return x
	}

	// Left zone.
	leftEnd := drawGroups(left, padding)

	// Center zone: the tally anchors the centering; the tag follows it.
	wTally := groupsWidth(tally)
	wTag := 0
	if len(tag) > 0 {
		wTag = groupsWidth([][]statusItem{tag})
	}
	if wTally+wTag > 0 {
		anchor := wTally
		if anchor == 0 {
			anchor = wTag
		}
		cx := (a.width - anchor) / 2
		if min := leftEnd + gap; cx < min {
			cx = min // never collide with the left cluster
		}
		x := cx
		if wTally > 0 {
			x = drawGroups(tally, x)
			if wTag > 0 {
				x += gap
			}
		}
		if wTag > 0 {
			drawGroups([][]statusItem{tag}, x)
		}
	}

	// Right zone.
	if right == "" {
		return
	}
	wRight := font.MeasureString(a.fontSmall, right).Round()
	a.drawText(canvas, right, a.width-padding-wRight, padding, a.fontSmall, rightColor)
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
	case quiz.ShowPreview:
		a.renderPreview(canvas)
	case quiz.Done:
		a.renderSummary(canvas)
	}

	// Persistent warning when progress isn't being saved. Pinned to the very
	// bottom edge, below the normal footer, so it's visible on every screen.
	if a.saveFailed {
		a.drawTextCentered(canvas, "progress not saved — see terminal", a.height-8, a.fontSmall, redColor)
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

	// Content starts below the status bar's reserved strip.
	y := padding + a.statusBarHeight()

	seen := a.engine.TotalSeen
	remaining := a.engine.Remaining()
	prog := fmt.Sprintf("[%d/%d]", seen+1, seen+remaining)
	cardTag, tagColor := "", dimColor
	if a.engine.IsRetry() {
		cardTag = "retry"
	} else if a.engine.CurrentIsNew() {
		cardTag, tagColor = "new", accentColor
	}

	// Question image.
	y = a.renderQuestionImageBlock(canvas, y)

	// Question text.
	y = a.renderQuestionText(canvas, card, y)

	y += a.scaled(10)

	var action string
	if a.engine.Mode() == deck.ModeType {
		// Text input field.
		prompt := "> " + a.inputBuf + "_"
		a.drawText(canvas, prompt, padding+20, y, a.fontRegular, accentColor)
		action = "enter: submit"
	} else {
		// Choices.
		opts := a.engine.Options()
		for i, opt := range opts {
			line := fmt.Sprintf("%d)  %s", i+1, opt)
			a.drawText(canvas, line, padding+20, y, a.fontRegular, textColor)
			y += lineHeight(a.fontRegular)
		}
		action = fmt.Sprintf("1-%d: answer", len(opts))
	}

	lines := append([]string{action}, a.audioLines(card.Question)...)
	a.drawControlsBox(canvas, append(lines, "esc: end"))

	// Status overlay: progress, tags, tally and audio badge left; countdown
	// timer right.
	right, rightColor := "", dimColor
	if secs := a.secondsLeft(); secs >= 0 {
		right = fmt.Sprintf("%ds", secs)
		rightColor = yellowColor
		if secs <= 3 {
			rightColor = redColor
		}
	}
	a.drawTopStatus(canvas, a.leftStatusItems(prog, dimColor, card.Question), a.tallyItems(), tagItems(cardTag, tagColor), right, rightColor)
}

func (a *App) renderResult(canvas *image.RGBA) {
	if a.result == nil {
		return
	}
	card := a.result.Card
	// Content starts below the status bar's reserved strip.
	y := padding + a.statusBarHeight()

	// The audio (and so the badge and the control hints) follows the side the
	// clip lives on: the reveal (answer) side in reverse mode, the question
	// side otherwise.
	audioSide := card.Question
	if a.reverse {
		audioSide = card.Answer
	}

	seen := a.engine.TotalSeen
	remaining := a.engine.Remaining()
	prog := fmt.Sprintf("[%d/%d]", seen, seen+remaining)

	// Question image.
	y = a.renderQuestionImageBlock(canvas, y)

	// Question text.
	y = a.renderQuestionText(canvas, card, y)

	y += a.scaled(10)

	if a.engine.Mode() == deck.ModeType {
		// Show what was typed; the ✔/✘ mark on this line is the verdict.
		markColor := greenColor
		if !a.result.Correct {
			markColor = redColor
		}
		typed := "> " + a.result.Typed
		a.drawText(canvas, typed, padding+20, y, a.fontRegular, markColor)
		a.drawMarkAfter(canvas, typed, padding+20, y, a.result.Correct, markColor)
		y += lineHeight(a.fontRegular)

		if a.reverse {
			// Reveal the target language the user was asked to produce — native
			// script + romanization, plus any image that rode along — always
			// (right or wrong). The clip was spoken by showReveal when the
			// result appeared. This replaces the plain "= answer" line, since
			// the reveal already includes it.
			y += a.scaled(6)
			if a.revealImg != nil {
				y += a.renderImage(canvas, a.revealImg, y, imageHFrac) + a.scaled(20)
			}
			y = a.renderTextMedia(canvas, a.result.Card.Answer, y, greenColor)
		} else if !a.result.Correct {
			a.drawText(canvas, "= "+a.result.Answer, padding+20, y, a.fontRegular, greenColor)
			y += lineHeight(a.fontRegular)
		}
	} else {
		// Choices with highlighting: the correct option gets the ✔, a wrongly
		// chosen one the ✘.
		opts := a.engine.Options()
		for i, opt := range opts {
			line := fmt.Sprintf("%d)  %s", i+1, opt)
			c := dimColor
			marked, correct := false, false
			if opt == a.result.Answer {
				marked, correct = true, true
				c = greenColor
			} else if i == a.result.Chosen && !a.result.Correct {
				marked = true
				c = redColor
			}
			a.drawText(canvas, line, padding+20, y, a.fontRegular, c)
			if marked {
				a.drawMarkAfter(canvas, line, padding+20, y, correct, c)
			}
			y += lineHeight(a.fontRegular)
		}
	}

	y += a.scaled(16)

	// The verdict lives on the answer lines themselves (the ✓/╳ marks and the
	// green "=" reveal), so no separate Correct/Wrong banner — it only repeated
	// them. A timeout is the exception: nothing was answered, so the reason is
	// worth a line.
	if a.result.TimedOut {
		a.drawTextCentered(canvas, "Time's up!", y, a.fontBold, redColor)
		y += lineHeight(a.fontBold)
	}

	// Confusion contrast: the wrong answer belongs to another card — reproduce
	// that card as it appears when prompted (its image and question lines at
	// card size, via the same script-shaped rendering), closed by the usual
	// "=" reveal carrying its answer: exactly what the user provided, which is
	// what says why this card is being shown. The yellow tint keeps the block
	// a note rather than a second live question.
	if cw := a.result.ConfusedWith; cw != nil && (a.confusedImg != nil || deck.JoinText(cw.Question) != "") {
		if a.confusedImg != nil {
			y += a.renderImage(canvas, a.confusedImg, y, noteImageHFrac) + a.scaled(20)
		}
		y = a.renderTextMedia(canvas, cw.Question, y, yellowColor)
		y += a.scaled(4) + a.fontRegular.Metrics().Ascent.Ceil()
		a.drawShapedCentered(canvas, "= "+cw.AnswerText, y, fontMulRegular, a.fontRegular, yellowColor)
	}

	// Controls.
	lines := append([]string{"enter: continue"}, a.audioLines(audioSide)...)
	a.drawControlsBox(canvas, append(lines, "esc: end"))

	// Status overlay: progress, tags, tally and audio badge left; wrong-answer
	// pause countdown right (the same corner as the question timer).
	right := ""
	if secs := a.lockSecondsLeft(); secs > 0 {
		right = fmt.Sprintf("%ds", secs)
	}
	a.drawTopStatus(canvas, a.leftStatusItems(prog, dimColor, audioSide), a.tallyItems(), nil, right, redColor)
}

// renderPreview draws an answer-visible card: the first-viewing reveal (study
// a brand-new card once before it's quizzed) and flip-through mode (every card,
// no quizzing) share this screen.
func (a *App) renderPreview(canvas *image.RGBA) {
	card := a.engine.Current()
	if card == nil {
		return
	}
	flip := a.engine.Order() == deck.OrderFlipThrough

	// Content starts below the status bar's reserved strip.
	y := padding + a.statusBarHeight()

	// Audio may live on either side of the card; badge and hints follow
	// whichever has it.
	audioSide := card.Question
	if !hasAudio(audioSide) {
		audioSide = card.Answer
	}

	// Progress indicator, drawn last as an overlay — content flows from the
	// window top. Flip-through shows the position within the wrapping lap (its
	// mode is named by the order tag); the first-viewing reveal shows the usual
	// session counter with the "new" tag.
	var prog, cardTag string
	if flip {
		size := a.engine.DeckSize()
		prog = fmt.Sprintf("[%d/%d]", a.engine.TotalSeen%size+1, size)
	} else {
		seen := a.engine.TotalSeen
		remaining := a.engine.Remaining()
		prog = fmt.Sprintf("[%d/%d]", seen+1, seen+remaining)
		cardTag = "new"
	}

	// Question side.
	y = a.renderQuestionImageBlock(canvas, y)
	y = a.renderQuestionText(canvas, card, y)

	y += a.scaled(10)

	// Answer side, revealed: its image (loaded by showReveal) and every text
	// line in green — the same presentation as a reverse-mode result reveal.
	if a.revealImg != nil {
		y += a.renderImage(canvas, a.revealImg, y, imageHFrac) + a.scaled(20)
	}
	y = a.renderTextMedia(canvas, card.Answer, y, greenColor)

	y += a.scaled(16)

	// Controls.
	action := "enter: quiz this card"
	if flip {
		action = "enter: next"
	}
	lines := append([]string{action}, a.audioLines(audioSide)...)
	a.drawControlsBox(canvas, append(lines, "esc: end"))

	a.drawTopStatus(canvas, a.leftStatusItems(prog, dimColor, audioSide), a.tallyItems(), tagItems(cardTag, accentColor), "", dimColor)
}

func (a *App) renderSummary(canvas *image.RGBA) {
	elapsed := time.Since(a.start).Round(time.Second)
	y := a.height / 4

	a.drawTextCentered(canvas, "Session Complete", y, a.fontLarge, accentColor)
	y += lineHeight(a.fontLarge) + a.scaled(20)

	type statRow struct {
		label string
		value string
		color color.RGBA
	}
	var stats []statRow

	// Flip-through is reading, not recall: no answers were given, so correct/
	// wrong/accuracy would all be meaningless zeros.
	flip := a.engine.Order() == deck.OrderFlipThrough
	if flip {
		stats = []statRow{
			{"Cards viewed", fmt.Sprintf("%d", a.engine.TotalSeen), textColor},
			{"Time", elapsed.String(), textColor},
		}
	} else {
		stats = []statRow{
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
		stats = append(stats,
			statRow{"Accuracy", fmt.Sprintf("%.0f%%", acc), accColor},
			statRow{"Time", elapsed.String(), textColor})
	}

	// Two-column table centered as a block: labels right-aligned, values
	// left-aligned. Padding tricks don't align in a proportional font.
	maxLabelW, maxValueW := 0, 0
	for _, s := range stats {
		if w := font.MeasureString(a.fontRegular, s.label).Round(); w > maxLabelW {
			maxLabelW = w
		}
		if w := font.MeasureString(a.fontRegular, s.value).Round(); w > maxValueW {
			maxValueW = w
		}
	}
	colGap := a.scaled(28)
	labelRight := (a.width-maxLabelW-colGap-maxValueW)/2 + maxLabelW
	valueLeft := labelRight + colGap
	for _, s := range stats {
		a.drawTextRight(canvas, s.label, labelRight, y, a.fontRegular, s.color)
		a.drawText(canvas, s.value, valueLeft, y, a.fontRegular, s.color)
		y += lineHeight(a.fontRegular)
	}

	// All-time stats, scoped to this deck's cards in this direction (orphaned
	// progress from removed cards and the other direction's history excluded).
	// Skipped for flip-through, which doesn't touch the record.
	totalCorrect, totalWrong, cardsStudied := 0, 0, 0
	if !flip {
		totalCorrect, totalWrong, cardsStudied = a.store.SummaryFor(a.engine.CardIDs())
	}
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

	a.drawControlsBox(canvas, []string{"enter / esc: exit"})
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

// drawMarkAfter draws the verdict mark following text already drawn at x,y in
// the regular face: the heavy ✔/✘ from the symbols face when one loaded, else
// the light ✓/╳ the CJK face covers.
func (a *App) drawMarkAfter(canvas *image.RGBA, text string, x, y int, correct bool, c color.RGBA) {
	mark, face := markCorrect, a.fontSymbols
	if !correct {
		mark = markWrong
	}
	if face == nil {
		mark, face = markCorrectFallback, a.fontRegular
		if !correct {
			mark = markWrongFallback
		}
	}
	x += font.MeasureString(a.fontRegular, text+"  ").Round()
	a.drawText(canvas, mark, x, y, face, c)
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

// renderQuestionImageBlock draws the current question's image starting at y and
// returns the y below it (including the trailing gap). If the image is present
// it's scaled and centered; if it was named but failed to decode, a dimmed
// "[image failed to load]" placeholder takes its place so the missing visual is
// acknowledged rather than silently absent. With no image on the card, y is
// returned unchanged.
func (a *App) renderQuestionImageBlock(canvas *image.RGBA, y int) int {
	switch {
	case a.questionImg != nil:
		imgH := a.renderImage(canvas, a.questionImg, y, imageHFrac)
		return y + imgH + a.scaled(20)
	case a.questionImgFailed:
		y += lineHeight(a.fontRegular)
		a.drawTextCentered(canvas, "[image failed to load]", y, a.fontRegular, dimColor)
		return y + a.scaled(20)
	default:
		return y
	}
}

// renderQuestionText draws the card's question text line(s) starting at top y
// (the value returned by renderQuestionImageBlock, which is the top of the next
// element). drawCardText positions text by its baseline, so the first line is
// shifted down by the font's ascent — otherwise its glyphs render upward into
// the image above and overlap it. Returns the y below the text.
func (a *App) renderQuestionText(canvas *image.RGBA, card *deck.Card, y int) int {
	return a.renderTextMedia(canvas, card.Question, y, textColor)
}

// renderTextMedia draws the text elements of a card side (skipping image/audio),
// one per line and script-shaped, in the given color. It's shared by the
// question prompt and — in reverse mode — the answer reveal.
func (a *App) renderTextMedia(canvas *image.RGBA, media []deck.Media, y int, c color.RGBA) int {
	first := true
	for _, m := range media {
		if m.Type != deck.Text {
			continue
		}
		if first {
			y += a.fontLarge.Metrics().Ascent.Ceil()
			first = false
		}
		a.drawCardText(canvas, m.Content, y, c)
		y += lineHeight(a.fontLarge)
	}
	return y
}

// showReveal presents the reversed card's held-back answer side once the user
// has answered: its audio is spoken and its image (if any) loaded for the
// result screen.
func (a *App) showReveal() {
	a.playRevealAudio()
	a.loadRevealImage()
}

// loadRevealImage caches the current card's answer-side image (reverse mode:
// the original prompt's image, riding along on the reveal). Nil when the card
// has none or it fails to decode.
func (a *App) loadRevealImage() {
	a.revealImg = nil
	card := a.engine.Current()
	if card == nil {
		return
	}
	for _, m := range card.Answer {
		if m.Type == deck.Image {
			img, err := loadImage(m.Content)
			if err != nil {
				fmt.Fprintf(os.Stderr, "study: could not load image %s: %v\n", m.Content, err)
				return
			}
			a.revealImg = img
			return
		}
	}
}

// loadConfusedImage caches the confused-with card's question-side image so the
// contrast note can show that card exactly as it appears when prompted. Nil
// when there is no confusion, the card has no image, or it fails to decode
// (logged, the note then shows text only).
func (a *App) loadConfusedImage() {
	a.confusedImg = nil
	if a.result == nil || a.result.ConfusedWith == nil {
		return
	}
	for _, m := range a.result.ConfusedWith.Question {
		if m.Type == deck.Image {
			img, err := loadImage(m.Content)
			if err != nil {
				fmt.Fprintf(os.Stderr, "study: could not load image %s: %v\n", m.Content, err)
				return
			}
			a.confusedImg = img
			return
		}
	}
}

// replayAudio replays whichever clip belongs to what's on screen: the
// question's audio while the question shows, and — in reverse mode — the
// reveal's audio on the result screen (there the question side is silent).
func (a *App) replayAudio() {
	if a.engine.State() == quiz.ShowResult && a.reverse {
		a.playRevealAudio()
		return
	}
	if a.engine.State() == quiz.ShowPreview {
		// Both sides are on screen; replay both sides' clips. Decks put audio
		// on one side, so in practice this replays whichever one exists.
		a.playQuestionAudio()
		a.playRevealAudio()
		return
	}
	a.playQuestionAudio()
}

// playRevealAudio speaks the current card's reveal audio — the clip that, in a
// reversed deck, lives on the answer (reveal) side and must not sound until the
// user has answered. A no-op for a card without answer-side audio.
func (a *App) playRevealAudio() {
	card := a.engine.Current()
	if card == nil {
		return
	}
	var audioMedia []deck.Media
	for _, m := range card.Answer {
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

// renderImage draws img centered at y, scaled to fit the window width and at
// most maxHFrac of the window height, and returns the drawn height. Full-role
// images (question, reveal) use imageHFrac; the confusion note uses the
// smaller noteImageHFrac so the note can't push the screen's own content off.
func (a *App) renderImage(canvas *image.RGBA, img image.Image, y int, maxHFrac float64) int {
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()

	maxW := a.width - padding*2
	maxH := int(float64(a.height) * maxHFrac)

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

// Verdict marks for the result screen and the audio-badge speaker. The heavy
// marks and the speaker need a symbols font; the fallbacks are what every
// supported CJK face covers.
const (
	markCorrect         = "✔" // U+2714 heavy check mark
	markWrong           = "✘" // U+2718 heavy ballot X
	markCorrectFallback = "✓" // U+2713
	markWrongFallback   = "╳" // U+2573
	speakerGlyph        = "🔊" // U+1F50A
	speakerFallback     = "♪" // U+266A
)

// symbolsFontPaths lists symbols-capable fonts in preference order. Noto Sans
// Symbols2 covers the speaker as well as the marks; DejaVu Sans (marks only,
// but near-universal on Linux) is the wider net under it.
var symbolsFontPaths = []string{
	"/usr/share/fonts/truetype/noto/NotoSansSymbols2-Regular.ttf",
	"/usr/share/fonts/noto/NotoSansSymbols2-Regular.ttf",
	"/usr/share/fonts/google-noto/NotoSansSymbols2-Regular.ttf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	"/usr/share/fonts/TTF/DejaVuSans.ttf",
	"/usr/share/fonts/dejavu-sans-fonts/DejaVuSans.ttf",
	"/usr/local/share/fonts/truetype/dejavu/DejaVuSans.ttf",
}

// hasGlyphs reports whether the font has a real glyph (not .notdef) for every
// rune in s.
func hasGlyphs(f *opentype.Font, s string) bool {
	var buf sfnt.Buffer
	for _, r := range s {
		gi, err := f.GlyphIndex(&buf, r)
		if err != nil || gi == 0 {
			return false
		}
	}
	return true
}

// loadSymbolsFont finds and parses a font covering the ✔/✘ verdict marks,
// trying the known paths and then a fontconfig charset query. Coverage is
// verified on the parsed font — never assumed from the file name; 🔊 coverage
// is noted but not required (DejaVu lacks it). Best-effort: fontSymbolsFont
// stays nil when nothing qualifies.
func (a *App) loadSymbolsFont() {
	candidates := append([]string{}, symbolsFontPaths...)
	if out, err := exec.Command("fc-match", "--format=%{file}", ":charset=2714 2718").Output(); err == nil {
		if p := strings.TrimSpace(string(out)); p != "" {
			candidates = append(candidates, p)
		}
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if f, err := parseFont(data); err == nil && hasGlyphs(f, markCorrect+markWrong) {
			a.fontSymbolsFont = f
			a.hasSpeaker = hasGlyphs(f, speakerGlyph)
			return
		}
	}
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
	a.loadSymbolsFont()
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
	var symbols, symbolsSmall font.Face
	if a.fontSymbolsFont != nil {
		symbols, err = newFace(a.fontSymbolsFont, a.baseFontPt*fontMulRegular*scale, a.dpi)
		if err != nil {
			regular.Close()
			bold.Close()
			small.Close()
			large.Close()
			return err
		}
		symbolsSmall, err = newFace(a.fontSymbolsFont, a.baseFontPt*fontMulSmall*scale, a.dpi)
		if err != nil {
			regular.Close()
			bold.Close()
			small.Close()
			large.Close()
			symbols.Close()
			return err
		}
	}

	a.closeFonts()
	a.fontRegular, a.fontBold, a.fontSmall, a.fontLarge = regular, bold, small, large
	a.fontSymbols, a.fontSymbolsSmall = symbols, symbolsSmall
	return nil
}

// closeFonts releases the current face set. Safe to call before the first build
// (the faces are nil) and after each rebuild.
func (a *App) closeFonts() {
	for _, f := range []font.Face{a.fontRegular, a.fontBold, a.fontSmall, a.fontLarge, a.fontSymbols, a.fontSymbolsSmall} {
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

// setSpeed sets the audio playback speed (clamped) and replays the on-screen
// clip at the new speed so the change is heard immediately. The replay happens
// even when the value is unchanged (e.g. already at a clamp bound), so pressing
// the key always gives audible feedback. Ctrl+/ resets to defaultSpeed via
// this; the +/- keys go through changeSpeed.
func (a *App) setSpeed(x float64) {
	a.audioSpeed = clampSpeed(x)
	if s := a.engine.State(); s == quiz.ShowQuestion || s == quiz.ShowResult || s == quiz.ShowPreview {
		a.replayAudio()
	}
	a.render()
}

// changeSpeed nudges the audio speed by delta (one increment is speedStep).
func (a *App) changeSpeed(delta float64) {
	a.setSpeed(a.audioSpeed + delta)
}

// drawControlsBox draws the control hints as a compact bordered box anchored
// to the bottom-right corner, one hint per line, left-aligned. The box is an
// overlay, not part of the content flow: the card keeps the window's full
// height, and the opaque background keeps the hints readable if tall content
// runs underneath.
func (a *App) drawControlsBox(canvas *image.RGBA, lines []string) {
	if len(lines) == 0 {
		return
	}
	pad := a.scaled(8)
	lh := lineHeight(a.fontSmall)
	maxW := 0
	for _, l := range lines {
		if w := font.MeasureString(a.fontSmall, l).Round(); w > maxW {
			maxW = w
		}
	}
	box := image.Rect(0, 0, maxW+pad*2, lh*len(lines)+pad*2).
		Add(image.Pt(a.width-padding-maxW-pad*2, a.height-padding-lh*len(lines)-pad*2))
	draw.Draw(canvas, box, image.NewUniform(bgColor), image.Point{}, draw.Src)
	strokeRect(canvas, box, dimColor)

	y := box.Min.Y + pad + a.fontSmall.Metrics().Ascent.Ceil()
	for _, l := range lines {
		a.drawText(canvas, l, box.Min.X+pad, y, a.fontSmall, dimColor)
		y += lh
	}
}

// strokeRect draws a 1px border just inside r.
func strokeRect(canvas *image.RGBA, r image.Rectangle, c color.RGBA) {
	for x := r.Min.X; x < r.Max.X; x++ {
		canvas.SetRGBA(x, r.Min.Y, c)
		canvas.SetRGBA(x, r.Max.Y-1, c)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		canvas.SetRGBA(r.Min.X, y, c)
		canvas.SetRGBA(r.Max.X-1, y, c)
	}
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
