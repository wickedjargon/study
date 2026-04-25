package quiz

import (
	"study/deck"
	"testing"
)

func testDeck(n int) *deck.Deck {
	cards := make([]deck.Card, n)
	answers := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	for i := 0; i < n; i++ {
		ans := answers[i%len(answers)]
		cards[i] = deck.Card{
			ID: ans,
			Question: []deck.Media{
				{Type: deck.Text, Content: "What is " + ans + "?"},
			},
			Answer: []deck.Media{
				{Type: deck.Text, Content: ans},
			},
			AnswerText: ans,
		}
	}
	return &deck.Deck{
		Name:    "test",
		Choices: 4,
		Cards:   cards,
	}
}

func TestEngineBasicFlow(t *testing.T) {
	d := testDeck(4)
	e := NewEngine(d, false, 0, nil)

	if e.State() != ShowQuestion {
		t.Fatal("expected ShowQuestion state")
	}
	if e.Current() == nil {
		t.Fatal("expected a current card")
	}

	opts := e.Options()
	if len(opts) != 4 {
		t.Fatalf("expected 4 options, got %d", len(opts))
	}
}

func TestEngineCorrectAnswer(t *testing.T) {
	d := testDeck(2)
	e := NewEngine(d, false, 0, nil)

	// Find the correct answer index.
	opts := e.Options()
	correct := -1
	for i, o := range opts {
		if o == e.Current().AnswerText {
			correct = i
			break
		}
	}
	if correct == -1 {
		t.Fatal("correct answer not in options")
	}

	result := e.Answer(correct)
	if result == nil {
		t.Fatal("expected a result")
	}
	if !result.Correct {
		t.Error("expected correct result")
	}
	if e.State() != ShowResult {
		t.Error("expected ShowResult state")
	}
	if e.TotalCorrect != 1 {
		t.Errorf("expected 1 correct, got %d", e.TotalCorrect)
	}
}

func TestEngineWrongAnswerCreatesRetry(t *testing.T) {
	d := testDeck(4)
	e := NewEngine(d, false, 0, nil)

	// Find a wrong answer index.
	opts := e.Options()
	wrong := -1
	for i, o := range opts {
		if o != e.Current().AnswerText {
			wrong = i
			break
		}
	}

	result := e.Answer(wrong)
	if result.Correct {
		t.Error("expected wrong result")
	}
	if len(e.retry) != 1 {
		t.Errorf("expected 1 card in retry queue, got %d", len(e.retry))
	}
}

func TestEngineCorrectAnswerCycle(t *testing.T) {
	d := testDeck(3)
	e := NewEngine(d, false, 0, nil)

	// Answer each card correctly once (3 cards).
	for i := 0; i < 3; i++ {
		if e.State() != ShowQuestion {
			t.Fatalf("round %d: expected ShowQuestion, got %d", i, e.State())
		}
		opts := e.Options()
		for j, o := range opts {
			if o == e.Current().AnswerText {
				e.Answer(j)
				break
			}
		}
		e.Next()
	}

	if e.TotalSeen != 3 {
		t.Errorf("expected 3 seen, got %d", e.TotalSeen)
	}
	if e.TotalCorrect != 3 {
		t.Errorf("expected 3 correct, got %d", e.TotalCorrect)
	}
	// Session should still be active (continuous mode).
	if e.State() == Done {
		t.Error("session should not be Done in continuous mode")
	}
}

func TestEngineRetryGraduation(t *testing.T) {
	d := testDeck(5)
	e := NewEngine(d, false, 0, nil)

	// Answer first card wrong.
	firstCard := e.Current()
	opts := e.Options()
	for i, o := range opts {
		if o != firstCard.AnswerText {
			e.Answer(i)
			break
		}
	}
	e.Next()

	// The card should be in the retry queue.
	if len(e.retry) != 1 {
		t.Fatalf("expected 1 retry card, got %d", len(e.retry))
	}
	if e.retry[0].card.ID != firstCard.ID {
		t.Error("wrong card in retry queue")
	}
	if e.retry[0].remaining != minRepeats {
		t.Errorf("expected %d remaining, got %d", minRepeats, e.retry[0].remaining)
	}
}

func TestEngineChoiceCountClamped(t *testing.T) {
	d := testDeck(2)
	e := NewEngine(d, false, 10, nil) // request 10 choices but only 2 cards

	opts := e.Options()
	if len(opts) != 2 {
		t.Errorf("expected 2 options (clamped), got %d", len(opts))
	}
}

func TestEngineCustomDistractors(t *testing.T) {
	d := &deck.Deck{
		Name:    "test",
		Choices: 4,
		Cards: []deck.Card{
			{
				ID:          "card1",
				Question:    []deck.Media{{Type: deck.Text, Content: "Q1"}},
				Answer:      []deck.Media{{Type: deck.Text, Content: "correct"}},
				AnswerText:  "correct",
				Distractors: []string{"wrong1", "wrong2", "wrong3"},
			},
		},
	}

	e := NewEngine(d, false, 0, nil)
	opts := e.Options()

	if len(opts) != 4 {
		t.Fatalf("expected 4 options, got %d", len(opts))
	}

	// Verify correct answer is present.
	found := false
	for _, o := range opts {
		if o == "correct" {
			found = true
			break
		}
	}
	if !found {
		t.Error("correct answer not in options")
	}

	// Verify all distractors are present.
	for _, d := range []string{"wrong1", "wrong2", "wrong3"} {
		found := false
		for _, o := range opts {
			if o == d {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("distractor %q not in options", d)
		}
	}
}
