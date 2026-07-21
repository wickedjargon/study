// Package deck parses plain-text deck files into structured card data.
//
// Deck format (sent-inspired):
//
//	# Comment or metadata (# choice-count: N)
//	@img path/to/image.png
//	@audio path/to/audio.mp3
//	Question text
//	= Alternative question wording (accepted in --reverse; never displayed)
//	---                        (or === — both separate question from answer)
//	Answer text
//	= Alternative accepted answer
//	~ Optional custom distractor
//
//	(blank line separates cards)
package deck

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// MediaType identifies the kind of media on a card side.
type MediaType int

const (
	Text MediaType = iota
	Image
	Audio
)

// Media represents a single element on a card side.
type Media struct {
	Type    MediaType
	Content string // text content or absolute file path
}

// SetItem is one required entry of a set-answer card: its canonical text
// plus the accepted variants authored as "=" lines directly under it.
type SetItem struct {
	Text   string
	Accept []string
}

// Card represents a single question/answer pair.
type Card struct {
	ID string // stable hash of the question's text lines
	// LegacyIDs are the hashes older versions of the parser produced for this
	// card, newest first (the undelimited text-line hash, and before that the
	// hash that included @img/@audio lines). Only hashes that differ from ID
	// are kept; used once at load time to migrate saved progress, never for
	// new writes.
	LegacyIDs  []string
	Question   []Media  // question side elements
	Answer     []Media  // answer side elements
	AnswerText string   // plain text of the answer (for choice generation)
	Accept     []string // extra answers accepted in type mode ("= " lines)
	// QuestionAccept holds "= " lines authored on the question side: alternative
	// wordings of the prompt. They are never displayed and play no role in a
	// forward session (the question isn't typed); a reversed card folds them into
	// its Accept list, so a learner producing the target language can answer with
	// any authored variant.
	QuestionAccept []string
	Distractors    []string // optional custom wrong answers
	// Note is the card's optional third section (question --- answer ---
	// note): explanation shown only where the answer is visible — the result
	// screen, the first-viewing reveal, and flip-through — never the question
	// screen, so it can't help with the answer. Text and images only; audio
	// is rejected at parse until its playback ordering is designed. Excluded
	// from the card ID, so annotating a card never orphans its progress.
	Note []Media
	// SetItems makes this a set-answer card ("+ " lines in the answer
	// section): the user enumerates the items, any order, one entry at a
	// time. Quota is how many distinct items complete the card (0 = all of
	// them — "name the countries" vs "name five countries"). Attempts caps
	// the counted entries (hits and wrong guesses — misspells and
	// duplicates are free): 0 = unlimited, otherwise the card ends as a
	// miss when the target can no longer be reached. Set cards are
	// type-mode only and don't reverse.
	SetItems  []SetItem
	Quota     int
	Attempts  int
	Mode      QuizMode // per-card mode (choice or type)
	Choices   int      // per-card choice count (0 = use deck default)
	TimeLimit int      // per-card time limit in seconds
	// (0 = inherit deck default, -1 = explicitly unlimited)
	Cloze bool // fill-in-the-blank card ({{...}} deletion in the question text)
}

// IsSet reports whether this is a set-answer card (an enumeration, not a
// single value).
func (c *Card) IsSet() bool { return len(c.SetItems) > 0 }

// SetTarget returns how many distinct items complete a set card: its quota,
// or every item when no quota is authored.
func (c *Card) SetTarget() int {
	if c.Quota > 0 {
		return c.Quota
	}
	return len(c.SetItems)
}

// EffectiveTimeLimit returns the time limit in seconds that applies to this
// card, given the deck's global limit. A return of 0 means no limit.
func (c *Card) EffectiveTimeLimit(deckLimit int) int {
	if c.TimeLimit < 0 {
		return 0 // explicitly unlimited
	}
	if c.TimeLimit > 0 {
		return c.TimeLimit // per-card override
	}
	return deckLimit // inherit deck default
}

// QuizMode determines how answers are submitted.
type QuizMode int

const (
	ModeChoice QuizMode = iota // multiple choice
	ModeType                   // user types the answer (default)
)

// Deck represents a parsed deck file.
type Deck struct {
	Name          string
	Path          string    // absolute path to deck file
	Choices       int       // number of answer choices (default 4)
	Mode          QuizMode  // choice or type
	// ModeSet records that the deck header declared its answer-mode
	// explicitly. Distractor-implied choice then stays off: a typed deck may
	// author "~" wrong answers purely for choice-mode sessions.
	ModeSet bool
	CaseSensitive bool      // case-sensitive matching for type mode
	TimeLimit     int       // global per-question time limit in seconds (0 = none)
	Order         OrderMode // session ordering mode ("# order:", default adaptive)
	// NewPerSession caps how many never-studied cards enter one adaptive
	// session (default defaultNewPerSession; -1 = unlimited). Introducing new
	// material in bounded batches keeps the 3-recall learning criterion
	// tractable on large decks.
	NewPerSession int
	// WrongPause is how many seconds the result screen of a wrong answer
	// refuses to advance, so a reflexive enter can't skip past the miss
	// (default defaultWrongPause; 0 = no pause).
	WrongPause int
	Preview bool // reveal a never-studied card's answer once before quizzing it
	// PreviewSet records that the deck header declared preview-new
	// explicitly, so a frontend can tell the author's "off" from the absent
	// default — the web seeds a guest's introduction preference from it.
	PreviewSet bool
	FontSize   int     // base font size in points (0 = use the app default)
	Speed      float64 // audio playback speed multiplier (0 = use the app default of 1.0)
	// ImgTint marks the deck's images as monochrome alpha masks ("# img-tint:
	// fg"): frontends recolor them to the theme's foreground so one image set
	// works in both light and dark mode. Deck-level rather than per-card
	// because image-only cards hash their @img lines into the card ID — a
	// per-card flag there would orphan progress.
	ImgTint bool
	Cards      []Card

	// Warnings collects non-fatal parse issues — directives whose value was
	// malformed or out of range and therefore ignored. The caller prints these
	// so a typo'd directive isn't silently dropped. (A fatal problem, e.g. a
	// card with no answer, is returned as an error instead.)
	Warnings []string
}

