package quiz

import (
	"testing"
	"time"

	"study/deck"
	"study/progress"
)

// scheduledStore returns a store where every card of a confusableDeck(n) has
// history and a review scheduled at due — a fully caught-up deck when due is
// in the future.
func scheduledStore(t *testing.T, n int, level int, due time.Time) *progress.Store {
	t.Helper()
	store, err := progress.NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	for i := 0; i < n; i++ {
		id := confusableDeck(n).Cards[i].ID
		store.RecordCorrect(id)
		cp := store.Get(id)
		cp.Level = level
		cp.Due = due
	}
	return store
}

// TestEmptyAdaptiveSessionStartsCaughtUp: an adaptive session with nothing to
// serve opens on the caught-up screen, not the empty summary — the session
// hasn't happened yet, so there is nothing to summarize.
func TestEmptyAdaptiveSessionStartsCaughtUp(t *testing.T) {
	full := confusableDeck(3)
	session := *full
	session.Cards = nil // what main composes when nothing is due
	e := NewEngine(&session, full.Cards, nil)

	if e.State() != CaughtUp {
		t.Fatalf("State = %v, want CaughtUp", e.State())
	}
	if e.Current() != nil {
		t.Errorf("Current = %v, want nil before the pass starts", e.Current())
	}
}

// TestEmptySequentialSessionStaysDone: only the adaptive order has a
// caught-up state; other orders with no cards are simply over.
func TestEmptySequentialSessionStaysDone(t *testing.T) {
	d := confusableDeck(0)
	d.Order = deck.OrderSequential
	e := NewEngine(d, nil, nil)
	if e.State() != Done {
		t.Fatalf("State = %v, want Done", e.State())
	}
}

// TestContinueAllServesWholeDeckAhead: continuing from caught-up runs a full
// pass — every scheduled card is served, flagged ahead, and a clean pass
// leaves every schedule untouched (early recalls are no evidence).
func TestContinueAllServesWholeDeckAhead(t *testing.T) {
	const n = 3
	due := time.Now().Add(48 * time.Hour)
	store := scheduledStore(t, n, 4, due)

	full := confusableDeck(n)
	session := *full
	session.Cards = nil
	e := NewEngine(&session, full.Cards, store)
	if e.State() != CaughtUp {
		t.Fatalf("State = %v, want CaughtUp", e.State())
	}

	e.ContinueAll()
	if e.State() != ShowQuestion {
		t.Fatalf("State after ContinueAll = %v, want ShowQuestion", e.State())
	}

	served := 0
	for e.State() != Done {
		if served > 10*n {
			t.Fatal("pass did not complete")
		}
		if !e.CurrentIsAhead() {
			t.Errorf("card %s served in a caught-up pass should be ahead", e.Current().ID)
		}
		answerCurrent(e, true)
		e.Next()
		served++
	}
	if served != n { // review criterion is 1 recall per card
		t.Errorf("served %d cards, want %d", served, n)
	}
	for i := 0; i < n; i++ {
		cp := store.Get(confusableDeck(n).Cards[i].ID)
		if cp.Level != 4 || !cp.Due.Equal(due) {
			t.Errorf("card %d schedule moved (level %d, due %v); a clean ahead pass must not touch it", i, cp.Level, cp.Due)
		}
	}
}

// TestContinueAllRepeatsIndefinitely: each completed pass can be followed by
// another — the summary's "keep studying" never runs out.
func TestContinueAllRepeatsIndefinitely(t *testing.T) {
	const n = 2
	store := scheduledStore(t, n, 4, time.Now().Add(48*time.Hour))
	full := confusableDeck(n)
	session := *full
	session.Cards = nil
	e := NewEngine(&session, full.Cards, store)

	for pass := 0; pass < 3; pass++ {
		e.ContinueAll()
		for i := 0; e.State() != Done; i++ {
			if i > 10*n {
				t.Fatalf("pass %d did not complete", pass)
			}
			answerCurrent(e, true)
			e.Next()
		}
	}
	if e.TotalSeen != 3*n {
		t.Errorf("TotalSeen = %d, want %d across three passes", e.TotalSeen, 3*n)
	}
}

// TestContinueAllMissStillCounts: a lapse during an ahead pass is real
// evidence — forgetting before the due date — and reschedules the card, same
// as the --ahead flag's semantics.
func TestContinueAllMissStillCounts(t *testing.T) {
	due := time.Now().Add(48 * time.Hour)
	store := scheduledStore(t, 1, 4, due)
	full := confusableDeck(1)
	session := *full
	session.Cards = nil
	e := NewEngine(&session, full.Cards, store)

	e.ContinueAll()
	answerCurrent(e, false)
	e.Next()
	for i := 0; i < 10 && e.State() != Done; i++ {
		answerCurrent(e, true)
		e.Next()
	}
	if e.State() != Done {
		t.Fatal("pass should complete")
	}
	cp := store.Get("ans0")
	if cp.Level != 2 { // lapse halves level 4 → 2
		t.Errorf("Level after lapsed ahead pass = %d, want 2", cp.Level)
	}
}

