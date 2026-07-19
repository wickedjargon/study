// Package gio is the cross-platform GUI: the same session engine as the X11
// gui package, rendered with Gio (gioui.org) so study can eventually run
// beyond X11 — Windows is the target. First slice: text cards only (type,
// choice, set entry, near-miss practice, preview, caught-up, summary). No
// images, audio, library, or reverse mode yet; the X11 GUI remains the daily
// driver until parity, and both install side by side for comparison.
//
// Interaction model: one persistent input line drives the whole session,
// keyboard-first like the X11 version. On a question it takes the answer (or
// a choice number); on every other screen an empty enter advances. Buttons
// mirror the choices for the mouse.
package gio

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"study/deck"
	"study/progress"
	"study/quiz"
	"study/session"
)

// The X11 GUI's palette, roughly: verdicts green/red, structure dim.
var (
	okColor     = color.NRGBA{R: 0x2e, G: 0x7d, B: 0x32, A: 0xff}
	badColor    = color.NRGBA{R: 0xc6, G: 0x28, B: 0x28, A: 0xff}
	dimColor    = color.NRGBA{R: 0x8a, G: 0x88, B: 0x7a, A: 0xff}
	accentColor = color.NRGBA{R: 0x8f, G: 0x5f, B: 0x00, A: 0xff}
)

// App is one running session in a Gio window.
type App struct {
	engine   *quiz.Engine
	store    *progress.Store
	deckName string

	input   widget.Editor
	choices []widget.Clickable
	noIdea  widget.Clickable
	endBtn  widget.Clickable

	result  *quiz.Result
	setHint string // transient: duplicate / near-spelling feedback
}

// Run loads the deck, starts a session, and blocks in the Gio main loop.
func Run(deckPath string) error {
	d, store, err := session.Load(deckPath, false, os.Stderr)
	if err != nil {
		return err
	}
	engine, err := session.Start(d, store, session.Ahead{}, time.Now())
	if err != nil {
		return err
	}

	a := &App{
		engine:   engine,
		store:    store,
		deckName: d.Name,
	}
	a.input.SingleLine = true
	a.input.Submit = true

	go func() {
		w := new(app.Window)
		w.Option(app.Title("study — "+d.Name), app.Size(unit.Dp(920), unit.Dp(660)))
		if err := a.loop(w); err != nil {
			log.Printf("study-gio: %v", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
	return nil
}

func (a *App) loop(w *app.Window) error {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			a.update(gtx)
			a.frame(gtx, th)
			e.Frame(gtx.Ops)
		}
	}
}

// update processes input events before drawing the frame.
func (a *App) update(gtx layout.Context) {
	// The one input line: submits route by state.
	for {
		ev, ok := a.input.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			a.submit(strings.TrimSpace(a.input.Text()))
			a.input.SetText("")
		}
	}

	// Choice buttons.
	if a.engine.State() == quiz.ShowQuestion && a.engine.Mode() == deck.ModeChoice {
		for i := range a.choices {
			if a.choices[i].Clicked(gtx) {
				a.finishAnswer(a.engine.Answer(i))
			}
		}
		if a.noIdea.Clicked(gtx) {
			a.finishAnswer(a.engine.AnswerNoIdea())
		}
	}
	if a.endBtn.Clicked(gtx) {
		if a.engine.State() == quiz.Done {
			os.Exit(0)
		}
		a.engine.End()
	}
}

