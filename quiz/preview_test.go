package quiz

import "testing"

// TestPreviewFirstViewing: with "# preview-new: on", every never-answered card is
// first revealed (ShowPreview); confirming quizzes that same card, and the
// next fresh card is revealed in turn.
func TestPreviewFirstViewing(t *testing.T) {
	d := testDeck(2)
	d.Preview = true
	e := NewEngine(d, nil)

	if e.State() != ShowPreview {
		t.Fatalf("state = %v, want ShowPreview", e.State())
	}
	first := e.Current()

	// Answers are rejected while the reveal is up.
	if r := e.Answer(0); r != nil {
		t.Error("Answer during preview should return nil")
	}
	if r := e.AnswerTyped("x"); r != nil {
		t.Error("AnswerTyped during preview should return nil")
	}

	e.ConfirmPreview()
	if e.State() != ShowQuestion {
		t.Fatalf("state after confirm = %v, want ShowQuestion", e.State())
	}
	if e.Current() != first {
		t.Error("confirm must quiz the same card, not advance")
	}

	// Answer correctly; the next fresh card gets its own reveal.
	if r := e.AnswerTyped(first.AnswerText); r == nil || !r.Correct {
		t.Fatalf("expected correct answer, got %+v", r)
	}
	e.Next()
	if e.State() != ShowPreview {
		t.Fatalf("second card: state = %v, want ShowPreview", e.State())
	}
	if e.Current() == first {
		t.Error("expected a different card after answering the first")
	}
}

// TestPreviewOnlyOnce: a card is revealed exactly once — its retry drills and
// later requeued appearances are quizzed directly.
func TestPreviewOnlyOnce(t *testing.T) {
	d := testDeck(1)
	d.Preview = true
	e := NewEngine(d, nil)

	if e.State() != ShowPreview {
		t.Fatalf("state = %v, want ShowPreview", e.State())
	}
	e.ConfirmPreview()

	// Miss it (immediate repeat + retry drill), then answer correctly for a
	// while; the reveal must never come back.
	e.AnswerTyped("definitely-wrong")
	for i := 0; i < 20 && e.State() == ShowResult; i++ {
		e.Next()
		if e.State() == ShowPreview {
			t.Fatalf("iteration %d: card revealed again after being answered", i)
		}
		e.AnswerTyped(d.Cards[0].AnswerText)
	}
}

// TestPreviewSkipsStudiedCards: a card with recorded history is quizzed
// directly — only genuinely never-seen cards are revealed.
func TestPreviewSkipsStudiedCards(t *testing.T) {
	d := testDeck(2)
	d.Preview = true
	store := newTestStore(t)
	store.RecordCorrect(d.Cards[0].ID) // card 0 studied before, card 1 never

	e := NewEngine(d, store)
	if e.State() != ShowQuestion {
		t.Fatalf("studied card: state = %v, want ShowQuestion", e.State())
	}
	// PrioritizeCards is not in play here (NewEngine keeps deck order), so the
	// studied card is served first and the fresh one second.
	if e.Current() != &d.Cards[0] {
		t.Fatal("expected the studied card first (deck order)")
	}

	e.AnswerTyped(e.Current().AnswerText)
	e.Next()
	if e.State() != ShowPreview {
		t.Fatalf("fresh card: state = %v, want ShowPreview", e.State())
	}
}

// TestPreviewOffByDefault: without the directive/flag nothing is revealed.
func TestPreviewOffByDefault(t *testing.T) {
	e := NewEngine(testDeck(2), nil)
	if e.State() != ShowQuestion {
		t.Fatalf("state = %v, want ShowQuestion with preview off", e.State())
	}
	e.ConfirmPreview() // no-op outside ShowPreview
	if e.State() != ShowQuestion {
		t.Fatal("ConfirmPreview outside a preview must be a no-op")
	}
}
