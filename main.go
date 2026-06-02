// study — a suckless quiz tool for flashcard-style learning.
//
// Usage: study [flags] <deck-file>
//
// Requires: sxiv or feh (for image decks), mpv or aplay (for audio decks)
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"study/deck"
	"study/gui"
	"study/media"
	"study/progress"
	"study/quiz"
)

const helpText = `study — suckless quiz tool

usage: study [flags] <deck-file>

flags:
  --choices N       number of answer choices (overrides deck header)
  --time N          per-question time limit in seconds, 0 to disable
                    (overrides deck header)
  --sequential      present cards in deck order (default: shuffled)
  --stats           print saved progress summary for the deck and exit
  --reset           clear progress for this deck
  --help            show this help

deck format:
  plain text, blank lines separate cards.
  --- separates question from answer.
  @img <path>       image on question/answer side
  @audio <path>     audio on question/answer side
  ~ <text>          custom wrong answer (distractor)
  # comment         comment or metadata (# choices: N, # time: N)

examples:
  study japanese.deck
  study --choices 3 mahjong.deck
  study --time 10 --sequential vocab.deck`

func main() {
	choices := flag.Int("choices", 0, "number of answer choices (overrides deck header)")
	timeLimit := flag.Int("time", -1, "per-question time limit in seconds, 0 to disable (overrides deck header)")
	sequential := flag.Bool("sequential", false, "present cards in deck order")
	stats := flag.Bool("stats", false, "print saved progress summary for the deck and exit")
	reset := flag.Bool("reset", false, "clear progress for this deck")
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

	// Check media viewers (audio only — images rendered in GUI).
	viewer := media.NewViewer()

	// Load progress.
	store, err := progress.NewStore(d.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ progress: %v\n", err)
		os.Exit(1)
	}

	// --stats: print the saved summary for this deck and exit without
	// entering the quiz. This is a read-only view of the same numbers the
	// in-app "Session Complete" screen shows.
	if *stats {
		printStats(d, store)
		os.Exit(0)
	}

	if *reset {
		store.Reset()
		if err := store.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "✗ saving reset: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ progress reset for", d.Name)
	}

	// A --time flag overrides the deck's global time limit (per-card
	// overrides in the deck still apply). -1 means the flag was not set.
	if *timeLimit >= 0 {
		d.TimeLimit = *timeLimit
	}

	// Prioritize cards based on past performance.
	cards := store.PrioritizeCards(d.Cards)
	d.Cards = cards

	// Create engine.
	shuffle := !*sequential
	engine := quiz.NewEngine(d, shuffle, *choices, store)

	// Run GUI.
	if err := gui.Run(engine, viewer, store); err != nil {
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
		question string
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
			question: questionPreview(c),
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
		fmt.Printf("    %3.0f%% acc  conf %3.0f   %s\n", r.accuracy, r.conf, r.question)
	}
}

// questionPreview returns a single-line, length-capped preview of a card's
// question text for use in the stats listing.
func questionPreview(c *deck.Card) string {
	var parts []string
	for _, m := range c.Question {
		if m.Type == deck.Text && m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	s := strings.Join(parts, " ")
	if s == "" {
		s = "(media card)"
	}
	const max = 48
	if r := []rune(s); len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return s
}
