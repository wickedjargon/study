// Package deck parses plain-text deck files into structured card data.
//
// Deck format (sent-inspired):
//
//	# Comment or metadata (# choices: N)
//	@img path/to/image.png
//	@audio path/to/audio.mp3
//	Question text
//	---                        (or === — both separate question from answer)
//	Answer text
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

// Card represents a single question/answer pair.
type Card struct {
	ID          string   // stable hash of question content
	Question    []Media  // question side elements
	Answer      []Media  // answer side elements
	AnswerText  string   // plain text of the answer (for choice generation)
	Accept      []string // extra answers accepted in type mode ("= " lines)
	Distractors []string // optional custom wrong answers
	Mode        QuizMode // per-card mode (choice or type)
	Choices     int      // per-card choice count (0 = use deck default)
	TimeLimit   int      // per-card time limit in seconds
	// (0 = inherit deck default, -1 = explicitly unlimited)
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
	Path          string   // absolute path to deck file
	Choices       int      // number of answer choices (default 4)
	Mode          QuizMode // choice or type
	CaseSensitive bool     // case-sensitive matching for type mode
	TimeLimit     int      // global per-question time limit in seconds (0 = none)
	Sequential    bool     // present cards in deck order (default: shuffled)
	FontSize      int      // base font size in points (0 = use the app default)
	Speed         float64  // audio playback speed multiplier (0 = use the app default of 1.0)
	Cards         []Card

	// Warnings collects non-fatal parse issues — directives whose value was
	// malformed or out of range and therefore ignored. The caller prints these
	// so a typo'd directive isn't silently dropped. (A fatal problem, e.g. a
	// card with no answer, is returned as an error instead.)
	Warnings []string
}

// warn records a non-fatal parse issue on the deck.
func (d *Deck) warn(format string, args ...any) {
	d.Warnings = append(d.Warnings, fmt.Sprintf(format, args...))
}

// maxTimeLimit caps a per-question time limit (in seconds). A value above this
// is almost certainly a typo (a question timer measured in hours makes no
// sense), so it's rejected with a warning rather than honored.
const maxTimeLimit = 3600

