package quiz

import (
	"fmt"
	"testing"

	"study/deck"
)

// tenNewCards builds a type-mode deck of n never-studied cards.
func newCardsDeck(n int) *deck.Deck {
	d := &deck.Deck{Order: deck.OrderAdaptive, NewPerSession: -1}
	for i := 0; i < n; i++ {
		d.Cards = append(d.Cards, deck.Card{
			ID:         fmt.Sprintf("c%02d", i),
			AnswerText: fmt.Sprintf("a%02d", i),
			Mode:       deck.ModeType,
		})
	}
	return d
}

// TestGradualIntroduction: new cards enter the session a window at a time —
// no more than activeNewLimit distinct cards appear before the first one
// completes its criterion, and the whole batch is still served and completed.
func TestGradualIntroduction(t *testing.T) {
	store := newTestStore(t)
	d := newCardsDeck(10)
	e := NewEngine(d, nil, store)

	if got := e.Remaining(); got != 10 {
		t.Fatalf("Remaining at start = %d, want 10 (held-back cards must count)", got)
	}

	seen := make(map[string]bool)
	correct := make(map[string]int)
	completedFirst := false
	steps := 0
	for e.State() != Done {
		if steps++; steps > 200 {
			t.Fatal("session did not complete in 200 steps")
		}
		c := e.Current()
		seen[c.ID] = true

		if !completedFirst && len(seen) > activeNewLimit {
			t.Fatalf("%d distinct cards served before any completed its criterion (window is %d)",
				len(seen), activeNewLimit)
		}

		e.AnswerTyped(c.AnswerText)
		correct[c.ID]++
		if correct[c.ID] >= needNew {
			completedFirst = true
		}
		e.Next()
	}

	if len(seen) != 10 {
		t.Errorf("served %d distinct cards over the session, want all 10", len(seen))
	}
	if got := e.TotalCorrect; got != 10*needNew {
		t.Errorf("TotalCorrect = %d, want %d (three recalls each)", got, 10*needNew)
	}
}

// TestIntroductionSurvivesMisses: a card that keeps being missed never frees
// its window slot, but the drained-queue path still admits the rest rather
// than ending the session early.
func TestIntroductionSurvivesMisses(t *testing.T) {
	store := newTestStore(t)
	d := newCardsDeck(6)
	e := NewEngine(d, nil, store)

	// Miss everything a few times, then answer everything correctly.
	for i := 0; i < 30 && e.State() != Done; i++ {
		e.AnswerTyped("wrong")
		e.Next()
	}
	seen := make(map[string]bool)
	for i := 0; i < 300 && e.State() != Done; i++ {
		c := e.Current()
		seen[c.ID] = true
		e.AnswerTyped(c.AnswerText)
		e.Next()
	}

	if e.State() != Done {
		t.Fatal("session did not complete")
	}
	if e.Remaining() != 0 {
		t.Errorf("Remaining after Done = %d, want 0", e.Remaining())
	}
}
