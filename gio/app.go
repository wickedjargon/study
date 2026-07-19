// Package gio is the cross-platform GUI: the same session engine as the X11
// gui package, rendered with Gio (gioui.org) so study can eventually run
// beyond X11 — Windows is the target. First slice: text cards only (type,
// choice, set entry, near-miss practice, preview, caught-up, summary). No
// images, audio, library, or reverse mode yet; the X11 GUI remains the daily
// driver until parity, and both install side by side for comparison.
//
// Interaction model: one persistent input line drives the whole session,
// keyboard-first like the X11 version. On a question it takes the answer (or
// a choice number); on every other screen an empty enter advances. Escape
// ends the session, then quits the summary. Buttons mirror the choices for
// the mouse. Layout and palette follow the X11 light scheme so the two can
// be compared side by side without the paint getting in the way.
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
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"study/deck"
	"study/progress"
	"study/quiz"
	"study/session"
)

// The X11 GUI's light scheme (gui/app.go), same color roles: dim for
// structure, accent for "what's being asked of me", green/red for verdicts,
// yellow for the confusion contrast only.
var (
	bgColor     = color.NRGBA{R: 0xfb, G: 0xf1, B: 0xc7, A: 0xff}
	textColor   = color.NRGBA{R: 0x3c, G: 0x38, B: 0x36, A: 0xff}
	dimColor    = color.NRGBA{R: 0x92, G: 0x83, B: 0x74, A: 0xff}
	okColor     = color.NRGBA{R: 0x79, G: 0x74, B: 0x0e, A: 0xff}
	badColor    = color.NRGBA{R: 0x9d, G: 0x00, B: 0x06, A: 0xff}
	yellowColor = color.NRGBA{R: 0xb5, G: 0x76, B: 0x14, A: 0xff}
	accentColor = color.NRGBA{R: 0x07, G: 0x66, B: 0x78, A: 0xff}
)

// App is one running session in a Gio window.
type App struct {
	engine   *quiz.Engine
	store    *progress.Store
	deckName string

	input   widget.Editor
	choices []widget.Clickable
	noIdea  widget.Clickable

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
	th.Palette.Bg = bgColor
	th.Palette.Fg = textColor
	th.Palette.ContrastBg = accentColor
	th.Palette.ContrastFg = bgColor
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			paint.Fill(gtx.Ops, bgColor)
			a.update(gtx)
			a.frame(gtx, th)
			e.Frame(gtx.Ops)
		}
	}
}

// update processes input events before drawing the frame.
func (a *App) update(gtx layout.Context) {
	// Escape ends the session (the summary shows), then quits — the X11
	// binding. The filter rides the input's focus, which the input always
	// holds.
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &a.input, Name: key.NameEscape})
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
			a.escape()
		}
	}

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
}

func (a *App) escape() {
	if a.engine.State() == quiz.Done {
		os.Exit(0)
	}
	a.engine.End()
	a.result = nil
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

// frame draws the current screen: header, body, the controls box on the
// right, the prompt line, footer — the X11 arrangement.
func (a *App) frame(gtx layout.Context, th *material.Theme) {
	gtx.Execute(key.FocusCmd{Tag: &a.input})

	inset := layout.UniformInset(unit.Dp(24))
	inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.header(th)),
			layout.Rigid(spacer(16)),
			layout.Flexed(1, a.body(th)),
			layout.Rigid(a.controlsBox(th)),
			layout.Rigid(spacer(8)),
			layout.Rigid(a.inputRow(th)),
			layout.Rigid(a.footer(th)),
		)
	})
}

