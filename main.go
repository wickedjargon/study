// study — a suckless quiz tool for flashcard-style learning.
//
// Usage: study [flags] <deck-file | pack-directory>
//
// Requires: sxiv or feh (for image decks), mpv or aplay (for audio decks)
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"study/deck"
	"study/gui"
	"study/media"
	"study/progress"
	"study/quiz"
)

const helpText = `study — suckless quiz tool

usage: study [flags] <deck-file | pack-directory>

a directory is a pack: every *.deck file inside is merged into one session.

flags (each overrides the deck header's setting for this session):
  --reverse             flip the deck: see English, produce the target language
  --order <mode>        card order (see # order: below)
  --time-limit <N|none> per-question time limit, uniform for every card
  --wrong-pause <N|none>
                        how long a wrong answer's result screen refuses to
                        advance (default 5s)
  --preview-new         reveal a never-studied card's answer once before
                        quizzing it
  --new-per-session <N|all>
                        how many never-studied cards enter an adaptive session
  --font-size <N>       base font size (8-48, or small/medium/large/x-large)
  --audio-speed <X>     audio playback speed (0.25-4.0)
  --stats               print saved progress summary for the deck and exit
  --forget              clear saved progress for this deck (this direction
                        only; combine with --reverse to clear reverse progress)
  --help                show this help

deck format:
  plain text, blank lines separate cards.
  --- or === separates question from answer.
  cards default to type-in; a card with ~ distractors is multiple choice
    automatically, or opt in with # answer-mode: choice.
  a card with no separator but a {{...}} deletion is a fill-in-the-blank
    (cloze) card: the braced text is blanked out and becomes the answer.
  @img <path>       image on question/answer side
  @audio <path>     audio on question/answer side
  = <text>          extra accepted answer (type mode)
  ~ <text>          custom wrong answer (distractor)
  # comment         comment or metadata: # answer-mode: choice|type,
                    # choice-count: N, # answer-case: sensitive|insensitive,
                    # time-limit: N|none, # preview-new: on|off,
                    # new-per-session: N|all, # wrong-pause: N|none,
                    # font-size: N, # audio-speed: X,
                    # order: adaptive|sequential|flip-through|weak-only

examples:
  study japanese.deck
  study study-farsi.deck/          (a pack directory)
  study --stats mahjong.deck
  study --forget vocab.deck`

