// Package deck parses plain-text deck files into structured card data.
//
// Deck format (sent-inspired):
//
//	# Comment or metadata (# choices: N)
//	@img path/to/image.png
//	@audio path/to/audio.mp3
//	Question text
//	---
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
	Text  MediaType = iota
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
	ID          string    // stable hash of question content
	Question    []Media   // question side elements
	Answer      []Media   // answer side elements
	AnswerText  string    // plain text of the answer (for choice generation)
	Distractors []string  // optional custom wrong answers
	Mode        QuizMode  // per-card mode (choice or type)
	Choices     int       // per-card choice count (0 = use deck default)
}

// QuizMode determines how answers are submitted.
type QuizMode int

const (
	ModeChoice QuizMode = iota // multiple choice (default)
	ModeType                   // user types the answer
)

// Deck represents a parsed deck file.
type Deck struct {
	Name          string
	Path          string   // absolute path to deck file
	Choices       int      // number of answer choices (default 4)
	Mode          QuizMode // choice or type
	CaseSensitive bool     // case-sensitive matching for type mode
	Cards         []Card
}

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
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading deck: %w", err)
	}

	// Extract metadata from comment lines at the top.
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		if after, ok := strings.CutPrefix(trimmed, "# choices:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(after))
			if err == nil && n >= 2 {
				deck.Choices = n
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# mode:"); ok {
			switch strings.TrimSpace(after) {
			case "type":
				deck.Mode = ModeType
			case "choice":
				deck.Mode = ModeChoice
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# case:"); ok {
			switch strings.TrimSpace(after) {
			case "sensitive":
				deck.CaseSensitive = true
			case "insensitive":
				deck.CaseSensitive = false
			}
		}
	}

	// Split lines into card blocks separated by blank lines.
	blocks := splitBlocks(lines)

	for _, block := range blocks {
		card, err := parseCard(block, dir, deck.Mode)
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

// parseCard parses a block of lines into a Card.
// Returns nil for comment-only blocks.
func parseCard(block []string, baseDir string, defaultMode QuizMode) (*Card, error) {
	// Check for per-card metadata before filtering comments.
	cardMode := defaultMode
	cardChoices := 0
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "# mode:"); ok {
			switch strings.TrimSpace(after) {
			case "type":
				cardMode = ModeType
			case "choice":
				cardMode = ModeChoice
			}
		}
		if after, ok := strings.CutPrefix(trimmed, "# choices:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(after))
			if err == nil && n >= 2 {
				cardChoices = n
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

	// Split on --- separator.
	sepIdx := -1
	for i, line := range filtered {
		if strings.TrimSpace(line) == "---" {
			sepIdx = i
			break
		}
	}

	if sepIdx == -1 {
		return nil, fmt.Errorf("card missing --- separator: %q", strings.Join(filtered, " / "))
	}
	if sepIdx == 0 {
		return nil, fmt.Errorf("card has no question (--- at start): %q", strings.Join(filtered, " / "))
	}

	questionLines := filtered[:sepIdx]
	afterSep := filtered[sepIdx+1:]

	if len(afterSep) == 0 {
		return nil, fmt.Errorf("card has no answer (nothing after ---): %q", strings.Join(filtered, " / "))
	}

	// Separate answer lines from distractor lines (~ prefix).
	var answerLines []string
	var distractors []string
	for _, line := range afterSep {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "~ ") {
			distractors = append(distractors, strings.TrimPrefix(trimmed, "~ "))
		} else if strings.HasPrefix(trimmed, "~") && len(trimmed) > 1 {
			distractors = append(distractors, strings.TrimSpace(trimmed[1:]))
		} else {
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

	card := &Card{
		ID:          cardID(questionLines),
		Question:    question,
		Answer:      answer,
		AnswerText:  answerText,
		Distractors: distractors,
		Mode:        cardMode,
		Choices:     cardChoices,
	}

	return card, nil
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