// header: position left, badge centered (accent, like the X11 status bar),
// tally right — hidden until something has been answered.
func (a *App) header(th *material.Theme) layout.Widget {
	e := a.engine
	return func(gtx layout.Context) layout.Dimensions {
		pos := fmt.Sprintf("[%d/%d]", e.TotalSeen+1, e.TotalSeen+e.Remaining())
		if e.State() == quiz.ShowResult || e.State() == quiz.Done {
			pos = fmt.Sprintf("[%d/%d]", e.TotalSeen, e.TotalSeen+e.Remaining())
		}
		tally := ""
		if e.TotalSeen > 0 {
			tally = fmt.Sprintf("✓%d  ✗%d", e.TotalCorrect, e.TotalWrong)
		}
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(coloredLabel(th, unit.Sp(14), pos, dimColor, text.Start)),
			layout.Flexed(1, coloredLabel(th, unit.Sp(14), a.badge(), accentColor, text.Middle)),
			layout.Rigid(coloredLabel(th, unit.Sp(14), tally, dimColor, text.End)),
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

// question draws centered and large, the X11 presentation.
func (a *App) question(th *material.Theme, card *deck.Card) layout.FlexChild {
	return layout.Rigid(coloredLabel(th, unit.Sp(26), textOf(card.Question), textColor, text.Middle))
}

func (a *App) questionBody(th *material.Theme) layout.Widget {
	e := a.engine
	card := e.Current()
	return func(gtx layout.Context) layout.Dimensions {
		if card == nil {
			return layout.Dimensions{}
		}
		rows := []layout.FlexChild{a.question(th, card), layout.Rigid(spacer(24))}
		if card.IsSet() {
			counter := fmt.Sprintf("named %d of %d", e.SetNamedCount(), card.SetTarget())
			if left := e.SetAttemptsLeft(); left >= 0 {
				counter += fmt.Sprintf(" · %d tries left", left)
			}
			rows = append(rows, layout.Rigid(coloredLabel(th, unit.Sp(14), counter, dimColor, text.Start)))
			for _, en := range e.SetLog() {
				c, mark := okColor, " ✓"
				if !en.Hit {
					c, mark = badColor, " ✗"
				}
				rows = append(rows, layout.Rigid(coloredLabel(th, unit.Sp(17), en.Text+mark, c, text.Start)))
			}
			if a.setHint != "" {
				rows = append(rows, layout.Rigid(coloredLabel(th, unit.Sp(15), a.setHint, dimColor, text.Start)))
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
					b.Background = color.NRGBA{}
					b.Color = textColor
					b.Inset = layout.UniformInset(unit.Dp(4))
					return layout.W.Layout(gtx, b.Layout)
				}), layout.Rigid(spacer(2)))
			}
			rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				b := material.Button(th, &a.noIdea, "0)  no idea")
				b.Background = color.NRGBA{}
				b.Color = dimColor
				b.Inset = layout.UniformInset(unit.Dp(4))
				return layout.W.Layout(gtx, b.Layout)
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
		rows := []layout.FlexChild{a.question(th, card), layout.Rigid(spacer(24))}
		add := func(w layout.Widget) { rows = append(rows, layout.Rigid(w)) }

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
			verdict, c := "✓", okColor
			if !res.Correct {
				verdict, c = "✗", badColor
			}
			add(coloredLabel(th, unit.Sp(18),
				fmt.Sprintf("%s named %d of %d", verdict, len(got), card.SetTarget()), c, text.Start))
			if len(got) > 0 {
				add(coloredLabel(th, unit.Sp(16), strings.Join(got, ", "), okColor, text.Start))
			}
			if len(missed) > 0 {
				add(coloredLabel(th, unit.Sp(16), strings.Join(missed, ", "), dimColor, text.Start))
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
			add(coloredLabel(th, unit.Sp(18), verdict, c, text.Start))
			if !res.Correct && res.Typed != "" {
				add(coloredLabel(th, unit.Sp(16), "> "+res.Typed, badColor, text.Start))
			}
			if !res.Correct {
				add(coloredLabel(th, unit.Sp(16), "= "+res.Answer, okColor, text.Start))
			}
		}

		if note := textOf(card.Note); note != "" {
			add(spacer(12))
			add(coloredLabel(th, unit.Sp(15), note, dimColor, text.Start))
		}
		if res.ConfusedWith != nil {
			add(spacer(12))
			add(coloredLabel(th, unit.Sp(15),
				"that's the answer to: "+deck.CardLabel(res.ConfusedWith), yellowColor, text.Start))
		}
		if owed := e.PracticeOwed(); owed > 0 {
			times := "times"
			if owed == 1 {
				times = "time"
			}
			add(spacer(14))
			add(coloredLabel(th, unit.Sp(15),
				fmt.Sprintf("almost — type it %d more %s to continue", owed, times), dimColor, text.Start))
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
		rows := []layout.FlexChild{
			a.question(th, card),
			layout.Rigid(spacer(18)),
			layout.Rigid(coloredLabel(th, unit.Sp(18), textOf(card.Answer), okColor, text.Middle)),
		}
		if note := textOf(card.Note); note != "" {
			rows = append(rows,
				layout.Rigid(spacer(12)),
				layout.Rigid(coloredLabel(th, unit.Sp(15), note, dimColor, text.Middle)))
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
			layout.Rigid(coloredLabel(th, unit.Sp(20), "✓  "+msg, okColor, text.Middle)),
		)
	}
}

func (a *App) summaryBody(th *material.Theme) layout.Widget {
	e := a.engine
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(coloredLabel(th, unit.Sp(20), "Session complete", textColor, text.Middle)),
			layout.Rigid(spacer(10)),
			layout.Rigid(coloredLabel(th, unit.Sp(16),
				fmt.Sprintf("✓ %d correct   ✗ %d wrong", e.TotalCorrect, e.TotalWrong), dimColor, text.Middle)),
		)
	}
}

