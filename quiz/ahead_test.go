package quiz

import (
	"testing"
	"time"

	"study/progress"
)

// aheadStore returns a store where the named card has history and a review
// scheduled in the future — an ahead-of-schedule card if studied now.
func aheadStore(t *testing.T, id string, level int, due time.Time) *progress.Store {
	t.Helper()
	store, err := progress.NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	store.RecordCorrect(id)
	cp := store.Get(id)
	cp.Level = level
	cp.Due = due
	return store
}

// TestAheadCleanReviewKeepsSchedule: completing an ahead-of-schedule card
// without a miss must not touch its ladder or due date — easy early recalls
// are no evidence, and rescheduling on them would inflate the intervals.
func TestAheadCleanReviewKeepsSchedule(t *testing.T) {
	due := time.Now().Add(48 * time.Hour)
	store := aheadStore(t, "ans0", 4, due)
	e := NewEngine(confusableDeck(1), nil, store)

	if !e.CurrentIsAhead() {
		t.Fatal("card scheduled in the future should be CurrentIsAhead")
	}
	answerCurrent(e, true) // review criterion is 1 recall — card completes
	e.Next()
	if e.State() != Done {
		t.Fatal("session should complete")
	}

	cp := store.Get("ans0")
	if cp.Level != 4 {
		t.Errorf("Level after clean ahead review = %d, want unchanged 4", cp.Level)
	}
	if !cp.Due.Equal(due) {
		t.Errorf("Due after clean ahead review = %v, want unchanged %v", cp.Due, due)
	}
}

// TestAheadLapsedReviewReschedules: missing an ahead-of-schedule card is real
// evidence — forgetting before the due date means the interval was too long —
// so the lapse drops the ladder exactly as an on-time miss would.
func TestAheadLapsedReviewReschedules(t *testing.T) {
	due := time.Now().Add(48 * time.Hour)
	store := aheadStore(t, "ans0", 4, due)
	e := NewEngine(confusableDeck(1), nil, store)

	answerCurrent(e, false)
	e.Next()
	for i := 0; i < 10 && e.State() != Done; i++ {
		answerCurrent(e, true)
		e.Next()
	}
	if e.State() != Done {
		t.Fatal("session should complete")
	}

	cp := store.Get("ans0")
	if cp.Level != 2 { // lapse halves level 4 → 2
		t.Errorf("Level after lapsed ahead review = %d, want 2", cp.Level)
	}
	// Rescheduled onto the halved rung: 3 days out (ladder level 2), fuzzed
	// to 2-4 days and anchored to the start of that local day.
	dayAt := func(n int) time.Time {
		y, m, d := time.Now().AddDate(0, 0, n).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}
	if cp.Due.Before(dayAt(2)) || cp.Due.After(dayAt(4)) {
		t.Errorf("Due after lapsed ahead review = %v, want a day start 2-4 days out", cp.Due)
	}
}

// TestDueAndNewCardsAreNotAhead: cards genuinely due (or never studied) are
// not ahead — their completions advance the ladder as usual.
func TestDueAndNewCardsAreNotAhead(t *testing.T) {
	store, err := progress.NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	store.RecordCorrect("ans0")
	store.Get("ans0").Due = time.Now().Add(-time.Hour) // overdue
	e := NewEngine(confusableDeck(2), nil, store)      // ans1 is new

	for e.State() != Done {
		if e.CurrentIsAhead() {
			t.Fatalf("%s should not be ahead", e.Current().ID)
		}
		answerCurrent(e, true)
		e.Next()
	}
	if store.Get("ans0").Level == 0 || store.Get("ans0").Due.Before(time.Now()) {
		t.Error("due card's clean completion should advance its schedule")
	}
}
