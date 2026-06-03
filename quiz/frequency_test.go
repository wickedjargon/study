package quiz

import (
	"study/progress"
	"testing"
)

// newTestStore builds an in-memory progress store backed by a temp dir.
func newTestStore(t *testing.T) *progress.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := progress.NewStore(dir + "/deck.txt")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return s
}

// TestMissedCardAppearsMoreOftenDespiteHistory reproduces the user's report:
// a deck that has been studied a lot (every card has a long correct history),
// then two cards are missed this session. The missed cards must appear MORE
// often afterwards than the cards with intact streaks — not less, not never.
func TestMissedCardAppearsMoreOftenDespiteHistory(t *testing.T) {
	d := testDeck(5)
	store := newTestStore(t)

	// Simulate heavy prior study: every card has a long correct streak and
	// near-perfect lifetime accuracy.
	for _, c := range d.Cards {
		for i := 0; i < 8; i++ {
			store.RecordCorrect(c.ID)
		}
	}

	e := NewEngine(d, false, 0, store)

	// Miss the first two distinct cards we are shown (cold recall), then drill
	// each back to graduation, then answer everything correctly for a while.
	missed := map[string]bool{}
	counts := map[string]int{}

	for step := 0; step < 200 && e.State() != Done; step++ {
		cur := e.Current()
		counts[cur.ID]++
		fromRetry := e.IsRetry()

		// Decide correctness.
		makeWrong := false
		if !fromRetry && len(missed) < 2 && !missed[cur.ID] {
			// First cold sighting of a not-yet-missed card: miss it.
			makeWrong = true
			missed[cur.ID] = true
		}

		// Apply the answer. The engine records the outcome to the store itself
		// (cold recall only — retry-drill reps don't touch the store), so the
		// test no longer mirrors that bookkeeping.
		if makeWrong {
			e.AnswerTyped("definitely-wrong")
		} else {
			e.AnswerTyped(cur.AnswerText)
		}
		e.Next()
	}

	// Tally appearances of missed vs. never-missed cards.
	missedTotal, intactTotal, intactCards := 0, 0, 0
	for _, c := range d.Cards {
		if missed[c.ID] {
			missedTotal += counts[c.ID]
		} else {
			intactTotal += counts[c.ID]
			intactCards++
		}
	}

	t.Logf("appearance counts: %v (missed=%v)", counts, missed)

	if len(missed) != 2 {
		t.Fatalf("expected to miss 2 cards, missed %d", len(missed))
	}

	// Every missed card must have reappeared after its drill (count well above
	// the 1 cold sighting + 3 drill reps = 4).
	for id := range missed {
		if counts[id] <= 4 {
			t.Errorf("missed card %s only appeared %d times — it is not recurring", id, counts[id])
		}
	}

	// On average, a missed card should appear more often than an intact one.
	avgMissed := float64(missedTotal) / 2.0
	avgIntact := float64(intactTotal) / float64(intactCards)
	if avgMissed <= avgIntact {
		t.Errorf("missed cards should appear more often than intact ones: avgMissed=%.1f avgIntact=%.1f",
			avgMissed, avgIntact)
	}
}
