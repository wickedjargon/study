package quiz

import (
	"testing"
	"time"

	"study/deck"
)

func TestSplitDue(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	cards := []deck.Card{{ID: "new"}, {ID: "overdue"}, {ID: "verylate"}, {ID: "ahead"}}

	// overdue: due an hour ago; verylate: due a day ago; ahead: due tomorrow.
	store.RecordCorrect("overdue")
	store.Get("overdue").Due = now.Add(-time.Hour)
	store.RecordCorrect("verylate")
	store.Get("verylate").Due = now.Add(-24 * time.Hour)
	store.RecordCorrect("ahead")
	store.Get("ahead").Due = now.Add(24 * time.Hour)

	reviews, fresh, future, nextDue := SplitDue(cards, store, now)

	// Reviews: both due cards, most overdue first. The future card is out.
	if len(reviews) != 2 || reviews[0].ID != "verylate" || reviews[1].ID != "overdue" {
		t.Errorf("reviews = %v, want [verylate overdue]", reviews)
	}
	if len(fresh) != 1 || fresh[0].ID != "new" {
		t.Errorf("fresh = %v, want [new]", fresh)
	}
	if len(future) != 1 || future[0].ID != "ahead" {
		t.Errorf("future = %v, want [ahead]", future)
	}
	if nextDue.IsZero() || !nextDue.Equal(store.Get("ahead").Due) {
		t.Errorf("nextDue = %v, want ahead's due time", nextDue)
	}
}

func TestComposeAdaptive(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	d := &deck.Deck{
		Order:         deck.OrderAdaptive,
		NewPerSession: 1,
		Cards:         []deck.Card{{ID: "new1"}, {ID: "new2"}, {ID: "due"}, {ID: "ahead"}},
	}
	store.RecordCorrect("due")
	store.Get("due").Due = now.Add(-time.Hour)
	store.RecordCorrect("ahead")
	store.Get("ahead").Due = now.Add(24 * time.Hour)

	Compose(d, store, now)

	// One due review first, then the fresh batch capped at NewPerSession;
	// the ahead-of-schedule card stays out.
	if len(d.Cards) != 2 {
		t.Fatalf("composed %d cards, want 2: %v", len(d.Cards), d.Cards)
	}
	if d.Cards[0].ID != "due" {
		t.Errorf("first card = %s, want the due review", d.Cards[0].ID)
	}
	if id := d.Cards[1].ID; id != "new1" && id != "new2" {
		t.Errorf("second card = %s, want a fresh card", id)
	}
}

// TestSequentialResume: composition rotates the lap to start just after the
// most recently answered card, so quitting mid-lap and relaunching continues
// where the last session left off.
func TestSequentialResume(t *testing.T) {
	store := newTestStore(t)
	d := testDeck(5)
	d.Order = deck.OrderSequential

	// Cards 0..2 answered in order; card 2 is the most recent.
	for i := 0; i <= 2; i++ {
		store.RecordCorrect(d.Cards[i].ID)
	}

	Compose(d, store, time.Now())
	if d.Cards[0].ID != "delta" { // testDeck answers: alpha..epsilon; index 3
		t.Fatalf("resume start = %s, want delta (the card after the last answered)", d.Cards[0].ID)
	}
	// The lap wraps: all five cards still present, order preserved.
	if len(d.Cards) != 5 || d.Cards[4].ID != "gamma" {
		t.Fatalf("rotated lap malformed: %v", []string{d.Cards[0].ID, d.Cards[1].ID, d.Cards[2].ID, d.Cards[3].ID, d.Cards[4].ID})
	}
}

// TestSequentialResumeFreshAndFinished: a never-studied deck starts at the
// top, and so does one whose last answer was the final card.
func TestSequentialResumeFreshAndFinished(t *testing.T) {
	store := newTestStore(t)
	d := testDeck(3)
	d.Order = deck.OrderSequential
	Compose(d, store, time.Now())
	if d.Cards[0].ID != "alpha" {
		t.Fatalf("fresh deck starts at %s, want alpha", d.Cards[0].ID)
	}

	store.RecordCorrect(d.Cards[2].ID) // last card answered most recently
	d2 := testDeck(3)
	d2.Order = deck.OrderSequential
	Compose(d2, store, time.Now())
	if d2.Cards[0].ID != "alpha" {
		t.Fatalf("finished lap resumes at %s, want alpha (wrap to top)", d2.Cards[0].ID)
	}
}