func main() {
	reverse := flag.Bool("reverse", false, "flip the deck: see English, produce the target language")
	orderFlag := flag.String("order", "", "override the deck's card order for this session (see # order: values)")
	timeLimitFlag := flag.String("time-limit", "", "override every per-question time limit (seconds, or none)")
	wrongPauseFlag := flag.String("wrong-pause", "", "override the wrong-answer pause (seconds, or none)")
	previewNew := flag.Bool("preview-new", false, "reveal a never-studied card's answer once before quizzing it")
	newPerSessionFlag := flag.String("new-per-session", "", "override how many never-studied cards enter an adaptive session (N, or all)")
	fontSizeFlag := flag.String("font-size", "", "override the base font size (8-48, or small/medium/large/x-large)")
	audioSpeedFlag := flag.String("audio-speed", "", "override audio playback speed (0.25-4.0)")
	stats := flag.Bool("stats", false, "print saved progress summary for the deck and exit")
	forget := flag.Bool("forget", false, "clear saved progress for this deck")
	help := flag.Bool("help", false, "show help")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, helpText)
	}
	flag.Parse()

	if *help || flag.NArg() == 0 {
		fmt.Println(helpText)
		os.Exit(0)
	}

	deckPath := flag.Arg(0)

	// Parse deck.
	d, err := deck.Parse(deckPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}

	// Surface non-fatal parse issues (e.g. a typo'd directive that was ignored)
	// so they aren't silently dropped.
	for _, w := range d.Warnings {
		fmt.Fprintf(os.Stderr, "study: %s\n", w)
	}

	// --reverse flips the deck for production practice (see English, type the
	// target language). Applied before progress is loaded and cards are ordered
	// so the whole session — stats, prioritization, the quiz itself — operates on
	// the reversed cards, whose "r:"-prefixed IDs track separately from forward.
	if *reverse {
		d = d.Reversed()
		// Cards that can't be reversed (cloze, media-only prompts, answers with
		// no typeable Latin text) are dropped; a deck of nothing else can't run.
		if len(d.Cards) == 0 {
			fmt.Fprintf(os.Stderr, "✗ %s has no reversible cards\n", d.Name)
			os.Exit(1)
		}
	}

	// Session overrides. Precedence is built-in default ← deck header ← flag,
	// so a flag always wins; only session-shaped settings have flags (order,
	// timing, preview, presentation) — settings that change what counts as a
	// correct answer (answer-mode, answer-case, choice-count) are deliberately
	// file-only. Applied after Reversed() so they survive the deck copy.
	if *orderFlag != "" {
		m, ok := deck.ParseOrderMode(*orderFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "✗ --order: unknown mode %q\n", *orderFlag)
			os.Exit(1)
		}
		d.Order = m
	}
	if *timeLimitFlag != "" {
		n, ok := deck.ParseTimeLimit(*timeLimitFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "✗ --time-limit: need 0-3600 seconds, or none (got %q)\n", *timeLimitFlag)
			os.Exit(1)
		}
		// The session limit is uniform: per-card overrides are cleared so the
		// flag's value (including "none") applies to every question.
		d.TimeLimit = n
		for i := range d.Cards {
			d.Cards[i].TimeLimit = 0
		}
	}
	if *wrongPauseFlag != "" {
		n, ok := deck.ParseTimeLimit(*wrongPauseFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "✗ --wrong-pause: need 0-3600 seconds, or none (got %q)\n", *wrongPauseFlag)
			os.Exit(1)
		}
		d.WrongPause = n
	}
	if *previewNew {
		d.Preview = true
	}
	if *newPerSessionFlag != "" {
		n, ok := deck.ParseNewPerSession(*newPerSessionFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "✗ --new-per-session: need an integer >= 0, or all (got %q)\n", *newPerSessionFlag)
			os.Exit(1)
		}
		d.NewPerSession = n
	}
	if *fontSizeFlag != "" {
		n, ok := deck.ParseFontSize(*fontSizeFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "✗ --font-size: need 8-48, or small/medium/large/x-large (got %q)\n", *fontSizeFlag)
			os.Exit(1)
		}
		d.FontSize = n
	}
	if *audioSpeedFlag != "" {
		x, ok := deck.ParseSpeed(*audioSpeedFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "✗ --audio-speed: need 0.25-4.0 (got %q)\n", *audioSpeedFlag)
			os.Exit(1)
		}
		d.Speed = x
	}

	// Check media viewers (audio only — images rendered in GUI).
	viewer := media.NewViewer()

	// Load progress.
	store, err := progress.NewStore(d.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ progress: %v\n", err)
		os.Exit(1)
	}

	// One-time migration: progress saved under a card's legacy ID (the old
	// hash included @audio/@img lines, so renaming a media file orphaned the
	// card's history) is moved to its current ID.
	if store.MigrateIDs(d.Cards) {
		if err := store.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "✗ saving migrated progress: %v\n", err)
			os.Exit(1)
		}
	}

	// --stats: print the saved summary for this deck and exit without
	// entering the quiz. This is a read-only view of the same numbers the
	// in-app "Session Complete" screen shows.
	if *stats {
		printStats(d, store)
		os.Exit(0)
	}

	// --forget clears saved progress and exits without launching the quiz,
	// mirroring --stats. It's a maintenance action, not the start of a study
	// session. Only the direction being studied is cleared: plain --forget
	// resets forward progress, --forget --reverse resets reverse progress —
	// so forgetting one skill doesn't destroy the other's history.
	if *forget {
		store.ResetDirection(*reverse)
		if err := store.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "✗ saving reset: %v\n", err)
			os.Exit(1)
		}
		direction := "forward"
		if *reverse {
			direction = "reverse"
		}
		fmt.Printf("✓ %s progress reset for %s\n", direction, d.Name)
		os.Exit(0)
	}

	// The session below may be a filtered subset of the deck (due cards, weak
	// cards), but a confused answer can belong to any card in the file — the
	// engine keeps the full list for confusion detection.
	full := d.Cards

	// Compose and order the session according to the deck's "# order:" mode.
	// This sets up what's served and in what starting order — how cards recur
	// afterwards (spaced criterion scheduling vs. laps) is the engine's side
	// of the same mode.
	switch d.Order {
	case deck.OrderAdaptive:
		// The default is a successive-relearning session: cards due for review
		// (most overdue first), then a bounded batch of never-studied cards.
		// Cards scheduled further out are excluded — distributing practice
		// across days is the point, and re-drilling tomorrow's cards today
		// would just collapse the spacing.
		reviews, fresh, nextDue := splitDue(d.Cards, store, time.Now())
		if d.NewPerSession >= 0 && len(fresh) > d.NewPerSession {
			fresh = fresh[:d.NewPerSession]
		}
		d.Cards = append(reviews, fresh...)
		if len(d.Cards) == 0 {
			msg := "✓ all caught up — nothing due in " + d.Name
			if !nextDue.IsZero() {
				msg += "; next review " + nextDue.Local().Format("Mon Jan 2 15:04")
			}
			fmt.Println(msg)
			fmt.Println("  (to study anyway: --order weak-only, sequential, or flip-through)")
			os.Exit(0)
		}
	case deck.OrderWeakOnly:
		d.Cards = store.FilterWeak(d.Cards)
		if len(d.Cards) == 0 {
			fmt.Printf("✓ nothing to cram — every card in %s is above the weak threshold\n", d.Name)
			os.Exit(0)
		}
		shuffleCards(d.Cards)
		d.Cards = store.PrioritizeCards(d.Cards)
	case deck.OrderSequential, deck.OrderFlipThrough:
		// Authored order, untouched.
	}

	engine := quiz.NewEngine(d, full, store)

	// Run GUI.
	if err := gui.Run(engine, viewer, store, *reverse); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
}