// controlsBox is the X11 GUI's bordered action legend, bottom-right: what
// enter and escape do on this screen.
func (a *App) controlsBox(th *material.Theme) layout.Widget {
	e := a.engine
	var lines []string
	switch e.State() {
	case quiz.ShowQuestion:
		c := e.Current()
		switch {
		case c != nil && c.IsSet():
			lines = []string{"enter: submit", "empty enter: give up"}
		case e.Mode() == deck.ModeChoice:
			lines = []string{"1-9: pick", "0: no idea"}
		default:
			lines = []string{"enter: submit"}
		}
		lines = append(lines, "esc: end")
	case quiz.ShowResult:
		if e.PracticeOwed() > 0 {
			lines = []string{"enter: check", "esc: end"}
		} else {
			lines = []string{"enter: continue", "esc: end"}
		}
	case quiz.ShowPreview:
		lines = []string{"enter: got it, quiz me", "esc: end"}
	case quiz.CaughtUp:
		lines = []string{"enter: keep studying", "esc: end"}
	default:
		lines = []string{"enter or esc: quit"}
	}
	return func(gtx layout.Context) layout.Dimensions {
		return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return widget.Border{Color: dimColor, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						var rows []layout.FlexChild
						for _, l := range lines {
							rows = append(rows, layout.Rigid(coloredLabel(th, unit.Sp(13), l, dimColor, text.Start)))
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
					})
				})
		})
	}
}

func (a *App) inputRow(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		ed := material.Editor(th, &a.input, a.inputHint())
		ed.TextSize = unit.Sp(18)
		ed.HintColor = dimColor
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(coloredLabel(th, unit.Sp(18), "> ", accentColor, text.Start)),
			layout.Flexed(1, ed.Layout),
		)
	}
}

// inputHint stays minimal — the controls box carries the guidance. The one
// exception is practice, where the answer itself is the ghost to type over.
func (a *App) inputHint() string {
	if a.engine.State() == quiz.ShowResult && a.engine.PracticeOwed() > 0 && a.result != nil {
		return a.result.Answer
	}
	return ""
}

func (a *App) footer(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return coloredLabel(th, unit.Sp(13), "study-gio (preview) — "+a.deckName, dimColor, text.Start)(gtx)
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

func coloredLabel(th *material.Theme, size unit.Sp, txt string, c color.NRGBA, align text.Alignment) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, size, txt)
		l.Color = c
		l.Alignment = align
		return l.Layout(gtx)
	}
}

func spacer(dp unit.Dp) layout.Widget {
	return layout.Spacer{Height: dp}.Layout
}
