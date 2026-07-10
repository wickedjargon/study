package quiz

import (
	"study/deck"
	"testing"
)

// answerCurrent answers the current card; correct==true picks the right
// answer, false picks a wrong one. Works in both choice and type modes.
func answerCurrent(e *Engine, correct bool) {
	if e.Mode() == deck.ModeType {
		if correct {
			e.AnswerTyped(e.Current().AnswerText)
		} else {
			e.AnswerTyped(e.Current().AnswerText + "_nope")
		}
		return
	}
	opts := e.Options()
	for i, o := range opts {
		if (o == e.Current().AnswerText) == correct {
			e.Answer(i)
			return
		}
	}
}

// TestMissIsSpacedNotMassed: after a wrong answer the evidence scheduler must
// NOT repeat the card immediately — other pending cards intervene, so the
// eventual re-test is a retrieval from long-term memory rather than an echo of
// the answer just shown (Karpicke & Bauernschmidt 2011).
func TestMissIsSpacedNotMassed(t *testing.T) {
	d := testDeck(5)
	e := NewEngine(d, nil)

	missedID := e.Current().ID
	answerCurrent(e, false)
	e.Next()

	for i := 0; i < gapAfterMiss-1; i++ {
		if e.Current().ID == missedID {
			t.Fatalf("missed card returned after only %d intervening serves", i)
		}
		answerCurrent(e, true)
		e.Next()
	}
}

// TestMissedCardReappearsUntilCriterion: a missed card keeps returning until
// it has been correctly recalled the required number of times, then the
// session completes — it can neither vanish nor recur forever.
func TestMissedCardReappearsUntilCriterion(t *testing.T) {
	d := testDeck(5)
	e := NewEngine(d, nil)

	missedID := e.Current().ID
	answerCurrent(e, false)
	e.Next()

	appearances := 1
	for i := 0; i < 100 && e.State() != Done; i++ {
		if e.Current().ID == missedID {
			appearances++
		}
		answerCurrent(e, true)
		e.Next()
	}

	if e.State() != Done {
		t.Fatal("session never completed")
	}
	// New card: needs 3 corrects; the miss bumped nothing (3 > 2). 1 miss + 3
	// corrects = 4 appearances.
	if appearances != 4 {
		t.Errorf("missed card appeared %d times, want 4 (1 miss + 3 corrects)", appearances)
	}
}

// TestNoCardStarvation guarantees every card in the deck is shown at least
// once when the user answers everything correctly.
func TestNoCardStarvation(t *testing.T) {
	d := testDeck(5)
	e := NewEngine(d, nil)

	seen := make(map[string]bool)
	for i := 0; i < 40 && e.State() != Done; i++ {
		seen[e.Current().ID] = true
		answerCurrent(e, true)
		e.Next()
	}

	if len(seen) != len(d.Cards) {
		t.Errorf("expected all %d cards shown, only saw %d", len(d.Cards), len(seen))
	}
}

// TestSessionCompletesAtCriterion: new cards leave after 3 correct recalls and
// the session reaches Done on its own — 5 new cards, all answered correctly,
// is exactly 15 serves.
func TestSessionCompletesAtCriterion(t *testing.T) {
	d := testDeck(5)
	e := NewEngine(d, nil)

	serves := 0
	for e.State() != Done && serves < 100 {
		serves++
		answerCurrent(e, true)
		e.Next()
	}

	if e.State() != Done {
		t.Fatal("session never completed")
	}
	if want := len(d.Cards) * needNew; serves != want {
		t.Errorf("session took %d serves, want %d (%d cards x %d recalls)", serves, want, len(d.Cards), needNew)
	}
	if e.Current() != nil {
		t.Error("no current card once the session is complete")
	}
}