// submit routes the input line's enter by session state — the whole
// keyboard interface of this first slice.
func (a *App) submit(text string) {
	e := a.engine
	switch e.State() {
	case quiz.ShowQuestion:
		c := e.Current()
		switch {
		case c != nil && c.IsSet():
			if text == "" {
				a.setHint = ""
				a.finishAnswer(e.AnswerSetGiveUp())
				return
			}
			if out := e.AnswerSetEntry(text); out != nil {
				switch out.Verdict {
				case quiz.SetDuplicate:
					a.setHint = "already named"
				case quiz.SetClose:
					a.setHint = "close — check the spelling"
				default:
					a.setHint = ""
				}
				if out.Result != nil {
					a.setHint = ""
					a.finishAnswer(out.Result)
				}
			}
		case e.Mode() == deck.ModeChoice:
			// Digits pick a choice, 0 declines; anything else is ignored.
			if n, err := strconv.Atoi(text); err == nil {
				if n == 0 {
					a.finishAnswer(e.AnswerNoIdea())
				} else if n >= 1 && n <= len(e.Options()) {
					a.finishAnswer(e.Answer(n - 1))
				}
			}
		default:
			a.finishAnswer(e.AnswerTyped(text))
		}
	case quiz.ShowResult:
		if e.PracticeOwed() > 0 {
			e.PracticeTyped(text)
			return
		}
		e.Next()
		if e.State() != quiz.ShowResult {
			a.result = nil
		}
	case quiz.ShowPreview:
		e.ConfirmPreview()
	case quiz.CaughtUp:
		e.ContinueAll()
	case quiz.Done:
		os.Exit(0)
	}
}

// finishAnswer records a graded result and persists, mirroring the X11
// GUI's save-after-every-answer.
func (a *App) finishAnswer(res *quiz.Result) {
	if res == nil {
		return
	}
	a.result = res
	if err := a.store.Save(); err != nil {
		log.Printf("study-gio: saving progress: %v", err)
	}
}

// frame draws the current screen.
func (a *App) frame(gtx layout.Context, th *material.Theme) {
	// The input line owns the keyboard on every screen.
	gtx.Execute(key.FocusCmd{Tag: &a.input})

	inset := layout.UniformInset(unit.Dp(24))
	inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.header(th)),
			layout.Rigid(spacer(12)),
			layout.Flexed(1, a.body(th)),
			layout.Rigid(spacer(12)),
			layout.Rigid(a.inputRow(th)),
			layout.Rigid(a.footer(th)),
		)
	})
}

func (a *App) header(th *material.Theme) layout.Widget {
	e := a.engine
	return func(gtx layout.Context) layout.Dimensions {
		pos := fmt.Sprintf("[%d/%d]", e.TotalSeen+1, e.TotalSeen+e.Remaining())
		if e.State() == quiz.ShowResult || e.State() == quiz.Done {
			pos = fmt.Sprintf("[%d/%d]", e.TotalSeen, e.TotalSeen+e.Remaining())
		}
		tally := fmt.Sprintf("✓%d  ✗%d", e.TotalCorrect, e.TotalWrong)
		return layout.Flex{Spacing: layout.SpaceBetween}.Layout(gtx,
			layout.Rigid(coloredLabel(th, unit.Sp(14), pos+"  "+a.badge(), dimColor)),
			layout.Rigid(coloredLabel(th, unit.Sp(14), tally, dimColor)),
		)
	}
}

// badge mirrors the X11 badge priority: retry beats new beats learning
// beats ahead.
func (a *App) badge() string {
	e := a.engine
	if e.State() != quiz.ShowQuestion && e.State() != quiz.ShowPreview {
		return ""
	}
	switch {
	case e.IsRetry():
		return "retry"
	case e.CurrentIsNew():
		return "new"
	case e.CurrentIsLearning():
		return "learning"
	case e.CurrentIsAhead():
		return "ahead"
	}
	return ""
}

func (a *App) body(th *material.Theme) layout.Widget {
	switch a.engine.State() {
	case quiz.ShowQuestion:
		return a.questionBody(th)
	case quiz.ShowResult:
		return a.resultBody(th)
	case quiz.ShowPreview:
		return a.previewBody(th)
	case quiz.CaughtUp:
		return a.caughtUpBody(th)
	default:
		return a.summaryBody(th)
	}
}