// Parse reads a deck file and returns a structured Deck.
func Parse(path string) (*Deck, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("opening deck: %w", err)
	}
	defer f.Close()

	dir := filepath.Dir(absPath)
	deck := &Deck{
		Name:    deckName(absPath),
		Path:    absPath,
		Choices: 4,
		Mode:    ModeType, // default to active recall; # mode: choice opts in
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

	for _, block := range blocks {
		card, err := parseCard(block, dir, deck.Mode, deck.warn)
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

// applyDeckMetadata reads deck-level directives from a comment-only block and
// applies them to the deck.
func applyDeckMetadata(deck *Deck, block []string) {
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "# choices:"); ok {
			v := strings.TrimSpace(after)
			if n, err := strconv.Atoi(v); err == nil && n >= 2 {
				deck.Choices = n
			} else {
				deck.warn("ignoring %q (# choices: needs an integer >= 2)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# mode:"); ok {
			switch strings.TrimSpace(after) {
			case "type":
				deck.Mode = ModeType
			case "choice":
				deck.Mode = ModeChoice
			default:
				deck.warn("ignoring %q (# mode: must be type or choice)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# case:"); ok {
			switch strings.TrimSpace(after) {
			case "sensitive":
				deck.CaseSensitive = true
			case "insensitive":
				deck.CaseSensitive = false
			default:
				deck.warn("ignoring %q (# case: must be sensitive or insensitive)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# time:"); ok {
			if n, ok := parseTimeLimit(after); ok {
				if n > 0 {
					deck.TimeLimit = n
				}
			} else {
				deck.warn("ignoring %q (# time: needs 0-%d seconds, or none)", trimmed, maxTimeLimit)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# order:"); ok {
			switch strings.TrimSpace(after) {
			case "sequential":
				deck.Sequential = true
			case "shuffled":
				deck.Sequential = false
			default:
				deck.warn("ignoring %q (# order: must be sequential or shuffled)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# font-size:"); ok {
			if n, ok := parseFontSize(after); ok {
				deck.FontSize = n
			} else {
				deck.warn("ignoring %q (# font-size: needs 8-48, or small/medium/large/x-large)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# speed:"); ok {
			if x, ok := parseSpeed(after); ok {
				deck.Speed = x
			} else {
				deck.warn("ignoring %q (# speed: needs 0.25-4.0)", trimmed)
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
func parseCard(block []string, baseDir string, defaultMode QuizMode, warn func(string, ...any)) (*Card, error) {
	// A comment-only block carries no card. Leading ones are deck metadata
	// (handled in applyDeckMetadata); any later one is just a comment. Return
	// early so the per-card directive scan below doesn't double-report a bad
	// value the deck-level pass already warned about.
	if isCommentOnly(block) {
		return nil, nil
	}

	// Check for per-card metadata before filtering comments.
	cardMode := defaultMode
	cardChoices := 0
	cardTime := 0
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "# mode:"); ok {
			switch strings.TrimSpace(after) {
			case "type":
				cardMode = ModeType
			case "choice":
				cardMode = ModeChoice
			default:
				warn("ignoring %q (# mode: must be type or choice)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# choices:"); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(after)); err == nil && n >= 2 {
				cardChoices = n
			} else {
				warn("ignoring %q (# choices: needs an integer >= 2)", trimmed)
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# time:"); ok {
			if n, ok := parseTimeLimit(after); ok {
				if n <= 0 {
					cardTime = -1 // explicitly unlimited
				} else {
					cardTime = n
				}
			} else {
				warn("ignoring %q (# time: needs 0-%d seconds, or none)", trimmed, maxTimeLimit)
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

	// Split on the question/answer separator (--- or ===).
	sepIdx := -1
	for i, line := range filtered {
		if isSeparator(line) {
			sepIdx = i
			break
		}
	}

	if sepIdx == -1 {
		return nil, fmt.Errorf("card missing --- or === separator: %q", strings.Join(filtered, " / "))
	}
	if sepIdx == 0 {
		return nil, fmt.Errorf("card has no question (separator at start): %q", strings.Join(filtered, " / "))
	}

	questionLines := filtered[:sepIdx]
	afterSep := filtered[sepIdx+1:]

	if len(afterSep) == 0 {
		return nil, fmt.Errorf("card has no answer (nothing after separator): %q", strings.Join(filtered, " / "))
	}

	// Separate the answer lines from distractor lines (~ prefix) and extra
	// accepted-answer lines (= prefix). Distractors are wrong answers shown in
	// choice mode; accepted answers are additional spellings counted correct in
	// type mode (e.g. "= hi" alongside the primary answer "hello").
	var answerLines []string
	var distractors []string
	var accepts []string
	for _, line := range afterSep {
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
			answerLines = append(answerLines, line)
		}
	}

	if len(answerLines) == 0 {
		return nil, fmt.Errorf("card has no answer (only distractors after ---): %q", strings.Join(filtered, " / "))
	}

	question, err := parseMediaLines(questionLines, baseDir)
	if err != nil {
		return nil, fmt.Errorf("parsing question: %w", err)
	}

	answer, err := parseMediaLines(answerLines, baseDir)
	if err != nil {
		return nil, fmt.Errorf("parsing answer: %w", err)
	}

	// Build answer text from text elements on the answer side.
	answerText := extractText(answer)
	if answerText == "" {
		return nil, fmt.Errorf("card answer has no text (needed for choices): %q", strings.Join(filtered, " / "))
	}
	// Answers are a single line: the user types or matches one value. Multiple
	// text lines (joined with a newline by extractText) are rejected.
	if strings.Contains(answerText, "\n") {
		return nil, fmt.Errorf("card answer must be a single line: %q", strings.Join(filtered, " / "))
	}

	card := &Card{
		ID:          cardID(questionLines),
		Question:    question,
		Answer:      answer,
		AnswerText:  answerText,
		Accept:      accepts,
		Distractors: distractors,
		Mode:        cardMode,
		Choices:     cardChoices,
		TimeLimit:   cardTime,
	}

	return card, nil
}

// parseTimeLimit parses a time-limit metadata value. It accepts a plain
// integer number of seconds (0 to maxTimeLimit), an optional trailing "s"
// (e.g. "30s"), or the words "none"/"off"/"0" to mean no limit (returned as 0).
// The bool reports whether the value was understood; a negative or absurdly
// large value is rejected (ok=false) so the caller can warn rather than honor a
// typo.
func parseTimeLimit(s string) (int, bool) {
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

// parseFontSize parses a font-size metadata value. It accepts a plain integer
// point size (e.g. "20") or one of the named sizes small/medium/large/x-large,
// which map onto the app's increment grid. The bool reports whether the value
// was understood; sizes outside a sane range are rejected.
func parseFontSize(s string) (int, bool) {
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

// parseSpeed parses an audio-speed metadata value: a decimal multiplier, with
// an optional trailing "x" (e.g. "0.75", "1.5x"). The value is rejected unless
// it falls within the same playback range the GUI allows (0.25–4.0), so a deck
// can't request a speed the runtime would only clamp away. The bool reports
// whether the value was understood.
func parseSpeed(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.ToLower(s), "x")
	x, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || x < 0.25 || x > 4.0 {
		return 0, false
	}
	return x, true
}

// parseMediaLines converts raw text lines into Media elements.
func parseMediaLines(lines []string, baseDir string) ([]Media, error) {
	var media []Media
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "@img ") {
			relPath := strings.TrimSpace(strings.TrimPrefix(trimmed, "@img "))
			absPath := resolvePath(relPath, baseDir)
			if _, err := os.Stat(absPath); err != nil {
				return nil, fmt.Errorf("image not found: %s", absPath)
			}
			media = append(media, Media{Type: Image, Content: absPath})
		} else if strings.HasPrefix(trimmed, "@audio ") {
			relPath := strings.TrimSpace(strings.TrimPrefix(trimmed, "@audio "))
			absPath := resolvePath(relPath, baseDir)
			if _, err := os.Stat(absPath); err != nil {
				return nil, fmt.Errorf("audio not found: %s", absPath)
			}
			media = append(media, Media{Type: Audio, Content: absPath})
		} else {
			media = append(media, Media{Type: Text, Content: trimmed})
		}
	}
	return media, nil
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

// cardID generates a stable ID from question content.
func cardID(questionLines []string) string {
	h := sha256.New()
	for _, line := range questionLines {
		h.Write([]byte(line))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
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
