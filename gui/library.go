package gui

// The library screen: the app's face when started bare (`study` with no deck
// argument). It lists every deck under the watched directories with its due
// counts, launches a session on a keystroke, and takes the session's place
// back when it ends — so finishing a deck lands on what else is due, not on a
// closed window.

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/font"

	"study/deck"
	"study/library"
	"study/progress"
	"study/session"
)

// libRow is one line of the library screen. A row with a non-empty heading is
// a watched-directory divider (not selectable, err set when the directory
// couldn't be read); otherwise it is a launchable deck with the numbers its
// line shows.
type libRow struct {
	heading string
	err     error
	entry   library.Entry
	info    library.Info
	child   bool // a member deck of an expanded pack, drawn indented
}

// refreshLibrary rescans the watched directories and re-reads every deck's
// progress. Called when the screen first opens and every time a session
// returns to it, so the due counts always reflect the session just finished.
// The selection stays on the same deck across a rescan when it survives one.
func (a *App) refreshLibrary() {
	prev := ""
	if r := a.selectedRow(); r != nil {
		prev = r.entry.Path
	}

	a.rows = a.rows[:0]
	now := time.Now()
	for _, g := range a.reg.Scan() {
		a.rows = append(a.rows, libRow{heading: g.Dir, err: g.Err})
		for _, e := range g.Entries {
			a.rows = append(a.rows, libRow{entry: e, info: library.Describe(e.Path, now)})
			// An expanded pack unfolds into its member decks, each its own
			// launchable row — the pack row above stays the "study it all"
			// unit, like the web group page's All.
			if e.Pack && a.expanded[e.Path] {
				for _, c := range library.PackEntries(e.Path) {
					a.rows = append(a.rows, libRow{entry: c, child: true, info: library.Describe(c.Path, now)})
				}
			}
		}
	}

	a.sel = a.nextSelectable(-1, +1)
	for i, r := range a.rows {
		if r.heading == "" && r.entry.Path == prev {
			a.sel = i
			break
		}
	}
	a.libTop = 0
}

// selectedRow returns the selected deck row, or nil when the library is empty.
func (a *App) selectedRow() *libRow {
	if a.sel < 0 || a.sel >= len(a.rows) || a.rows[a.sel].heading != "" {
		return nil
	}
	return &a.rows[a.sel]
}

// nextSelectable walks from row i in the given direction to the next deck
// row, skipping headings. Returns i itself when there is nowhere to go.
func (a *App) nextSelectable(i, dir int) int {
	for j := i + dir; j >= 0 && j < len(a.rows); j += dir {
		if a.rows[j].heading == "" {
			return j
		}
	}
	return i
}

// moveSel moves the selection by one deck row.
func (a *App) moveSel(dir int) {
	if next := a.nextSelectable(a.sel, dir); next != a.sel {
		a.sel = next
		a.render()
	}
}

// launchOpts is one library launch variant: each key below sets at most one
// knob, everything else stays the deck's own. The zero value is a plain
// `study <path>` run.
type launchOpts struct {
	reverse  bool
	order    deck.OrderMode
	orderSet bool
	mode     deck.QuizMode // forced answer mode (typed / choice)
	modeSet  bool
}

func (a *App) handleLibraryKey(key string) {
	// An armed forget prompt owns the next key: y clears, anything else
	// cancels. Nothing is deleted on the keystroke that asked.
	if a.forgetPending {
		a.forgetPending = false
		if key == "y" {
			a.forgetSelected()
		} else {
			a.libMsg = ""
		}
		a.render()
		return
	}

	switch key {
	case "j", "Down":
		a.moveSel(+1)
	case "k", "Up":
		a.moveSel(-1)
	case "Return":
		// The deck exactly as `study <path>` runs it: its own order, forward.
		a.launchSelected(launchOpts{})
	case "r":
		a.launchSelected(launchOpts{reverse: true})
	case "f":
		a.launchSelected(launchOpts{order: deck.OrderFlipThrough, orderSet: true})
	case "w":
		a.launchSelected(launchOpts{order: deck.OrderWeakOnly, orderSet: true})
	case "t":
		a.launchSelected(launchOpts{mode: deck.ModeType, modeSet: true})
	case "c":
		a.launchSelected(launchOpts{mode: deck.ModeChoice, modeSet: true})
	case "Tab":
		if row := a.selectedRow(); row != nil && row.entry.Pack {
			if a.expanded == nil {
				a.expanded = make(map[string]bool)
			}
			a.expanded[row.entry.Path] = !a.expanded[row.entry.Path]
			a.refreshLibrary()
			a.render()
		}
	case "s":
		if row := a.selectedRow(); row != nil {
			a.libMsg = ""
			a.openStats(row.entry.Path, false)
		}
	case "x":
		if row := a.selectedRow(); row != nil {
			a.forgetPending = true
			a.libMsg = fmt.Sprintf("forget %s — clear all its saved progress? y: yes · any other key: cancel", row.entry.Name)
			a.render()
		}
	case "q", "Escape":
		a.exit()
	}
}

