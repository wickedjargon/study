package quiz

import (
	"study/deck"
	"testing"
)

// TestSequentialLoops: deck order, wrapping to the top, forever.
func TestSequentialLoops(t *testing.T) {
	d := testDeck(3)
	d.Order = deck.OrderSequential
	e := NewEngine(d, nil, nil)

	want := []string{"alpha", "beta", "gamma", "alpha", "beta", "gamma", "alpha"}
	for i, id := range want {
		if got := e.Current().ID; got != id {
			t.Fatalf("serve %d: got %s, want %s", i, got, id)
		}
		e.AnswerTyped(e.Current().AnswerText)
		e.Next()
	}
}

// TestSequentialMissKeepsOrder: a missed card requeues at the tail of the
// cycle; the lap resumes where it was for everyone else.
func TestSequentialMissKeepsOrder(t *testing.T) {
	d := testDeck(3)
	d.Order = deck.OrderSequential
	e := NewEngine(d, nil, nil)

	// Miss alpha: it rejoins behind gamma.
	e.AnswerTyped("definitely-wrong")
	e.Next()

	want := []string{"beta", "gamma", "alpha", "beta", "gamma"}
	for i, id := range want {
		if got := e.Current().ID; got != id {
			t.Fatalf("serve %d after miss: got %s, want %s", i, got, id)
		}
		e.AnswerTyped(e.Current().AnswerText)
		e.Next()
	}
}

// TestFlipThrough: no quizzing — cards are presented answer-visible in deck
// order, wrap at the end, and nothing is recorded.
func TestFlipThrough(t *testing.T) {
	d := testDeck(3)
	d.Order = deck.OrderFlipThrough
	store := newTestStore(t)
	e := NewEngine(d, nil, store)

	if e.State() != ShowPreview {
		t.Fatalf("state = %v, want ShowPreview", e.State())
	}
	if r := e.AnswerTyped("x"); r != nil {
		t.Error("flip-through must not accept answers")
	}

	want := []string{"alpha", "beta", "gamma", "alpha", "beta", "gamma", "alpha"}
	for i, id := range want {
		if got := e.Current().ID; got != id {
			t.Fatalf("view %d: got %s, want %s", i, got, id)
		}
		if e.State() != ShowPreview {
			t.Fatalf("view %d: state = %v, want ShowPreview", i, e.State())
		}
		e.ConfirmPreview()
	}
	if e.TotalSeen != len(want) {
		t.Errorf("TotalSeen = %d, want %d", e.TotalSeen, len(want))
	}

	if c, w, n := store.Summary(); c+w+n != 0 {
		t.Errorf("flip-through recorded progress: correct=%d wrong=%d cards=%d", c, w, n)
	}
}
