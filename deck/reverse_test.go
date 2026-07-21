package deck

import "testing"

// scriptCard builds a forward card shaped like the language packs: an audio
// clip, a native-script line, a romanization line, an English answer, and a
// couple of English "=" alternatives.
func scriptCard() *Card {
	return &Card{
		ID: "abc123",
		Question: []Media{
			{Type: Audio, Content: "/deck/audio/l1-salam.mp3"},
			{Type: Text, Content: "سلام"},
			{Type: Text, Content: "salâm"},
		},
		Answer:     []Media{{Type: Text, Content: "hello"}},
		AnswerText: "hello",
		Accept:     []string{"hi", "hey"},
		Mode:       ModeType,
	}
}

func TestReverseCardScriptPack(t *testing.T) {
	c, ok := reverseCard(scriptCard())
	if !ok {
		t.Fatal("reverseCard returned ok=false for a normal card")
	}

	// Prompt is the English canonical answer, and only that.
	if got := c.AnswerText; got != "salâm" {
		t.Errorf("primary answer = %q, want the romanization %q", got, "salâm")
	}
	if len(c.Question) != 1 || c.Question[0].Type != Text || c.Question[0].Content != "hello" {
		t.Errorf("prompt = %+v, want a single English text line %q", c.Question, "hello")
	}

	// Native script is accepted too (typing the script isn't marked wrong).
	if len(c.Accept) != 1 || c.Accept[0] != "سلام" {
		t.Errorf("accept = %v, want the script %q", c.Accept, "سلام")
	}

	// Reveal keeps the original prompt verbatim: audio + script + romanization.
	if len(c.Answer) != 3 || c.Answer[0].Type != Audio {
		t.Errorf("reveal = %+v, want audio+script+romanization", c.Answer)
	}

	// Reverse recall is a separate skill: the ID is namespaced.
	if c.ID != "r:abc123" {
		t.Errorf("id = %q, want %q", c.ID, "r:abc123")
	}
	// Multiple choice can't reverse; production is always type-in.
	if c.Mode != ModeType {
		t.Errorf("mode = %v, want ModeType", c.Mode)
	}
}

func TestReverseCardLatinPack(t *testing.T) {
	// A Latin-script pack (Spanish/Portuguese) has a one-line prompt: the word
	// itself, with no separate romanization.
	fwd := &Card{
		ID:         "def456",
		Question:   []Media{{Type: Audio, Content: "/a/l1-buenas.mp3"}, {Type: Text, Content: "Buenas"}},
		Answer:     []Media{{Type: Text, Content: "hello"}},
		AnswerText: "hello",
		Accept:     []string{"hi"},
		Mode:       ModeType,
	}
	c, ok := reverseCard(fwd)
	if !ok {
		t.Fatal("reverseCard returned ok=false")
	}
	if c.AnswerText != "Buenas" {
		t.Errorf("primary = %q, want %q", c.AnswerText, "Buenas")
	}
	if len(c.Accept) != 0 {
		t.Errorf("accept = %v, want none (no script line to accept)", c.Accept)
	}
}

func TestReverseCardQuestionAlternativesAccepted(t *testing.T) {
	// Question-side "=" lines are variant wordings of the target-language
	// prompt; reversed, they join the accept list after the script lines.
	fwd := scriptCard()
	fwd.QuestionAccept = []string{"salaam"}
	c, ok := reverseCard(fwd)
	if !ok {
		t.Fatal("reverseCard returned ok=false")
	}
	if len(c.Accept) != 2 || c.Accept[0] != "سلام" || c.Accept[1] != "salaam" {
		t.Errorf("accept = %v, want [سلام salaam]", c.Accept)
	}
}

func TestReverseCardNoTextIsSkipped(t *testing.T) {
	// A prompt with no text to produce (pure media) can't be reversed.
	fwd := &Card{
		ID:         "img789",
		Question:   []Media{{Type: Image, Content: "/a/flag.png"}},
		Answer:     []Media{{Type: Text, Content: "France"}},
		AnswerText: "France",
	}
	if _, ok := reverseCard(fwd); ok {
		t.Error("reverseCard returned ok=true for a card with no target-language text")
	}
}

func TestReverseCardClozeIsSkipped(t *testing.T) {
	// A cloze card's "question" is the blanked sentence; reversing it would ask
	// the user to type ____-riddled text, so it's dropped instead.
	fwd := &Card{
		ID:         "cloze1",
		Question:   []Media{{Type: Text, Content: "The capital of France is ____."}},
		Answer:     []Media{{Type: Text, Content: "Paris"}},
		AnswerText: "Paris",
		Cloze:      true,
	}
	if _, ok := reverseCard(fwd); ok {
		t.Error("reverseCard returned ok=true for a cloze card")
	}
}

func TestReverseCardNonLatinAnswerIsSkipped(t *testing.T) {
	// A single-line script drill (あ → a) reverses into "type あ", which needs
	// an IME this GUI never receives — so it's dropped.
	fwd := &Card{
		ID:         "kana1",
		Question:   []Media{{Type: Audio, Content: "/a/hir-a.mp3"}, {Type: Text, Content: "あ"}},
		Answer:     []Media{{Type: Text, Content: "a"}},
		AnswerText: "a",
	}
	if _, ok := reverseCard(fwd); ok {
		t.Error("reverseCard returned ok=true for a card whose answer has no Latin text")
	}
}

func TestReverseCardPropagatesLegacyID(t *testing.T) {
	fwd := scriptCard()
	fwd.LegacyIDs = []string{"old999"}
	c, ok := reverseCard(fwd)
	if !ok {
		t.Fatal("reverseCard returned ok=false")
	}
	if len(c.LegacyIDs) == 0 || c.LegacyIDs[0] != "r:old999" {
		t.Errorf("legacy ids = %v, want r:old999 first", c.LegacyIDs)
	}
}

func TestReversedDeck(t *testing.T) {
	d := &Deck{
		Mode: ModeChoice, // should be forced to type-in
		Cards: []Card{
			*scriptCard(),
			{ID: "media", Question: []Media{{Type: Image, Content: "/x.png"}}, Answer: []Media{{Type: Text, Content: "x"}}, AnswerText: "x"},
		},
	}
	rev := d.Reversed()

	if rev.Mode != ModeType {
		t.Errorf("reversed deck mode = %v, want ModeType", rev.Mode)
	}
	// The media-only card drops out; only the reversible one survives.
	if len(rev.Cards) != 1 {
		t.Fatalf("reversed deck has %d cards, want 1 (media card skipped)", len(rev.Cards))
	}
	if rev.Cards[0].ID != "r:abc123" {
		t.Errorf("surviving card id = %q, want %q", rev.Cards[0].ID, "r:abc123")
	}
	// The original deck must be untouched (Reversed returns a copy).
	if d.Cards[0].ID != "abc123" || d.Mode != ModeChoice {
		t.Error("Reversed mutated the original deck")
	}
}
