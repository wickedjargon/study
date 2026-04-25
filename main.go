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
  --sequential      present cards in deck order (default: shuffled)
  --reset           clear progress for this deck
  --help            show this help

deck format:
  plain text, blank lines separate cards.
  --- separates question from answer.
  @img <path>       image on question/answer side
  @audio <path>     audio on question/answer side
  ~ <text>          custom wrong answer (distractor)
  # comment         comment or metadata (# choices: N)

examples:
  study japanese.deck
  study --choices 3 mahjong.deck
  study --sequential --reset vocab.deck`

func main() {
	choices := flag.Int("choices", 0, "number of answer choices (overrides deck header)")
	sequential := flag.Bool("sequential", false, "present cards in deck order")
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

	if *reset {
		store.Reset()
		if err := store.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "✗ saving reset: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ progress reset for", d.Name)
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
