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

// TestWrongCardReappearsAfterGraduation reproduces the user's report: miss the
// first card, drill it, then keep answering correctly. The missed card must
// come back later in the session rather than disappearing while the cards the
// user knew keep cycling.
func TestWrongCardReappearsAfterGraduation(t *testing.T) {
	d := testDeck(5)
	e := NewEngine(d, false, 0, nil)

	missedID := e.Current().ID

	// Miss it once, then answer everything correctly for a long stretch.
	answerCurrent(e, false)
	e.Next()

	graduated := false
	reappeared := false
	for i := 0; i < 60 && e.State() != Done; i++ {
		cur := e.Current()
		fromRetry := e.IsRetry()

		// Once the missed card is no longer in the retry queue, it has
		// graduated; after that point we want to see it surface again.
		inRetry := false
		for _, rc := range e.retry {
			if rc.card.ID == missedID {
				inRetry = true
			}
		}
		if !inRetry {
			graduated = true
		}
		if graduated && !fromRetry && cur.ID == missedID && i > 0 {
			reappeared = true
			break
		}

		answerCurrent(e, true)
		e.Next()
	}

	if !graduated {
		t.Fatal("missed card never graduated from the retry queue")
	}
	if !reappeared {
		t.Error("missed card never reappeared after graduating — wrong cards must keep recurring")
	}
}

// TestNoCardStarvation guarantees every card in the deck is shown at least
// once even when the user answers everything correctly (which re-queues cards
// and previously buried the tail forever).
func TestNoCardStarvation(t *testing.T) {
	d := testDeck(5)
	e := NewEngine(d, false, 0, nil)

	seen := make(map[string]bool)
	for i := 0; i < 40 && len(seen) < len(d.Cards); i++ {
		seen[e.Current().ID] = true
		answerCurrent(e, true)
		e.Next()
	}

	if len(seen) != len(d.Cards) {
		t.Errorf("expected all %d cards shown, only saw %d", len(d.Cards), len(seen))
	}
}
