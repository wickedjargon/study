package quiz

import (
	"testing"

	"study/deck"
)

// TestChoiceExcludesAcceptedVariants: an option the card would accept as
// correct (an "=" variant) must never be offered as a wrong choice — even
// when another card's answer or a custom distractor is exactly that variant.
func TestChoiceExcludesAcceptedVariants(t *testing.T) {
	d := &deck.Deck{
		Order:   deck.OrderSequential,
		Mode:    deck.ModeChoice,
		Choices: 4,
		Cards: []deck.Card{
			{
				ID:         "welcome",
				AnswerText: "you're welcome",
				Accept:     []string{"you are welcome"},
				// A hostile distractor that is really a variant of the answer.
				Distractors: []string{"you are welcome"},
				Mode:        deck.ModeChoice,
			},
			// Another card whose primary answer is the variant.
			{ID: "trap", AnswerText: "you are welcome", Mode: deck.ModeChoice},
			{ID: "a", AnswerText: "thanks", Mode: deck.ModeChoice},
			{ID: "b", AnswerText: "goodbye", Mode: deck.ModeChoice},
			{ID: "c", AnswerText: "hello", Mode: deck.ModeChoice},
		},
	}
	e := NewEngine(d, nil, nil)

	if e.Current().ID != "welcome" {
		t.Fatalf("current = %s, want the welcome card first", e.Current().ID)
	}
	correct := 0
	for _, opt := range e.Options() {
		if e.accepts(e.Current(), opt) {
			correct++
		}
		if opt == "you are welcome" {
			t.Errorf("options offer %q, a variant of the correct answer", opt)
		}
	}
	if correct != 1 {
		t.Errorf("%d options are acceptable answers, want exactly 1: %v", correct, e.Options())
	}
}
