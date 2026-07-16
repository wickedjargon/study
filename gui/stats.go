package gui

// The stats screen: the library's close-up view of one deck, the GUI twin of
// --stats. Opened with s on a library row, it reports one direction at a
// time; r flips to the reversed direction, esc returns to the library.

import (
	"fmt"
	"image"
	"time"

	"golang.org/x/image/font"

	"study/library"
)

// openStats loads the selected deck's report and switches the library to the
// stats screen. A deck that can't be read stays on the library with the
// reason in the notice line.
func (a *App) openStats(path string, reverse bool) {
	info, err := library.Stats(path, reverse, time.Now())
	if err != nil {
		a.stats = nil
		a.libMsg = err.Error()
		a.render()
		return
	}
	a.stats = &info
	a.statsPath = path
	a.statsReverse = reverse
	a.render()
}

func (a *App) handleStatsKey(key string) {
	switch key {
	case "r":
		// Flip direction: each is a separate skill with its own history.
		if a.statsReverse || a.stats.Reversible {
			a.openStats(a.statsPath, !a.statsReverse)
		}
	case "Escape", "Return", "q", "s":
		a.stats = nil
		a.render()
	}
}

func (a *App) renderStats(canvas *image.RGBA) {
	info := a.stats
	x := padding
	y := padding + lineHeight(a.fontLarge)

	title := info.Name
	if a.statsReverse {
		title += " — reversed"
	}
	a.drawText(canvas, title, x, y, a.fontLarge, accentColor)
	y += lineHeight(a.fontLarge) + a.scaled(10)

	type statRow struct {
		label, value string
	}
	rows := []statRow{
		{"Cards in deck", fmt.Sprintf("%d", info.Cards)},
		{"Studied", fmt.Sprintf("%d  (%.0f%%)", info.Studied, float64(info.Studied)/float64(max(info.Cards, 1))*100)},
		{"Mastered", fmt.Sprintf("%d", info.Mastered)},
		{"Due now", fmt.Sprintf("%d reviews + %d new", info.DueReviews, info.DueNew)},
	}
	if !info.NextDue.IsZero() {
		rows = append(rows, statRow{"Next review", info.NextDue.Local().Format("Mon Jan 2 15:04")})
	}
	if info.Studied > 0 {
		rows = append(rows,
			statRow{"All-time correct", fmt.Sprintf("%d", info.Correct)},
			statRow{"All-time wrong", fmt.Sprintf("%d", info.Wrong)},
			statRow{"All-time accuracy", fmt.Sprintf("%.0f%%", info.Accuracy())})
	}

	valueX := x
	for _, r := range rows {
		if w := font.MeasureString(a.fontRegular, r.label).Round(); x+w > valueX {
			valueX = x + w
		}
	}
	valueX += a.scaled(28)
	for _, r := range rows {
		a.drawText(canvas, r.label, x, y, a.fontRegular, dimColor)
		a.drawText(canvas, r.value, valueX, y, a.fontRegular, textColor)
		y += lineHeight(a.fontRegular)
	}

	if info.Studied == 0 {
		y += a.scaled(16)
		a.drawText(canvas, "no progress recorded yet for this direction", x, y, a.fontSmall, dimColor)
	} else if len(info.Weakest) > 0 {
		y += a.scaled(20)
		a.drawText(canvas, "── Weakest cards ──", x, y, a.fontSmall, dimColor)
		y += lineHeight(a.fontSmall) + a.scaled(6)

		// Number columns right-aligned so the labels line up in a
		// proportional font.
		accX := x + font.MeasureString(a.fontSmall, "100% acc").Round()
		confX := accX + a.scaled(24) + font.MeasureString(a.fontSmall, "conf 100").Round()
		labelX := confX + a.scaled(28)
		for _, wc := range info.Weakest {
			if y > a.height-padding {
				break
			}
			a.drawTextRight(canvas, fmt.Sprintf("%.0f%% acc", wc.Accuracy), accX, y, a.fontSmall, dimColor)
			a.drawTextRight(canvas, fmt.Sprintf("conf %.0f", wc.Confidence), confX, y, a.fontSmall, dimColor)
			a.drawText(canvas, wc.Label, labelX, y, a.fontRegular, textColor)
			y += lineHeight(a.fontRegular)
		}
	}

	hints := []string{"esc: back"}
	if info.Reversible {
		if a.statsReverse {
			hints = append([]string{"r: forward stats"}, hints...)
		} else {
			hints = append([]string{"r: reversed stats"}, hints...)
		}
	}
	a.drawControlsBox(canvas, hints)
}