// TestContinueAllServesDueAndNewFirst: a pass launched from the summary of an
// early-ended session still owes due reviews and new cards — they come before
// the ahead cards, and keep their honest flags.
func TestContinueAllServesDueAndNewFirst(t *testing.T) {
	// Three cards: ans0 due now, ans1 never studied, ans2 scheduled ahead.
	store, err := progress.NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	store.RecordCorrect("ans0")
	store.Get("ans0").Due = time.Now().Add(-time.Hour)
	store.RecordCorrect("ans2")
	store.Get("ans2").Due = time.Now().Add(48 * time.Hour)

	full := confusableDeck(3)
	session := *full
	session.Cards = nil
	session.NewPerSession = -1 // no cap; batching has its own test
	e := NewEngine(&session, full.Cards, store)

	e.ContinueAll()
	first, second := e.Current().ID, ""
	if first != "ans0" {
		t.Errorf("first served = %s, want the due review ans0", first)
	}
	if e.CurrentIsAhead() {
		t.Error("a genuinely due card must not be flagged ahead")
	}
	answerCurrent(e, true)
	e.Next()
	second = e.Current().ID
	if second != "ans1" {
		t.Errorf("second served = %s, want the new card ans1", second)
	}
	if !e.CurrentIsNew() {
		t.Error("a never-studied card should be flagged new")
	}
}

// studiedCount reports how many of the deck's n cards have recorded history.
func studiedCount(store *progress.Store, n int) int {
	count := 0
	for i := 0; i < n; i++ {
		cp := store.Get(confusableDeck(n).Cards[i].ID)
		if cp.TimesCorrect+cp.TimesWrong > 0 {
			count++
		}
	}
	return count
}

// completePass answers every card correctly until the pass is done.
func completePass(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; e.State() != Done; i++ {
		if i > limit {
			t.Fatal("pass did not complete")
		}
		answerCurrent(e, true)
		e.Next()
	}
}

// TestContinueAllBatchesNewCards: each pass introduces at most the deck's
// new-per-session count of never-studied cards — the launch composition's
// pacing — and the next pass brings the next batch, until the deck runs out.
func TestContinueAllBatchesNewCards(t *testing.T) {
	const n, batch = 5, 2
	store, err := progress.NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	full := confusableDeck(n)
	session := *full
	session.Cards = nil
	session.NewPerSession = batch
	e := NewEngine(&session, full.Cards, store)

	// Pass 1: one batch of new cards, nothing else to serve.
	e.ContinueAll()
	completePass(t, e, 10*n)
	if got := studiedCount(store, n); got != batch {
		t.Fatalf("after pass 1: %d cards studied, want one batch of %d", got, batch)
	}

	// Pass 2: the next batch, plus the first batch again (now ahead).
	e.ContinueAll()
	completePass(t, e, 10*n)
	if got := studiedCount(store, n); got != 2*batch {
		t.Fatalf("after pass 2: %d cards studied, want %d", got, 2*batch)
	}

	// Pass 3: the last new card joins; the whole deck is now in rotation.
	e.ContinueAll()
	completePass(t, e, 10*n)
	if got := studiedCount(store, n); got != n {
		t.Fatalf("after pass 3: %d cards studied, want all %d", got, n)
	}
}

// TestContinueAllRespectsZeroCap: new-per-session 0 means "never introduce
// new material" — a continue pass over a deck with nothing but new cards
// serves nothing rather than overriding the setting.
func TestContinueAllRespectsZeroCap(t *testing.T) {
	full := confusableDeck(2)
	session := *full
	session.Cards = nil // NewPerSession stays 0
	e := NewEngine(&session, full.Cards, nil)

	e.ContinueAll()
	if e.State() != Done {
		t.Fatalf("State = %v, want Done: cap 0 leaves nothing to serve", e.State())
	}
}

// TestContinueAllNoOpOutsideAdaptive: the caught-up pass belongs to the
// adaptive order; other orders ignore it.
func TestContinueAllNoOpOutsideAdaptive(t *testing.T) {
	d := confusableDeck(2)
	d.Order = deck.OrderSequential
	e := NewEngine(d, nil, nil)
	before := e.Remaining()
	e.ContinueAll()
	if e.Remaining() != before {
		t.Errorf("Remaining changed %d → %d; ContinueAll must be a no-op outside adaptive", before, e.Remaining())
	}
}