// OrderMode selects what a session serves and how it schedules the cards.
// See the README's ordering table for the full behavior of each.
type OrderMode int

const (
	// OrderAdaptive is the default: an evidence-based review session — cards
	// due for review (most overdue first) plus a bounded batch of new cards,
	// each studied to its session criterion with spaced repetitions, the next
	// review scheduled days out on completion.
	OrderAdaptive OrderMode = iota
	// OrderSequential cycles the deck in authored order forever; the order
	// never changes (misses drill immediately). A rote tool for material
	// where the sequence is the content — verse, digits, procedures.
	OrderSequential
	// OrderFlipThrough shows each card with its answer visible, in authored
	// order, wrapping at the end. No quizzing, nothing recorded.
	OrderFlipThrough
	// OrderWeakOnly restricts the session to weak/never-studied cards (cram
	// mode, ignoring due dates); criterion scheduling within the session.
	OrderWeakOnly
)

// ParseOrderMode resolves an order-mode name — a "# order:" directive value or
// a --order flag value — to its mode. The bool reports whether the name is
// known.
func ParseOrderMode(v string) (OrderMode, bool) {
	m, ok := orderModes[v]
	return m, ok
}

// orderModes maps order-mode names to their modes.
var orderModes = map[string]OrderMode{
	"adaptive":     OrderAdaptive,
	"sequential":   OrderSequential,
	"flip-through": OrderFlipThrough,
	"weak-only":    OrderWeakOnly,
}

// String returns the mode's user-facing name — the same word the "# order:"
// header and --order flag accept.
func (m OrderMode) String() string {
	for name, mode := range orderModes {
		if mode == m {
			return name
		}
	}
	return "unknown"
}

// defaultNewPerSession is the default cap on never-studied cards per adaptive
// session. Ten new cards is ~30 spaced recalls — a realistic daily bite that
// spreads a large deck across days, which is where the retention gains of
// distributed practice actually come from (successive relearning was studied
// as short sessions across days, not one long grind).
const defaultNewPerSession = 10

// defaultWrongPause is the default wrong-answer pause in seconds.
const defaultWrongPause = 5

// ParseNewPerSession parses a "# new-per-session:" directive or
// --new-per-session flag value: a non-negative integer, or "all" for no cap
// (returned as -1). The bool reports whether the value was understood.
func ParseNewPerSession(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "all") {
		return -1, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// warn records a non-fatal parse issue on the deck.
func (d *Deck) warn(format string, args ...any) {
	d.Warnings = append(d.Warnings, fmt.Sprintf(format, args...))
}

// ForceAnswerMode makes every card in the deck answer as m for this session,
// outranking the deck's "# answer-mode:", per-card directives, and the
// distractor-implied choice inference. The --answer-mode flag and the library
// screen's typed/choice launches both apply it — so a recognition deck can be
// drilled as production and vice versa. The card's history is shared between
// modes (recognition successes are easier evidence than production ones),
// which is the price of forcing.
func (d *Deck) ForceAnswerMode(m QuizMode) {
	d.Mode = m
	for i := range d.Cards {
		// Set cards have no choice presentation; they stay typed even when
		// the session forces choice.
		if m == ModeChoice && d.Cards[i].IsSet() {
			continue
		}
		d.Cards[i].Mode = m
	}
}

// maxTimeLimit caps a per-question time limit (in seconds). A value above this
// is almost certainly a typo (a question timer measured in hours makes no
// sense), so it's rejected with a warning rather than honored.
const maxTimeLimit = 3600

// maxChoices caps a choice card's option count: options are picked with the
// number keys, which run out at 9. A count above that leaves options no key
// can reach, so it's rejected with a warning rather than honored.
const maxChoices = 9

// clozeBlank is what a {{...}} deletion is replaced with in the displayed
// question text.
const clozeBlank = "____"

// Parse reads a deck file and returns a structured Deck. A directory is a
// pack: every *.deck file inside (sorted by name) is parsed and merged into
// one combined deck (see parseDir).
func Parse(path string) (*Deck, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	if info, err := os.Stat(absPath); err == nil && info.IsDir() {
		return parseDir(absPath)
	}

	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("opening deck: %w", err)
	}
	defer f.Close()

	dir := filepath.Dir(absPath)
	deck := &Deck{
		Name:          deckName(absPath),
		Path:          absPath,
		Choices:       4,
		Mode:          ModeType, // default to active recall; # answer-mode: choice opts in
		NewPerSession: defaultNewPerSession,
		WrongPause:    defaultWrongPause,
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading deck: %w", err)
	}

	// Split lines into card blocks separated by blank lines.
	blocks := splitBlocks(lines)

	// Deck-level metadata lives in the leading comment-only blocks (those before
	// the first card). Scanning by block rather than by contiguous line means a
	// blank line between header directives no longer silently drops the rest, and
	// it cleanly separates deck-level directives from a card's own per-card
	// directives, which share the card's block (and are read in parseCard).
	for _, block := range blocks {
		if !isCommentOnly(block) {
			break
		}
		applyDeckMetadata(deck, block)
	}

	header := true
	for _, block := range blocks {
		// A comment-only block past the header is just a comment, but a deck
		// directive inside one is being silently ignored — say so.
		if isCommentOnly(block) {
			if !header {
				for _, line := range block {
					warnMisplacedDirective(strings.TrimSpace(line), false, deck.warn)
				}
			}
			continue
		}
		header = false
		card, err := parseCard(block, dir, deck.Mode, deck.ModeSet, deck.warn)
		if err != nil {
			return nil, err
		}
		if card != nil {
			deck.Cards = append(deck.Cards, *card)
		}
	}

	if len(deck.Cards) == 0 {
		return nil, fmt.Errorf("deck has no cards")
	}

	return deck, nil
}