// forgetSelected clears every trace of studying the selected deck — both
// directions, orphaned entries included — the GUI counterpart of running
// --forget for each direction. The deck itself stays shelved: forgetting is
// about the history, membership is the watched directory.
func (a *App) forgetSelected() {
	row := a.selectedRow()
	if row == nil {
		return
	}
	name := row.entry.Name

	store, err := progress.NewStore(row.entry.Path)
	if err == nil {
		store.Reset()
		err = store.Save()
	}
	if err != nil {
		a.libMsg = fmt.Sprintf("✗ forgetting %s: %v", name, err)
		return
	}

	a.refreshLibrary()
	a.libMsg = "progress cleared for " + name
}

// launchSelected starts a session on the selected deck: reversed, under a
// forced order (flip-through, weak-only) or answer mode (typed, choice) when
// the variant asks — launch settings, not deck edits — and the deck's own
// settings otherwise. A launch with nothing to serve (unparsable deck, no
// reversible cards, nothing weak) stays on the library and says why in the
// notice line.
func (a *App) launchSelected(opts launchOpts) {
	row := a.selectedRow()
	if row == nil {
		return
	}
	a.libMsg = ""

	d, store, err := session.Load(row.entry.Path, opts.reverse, os.Stderr)
	if err != nil {
		a.libMsg = err.Error()
		a.render()
		return
	}
	if opts.orderSet {
		d.Order = opts.order
	}
	if opts.modeSet {
		d.ForceAnswerMode(opts.mode)
	}
	engine, err := session.Start(d, store, session.Ahead{}, time.Now())
	if err != nil {
		a.libMsg = err.Error()
		a.render()
		return
	}

	a.engine = engine
	a.store = store
	a.reverse = opts.reverse
	a.start = time.Now()

	// The deck's own presentation settings, exactly as a direct run applies
	// them; Ctrl+0 returns to the deck's starting size for this session.
	pt := defaultFontPt
	if v := engine.FontSize(); v > 0 {
		pt = clampFontPt(float64(v))
	}
	a.initialFontPt = pt
	a.setFontSize(pt)
	a.audioSpeed = defaultSpeed
	if x := engine.Speed(); x > 0 {
		a.audioSpeed = clampSpeed(x)
	}

	a.presentCard()
	a.render()
}

// returnToLibrary sheds the finished session and puts the library screen back
// up with freshly scanned due counts — the numbers the session just changed.
func (a *App) returnToLibrary() {
	a.engine = nil
	a.store = nil
	a.reverse = false
	a.result = nil
	a.revealImg = nil
	a.confusedImg = nil
	a.questionImg = nil
	a.inputBuf = ""
	a.deadline = time.Time{}
	a.resultLock = time.Time{}
	a.libMsg = ""

	// Back to the app default size; the deck's "# font-size:" was a session
	// setting, not a library one.
	a.initialFontPt = defaultFontPt
	a.setFontSize(defaultFontPt)

	a.refreshLibrary()
	a.render()
}

