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
	"sort"
	"strconv"
	"strings"
	"time"

	"study/deck"
	"study/gui"
	"study/library"
	"study/media"
	"study/progress"
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
  --calibrate           print measured recall rates from the review log (by
                        ladder rung, card state, and answer mode) and exit
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
	calibrate := flag.Bool("calibrate", false, "print recall rates by rung, state, and answer mode from the review log and exit")
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
		// A deck-scoped flag with no deck to act on is a mistake, not a
		// request for the library; refuse it rather than silently ignore it.
		var set []string
		flag.Visit(func(f *flag.Flag) { set = append(set, f.Name) })
		if stray := strayDeckFlags(set); len(stray) > 0 {
			for _, name := range stray {
				fmt.Fprintf(os.Stderr, "✗ --%s needs a deck argument\n", name)
			}
			os.Exit(1)
		}
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
		d.ForceAnswerMode(m)
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

	// --calibrate: aggregate the review log into recall rates and exit. This
	// is the ladder's report card — per-rung recall is its measured forgetting
	// curve — and the adjudicator for any future scheduler change.
	if *calibrate {
		printCalibrate(d, store)
		os.Exit(0)
	}

	// --forget clears saved progress and exits without launching the quiz,
	// mirroring --stats. It's a maintenance action, not the start of a study
	// session. Only the direction being studied is cleared: plain --forget
	// resets forward progress, --forget --reverse resets reverse progress —
	// so forgetting one skill doesn't destroy the other's history.
	if *forget {
		if progress.PackMemberOf(d.Path) != "" {
			// A member shares the pack's store: clear this deck's cards in
			// this direction (their IDs already carry the prefix), not every
			// sibling's history in the same file.
			ids := make([]string, 0, len(d.Cards))
			for i := range d.Cards {
				ids = append(ids, d.Cards[i].ID)
			}
			store.ResetIDs(ids)
		} else {
			store.ResetDirection(*reverse)
		}
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

// deckScopedFlags names every flag that configures or acts on a single deck
// and so is meaningless without a deck argument. The rest — library
// maintenance and --help — stand alone. Kept in sync with the definitions in
// main.
var deckScopedFlags = map[string]bool{
	"reverse":         true,
	"order":           true,
	"answer-mode":     true,
	"ahead":           true,
	"time-limit":      true,
	"wrong-pause":     true,
	"preview-new":     true,
	"new-per-session": true,
	"font-size":       true,
	"audio-speed":     true,
	"stats":           true,
	"calibrate":       true,
	"forget":          true,
}

// strayDeckFlags filters the flag names actually set on the command line down
// to the deck-scoped ones — the flags that need a deck argument to mean
// anything.
func strayDeckFlags(set []string) []string {
	var stray []string
	for _, name := range set {
		if deckScopedFlags[name] {
			stray = append(stray, name)
		}
	}
	return stray
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
	var bits []string
	for _, p := range library.DirParts(info.DueReviews, info.DueNew, info.Cards) {
		bits = append(bits, p.Text)
	}
	for i, p := range library.DirParts(info.RevReviews, info.RevNew, info.RevCards) {
		text := p.Text
		if i == 0 {
			text = "reversed " + text
		}
		bits = append(bits, text)
	}
	cards := "cards"
	if info.Cards == 1 {
		cards = "card"
	}
	return fmt.Sprintf("%-24s %4d %-6s  %-42s %s",
		name, info.Cards, cards, strings.Join(bits, " · "), library.AgoLabel(info.LastStudied, now))
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

// printStats writes a plain-text progress summary for the deck to stdout —
// the same numbers the library's stats screen shows (library.StatsOf).
func printStats(d *deck.Deck, store *progress.Store) {
	info := library.StatsOf(d, store, time.Now())

	fmt.Printf("study — %s\n\n", d.Name)
	fmt.Printf("  Cards in deck    %d\n", info.Cards)

	dueLine := fmt.Sprintf("  Due now          %d reviews + %d new\n", info.DueReviews, info.DueNew)

	if info.Studied == 0 {
		fmt.Println("  Studied          0")
		fmt.Print(dueLine)
		fmt.Println("\n  No progress recorded yet for this deck.")
		return
	}

	pct := float64(info.Studied) / float64(info.Cards) * 100
	fmt.Printf("  Studied          %d  (%.0f%%)\n", info.Studied, pct)
	fmt.Printf("  Mastered         %d\n", info.Mastered)
	fmt.Print(dueLine)
	if !info.NextDue.IsZero() {
		fmt.Printf("  Next review      %s\n", info.NextDue.Local().Format("Mon Jan 2 15:04"))
	}

	fmt.Printf("\n  All-time\n")
	fmt.Printf("    Correct        %d\n", info.Correct)
	fmt.Printf("    Wrong          %d\n", info.Wrong)
	fmt.Printf("    Accuracy       %.0f%%\n", info.Accuracy())

	// Weakest first, so the things worth reviewing are at the top; only cards
	// below the weak threshold qualify at all.
	if len(info.Weakest) == 0 {
		fmt.Println("\n  No weak cards — everything studied is above the weak threshold.")
		return
	}
	fmt.Printf("\n  Weak cards\n")
	for _, r := range info.Weakest {
		fmt.Printf("    %3.0f%% acc  conf %3.0f   %s\n", r.Accuracy, r.Confidence, r.Label)
	}
}

// printCalibrate writes the review log's recall rates for the deck (in the
// direction being studied) to stdout: per-rung recall is the review ladder's
// measured forgetting curve. A rung recalling above ~95% is scheduled too
// short, below ~80% too long — the bands SCHEDULING.md item 9 set out.
func printCalibrate(d *deck.Deck, store *progress.Store) {
	events, err := store.ReadLog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ reading review log: %v\n", err)
		os.Exit(1)
	}
	ids := make(map[string]bool, len(d.Cards))
	for i := range d.Cards {
		ids[d.Cards[i].ID] = true
	}
	cal := progress.Calibrate(events, ids)

	fmt.Printf("study — %s (calibration)\n\n", d.Name)
	if cal.Events == 0 {
		fmt.Println("  No logged answers yet for this deck.")
		return
	}
	fmt.Printf("  Logged answers   %d\n", cal.Events)
	fmt.Printf("  Review asks      %d  (first ask of a due card in a session)\n", cal.Reviews)

	if cal.Reviews > 0 {
		fmt.Printf("\n  Recall by rung — the ladder's measured forgetting curve\n")
		fmt.Printf("    rung  interval  asks  recall\n")
		rungs := make([]int, 0, len(cal.Rungs))
		for r := range cal.Rungs {
			rungs = append(rungs, r)
		}
		sort.Ints(rungs)
		for _, r := range rungs {
			b := cal.Rungs[r]
			fmt.Printf("    %4d  %7s  %4d  %5.0f%%\n", r, fmt.Sprintf("%dd", progress.LadderDays(r)), b.Asks, b.Recall())
		}
		fmt.Println("    above ~95%: the rung is too short; below ~80%: too long")
	}

	fmt.Printf("\n  Recall by state — every graded answer\n")
	for _, s := range []string{"new", "learning", "retry", "review", "ahead"} {
		if b := cal.States[s]; b != nil {
			fmt.Printf("    %-9s %4d  %5.0f%%\n", s, b.Asks, b.Recall())
		}
	}

	if len(cal.Modes) > 0 {
		fmt.Printf("\n  Recall by answer mode — review asks only\n")
		for _, m := range []string{"type", "choice"} {
			if b := cal.Modes[m]; b != nil {
				fmt.Printf("    %-9s %4d  %5.0f%%\n", m, b.Asks, b.Recall())
			}
		}
	}
}