func (a *App) questionBody(th *material.Theme) layout.Widget {
	e := a.engine
	card := e.Current()
	return func(gtx layout.Context) layout.Dimensions {
		var rows []layout.FlexChild
		rows = append(rows,
			layout.Rigid(coloredLabel(th, unit.Sp(22), textOf(card.Question), th.Palette.Fg)),
			layout.Rigid(spacer(16)),
		)
		if card.IsSet() {
			counter := fmt.Sprintf("named %d of %d", e.SetNamedCount(), card.SetTarget())
			if left := e.SetAttemptsLeft(); left >= 0 {
				counter += fmt.Sprintf(" · %d tries left", left)
			}
			rows = append(rows, layout.Rigid(coloredLabel(th, unit.Sp(14), counter, dimColor)))
			for _, en := range e.SetLog() {
				c, mark := okColor, " ✓"
				if !en.Hit {
					c, mark = badColor, " ✗"
				}
				rows = append(rows, layout.Rigid(coloredLabel(th, unit.Sp(17), en.Text+mark, c)))
			}
			if a.setHint != "" {
				rows = append(rows, layout.Rigid(coloredLabel(th, unit.Sp(15), a.setHint, dimColor)))
			}
		} else if e.Mode() == deck.ModeChoice {
			opts := e.Options()
			if len(a.choices) != len(opts) {
				a.choices = make([]widget.Clickable, len(opts))
			}
			for i, opt := range opts {
				i, opt := i, opt
				rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					b := material.Button(th, &a.choices[i], fmt.Sprintf("%d)  %s", i+1, opt))
					b.Background = color.NRGBA{A: 0}
					b.Color = th.Palette.Fg
					b.Inset = layout.UniformInset(unit.Dp(4))
					return leftAlign(gtx, b.Layout)
				}), layout.Rigid(spacer(2)))
			}
			rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				b := material.Button(th, &a.noIdea, "0)  no idea")
				b.Background = color.NRGBA{A: 0}
				b.Color = dimColor
				b.Inset = layout.UniformInset(unit.Dp(4))
				return leftAlign(gtx, b.Layout)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	}
}

func (a *App) resultBody(th *material.Theme) layout.Widget {
	e := a.engine
	res := a.result
	return func(gtx layout.Context) layout.Dimensions {
		if res == nil {
			return layout.Dimensions{}
		}
		card := res.Card
		var rows []layout.FlexChild
		add := func(w layout.Widget) { rows = append(rows, layout.Rigid(w)) }

		add(coloredLabel(th, unit.Sp(22), textOf(card.Question), th.Palette.Fg))
		add(spacer(16))

		if card.IsSet() {
			named := e.SetNamed()
			var got, missed []string
			for i, it := range card.SetItems {
				if i < len(named) && named[i] {
					got = append(got, it.Text)
				} else {
					missed = append(missed, it.Text)
				}
			}
			verdict, c := fmt.Sprintf("✓ named %d of %d", len(got), card.SetTarget()), okColor
			if !res.Correct {
				verdict, c = fmt.Sprintf("✗ named %d of %d", len(got), card.SetTarget()), badColor
			}
			add(coloredLabel(th, unit.Sp(18), verdict, c))
			if len(got) > 0 {
				add(coloredLabel(th, unit.Sp(16), strings.Join(got, ", "), okColor))
			}
			if len(missed) > 0 {
				add(coloredLabel(th, unit.Sp(16), strings.Join(missed, ", "), dimColor))
			}
		} else {
			verdict, c := "✓ correct", okColor
			switch {
			case res.Correct:
			case res.TimedOut:
				verdict, c = "✗ time's up", badColor
			case res.NoIdea:
				verdict, c = "✗ no idea", badColor
			default:
				verdict, c = "✗ wrong", badColor
			}
			add(coloredLabel(th, unit.Sp(18), verdict, c))
			if !res.Correct && res.Typed != "" {
				add(coloredLabel(th, unit.Sp(16), "> "+res.Typed, badColor))
			}
			if !res.Correct {
				add(coloredLabel(th, unit.Sp(16), "= "+res.Answer, okColor))
			}
		}

		if note := textOf(card.Note); note != "" {
			add(spacer(10))
			add(coloredLabel(th, unit.Sp(15), note, dimColor))
		}
		if res.ConfusedWith != nil {
			add(spacer(10))
			add(coloredLabel(th, unit.Sp(15),
				"that's the answer to: "+deck.CardLabel(res.ConfusedWith), accentColor))
		}
		if owed := e.PracticeOwed(); owed > 0 {
			add(spacer(14))
			add(coloredLabel(th, unit.Sp(15),
				fmt.Sprintf("almost — type it %d more times to continue: %s", owed, res.Answer),
				accentColor))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	}
}

func (a *App) previewBody(th *material.Theme) layout.Widget {
	card := a.engine.Current()
	return func(gtx layout.Context) layout.Dimensions {
		if card == nil {
			return layout.Dimensions{}
		}
		var rows []layout.FlexChild
		rows = append(rows,
			layout.Rigid(coloredLabel(th, unit.Sp(22), textOf(card.Question), th.Palette.Fg)),
			layout.Rigid(spacer(14)),
			layout.Rigid(coloredLabel(th, unit.Sp(18), textOf(card.Answer), okColor)),
		)
		if note := textOf(card.Note); note != "" {
			rows = append(rows,
				layout.Rigid(spacer(10)),
				layout.Rigid(coloredLabel(th, unit.Sp(15), note, dimColor)))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	}
}

func (a *App) caughtUpBody(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		due, _ := a.engine.NextDue()
		msg := "You're all caught up."
		if !due.IsZero() {
			msg += "  Next review " + due.Local().Format("Mon 15:04") + "."
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(coloredLabel(th, unit.Sp(20), "✓  "+msg, okColor)),
			layout.Rigid(spacer(8)),
			layout.Rigid(coloredLabel(th, unit.Sp(15), "enter: keep studying ahead of schedule", dimColor)),
		)
	}
}

func (a *App) summaryBody(th *material.Theme) layout.Widget {
	e := a.engine
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(coloredLabel(th, unit.Sp(20), "Session complete", th.Palette.Fg)),
			layout.Rigid(spacer(10)),
			layout.Rigid(coloredLabel(th, unit.Sp(16),
				fmt.Sprintf("✓ %d correct   ✗ %d wrong", e.TotalCorrect, e.TotalWrong), dimColor)),
			layout.Rigid(spacer(8)),
			layout.Rigid(coloredLabel(th, unit.Sp(15), "enter: quit", dimColor)),
		)
	}
}

