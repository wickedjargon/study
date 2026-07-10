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

// TestMissedCardsGetMorePractice: in a review session, a missed card must be
// re-established (two more spaced recalls) while intact cards are recalled
// once and leave — so misses get strictly more practice, and the session
// still completes.
func TestMissedCardsGetMorePractice(t *testing.T) {
	d := testDeck(5)
	store := newTestStore(t)

	// Simulate prior study: every card has history, so all are reviews.
	for _, c := range d.Cards {
		for i := 0; i < 8; i++ {
			store.RecordCorrect(c.ID)
		}
	}

	e := NewEngine(d, store)

	// Miss the first two distinct cards on first sight, then answer everything
	// correctly.
	missed := map[string]bool{}
	counts := map[string]int{}
	for i := 0; i < 100 && e.State() != Done; i++ {
		cur := e.Current()
		counts[cur.ID]++
		if len(missed) < 2 && counts[cur.ID] == 1 {
			missed[cur.ID] = true
			answerCurrent(e, false)
		} else {
			answerCurrent(e, true)
		}
		e.Next()
	}

	t.Logf("appearance counts: %v (missed=%v)", counts, missed)

	if e.State() != Done {
		t.Fatal("session should complete once every card meets its criterion")
	}
	// A missed review owes 2 recalls after the miss: 1 miss + 2 corrects = 3.
	// An intact review is recalled once.
	for _, c := range d.Cards {
		want := 1
		if missed[c.ID] {
			want = 3
		}
		if counts[c.ID] != want {
			t.Errorf("card %s appeared %d times, want %d", c.ID, counts[c.ID], want)
		}
	}
}
