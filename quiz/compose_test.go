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