// parseDir parses a pack: a directory containing *.deck files. The files are
// parsed individually (each resolves its own media paths and per-card
// directives) and their cards concatenated in sorted-filename order. Deck-level
// settings come from the first file; a later file that sets a conflicting value
// gets a warning rather than silently changing the session's behavior halfway
// through the card list. Cards whose ID already appeared are skipped — packs
// deliberately reuse a phrase across their decks, and one combined session
// shouldn't drill the same card twice under one progress entry.
func parseDir(absDir string) (*Deck, error) {
	entries, err := filepath.Glob(filepath.Join(absDir, "*.deck"))
	if err != nil {
		return nil, fmt.Errorf("scanning pack: %w", err)
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		return nil, fmt.Errorf("no .deck files in %s", absDir)
	}

	var merged *Deck
	seen := make(map[string]bool)
	for _, path := range entries {
		d, err := Parse(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		if merged == nil {
			merged = d
			merged.Name = deckName(absDir)
			merged.Path = absDir
			for i := range merged.Cards {
				seen[merged.Cards[i].ID] = true
			}
			continue
		}
		if d.Mode != merged.Mode || d.CaseSensitive != merged.CaseSensitive ||
			d.TimeLimit != merged.TimeLimit || d.Order != merged.Order ||
			d.Preview != merged.Preview || d.PreviewSet != merged.PreviewSet ||
			d.NewPerSession != merged.NewPerSession ||
			d.WrongPause != merged.WrongPause || d.ImgTint != merged.ImgTint ||
			d.Choices != merged.Choices || d.FontSize != merged.FontSize ||
			d.Speed != merged.Speed {
			merged.warn("%s: header settings differ from %s; using the first file's",
				filepath.Base(path), filepath.Base(entries[0]))
		}
		merged.Warnings = append(merged.Warnings, d.Warnings...)
		for i := range d.Cards {
			if seen[d.Cards[i].ID] {
				continue
			}
			seen[d.Cards[i].ID] = true
			merged.Cards = append(merged.Cards, d.Cards[i])
		}
	}
	return merged, nil
}

// isCommentOnly reports whether every line in a block is a comment (blocks from
// splitBlocks never contain blank lines, so this identifies a metadata-only
// block that precedes any card content).
func isCommentOnly(block []string) bool {
	for _, line := range block {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			return false
		}
	}
	return true
}

// legacyDirectives maps removed directive names to their replacements. The old
// names are no longer honored; recognizing them purely to warn means an old
// deck fails loudly with the fix spelled out, instead of silently running on
// defaults because its directives became plain comments.
var legacyDirectives = map[string]string{
	"mode":    "answer-mode",
	"choices": "choice-count",
	"case":    "answer-case",
	"time":    "time-limit",
	"preview": "preview-new",
	"speed":   "audio-speed",
}

// warnLegacyDirective emits a warning if the line uses a removed directive
// name, naming its replacement.
func warnLegacyDirective(line string, warn func(string, ...any)) {
	for old, replacement := range legacyDirectives {
		if _, ok := strings.CutPrefix(line, "# "+old+":"); ok {
			warn("ignoring %q (directive renamed: use # %s:)", line, replacement)
			return
		}
	}
}

// deckDirectives lists every deck-header directive, and perCardDirectives the
// subset also honored inside a card's block. Used to warn when a directive
// lands where it takes no effect — a correctly spelled directive in the wrong
// place must not be quieter than a typo'd one.
var deckDirectives = []string{
	"choice-count", "answer-mode", "answer-case", "time-limit", "order",
	"wrong-pause", "new-per-session", "preview-new", "font-size", "img-tint",
	"audio-speed",
}

var perCardDirectives = map[string]bool{
	"answer-mode":  true,
	"choice-count": true,
	"time-limit":   true,
}

// warnMisplacedDirective emits a warning if the line is a deck directive that
// is being ignored: any of them in a comment block after the first card
// (inCard=false), or a deck-only one inside a card's block (inCard=true).
func warnMisplacedDirective(line string, inCard bool, warn func(string, ...any)) {
	for _, name := range deckDirectives {
		if _, ok := strings.CutPrefix(line, "# "+name+":"); !ok {
			continue
		}
		if inCard && perCardDirectives[name] {
			return
		}
		warn("ignoring %q (# %s: is a deck header directive; it belongs before the first card)", line, name)
		return
	}
}

