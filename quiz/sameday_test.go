package quiz

import (
	"testing"
	"time"

	"study/deck"
)

// sameDayDeck builds an adaptive deck of n typed cards backed by a store
// where extra cards (outside the session) were studied at last — the shape of
// relaunching a deck whose day's batch is done: the session composes to fresh
// cards only, while the store remembers today's work.
func sameDaySession(t *testing.T, n int, last time.Time) *Engine {
	t.Helper()
	full := testDeck(n + 1)
	full.Order = deck.OrderAdaptive
	store := newTestStore(t)
	// The extra card carries today's history and a future review, so it is
	// excluded from the composed session.
	extra := full.Cards[n].ID
	store.RecordCorrect(extra)
	cp := store.Get(extra)
	cp.LastSeen = last
	cp.Due = time.Now().Add(24 * time.Hour)

	session := *full
	session.Cards = session.Cards[:n] // what Compose serves: the fresh cards
	return NewEngine(&session, full.Cards, store)
}

// TestSameDayAllNewLaunchOpensCaughtUp: relaunching a deck already studied
// today, with nothing due and only new cards to serve, lands on the
// caught-up notice instead of quizzing — a second daily batch is a choice,
// not an accident. Continue then serves the queued batch as composed.
func TestSameDayAllNewLaunchOpensCaughtUp(t *testing.T) {
	e := sameDaySession(t, 3, time.Now())

	if e.State() != CaughtUp {
		t.Fatalf("State = %v, want CaughtUp on a same-day all-new launch", e.State())
	}
	if e.Remaining() != 3 {
		t.Errorf("Remaining = %d, want the queued batch of 3", e.Remaining())
	}

	e.ContinueAll()
	if e.State() != ShowQuestion {
		t.Fatalf("State after continue = %v, want ShowQuestion", e.State())
	}
	if e.Remaining() != 3 {
		t.Errorf("Remaining after continue = %d, want 3 — continue must serve the queue, not re-seed it", e.Remaining())
	}
}

// TestYesterdayAllNewLaunchQuizzes: the notice is a same-day gate only — the
// next day's batch starts immediately.
func TestYesterdayAllNewLaunchQuizzes(t *testing.T) {
	e := sameDaySession(t, 3, time.Now().Add(-24*time.Hour))
	if e.State() != ShowQuestion {
		t.Fatalf("State = %v, want ShowQuestion for yesterday's history", e.State())
	}
}

// TestFirstEverLaunchQuizzes: a never-studied deck has no "already studied
// today" to warn about.
func TestFirstEverLaunchQuizzes(t *testing.T) {
	d := testDeck(3)
	d.Order = deck.OrderAdaptive
	e := NewEngine(d, nil, newTestStore(t))
	if e.State() != ShowQuestion {
		t.Fatalf("State = %v, want ShowQuestion on first launch", e.State())
	}
}

// TestDueReviewsLaunchQuizzes: due reviews start immediately even on a
// same-day relaunch — they are the scheduled work.
func TestDueReviewsLaunchQuizzes(t *testing.T) {
	full := testDeck(2)
	full.Order = deck.OrderAdaptive
	store := newTestStore(t)
	id := full.Cards[0].ID
	store.RecordCorrect(id) // studied today, due immediately (zero Due)
	e := NewEngine(full, nil, store)
	if e.State() != ShowQuestion {
		t.Fatalf("State = %v, want ShowQuestion with a due review", e.State())
	}
}