// splitDue partitions the deck for an adaptive session: reviews are studied
// cards due now, sorted most overdue first (so an interrupted session spends
// its time where forgetting is most advanced); fresh are never-studied cards,
// shuffled. Cards scheduled in the future are excluded; nextDue reports the
// earliest of their review times (zero when none are scheduled).
func splitDue(cards []deck.Card, store *progress.Store, now time.Time) (reviews, fresh []deck.Card, nextDue time.Time) {
	for _, c := range cards {
		cp := store.Get(c.ID)
		switch {
		case cp.TimesCorrect+cp.TimesWrong == 0:
			fresh = append(fresh, c)
		case cp.DueNow(now):
			reviews = append(reviews, c)
		default:
			if nextDue.IsZero() || cp.Due.Before(nextDue) {
				nextDue = cp.Due
			}
		}
	}
	sort.SliceStable(reviews, func(i, j int) bool {
		return store.Get(reviews[i].ID).Due.Before(store.Get(reviews[j].ID).Due)
	})
	shuffleCards(fresh)
	return reviews, fresh, nextDue
}

// shuffleCards randomizes card order in place.
func shuffleCards(cards []deck.Card) {
	rand.Shuffle(len(cards), func(i, j int) {
		cards[i], cards[j] = cards[j], cards[i]
	})
}