// applyDeckMetadata reads deck-level directives from a comment-only block and
// applies them to the deck.
func applyDeckMetadata(deck *Deck, block []string) {
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		warnLegacyDirective(trimmed, deck.warn)
		if after, ok := strings.CutPrefix(trimmed, "# choice-count:"); ok {
			v := strings.TrimSpace(after)
			if n, err := strconv.Atoi(v); err == nil && n >= 2 && n <= maxChoices {
				deck.Choices = n
			} else {
				deck.warn("ignoring %q (# choice-count: needs an integer 2-%d)", trimmed, maxChoices)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# answer-mode:"); ok {
			switch strings.TrimSpace(after) {
			case "type":
				deck.Mode = ModeType
				deck.ModeSet = true
			case "choice":
				deck.Mode = ModeChoice
				deck.ModeSet = true
			default:
				deck.warn("ignoring %q (# answer-mode: must be type or choice)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# answer-case:"); ok {
			switch strings.TrimSpace(after) {
			case "sensitive":
				deck.CaseSensitive = true
			case "insensitive":
				deck.CaseSensitive = false
			default:
				deck.warn("ignoring %q (# answer-case: must be sensitive or insensitive)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# time-limit:"); ok {
			if n, ok := ParseTimeLimit(after); ok {
				if n > 0 {
					deck.TimeLimit = n
				}
			} else {
				deck.warn("ignoring %q (# time-limit: needs 0-%d seconds, or none)", trimmed, maxTimeLimit)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# order:"); ok {
			if m, ok := ParseOrderMode(strings.TrimSpace(after)); ok {
				deck.Order = m
			} else {
				deck.warn("ignoring %q (# order: must be adaptive, sequential, flip-through, or weak-only)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# wrong-pause:"); ok {
			if n, ok := ParseTimeLimit(after); ok {
				deck.WrongPause = n
			} else {
				deck.warn("ignoring %q (# wrong-pause: needs 0-%d seconds, or none)", trimmed, maxTimeLimit)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# new-per-session:"); ok {
			if n, ok := ParseNewPerSession(after); ok {
				deck.NewPerSession = n
			} else {
				deck.warn("ignoring %q (# new-per-session: needs an integer >= 0, or all)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# preview-new:"); ok {
			switch strings.TrimSpace(after) {
			case "on":
				deck.Preview = true
				deck.PreviewSet = true
			case "off":
				deck.Preview = false
				deck.PreviewSet = true
			default:
				deck.warn("ignoring %q (# preview-new: must be on or off)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# font-size:"); ok {
			if n, ok := ParseFontSize(after); ok {
				deck.FontSize = n
			} else {
				deck.warn("ignoring %q (# font-size: needs 8-48, or small/medium/large/x-large)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# img-tint:"); ok {
			switch strings.TrimSpace(after) {
			case "fg":
				deck.ImgTint = true
			case "off":
				deck.ImgTint = false
			default:
				deck.warn("ignoring %q (# img-tint: must be fg or off)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# audio-speed:"); ok {
			if x, ok := ParseSpeed(after); ok {
				deck.Speed = x
			} else {
				deck.warn("ignoring %q (# audio-speed: needs 0.25-4.0)", trimmed)
			}
		}
	}
}

// splitBlocks splits lines into groups separated by blank lines,
// filtering out comment-only blocks.
func splitBlocks(lines []string) [][]string {
	var blocks [][]string
	var current []string

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				blocks = append(blocks, current)
				current = nil
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}

	return blocks
}

// parseCard parses a block of lines into a Card. Returns nil for comment-only
// blocks. warn records non-fatal issues (e.g. a malformed per-card directive).
func parseCard(block []string, baseDir string, defaultMode QuizMode, deckModeSet bool, warn func(string, ...any)) (*Card, error) {
	// A comment-only block carries no card. Leading ones are deck metadata
	// (handled in applyDeckMetadata); any later one is just a comment. Return
	// early so the per-card directive scan below doesn't double-report a bad
	// value the deck-level pass already warned about.
	if isCommentOnly(block) {
		return nil, nil
	}

	// Check for per-card metadata before filtering comments. modeSet tracks an
	// explicit per-card answer-mode, which outranks the distractor inference
	// below.
	cardMode := defaultMode
	modeSet := false
	cardChoices := 0
	cardTime := 0
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		warnLegacyDirective(trimmed, warn)
		warnMisplacedDirective(trimmed, true, warn)
		if after, ok := strings.CutPrefix(trimmed, "# answer-mode:"); ok {
			switch strings.TrimSpace(after) {
			case "type":
				cardMode = ModeType
				modeSet = true
			case "choice":
				cardMode = ModeChoice
				modeSet = true
			default:
				warn("ignoring %q (# answer-mode: must be type or choice)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# choice-count:"); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(after)); err == nil && n >= 2 && n <= maxChoices {
				cardChoices = n
			} else {
				warn("ignoring %q (# choice-count: needs an integer 2-%d)", trimmed, maxChoices)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# time-limit:"); ok {
			if n, ok := ParseTimeLimit(after); ok {
				if n <= 0 {
					cardTime = -1 // explicitly unlimited
				} else {
					cardTime = n
				}
			} else {
				warn("ignoring %q (# time-limit: needs 0-%d seconds, or none)", trimmed, maxTimeLimit)
			}
		}
	}

	// Filter out comment lines.
	var filtered []string
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			filtered = append(filtered, line)
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}

	// Split on the separators (--- or ===): question, answer, and an
	// optional third section — the card's note.
	sepIdx := -1
	var noteLines []string
	for i, line := range filtered {
		if isSeparator(line) {
			sepIdx = i
			break
		}
	}
	if sepIdx != -1 {
		rest := filtered[sepIdx+1:]
		for i, line := range rest {
			if isSeparator(line) {
				for _, l := range rest[i+1:] {
					if isSeparator(l) {
						return nil, fmt.Errorf("card has too many sections (at most question --- answer --- note): %q", strings.Join(filtered, " / "))
					}
				}
				noteLines = rest[i+1:]
				filtered = filtered[:sepIdx+1+i]
				break
			}
		}
	}

	// A {{...}} deletion before the separator makes the block a cloze card
	// with a note: the deletions are the answer, so the only thing a second
	// section can be is the note. A third section has no meaning then, and an
	// empty note means a stray separator — both stay loud.
	if sepIdx != -1 && hasClozeDeletion(filtered[:sepIdx]) {
		if noteLines != nil {
			return nil, fmt.Errorf("cloze card has too many sections (at most cloze --- note): %q", strings.Join(filtered, " / "))
		}
		if len(filtered) == sepIdx+1 {
			return nil, fmt.Errorf("cloze card has an empty note section (drop the ---): %q", strings.Join(filtered, " / "))
		}
		card, _, err := parseClozeCard(filtered[:sepIdx], baseDir, cardMode, cardChoices, cardTime, warn)
		if err != nil {
			return nil, err
		}
		card.Note = parseNote(filtered[sepIdx+1:], filtered[:sepIdx], baseDir, warn)
		if !modeSet && !deckModeSet && len(card.Distractors) > 0 {
			card.Mode = ModeChoice // distractors imply choice (see below)
		}
		return card, nil
	}

	// No separator: a cloze card (fill-in-the-blank) is allowed, where the
	// answer comes from a {{...}} deletion in the text instead of a separate
	// answer side. Anything else without a separator is malformed.
	if sepIdx == -1 {
		if card, ok, err := parseClozeCard(filtered, baseDir, cardMode, cardChoices, cardTime, warn); ok || err != nil {
			if card != nil && !modeSet && !deckModeSet && len(card.Distractors) > 0 {
				card.Mode = ModeChoice // distractors imply choice (see below)
			}
			return card, err
		}
		return nil, fmt.Errorf("card missing --- or === separator: %q", strings.Join(filtered, " / "))
	}
	if sepIdx == 0 {
		return nil, fmt.Errorf("card has no question (separator at start): %q", strings.Join(filtered, " / "))
	}

	questionLines := filtered[:sepIdx]
	afterSep := filtered[sepIdx+1:]

	// "=" lines on the question side are alternative wordings of the prompt,
	// accepted when the card is reversed (where the question becomes the answer
	// to type). They're stripped here so they neither display nor participate in
	// the card ID — adding one to an existing card must not orphan its progress.
	var qAccepts, qLines []string
	for _, line := range questionLines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "= "):
			qAccepts = append(qAccepts, strings.TrimPrefix(trimmed, "= "))
		case strings.HasPrefix(trimmed, "=") && len(trimmed) > 1:
			qAccepts = append(qAccepts, strings.TrimSpace(trimmed[1:]))
		default:
			qLines = append(qLines, line)
		}
	}
	questionLines = qLines
	if len(questionLines) == 0 {
		return nil, fmt.Errorf("card question has only = alternatives, no prompt: %q", strings.Join(filtered, " / "))
	}

	if len(afterSep) == 0 {
		return nil, fmt.Errorf("card has no answer (nothing after separator): %q", strings.Join(filtered, " / "))
	}

	// Separate the answer lines from distractor lines (~ prefix), extra
	// accepted-answer lines (= prefix), set items (+ prefix), and a quota
	// directive. Distractors are wrong answers shown in choice mode; accepted
	// answers are additional spellings counted correct in type mode ("= hi"
	// alongside the primary answer "hello"). An "=" after a "+" item attaches
	// to that item, mirroring how it attaches to the single answer.
	var answerLines []string
	var distractors []string
	var accepts []string
	var setItems []SetItem
	quota := 0
	quotaSet := false
	attempts := 0
	attemptsSet := false
	for _, line := range afterSep {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "~ "):
			distractors = append(distractors, strings.TrimPrefix(trimmed, "~ "))
		case strings.HasPrefix(trimmed, "~") && len(trimmed) > 1:
			distractors = append(distractors, strings.TrimSpace(trimmed[1:]))
		case strings.HasPrefix(trimmed, "+ "):
			setItems = append(setItems, SetItem{Text: strings.TrimPrefix(trimmed, "+ ")})
		case strings.HasPrefix(trimmed, "quota:"):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "quota:"))); err == nil && n >= 1 {
				quota = n
				quotaSet = true
			} else {
				warn("ignoring %q (quota: needs an integer >= 1)", trimmed)
			}
		case strings.HasPrefix(trimmed, "attempts:"):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "attempts:"))); err == nil && n >= 1 {
				attempts = n
				attemptsSet = true
			} else {
				warn("ignoring %q (attempts: needs an integer >= 1)", trimmed)
			}
		case strings.HasPrefix(trimmed, "= "), strings.HasPrefix(trimmed, "=") && len(trimmed) > 1:
			a := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "= "), "="))
			if len(setItems) > 0 {
				it := &setItems[len(setItems)-1]
				it.Accept = append(it.Accept, a)
			} else {
				accepts = append(accepts, a)
			}
		default:
			answerLines = append(answerLines, line)
		}
	}

	// Set-answer card: two or more "+" items. Enumeration is typed entry by
	// entry, so the card is forced to type mode, can't carry distractors, and
	// its display answer is the joined list (the reveal every answer-visible
	// screen shows).
	if len(setItems) > 0 {
		// "+France" isn't an item ("+ " needs the space), so it fell into the
		// answer lines — the mixed-card error below would fire, but pointing
		// at the actual typo beats claiming the author mixed styles.
		for _, line := range answerLines {
			if t := strings.TrimSpace(line); strings.HasPrefix(t, "+") {
				return nil, fmt.Errorf("set item %q needs a space after the +: %q", t, strings.Join(filtered, " / "))
			}
		}
		if len(setItems) < 2 {
			return nil, fmt.Errorf("set card needs at least two + items: %q", strings.Join(filtered, " / "))
		}
		if len(distractors) > 0 {
			return nil, fmt.Errorf("set card can't have ~ distractors (type-mode only): %q", strings.Join(filtered, " / "))
		}
		if len(answerLines) > 0 && extractText(parseMediaLines(answerLines, baseDir, warn)) != "" {
			return nil, fmt.Errorf("set card mixes a plain answer line with + items: %q", strings.Join(filtered, " / "))
		}
		if quota > len(setItems) {
			return nil, fmt.Errorf("quota: %d exceeds the %d + items: %q", quota, len(setItems), strings.Join(filtered, " / "))
		}
		target := quota
		if target == 0 {
			target = len(setItems)
		}
		if attemptsSet && attempts < target {
			return nil, fmt.Errorf("attempts: %d can't reach the target of %d: %q", attempts, target, strings.Join(filtered, " / "))
		}
		if cardMode == ModeChoice && modeSet {
			warn("set card ignores answer-mode choice (enumeration is typed)")
		}
	} else if quotaSet {
		return nil, fmt.Errorf("quota: without + items: %q", strings.Join(filtered, " / "))
	} else if attemptsSet {
		return nil, fmt.Errorf("attempts: without + items: %q", strings.Join(filtered, " / "))
	}

	if len(answerLines) == 0 && len(setItems) == 0 {
		return nil, fmt.Errorf("card has no answer (only distractors after ---): %q", strings.Join(filtered, " / "))
	}

	// Custom distractors only mean anything in choice mode, so authoring them
	// implies it — no "# answer-mode: choice" needed on the card. Any
	// explicit answer-mode wins, per-card or deck header: a typed deck may
	// carry "~" wrong answers purely for choice-mode sessions.
	if !modeSet && !deckModeSet && len(distractors) > 0 {
		cardMode = ModeChoice
	}

	question := parseMediaLines(questionLines, baseDir, warn)
	if len(question) == 0 {
		return nil, fmt.Errorf("card question is empty (its media files are missing): %q", strings.Join(filtered, " / "))
	}

	answer := parseMediaLines(answerLines, baseDir, warn)

	// Build answer text from text elements on the answer side. A set card's
	// display answer is the joined item list — that's what every
	// answer-visible screen (preview, flip-through, the reveal) shows.
	var answerText string
	if len(setItems) > 0 {
		texts := make([]string, len(setItems))
		for i, it := range setItems {
			texts[i] = it.Text
		}
		answerText = strings.Join(texts, ", ")
		answer = append(answer, Media{Type: Text, Content: answerText})
		cardMode = ModeType
	} else {
		answerText = extractText(answer)
		if answerText == "" {
			return nil, fmt.Errorf("card answer has no text (needed for choices): %q", strings.Join(filtered, " / "))
		}
		// Answers are a single line: the user types or matches one value. Multiple
		// text lines (joined with a newline by extractText) are rejected.
		if strings.Contains(answerText, "\n") {
			return nil, fmt.Errorf("card answer must be a single line: %q", strings.Join(filtered, " / "))
		}
	}

	var note []Media
	if len(noteLines) > 0 {
		note = parseNote(noteLines, questionLines, baseDir, warn)
	}

	id, legacyIDs := stableCardID(questionLines)
	card := &Card{
		ID:             id,
		LegacyIDs:      legacyIDs,
		Question:       question,
		Answer:         answer,
		Note:           note,
		AnswerText:     answerText,
		Accept:         accepts,
		QuestionAccept: qAccepts,
		Distractors:    distractors,
		SetItems:       setItems,
		Quota:          quota,
		Attempts:       attempts,
		Mode:           cardMode,
		Choices:        cardChoices,
		TimeLimit:      cardTime,
	}

	return card, nil
}

