package deck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempDeck(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.deck")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseBasicDeck(t *testing.T) {
	content := `# Test Deck

hello
---
world

foo
---
bar
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(d.Cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(d.Cards))
	}
	if d.Choices != 4 {
		t.Errorf("expected default 4 choices, got %d", d.Choices)
	}
	if d.Cards[0].AnswerText != "world" {
		t.Errorf("expected answer 'world', got %q", d.Cards[0].AnswerText)
	}
	if d.Cards[1].AnswerText != "bar" {
		t.Errorf("expected answer 'bar', got %q", d.Cards[1].AnswerText)
	}
}

func TestParseDefaultModeIsType(t *testing.T) {
	// A deck with no # mode: header defaults to type-in (active recall).
	d, err := Parse(writeTempDeck(t, "2 + 2\n---\n4\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Mode != ModeType {
		t.Errorf("expected deck default mode ModeType, got %v", d.Mode)
	}
	if d.Cards[0].Mode != ModeType {
		t.Errorf("expected card default mode ModeType, got %v", d.Cards[0].Mode)
	}

	// # mode: choice still opts a deck into multiple choice.
	d, err = Parse(writeTempDeck(t, "# mode: choice\n\n2 + 2\n---\n4\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Mode != ModeChoice {
		t.Errorf("expected ModeChoice with '# mode: choice', got %v", d.Mode)
	}
}

func TestParseSeparatorVariants(t *testing.T) {
	// --- and ===, any length ≥ 3, are all accepted and mixable in one deck.
	content := "a\n===\n1\n\nb\n----\n2\n\nc\n========\n3\n\nd\n---\n4\n"
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"1", "2", "3", "4"}
	if len(d.Cards) != len(want) {
		t.Fatalf("expected %d cards, got %d", len(want), len(d.Cards))
	}
	for i, w := range want {
		if d.Cards[i].AnswerText != w {
			t.Errorf("card %d: expected answer %q, got %q", i, w, d.Cards[i].AnswerText)
		}
	}
}

func TestIsSeparator(t *testing.T) {
	yes := []string{"---", "----", "===", "========", "  ---  ", "-----------"}
	no := []string{"--", "==", "-", "", "-=-", "- - -", "===x", "a---"}
	for _, s := range yes {
		if !isSeparator(s) {
			t.Errorf("isSeparator(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isSeparator(s) {
			t.Errorf("isSeparator(%q) = true, want false", s)
		}
	}
}

func TestParseChoicesMetadata(t *testing.T) {
	content := `# choices: 6

q1
---
a1
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Choices != 6 {
		t.Errorf("expected 6 choices, got %d", d.Choices)
	}
}

