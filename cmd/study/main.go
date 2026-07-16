// study — a flashcard quiz tool.
//
// Usage: study [flags] <deck-file | pack-directory>
//
// Requires: sxiv or feh (for image decks), mpv or aplay (for audio decks)
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"study/deck"
	"study/gui"
	"study/library"
	"study/media"
	"study/progress"
	"study/quiz"
	"study/session"
)

const helpText = `study — A flashcard quiz tool

usage: study [flags] [deck-file | pack-directory]

a directory is a pack: every *.deck file inside is merged into one session.
with no deck argument, study opens the library: every deck under the
watched directories, each a keystroke from a session.

flags (each overrides the deck header's setting for this session):
  --reverse             flip the deck: see English, produce the target language
  --order <mode>        card order (see # order: below)
  --answer-mode <type|choice>
                        force how every card is answered this session,
                        overriding the deck's answer-mode and per-card
                        settings (incl. distractor-implied choice); progress
                        is shared between modes
  --ahead <N|all>       adaptive order: also review cards due within N days
                        (or all scheduled); a clean early review leaves the
                        card's schedule untouched, a miss still counts
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

library (the decks shelved for long-term study — studying a file never
shelves it; membership is the watched directories):
  --watch <dir>         add a directory to the library: every *.deck file
                        and pack subdirectory inside is a library deck
  --unwatch <dir>       remove a watched directory
  --library             print the library with due counts and exit

deck format:
  plain text, blank lines separate cards.
  --- or === separates question from answer.
  cards default to type-in; a card with ~ distractors is multiple choice
    automatically, or opt in with # answer-mode: choice. An explicit
    # answer-mode: type keeps the deck typed — its ~ answers then only
    serve choice-mode sessions (--answer-mode choice).
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
	answerModeFlag := flag.String("answer-mode", "", "force type or choice answering for every card this session")
	aheadFlag := flag.String("ahead", "", "also review cards due within N days (or all), without advancing their schedule on success")
	timeLimitFlag := flag.String("time-limit", "", "override every per-question time limit (seconds, or none)")
	wrongPauseFlag := flag.String("wrong-pause", "", "override the wrong-answer pause (seconds, or none)")
	previewNew := flag.Bool("preview-new", false, "reveal a never-studied card's answer once before quizzing it")
	newPerSessionFlag := flag.String("new-per-session", "", "override how many never-studied cards enter an adaptive session (N, or all)")
	fontSizeFlag := flag.String("font-size", "", "override the base font size (8-48, or small/medium/large/x-large)")
	audioSpeedFlag := flag.String("audio-speed", "", "override audio playback speed (0.25-4.0)")
	stats := flag.Bool("stats", false, "print saved progress summary for the deck and exit")
	forget := flag.Bool("forget", false, "clear saved progress for this deck")
	watch := flag.String("watch", "", "add a directory of decks to the library")
	unwatch := flag.String("unwatch", "", "remove a watched directory from the library")
	libraryFlag := flag.Bool("library", false, "print the library with due counts and exit")
	help := flag.Bool("help", false, "show help")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, helpText)
	}
	flag.Parse()

	// Library maintenance runs without a deck argument and exits, like --stats
	// and --forget do with one.
	if *watch != "" || *unwatch != "" {
		editRegistry(*watch, *unwatch)
		os.Exit(0)
	}
	if *libraryFlag {
		printLibrary()
		os.Exit(0)
	}

	if *help {
		fmt.Println(helpText)
		os.Exit(0)
	}

	// Bare `study` opens the library screen — the whole shelf, each deck a
	// keystroke from a session. With nothing watched yet there is no library
	// to show; print the help with a pointer at --watch instead.
	if flag.NArg() == 0 {
		reg := openRegistry()
		if reg.Empty() {
			fmt.Println(helpText)
			fmt.Println("\nno deck given and the library is empty — add a deck directory with --watch <dir>")
			os.Exit(0)
		}
		if err := gui.RunLibrary(reg, media.NewViewer()); err != nil {
			fmt.Fprintf(os.Stderr, "✗ %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	deckPath := flag.Arg(0)

	// Parse the deck, flip it under --reverse, and load its progress (warnings
	// go straight to stderr). Flag overrides are applied to the deck below,
	// between Load and Start — that's the seam the session package leaves for
	// per-frontend configuration.
	d, store, err := session.Load(deckPath, *reverse, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}

	// Session overrides. Precedence is built-in default ← deck header ← flag,
	// so a flag always wins. Most flags are session-shaped (order, timing,
	// preview, presentation); settings that change what counts as a correct
	// answer are file-only (answer-case, choice-count) with one deliberate
	// exception: --answer-mode, so a recognition deck can be drilled as
	// production. The card's history is shared between modes — recognition
	// successes are easier evidence than production successes — which is the
	// price of the flag. Applied after Reversed() so they survive the copy.
	if *orderFlag != "" {
		m, ok := deck.ParseOrderMode(*orderFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "✗ --order: unknown mode %q\n", *orderFlag)
			os.Exit(1)
		}
		d.Order = m
	}
	if *answerModeFlag != "" {
		var m deck.QuizMode
		switch strings.TrimSpace(strings.ToLower(*answerModeFlag)) {
		case "type":
			m = deck.ModeType
		case "choice":
			m = deck.ModeChoice
		default:
			fmt.Fprintf(os.Stderr, "✗ --answer-mode: need type or choice (got %q)\n", *answerModeFlag)
			os.Exit(1)
		}
		// Every card follows, outranking per-card directives and the
		// distractor-implied choice inference.
		d.Mode = m
		for i := range d.Cards {
			d.Cards[i].Mode = m
		}
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

	// --ahead composes into the adaptive session; under any other order the
	// whole deck is already in play, so the flag has nothing to add.
	if *aheadFlag != "" && d.Order != deck.OrderAdaptive {
		fmt.Fprintln(os.Stderr, "study: --ahead only applies to the adaptive order; ignored")
	}
	var ahead session.Ahead
	if *aheadFlag != "" {
		days, all, ok := parseAhead(*aheadFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "✗ --ahead: need a day count >= 1, or all (got %q)\n", *aheadFlag)
			os.Exit(1)
		}
		ahead = session.Ahead{Days: days, All: all}
	}

	// Compose the session and build the engine. An empty adaptive session
	// (nothing due, no new cards to serve) is not a dead end: the engine opens
	// in the CaughtUp state and the GUI says so — and offers a full
	// ahead-of-schedule pass, so the user is never prevented from studying.
	engine, err := session.Start(d, store, ahead, time.Now())
	if errors.Is(err, session.ErrNothingWeak) {
		fmt.Printf("✓ nothing to cram — every card in %s is above the weak threshold\n", d.Name)
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}

	// Run GUI.
	if err := gui.Run(engine, viewer, store, *reverse); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
}

// openRegistry loads the library registry from the study data directory,
// exiting on failure (a corrupt registry file should be seen, not silently
// replaced by an empty library).
func openRegistry() *library.Registry {
	dir, err := progress.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
	reg, err := library.Open(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
	return reg
}

// editRegistry applies the library-membership flags (--watch/--unwatch),
// saves, and prints the resulting registry so the state just changed is
// visible without a second command.
func editRegistry(watch, unwatch string) {
	reg := openRegistry()
	apply := func(arg string, op func(string) error) {
		if arg == "" {
			return
		}
		if err := op(arg); err != nil {
			fmt.Fprintf(os.Stderr, "✗ %v\n", err)
			os.Exit(1)
		}
	}
	apply(watch, reg.Watch)
	apply(unwatch, reg.Unwatch)
	if err := reg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
	if reg.Empty() {
		fmt.Println("library is empty — add a deck directory with --watch <dir>")
		return
	}
	for _, d := range reg.Dirs {
		fmt.Printf("watching  %s\n", d)
	}
}

// printLibrary writes the library table to stdout: every shelved deck with
// its due counts and when it was last studied. The CLI twin of the library
// screen, the way --stats mirrors the summary screen.
func printLibrary() {
	reg := openRegistry()
	if reg.Empty() {
		fmt.Println("library is empty — add a deck directory with --watch <dir>")
		return
	}
	now := time.Now()
	for i, g := range reg.Scan() {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("%s:\n", g.Dir)
		if g.Err != nil {
			fmt.Printf("  ✗ %v\n", g.Err)
			continue
		}
		if len(g.Entries) == 0 {
			fmt.Println("  (no decks)")
			continue
		}
		for _, e := range g.Entries {
			fmt.Printf("  %s\n", libraryRow(e, now))
		}
	}
}

// libraryRow renders one deck's line in the --library listing.
func libraryRow(e library.Entry, now time.Time) string {
	name := e.Name
	if e.Pack {
		name += "/"
	}
	info := library.Describe(e.Path, now)
	if info.Err != nil {
		return fmt.Sprintf("%-24s ✗ %v", name, info.Err)
	}
	due := library.DueLabel(info.DueReviews, info.DueNew)
	if info.Reversible {
		due += "  ·  reverse " + library.DueLabel(info.RevReviews, info.RevNew)
	}
	cards := "cards"
	if info.Cards == 1 {
		cards = "card"
	}
	return fmt.Sprintf("%-24s %4d %-6s  %-42s %s",
		name, info.Cards, cards, due, library.AgoLabel(info.LastStudied, now))
}

// parseAhead parses an --ahead value: a whole number of days >= 1, or "all".
func parseAhead(s string) (days int, all bool, ok bool) {
	if strings.TrimSpace(s) == "all" {
		return 0, true, true
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return 0, false, false
	}
	return n, false, true
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
	reviews, fresh, _, nextDue := quiz.SplitDue(d.Cards, store, time.Now())
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