// parseNote builds a card's optional note section: text and images only.
// Audio is rejected — the result screen already auto-plays the card's clip,
// and a second autoplaying source needs an ordering design that doesn't
// exist yet.
func parseNote(noteLines, questionLines []string, baseDir string, warn func(string, ...any)) []Media {
	var note []Media
	for _, m := range parseMediaLines(noteLines, baseDir, warn) {
		if m.Type == Audio {
			warn("ignoring note audio %q (audio in notes is not supported)", filepath.Base(m.Content))
			continue
		}
		note = append(note, m)
	}
	if note == nil {
		warn("card note section is empty: %q", strings.Join(questionLines, " / "))
	}
	return note
}

// hasClozeDeletion reports whether any authored text line carries a complete
// {{...}} deletion. Media, ~ distractor, and = accept lines don't count, and
// an unterminated {{ is literal text, mirroring parseClozeCard exactly.
func hasClozeDeletion(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@img ") || strings.HasPrefix(trimmed, "@audio ") ||
			strings.HasPrefix(trimmed, "~") || strings.HasPrefix(trimmed, "=") {
			continue
		}
		if disp, _, _ := blankClozes(line); disp != line {
			return true
		}
	}
	return false
}

// parseClozeCard builds a fill-in-the-blank card from a block without a
// separator, or from the question section of a cloze --- note block. Each
// {{...}} deletion is blanked out in the displayed question and its contents
// become the answer (multiple deletions join with a space). It returns
// ok=false (with a nil error) when the lines have no deletion at all, so the
// caller can fall back to its normal "missing separator" error; a malformed
// cloze (an empty {{}}) is a real error. ~ distractor and = accepted-answer
// lines are honored as elsewhere.
func parseClozeCard(filtered []string, baseDir string, mode QuizMode, choices, timeLimit int, warn func(string, ...any)) (*Card, bool, error) {
	var textLines, distractors, accepts []string
	for _, line := range filtered {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "~ "):
			distractors = append(distractors, strings.TrimPrefix(trimmed, "~ "))
		case strings.HasPrefix(trimmed, "~") && len(trimmed) > 1:
			distractors = append(distractors, strings.TrimSpace(trimmed[1:]))
		case strings.HasPrefix(trimmed, "= "):
			accepts = append(accepts, strings.TrimPrefix(trimmed, "= "))
		case strings.HasPrefix(trimmed, "=") && len(trimmed) > 1:
			accepts = append(accepts, strings.TrimSpace(trimmed[1:]))
		default:
			textLines = append(textLines, line)
		}
	}

	// Blank out the deletions, collecting their contents as the answer. Media
	// lines (@img/@audio) pass through untouched.
	var answerParts []string
	displayLines := make([]string, 0, len(textLines))
	sawCloze := false
	for _, line := range textLines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@img ") || strings.HasPrefix(trimmed, "@audio ") {
			displayLines = append(displayLines, line)
			continue
		}
		disp, parts, empty := blankClozes(line)
		if empty > 0 {
			return nil, true, fmt.Errorf("cloze card has an empty {{}} deletion: %q", strings.Join(filtered, " / "))
		}
		if len(parts) > 0 {
			sawCloze = true
		}
		displayLines = append(displayLines, disp)
		answerParts = append(answerParts, parts...)
	}
	if !sawCloze {
		return nil, false, nil // not a cloze card; let the caller report the missing separator
	}

	answerText := strings.Join(answerParts, " ")

	question := parseMediaLines(displayLines, baseDir, warn)
	if len(question) == 0 {
		return nil, true, fmt.Errorf("cloze card question is empty (its media files are missing): %q", strings.Join(filtered, " / "))
	}

	// Hash the authored text (with braces), so edits re-key the card.
	id, legacyIDs := stableCardID(textLines)
	return &Card{
		ID:          id,
		LegacyIDs:   legacyIDs,
		Question:    question,
		Answer:      []Media{{Type: Text, Content: answerText}},
		AnswerText:  answerText,
		Accept:      accepts,
		Distractors: distractors,
		Mode:        mode,
		Choices:     choices,
		TimeLimit:   timeLimit,
		Cloze:       true,
	}, true, nil
}

