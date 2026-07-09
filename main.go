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
	"strings"

	"study/deck"
	"study/gui"
	"study/media"
	"study/progress"
	"study/quiz"
)

const helpText = `study — suckless quiz tool

usage: study [flags] <deck-file | pack-directory>

a directory is a pack: every *.deck file inside is merged into one session.

flags:
  --reverse         flip the deck: see English, produce the target language
  --stats           print saved progress summary for the deck and exit
  --forget          clear saved progress for this deck (this direction only;
                    combine with --reverse to clear reverse progress)
  --help            show this help

deck format:
  plain text, blank lines separate cards.
  --- or === separates question from answer.
  cards default to type-in; use # mode: choice for multiple choice.
  a card with no separator but a {{...}} deletion is a fill-in-the-blank
    (cloze) card: the braced text is blanked out and becomes the answer.
  @img <path>       image on question/answer side
  @audio <path>     audio on question/answer side
  = <text>          extra accepted answer (type mode)
  ~ <text>          custom wrong answer (distractor)
  # comment         comment or metadata: # mode: choice|type,
                    # choices: N, # case: sensitive|insensitive,
                    # time: N|none, # order: sequential|shuffled,
                    # font-size: N, # speed: X

examples:
  study japanese.deck
  study study-farsi.deck/          (a pack directory)
  study --stats mahjong.deck
  study --forget vocab.deck`

func main() {
	reverse := flag.Bool("reverse", false, "flip the deck: see English, produce the target language")
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

	// Order cards for the session. Unless the deck opts into sequential order
	// (# order: sequential), shuffle first so the deck's authored order isn't
	// a memorization crutch; then PrioritizeCards (a stable sort) lifts weak
	// cards to the front while equal-confidence cards keep their now-randomized
	// relative order. The engine must not re-shuffle afterwards, or it would
	// throw this ordering away — which is exactly what used to happen in the
	// default mode.
	if !d.Sequential {
		rand.Shuffle(len(d.Cards), func(i, j int) {
			d.Cards[i], d.Cards[j] = d.Cards[j], d.Cards[i]
		})
	}
	d.Cards = store.PrioritizeCards(d.Cards)

	engine := quiz.NewEngine(d, store)

	// Run GUI.
	if err := gui.Run(engine, viewer, store, *reverse); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
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

	if len(studied) == 0 {
		fmt.Println("  Studied          0")
		fmt.Println("\n  No progress recorded yet for this deck.")
		return
	}

	pct := float64(len(studied)) / float64(len(d.Cards)) * 100
	fmt.Printf("  Studied          %d  (%.0f%%)\n", len(studied), pct)
	fmt.Printf("  Mastered         %d\n", mastered)

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
	if s := joinText(c.Question); s != "" {
		return clipLabel(s)
	}
	if s := joinText(c.Answer); s != "" {
		return clipLabel("→ " + s)
	}
	if s := firstMediaName(c.Question); s != "" {
		return clipLabel("[" + s + "]")
	}
	return "(media card)"
}

// joinText collapses the text segments of a card side into a single
// whitespace-normalized line, ignoring image and audio elements.
func joinText(media []deck.Media) string {
	var parts []string
	for _, m := range media {
		if m.Type == deck.Text && m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
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