// printStats writes a plain-text progress summary for the deck to stdout.
// Only cards that have actually been answered count as "studied"; aggregate
// numbers are computed over the deck's current cards (orphaned progress from
// removed cards is ignored).
func printStats(d *deck.Deck, store *progress.Store) {
	type row struct {
		label    string
		accuracy float64
		conf     float64
	}

	var studied []row
	totalCorrect, totalWrong, mastered := 0, 0, 0
	for i := range d.Cards {
		c := &d.Cards[i]
		cp := store.Get(c.ID)
		if cp.TimesCorrect+cp.TimesWrong == 0 {
			continue
		}
		totalCorrect += cp.TimesCorrect
		totalWrong += cp.TimesWrong
		if cp.IsMastered() {
			mastered++
		}
		studied = append(studied, row{
			label:    cardLabel(c),
			accuracy: cp.Accuracy(),
			conf:     cp.Confidence(),
		})
	}

	fmt.Printf("study — %s\n\n", d.Name)
	fmt.Printf("  Cards in deck    %d\n", len(d.Cards))

	// Review schedule: what an adaptive session would serve right now.
	reviews, fresh, nextDue := splitDue(d.Cards, store, time.Now())
	dueLine := fmt.Sprintf("  Due now          %d reviews + %d new\n", len(reviews), len(fresh))

	if len(studied) == 0 {
		fmt.Println("  Studied          0")
		fmt.Print(dueLine)
		fmt.Println("\n  No progress recorded yet for this deck.")
		return
	}

	pct := float64(len(studied)) / float64(len(d.Cards)) * 100
	fmt.Printf("  Studied          %d  (%.0f%%)\n", len(studied), pct)
	fmt.Printf("  Mastered         %d\n", mastered)
	fmt.Print(dueLine)
	if !nextDue.IsZero() {
		fmt.Printf("  Next review      %s\n", nextDue.Local().Format("Mon Jan 2 15:04"))
	}

	acc := 0.0
	if totalCorrect+totalWrong > 0 {
		acc = float64(totalCorrect) / float64(totalCorrect+totalWrong) * 100
	}
	fmt.Printf("\n  All-time\n")
	fmt.Printf("    Correct        %d\n", totalCorrect)
	fmt.Printf("    Wrong          %d\n", totalWrong)
	fmt.Printf("    Accuracy       %.0f%%\n", acc)

	// Weakest cards first, so the things worth reviewing are at the top.
	sort.SliceStable(studied, func(i, j int) bool {
		return studied[i].conf < studied[j].conf
	})
	n := len(studied)
	if n > 10 {
		n = 10
	}
	fmt.Printf("\n  Weakest cards\n")
	for _, r := range studied[:n] {
		fmt.Printf("    %3.0f%% acc  conf %3.0f   %s\n", r.accuracy, r.conf, r.label)
	}
}

// cardLabel returns a short, single-line identifier for a card, used in the
// stats listing so the user can tell cards apart. It tries, in order: the
// question text (what the user authored and sees while studying); the answer
// text, marked with "→" so it's clear the label is the answer side (this is
// what makes media-only question cards — e.g. an image flashcard whose answer
// is a word — distinguishable); the file name of the card's first media
// element; and finally "(media card)" only when a card carries no text or
// media name at all.
func cardLabel(c *deck.Card) string {
	if s := deck.JoinText(c.Question); s != "" {
		return clipLabel(s)
	}
	if s := deck.JoinText(c.Answer); s != "" {
		return clipLabel("→ " + s)
	}
	if s := firstMediaName(c.Question); s != "" {
		return clipLabel("[" + s + "]")
	}
	return "(media card)"
}

// firstMediaName returns the base file name of the first image or audio
// element on a card side, or "" if there is none.
func firstMediaName(media []deck.Media) string {
	for _, m := range media {
		if m.Type == deck.Image || m.Type == deck.Audio {
			return filepath.Base(m.Content)
		}
	}
	return ""
}

// clipLabel truncates a label to a fixed width for the listing.
func clipLabel(s string) string {
	const max = 48
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}