// blankClozes replaces every {{...}} run in a line with clozeBlank and returns
// the blanked line together with the (trimmed) contents of each deletion in
// order, plus a count of deletions that were empty — the caller rejects those
// rather than quiz on a blank with no answer. An unterminated "{{" is left as
// literal text.
func blankClozes(line string) (string, []string, int) {
	var b strings.Builder
	var answers []string
	empty := 0
	rest := line
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			b.WriteString(rest)
			break
		}
		j := strings.Index(rest[i+2:], "}}")
		if j < 0 {
			b.WriteString(rest) // no closing braces — leave the remainder as-is
			break
		}
		content := strings.TrimSpace(rest[i+2 : i+2+j])
		b.WriteString(rest[:i])
		b.WriteString(clozeBlank)
		if content == "" {
			empty++
		} else {
			answers = append(answers, content)
		}
		rest = rest[i+2+j+2:]
	}
	return b.String(), answers, empty
}

// ParseTimeLimit parses a time-limit metadata value. It accepts a plain
// integer number of seconds (0 to maxTimeLimit), an optional trailing "s"
// (e.g. "30s"), or the words "none"/"off"/"0" to mean no limit (returned as 0).
// The bool reports whether the value was understood; a negative or absurdly
// large value is rejected (ok=false) so the caller can warn rather than honor a
// typo.
func ParseTimeLimit(s string) (int, bool) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "none", "off", "unlimited":
		return 0, true
	}
	s = strings.TrimSuffix(s, "s")
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 || n > maxTimeLimit {
		return 0, false
	}
	return n, true
}