func TestParseOrderMetadata(t *testing.T) {
	// Default (no header) is shuffled.
	d, err := Parse(writeTempDeck(t, "q1\n---\na1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Sequential {
		t.Errorf("expected Sequential=false by default")
	}

	// # order: sequential opts into deck order.
	d, err = Parse(writeTempDeck(t, "# order: sequential\n\nq1\n---\na1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.Sequential {
		t.Errorf("expected Sequential=true with '# order: sequential'")
	}

	// # order: shuffled is the explicit default.
	d, err = Parse(writeTempDeck(t, "# order: shuffled\n\nq1\n---\na1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Sequential {
		t.Errorf("expected Sequential=false with '# order: shuffled'")
	}
}

func TestParseCustomDistractors(t *testing.T) {
	content := `question
---
correct answer
~ wrong1
~ wrong2
~ wrong3
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	card := d.Cards[0]
	if card.AnswerText != "correct answer" {
		t.Errorf("expected 'correct answer', got %q", card.AnswerText)
	}
	if len(card.Distractors) != 3 {
		t.Fatalf("expected 3 distractors, got %d", len(card.Distractors))
	}
	if card.Distractors[0] != "wrong1" {
		t.Errorf("expected distractor 'wrong1', got %q", card.Distractors[0])
	}
}

func TestParseAcceptedAlternatives(t *testing.T) {
	content := `question
---
hello
= hi
=hey
~ goodbye
`
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	card := d.Cards[0]
	if card.AnswerText != "hello" {
		t.Errorf("AnswerText = %q, want \"hello\"", card.AnswerText)
	}
	if len(card.Accept) != 2 || card.Accept[0] != "hi" || card.Accept[1] != "hey" {
		t.Errorf("Accept = %v, want [hi hey]", card.Accept)
	}
	// The = lines must not leak into the distractor set.
	if len(card.Distractors) != 1 || card.Distractors[0] != "goodbye" {
		t.Errorf("Distractors = %v, want [goodbye]", card.Distractors)
	}
}

func TestParseWithImage(t *testing.T) {
	dir := t.TempDir()

	// Create a fake image file.
	imgDir := filepath.Join(dir, "tiles")
	os.MkdirAll(imgDir, 0755)
	os.WriteFile(filepath.Join(imgDir, "test.png"), []byte("fake"), 0644)

	content := `@img tiles/test.png
---
test answer
`
	path := filepath.Join(dir, "test.deck")
	os.WriteFile(path, []byte(content), 0644)

	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	card := d.Cards[0]
	if len(card.Question) != 1 {
		t.Fatalf("expected 1 question element, got %d", len(card.Question))
	}
	if card.Question[0].Type != Image {
		t.Errorf("expected Image type, got %d", card.Question[0].Type)
	}
	expected := filepath.Join(imgDir, "test.png")
	if card.Question[0].Content != expected {
		t.Errorf("expected path %q, got %q", expected, card.Question[0].Content)
	}
}

func TestParseMissingSeparator(t *testing.T) {
	content := `question without separator
answer
`
	path := writeTempDeck(t, content)
	_, err := Parse(path)
	if err == nil {
		t.Fatal("expected error for missing separator")
	}
}

func TestParseEmptyDeck(t *testing.T) {
	content := `# Just comments
# nothing else
`
	path := writeTempDeck(t, content)
	_, err := Parse(path)
	if err == nil {
		t.Fatal("expected error for empty deck")
	}
}

func TestParseMissingImage(t *testing.T) {
	content := `@img nonexistent.png
---
answer
`
	path := writeTempDeck(t, content)
	_, err := Parse(path)
	if err == nil {
		t.Fatal("expected error for missing image")
	}
}

func TestParseMultilineAnswerRejected(t *testing.T) {
	content := `question
---
line one
line two
`
	path := writeTempDeck(t, content)
	if _, err := Parse(path); err == nil {
		t.Fatal("expected error for multi-line answer, got nil")
	}
}

func TestCardIDStable(t *testing.T) {
	lines1 := []string{"hello", "world"}
	lines2 := []string{"hello", "world"}
	lines3 := []string{"different"}

	id1 := cardID(lines1)
	id2 := cardID(lines2)
	id3 := cardID(lines3)

	if id1 != id2 {
		t.Errorf("same content should produce same ID: %s != %s", id1, id2)
	}
	if id1 == id3 {
		t.Error("different content should produce different IDs")
	}
}

func TestParseTimeLimitMetadata(t *testing.T) {
	content := `# time: 15

q1
---
a1

# time: 30s
q2
---
a2

# time: none
q3
---
a3

q4
---
a4
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if d.TimeLimit != 15 {
		t.Errorf("expected deck TimeLimit=15, got %d", d.TimeLimit)
	}
	if len(d.Cards) != 4 {
		t.Fatalf("expected 4 cards, got %d", len(d.Cards))
	}

	// Card 0: no per-card line → inherits deck default (0 = inherit).
	if d.Cards[0].TimeLimit != 0 {
		t.Errorf("card 0: expected TimeLimit=0 (inherit), got %d", d.Cards[0].TimeLimit)
	}
	if got := d.Cards[0].EffectiveTimeLimit(d.TimeLimit); got != 15 {
		t.Errorf("card 0: expected effective 15, got %d", got)
	}

	// Card 1: per-card override "30s".
	if d.Cards[1].TimeLimit != 30 {
		t.Errorf("card 1: expected TimeLimit=30, got %d", d.Cards[1].TimeLimit)
	}
	if got := d.Cards[1].EffectiveTimeLimit(d.TimeLimit); got != 30 {
		t.Errorf("card 1: expected effective 30, got %d", got)
	}

	// Card 2: "none" → explicitly unlimited (-1), effective 0 despite deck default.
	if d.Cards[2].TimeLimit != -1 {
		t.Errorf("card 2: expected TimeLimit=-1 (none), got %d", d.Cards[2].TimeLimit)
	}
	if got := d.Cards[2].EffectiveTimeLimit(d.TimeLimit); got != 0 {
		t.Errorf("card 2: expected effective 0 (unlimited), got %d", got)
	}

	// Card 3: no line, inherits deck default.
	if got := d.Cards[3].EffectiveTimeLimit(d.TimeLimit); got != 15 {
		t.Errorf("card 3: expected effective 15, got %d", got)
	}
}

func TestParseFontSizeMetadata(t *testing.T) {
	cases := []struct {
		name string
		line string
		want int
	}{
		{"numeric", "# font-size: 24", 24},
		{"named small", "# font-size: small", 14},
		{"named medium", "# font-size: medium", 18},
		{"named large", "# font-size: large", 22},
		{"named x-large", "# font-size: x-large", 26},
		{"out of range", "# font-size: 200", 0}, // rejected → unset
		{"unparseable", "# font-size: huge", 0}, // rejected → unset
		{"absent", "# choices: 4", 0},           // no directive → unset
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := tc.line + "\n\nq1\n---\na1\n"
			d, err := Parse(writeTempDeck(t, content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.FontSize != tc.want {
				t.Errorf("FontSize = %d, want %d", d.FontSize, tc.want)
			}
		})
	}
}

func TestParseSpeedMetadata(t *testing.T) {
	cases := []struct {
		name string
		line string
		want float64
	}{
		{"numeric", "# speed: 0.75", 0.75},
		{"with x suffix", "# speed: 1.5x", 1.5},
		{"min bound", "# speed: 0.25", 0.25},
		{"max bound", "# speed: 4.0", 4.0},
		{"below min", "# speed: 0.1", 0},    // rejected → unset
		{"above max", "# speed: 8", 0},      // rejected → unset
		{"unparseable", "# speed: fast", 0}, // rejected → unset
		{"absent", "# choices: 4", 0},       // no directive → unset
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := tc.line + "\n\nq1\n---\na1\n"
			d, err := Parse(writeTempDeck(t, content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Speed != tc.want {
				t.Errorf("Speed = %v, want %v", d.Speed, tc.want)
			}
		})
	}
}

func TestEffectiveTimeLimitNoDeckDefault(t *testing.T) {
	// With no deck-global limit, an un-annotated card has no limit.
	c := Card{TimeLimit: 0}
	if got := c.EffectiveTimeLimit(0); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
	// A per-card limit still applies even without a deck default.
	c2 := Card{TimeLimit: 20}
	if got := c2.EffectiveTimeLimit(0); got != 20 {
		t.Errorf("expected 20, got %d", got)
	}
}

func TestParseHeaderDirectivesAcrossBlankLines(t *testing.T) {
	// Deck-level directives are read from every leading comment-only block, so a
	// blank line separating header directives must not drop the ones after it.
	content := `# choices: 6

# mode: choice
# order: sequential

q1
---
a1
`
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Choices != 6 {
		t.Errorf("expected 6 choices, got %d", d.Choices)
	}
	if d.Mode != ModeChoice {
		t.Errorf("expected ModeChoice from directive after a blank line, got %v", d.Mode)
	}
	if !d.Sequential {
		t.Errorf("expected Sequential=true from directive after a blank line")
	}
}

func TestParsePerCardChoices(t *testing.T) {
	content := `# choices: 4

q1
---
a1

# choices: 6
q2
---
a2
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(d.Cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(d.Cards))
	}
	if d.Cards[0].Choices != 0 {
		t.Errorf("expected card 0 choices=0 (deck default), got %d", d.Cards[0].Choices)
	}
	if d.Cards[1].Choices != 6 {
		t.Errorf("expected card 1 choices=6, got %d", d.Cards[1].Choices)
	}
}

// questionText joins a card's question-side text Media into one string,
// matching how the GUI lays them out line by line.
func questionText(c *Card) string {
	var parts []string
	for _, m := range c.Question {
		if m.Type == Text {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func TestParseClozeBasic(t *testing.T) {
	// A separator-less card with a {{...}} deletion is a fill-in-the-blank card:
	// the braced text is blanked in the question and becomes the answer.
	d, err := Parse(writeTempDeck(t, "The capital of France is {{Paris}}.\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(d.Cards))
	}
	c := d.Cards[0]
	if c.AnswerText != "Paris" {
		t.Errorf("expected answer %q, got %q", "Paris", c.AnswerText)
	}
	if got := questionText(&c); got != "The capital of France is ____." {
		t.Errorf("expected blanked question, got %q", got)
	}
}

func TestParseClozeMultiple(t *testing.T) {
	d, err := Parse(writeTempDeck(t, "{{Romeo}} and {{Juliet}}\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := d.Cards[0]
	if c.AnswerText != "Romeo Juliet" {
		t.Errorf("expected joined answer %q, got %q", "Romeo Juliet", c.AnswerText)
	}
	if got := questionText(&c); got != "____ and ____" {
		t.Errorf("expected both deletions blanked, got %q", got)
	}
}

func TestParseClozeWithAcceptAndDistractors(t *testing.T) {
	content := `Water is H2{{O}}.
= oxygen
~ hydrogen
`
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := d.Cards[0]
	if c.AnswerText != "O" {
		t.Errorf("expected answer %q, got %q", "O", c.AnswerText)
	}
	if len(c.Accept) != 1 || c.Accept[0] != "oxygen" {
		t.Errorf("expected accept [oxygen], got %v", c.Accept)
	}
	if len(c.Distractors) != 1 || c.Distractors[0] != "hydrogen" {
		t.Errorf("expected distractors [hydrogen], got %v", c.Distractors)
	}
}

func TestParseClozeEmptyDeletionErrors(t *testing.T) {
	if _, err := Parse(writeTempDeck(t, "an empty {{}} blank\n")); err == nil {
		t.Fatal("expected error for empty {{}} deletion")
	}
}

func TestParseClozeStillRequiresSeparatorWhenNoBraces(t *testing.T) {
	// Without braces, a separator-less card is still the existing error, not a
	// silently-accepted cloze.
	if _, err := Parse(writeTempDeck(t, "no separator and no cloze\n")); err == nil {
		t.Fatal("expected missing-separator error")
	}
}

func TestParseTimeLimitUpperBound(t *testing.T) {
	// An absurd per-question limit is rejected (and warned) rather than honored.
	d, err := Parse(writeTempDeck(t, "# time: 999999\n\nq\n---\na\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.TimeLimit != 0 {
		t.Errorf("expected out-of-range time limit ignored (0), got %d", d.TimeLimit)
	}
	if len(d.Warnings) == 0 {
		t.Error("expected a warning for the out-of-range # time:")
	}
}

func TestParseWarnsOnInvalidDirective(t *testing.T) {
	d, err := Parse(writeTempDeck(t, "# choices: banana\n# mode: sideways\n\nq\n---\na\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d: %v", len(d.Warnings), d.Warnings)
	}
	// The bad directives are ignored: defaults stand.
	if d.Choices != 4 {
		t.Errorf("expected default 4 choices after invalid value, got %d", d.Choices)
	}
}

func TestParseValidDirectivesNoWarnings(t *testing.T) {
	// A well-formed deck must not emit spurious warnings.
	d, err := Parse(writeTempDeck(t, "# choices: 3\n# time: 30\n# mode: choice\n\nq\n---\na\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", d.Warnings)
	}
}
