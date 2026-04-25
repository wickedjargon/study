package deck

import (
	"os"
	"path/filepath"
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

func TestParseMultilineAnswer(t *testing.T) {
	content := `question
---
line one
line two
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Cards[0].AnswerText != "line one\nline two" {
		t.Errorf("expected multiline answer, got %q", d.Cards[0].AnswerText)
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