// ParseFontSize parses a font-size metadata value. It accepts a plain integer
// point size (e.g. "20") or one of the named sizes small/medium/large/x-large,
// which map onto the app's increment grid. The bool reports whether the value
// was understood; sizes outside a sane range are rejected.
func ParseFontSize(s string) (int, bool) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "small":
		return 14, true
	case "medium":
		return 18, true
	case "large":
		return 22, true
	case "x-large", "xlarge":
		return 26, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 8 || n > 48 {
		return 0, false
	}
	return n, true
}

// ParseSpeed parses an audio-speed metadata value: a decimal multiplier, with
// an optional trailing "x" (e.g. "0.75", "1.5x"). The value is rejected unless
// it falls within the same playback range the GUI allows (0.25–4.0), so a deck
// can't request a speed the runtime would only clamp away. The bool reports
// whether the value was understood.
func ParseSpeed(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.ToLower(s), "x")
	x, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || x < 0.25 || x > 4.0 {
		return 0, false
	}
	return x, true
}

// parseMediaLines converts raw text lines into Media elements. A named media
// file that doesn't exist is skipped with a warning rather than failing the
// whole deck — one missing clip (e.g. audio not yet generated, or a single
// failed TTS run) shouldn't make every other card unreachable. The card still
// works; it just shows/plays less.
func parseMediaLines(lines []string, baseDir string, warn func(string, ...any)) []Media {
	var media []Media
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "@img ") {
			relPath := strings.TrimSpace(strings.TrimPrefix(trimmed, "@img "))
			absPath := resolvePath(relPath, baseDir)
			if _, err := os.Stat(absPath); err != nil {
				warn("skipping missing image: %s", absPath)
				continue
			}
			media = append(media, Media{Type: Image, Content: absPath})
		} else if strings.HasPrefix(trimmed, "@audio ") {
			relPath := strings.TrimSpace(strings.TrimPrefix(trimmed, "@audio "))
			absPath := resolvePath(relPath, baseDir)
			if _, err := os.Stat(absPath); err != nil {
				warn("skipping missing audio: %s", absPath)
				continue
			}
			media = append(media, Media{Type: Audio, Content: absPath})
		} else {
			media = append(media, Media{Type: Text, Content: trimmed})
		}
	}
	return media
}

