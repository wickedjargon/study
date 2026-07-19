package gio

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"study/library"
	"study/progress"
	"study/session"
)

// The library screen: bare `study-gio` opens it, mirroring bare `study`.
// Watched directories with their decks, due counts, and last-studied labels;
// a row opens a session in the same window, and the session's summary
// returns here with the counts rescanned. First slice: packs launch whole
// (no expansion into member decks), rows open by click or by typing their
// number on the input line.

// libRow is one line of the library screen: a watched-directory heading, or
// a launchable deck entry with its scanned state.
type libRow struct {
	heading string
	err     error
	entry   library.Entry
	info    library.Info
	num     int // 1-based launch number; 0 on headings
	click   widget.Clickable
}

// RunLibrary opens the library window: the no-argument study-gio.
func RunLibrary() error {
	dir, err := progress.Dir()
	if err != nil {
		return err
	}
	reg, err := library.Open(dir)
	if err != nil {
		return err
	}
	a := &App{reg: reg, fromLibrary: true}
	a.input.SingleLine = true
	a.input.Submit = true
	a.libList.Axis = layout.Vertical
	a.rescanLibrary()

	go func() {
		w := newWindow("study — library")
		if err := a.loop(w); err != nil {
			log.Printf("study-gio: %v", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	appMain()
	return nil
}

// rescanLibrary rebuilds the rows from the watched directories, re-reading
// every deck's progress so the counts reflect the session just finished.
func (a *App) rescanLibrary() {
	a.libRows = a.libRows[:0]
	now := time.Now()
	n := 0
	for _, g := range a.reg.Scan() {
		a.libRows = append(a.libRows, &libRow{heading: g.Dir, err: g.Err})
		for _, e := range g.Entries {
			n++
			a.libRows = append(a.libRows, &libRow{
				entry: e,
				info:  library.Describe(e.Path, now),
				num:   n,
			})
		}
	}
}

// openRow launches a session for a library row in the same window.
func (a *App) openRow(r *libRow) {
	d, store, err := session.Load(r.entry.Path, false, os.Stderr)
	if err != nil {
		a.libErr = err
		return
	}
	engine, err := session.Start(d, store, session.Ahead{}, time.Now())
	if err != nil {
		a.libErr = err
		return
	}
	a.libErr = nil
	a.engine = engine
	a.store = store
	a.deckName = d.Name
	a.result = nil
	a.setHint = ""
}

// backToLibrary returns from a finished session, counts rescanned.
func (a *App) backToLibrary() {
	a.engine = nil
	a.store = nil
	a.result = nil
	a.setHint = ""
	a.rescanLibrary()
}

// librarySubmit routes the input line while the library is showing: a row
// number opens that deck.
func (a *App) librarySubmit(text string) {
	n, err := strconv.Atoi(text)
	if err != nil {
		return
	}
	for _, r := range a.libRows {
		if r.num == n {
			a.openRow(r)
			return
		}
	}
}

// libraryUpdate handles row clicks.
func (a *App) libraryUpdate(gtx layout.Context) {
	for _, r := range a.libRows {
		if r.num > 0 && r.click.Clicked(gtx) {
			a.openRow(r)
			return
		}
	}
}

// libraryBody draws the rows.
func (a *App) libraryBody(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if len(a.libRows) == 0 {
			return coloredLabel(th, unit.Sp(16),
				"library is empty — watch a directory: study --watch <dir>", dimColor, text.Start)(gtx)
		}
		lst := material.List(th, &a.libList)
		lst.AnchorStrategy = material.Overlay
		return lst.Layout(gtx, len(a.libRows), func(gtx layout.Context, i int) layout.Dimensions {
			return a.libRowWidget(th, a.libRows[i])(gtx)
		})
	}
}

func (a *App) libRowWidget(th *material.Theme, r *libRow) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if r.heading != "" {
			label := r.heading
			if r.err != nil {
				label += "  (" + r.err.Error() + ")"
			}
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(4)}.Layout(gtx,
				coloredLabel(th, unit.Sp(13), label, dimColor, text.Start))
		}

		name := fmt.Sprintf("%d)  %s", r.num, r.entry.Name)
		status, statusColor := "", dimColor
		if r.info.Err != nil {
			status, statusColor = r.info.Err.Error(), badColor
		} else {
			parts := library.DirParts(r.info.DueReviews, r.info.DueNew, r.info.Cards)
			for i, p := range parts {
				if i > 0 {
					status += " · "
				}
				status += p.Text
				if p.Kind == library.KindDue {
					statusColor = accentColor
				}
			}
			if status == "" {
				status = fmt.Sprintf("%d cards", r.info.Cards)
			}
		}
		ago := library.AgoLabel(r.info.LastStudied, time.Now())

		return r.click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{}.Layout(gtx,
						layout.Flexed(1, coloredLabel(th, unit.Sp(16), name, textColor, text.Start)),
						layout.Rigid(coloredLabel(th, unit.Sp(14), status+"   ", statusColor, text.End)),
						layout.Rigid(coloredLabel(th, unit.Sp(14), ago, dimColor, text.End)),
					)
				})
		})
	}
}