func (a *App) inputRow(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		hint := a.inputHint()
		ed := material.Editor(th, &a.input, hint)
		ed.TextSize = unit.Sp(18)
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(coloredLabel(th, unit.Sp(18), "> ", accentColor)),
			layout.Flexed(1, ed.Layout),
		)
	}
}

// inputHint is the input line's ghost text: what enter will do right now.
func (a *App) inputHint() string {
	e := a.engine
	switch e.State() {
	case quiz.ShowQuestion:
		c := e.Current()
		switch {
		case c != nil && c.IsSet():
			return "name one… (empty enter: give up)"
		case e.Mode() == deck.ModeChoice:
			return "type a choice number, 0 for no idea"
		default:
			return "type your answer…"
		}
	case quiz.ShowResult:
		if owed := e.PracticeOwed(); owed > 0 && a.result != nil {
			return a.result.Answer
		}
		return "enter: continue"
	case quiz.ShowPreview:
		return "enter: got it, quiz me"
	case quiz.CaughtUp:
		return "enter: keep studying"
	default:
		return "enter: quit"
	}
}

func (a *App) footer(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		label := "end session"
		if a.engine.State() == quiz.Done {
			label = "quit"
		}
		b := material.Button(th, &a.endBtn, label)
		b.Background = color.NRGBA{A: 0}
		b.Color = dimColor
		b.TextSize = unit.Sp(13)
		b.Inset = layout.UniformInset(unit.Dp(4))
		return layout.Flex{Spacing: layout.SpaceBetween}.Layout(gtx,
			layout.Rigid(coloredLabel(th, unit.Sp(13), "study-gio (preview) — "+a.deckName, dimColor)),
			layout.Rigid(b.Layout),
		)
	}
}

// ── helpers ─────────────────────────────────────────────────────────

// textOf joins a card side's text elements; media (images, audio) are not
// rendered in this slice.
func textOf(media []deck.Media) string {
	var parts []string
	for _, m := range media {
		if m.Type == deck.Text && strings.TrimSpace(m.Content) != "" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func coloredLabel(th *material.Theme, size unit.Sp, txt string, c color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, size, txt)
		l.Color = c
		return l.Layout(gtx)
	}
}

func spacer(dp unit.Dp) layout.Widget {
	return layout.Spacer{Height: dp}.Layout
}

func leftAlign(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return layout.W.Layout(gtx, w)
}