// isSeparator reports whether a line is a question/answer separator: a run of
// three or more dashes or equals (---, ----, ===, ========, …). Both
// characters are accepted, and any length ≥ 3, so deck authors can use
// whichever divider — and however long a rule — they find readable.
func isSeparator(line string) bool {
	s := strings.TrimSpace(line)
	if len(s) < 3 {
		return false
	}
	switch s[0] {
	case '-', '=':
		return strings.Trim(s, string(s[0])) == ""
	}
	return false
}

// JoinText collapses the text elements of a card side into a single
// whitespace-normalized line, ignoring image and audio elements. Empty for a
// media-only side. Used wherever a card must be named in one line (stats
// listings, the confusion contrast note).
func JoinText(media []Media) string {
	var parts []string
	for _, m := range media {
		if m.Type == Text && m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

// extractText joins all text-type Media elements into a single string.
func extractText(media []Media) string {
	var parts []string
	for _, m := range media {
		if m.Type == Text {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// resolvePath resolves a relative path against a base directory.
// If the path is already absolute, it is returned as-is.
func resolvePath(path, baseDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

// cardID generates a stable ID from question content. Lines are delimited in
// the hash — ["ab","c"] must not collide with ["a","bc"], or the two cards
// would share one progress history and parseDir would drop one as a duplicate
// — and trailing whitespace is dropped, so an editor stripping it on save
// doesn't re-key every card it touches.
func cardID(questionLines []string) string {
	h := sha256.New()
	for _, line := range questionLines {
		h.Write([]byte(strings.TrimRight(line, " \t")))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

// legacyCardID is the hash every older parser produced: raw lines, no
// delimiter, whitespace and all.
func legacyCardID(lines []string) string {
	h := sha256.New()
	for _, line := range lines {
		h.Write([]byte(line))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

// stableCardID returns the card's ID and the IDs older parsers produced for
// it, newest first: the undelimited hash of the text lines, then the
// undelimited hash of every question line including media. The ID itself
// hashes only the question's text lines, so renaming a media file doesn't
// re-key the card. A card whose question is media-only has no text to hash,
// so it hashes the media lines instead, exactly as before.
func stableCardID(questionLines []string) (id string, legacyIDs []string) {
	texts := textOnlyLines(questionLines)
	if len(texts) == 0 {
		texts = questionLines
	}
	id = cardID(texts)
	for _, legacy := range []string{legacyCardID(texts), legacyCardID(questionLines)} {
		if legacy != id && (len(legacyIDs) == 0 || legacyIDs[len(legacyIDs)-1] != legacy) {
			legacyIDs = append(legacyIDs, legacy)
		}
	}
	return id, legacyIDs
}

// textOnlyLines filters out @img/@audio directive lines, leaving the lines
// that carry the card's authored text.
func textOnlyLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "@img ") || strings.HasPrefix(t, "@audio ") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// deckName extracts a human-readable name from the deck file path.
func deckName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		base = base[:len(base)-len(ext)]
	}
	return base
}