func (a *App) renderLibrary(canvas *image.RGBA) {
	x := padding
	y := padding + lineHeight(a.fontLarge)
	a.drawText(canvas, "Library", x, y, a.fontLarge, accentColor)
	y += a.scaled(16)

	if len(a.rows) == 0 {
		a.drawTextCentered(canvas, "the watched directories hold no decks", a.height/2, a.fontRegular, textColor)
		a.drawTextCentered(canvas, "add one with: study --watch <dir>", a.height/2+lineHeight(a.fontRegular), a.fontSmall, dimColor)
		a.drawControlsBox(canvas, []string{"q: quit"})
		return
	}

	lhHead := lineHeight(a.fontSmall) + a.scaled(6)
	lhDeck := lineHeight(a.fontRegular) + a.scaled(4)
	bottom := a.height - padding - lineHeight(a.fontSmall) // room for the notice line

	// Name column: wide enough for the longest name (children measure with
	// their indent), capped so the counts always keep room. Selected names
	// render bold, so measure bold.
	nameX := x + a.scaled(20)
	childIndent := a.scaled(18)
	nameW := 0
	for _, r := range a.rows {
		if r.heading != "" {
			continue
		}
		w := font.MeasureString(a.fontBold, rowName(r.entry)).Round()
		if r.child {
			w += childIndent
		}
		if w > nameW {
			nameW = w
		}
	}
	if max := a.width * 2 / 5; nameW > max {
		nameW = max
	}
	detailX := nameX + nameW + a.scaled(28)

	// Keep the selection on screen: rows scroll as one column, headings and
	// decks alike, libTop pinned so the selected row is always drawn.
	fit := func(top int) int {
		yy, last := y+lhHead, top-1
		for i := top; i < len(a.rows) && yy <= bottom; i++ {
			if a.rows[i].heading != "" {
				yy += lhHead
			} else {
				yy += lhDeck
			}
			if yy <= bottom {
				last = i
			}
		}
		return last
	}
	if a.libTop > a.sel {
		a.libTop = a.sel
	}
	for fit(a.libTop) < a.sel {
		a.libTop++
	}

	yy := y + lhHead
	for i := a.libTop; i < len(a.rows); i++ {
		r := a.rows[i]
		if r.heading != "" {
			if yy+lhHead > bottom {
				break
			}
			yy += a.scaled(6)
			a.drawText(canvas, displayPath(r.heading), x, yy, a.fontSmall, dimColor)
			if r.err != nil {
				a.drawText(canvas, "✗ unreadable", detailX, yy, a.fontSmall, redColor)
			}
			yy += lhHead - a.scaled(6)
			continue
		}
		if yy+lhDeck > bottom {
			break
		}

		nameFace, nameColor := a.fontRegular, textColor
		if i == a.sel {
			nameFace, nameColor = a.fontBold, accentColor
			a.drawText(canvas, "›", x+a.scaled(4), yy, a.fontBold, accentColor)
		}
		nx := nameX
		if r.child {
			nx += childIndent
		}
		a.drawText(canvas, rowName(r.entry), nx, yy, nameFace, nameColor)

		if r.info.Err != nil {
			a.drawText(canvas, "✗ unreadable deck", detailX, yy, a.fontSmall, redColor)
		} else {
			a.drawText(canvas, rowDetail(r.info), detailX, yy, a.fontSmall, dimColor)
			a.drawTextRight(canvas, library.AgoLabel(r.info.LastStudied, time.Now()), a.width-padding, yy, a.fontSmall, dimColor)
		}
		yy += lhDeck
	}

	if a.libMsg != "" {
		a.drawText(canvas, a.libMsg, x, a.height-padding, a.fontSmall, yellowColor)
	}
	a.drawControlsBox(canvas, []string{
		"enter: study",
		"tab: open pack",
		"r: reversed",
		"f: flip through",
		"w: weak only",
		"t: typed",
		"c: choice",
		"s: stats",
		"x: forget",
		"q: quit",
	})
}

// rowName is a deck's display name; packs carry a trailing slash, the way the
// shell shows the directory they are.
func rowName(e library.Entry) string {
	name := e.Name
	if e.Pack {
		name += "/"
	}
	return name
}

// rowDetail is the counts part of a deck row: size, what's due forward, and
// what's due reversed when the deck can be flipped.
func rowDetail(info library.Info) string {
	parts := []string{
		fmt.Sprintf("%d cards", info.Cards),
		library.DueLabel(info.DueReviews, info.DueNew),
	}
	if info.Cards == 1 {
		parts[0] = "1 card"
	}
	if info.Reversible {
		parts = append(parts, "reversed "+library.DueLabel(info.RevReviews, info.RevNew))
	}
	return strings.Join(parts, "  ·  ")
}

// displayPath abbreviates the home directory to ~ for the heading lines.
func displayPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + rel
	}
	return p
}
