package quiz

import (
	"fmt"
	"testing"

	"study/deck"
	"study/progress"
)

// confusableDeck builds a type-mode deck of n cards with answers ans0..ansN.
func confusableDeck(n int) *deck.Deck {
	cards := make([]deck.Card, n)
	for i := range cards {
		ans := fmt.Sprintf("ans%d", i)
		cards[i] = deck.Card{
			ID:         ans,
			Question:   []deck.Media{{Type: deck.Text, Content: "What is " + ans + "?"}},
			Answer:     []deck.Media{{Type: deck.Text, Content: ans}},
			AnswerText: ans,
			Mode:       deck.ModeType,
		}
	}
	return &deck.Deck{Name: "confusable", Choices: 4, Cards: cards}
}

// TestTypedConfusionDetected: typing another card's answer sets ConfusedWith
// to that card; typing an unrelated wrong answer sets nothing.
func TestTypedConfusionDetected(t *testing.T) {
	d := confusableDeck(4)
	e := NewEngine(d, nil, nil)

	other := "ans2"
	if e.Current().ID == other {
		other = "ans3"
	}
	r := e.AnswerTyped(other)
	if r.Correct {
		t.Fatal("another card's answer must not be correct")
	}
	if r.ConfusedWith == nil || r.ConfusedWith.ID != other {
		t.Errorf("ConfusedWith = %v, want card %q", r.ConfusedWith, other)
	}

	e.Next()
	r = e.AnswerTyped("no card has this answer")
	if r.ConfusedWith != nil {
		t.Errorf("ConfusedWith = %q for an unrelated wrong answer, want nil", r.ConfusedWith.ID)
	}
}

// TestConfusionMatchesAcceptedAlternative: a card's "= " alternatives count as
// its answer for confusion detection, with the usual lenient normalization.
func TestConfusionMatchesAcceptedAlternative(t *testing.T) {
	d := confusableDeck(3)
	d.Cards[2].Accept = []string{"the third"}
	e := NewEngine(d, nil, nil)

	// Current is ans0 (deck order); "The Third!" normalizes to ans2's accept.
	r := e.AnswerTyped("The Third!")
	if r.ConfusedWith == nil || r.ConfusedWith.ID != "ans2" {
		t.Errorf("ConfusedWith = %v, want ans2 via accepted alternative", r.ConfusedWith)
	}
}

// TestChoiceConfusionDetected: in choice mode, picking a distractor that is
// another card's answer is the same confusion signal as typing it.
func TestChoiceConfusionDetected(t *testing.T) {
	d := testDeck(4) // choice mode, distractors drawn from other cards' answers
	e := NewEngine(d, nil, nil)

	opts := e.Options()
	wrong := -1
	for i, o := range opts {
		if o != e.Current().AnswerText {
			wrong = i
			break
		}
	}
	r := e.Answer(wrong)
	// testDeck card IDs equal their answers, so the picked option names the
	// card it belongs to.
	if r.ConfusedWith == nil || r.ConfusedWith.ID != opts[wrong] {
		t.Errorf("ConfusedWith = %v, want card %q", r.ConfusedWith, opts[wrong])
	}
}

// TestConfusedCardPulledForward: the confused-with card jumps its queue
// position to land near the missed card, instead of waiting its original turn.
func TestConfusedCardPulledForward(t *testing.T) {
	const n = 12
	d := confusableDeck(n)
	e := NewEngine(d, nil, nil)

	// Card 0 is current; confuse it with the last card in the queue.
	last := fmt.Sprintf("ans%d", n-1)
	r := e.AnswerTyped(last)
	if r.ConfusedWith == nil || r.ConfusedWith.ID != last {
		t.Fatalf("ConfusedWith = %v, want %q", r.ConfusedWith, last)
	}
	e.Next()

	// Without the pull the confused card would be the (n-1)th serve from here;
	// pulled to gapConfused it must show up well before that. Cards between it
	// and the front (including the missed card's own return) still intervene.
	for i := 1; i <= gapConfused+3; i++ {
		if e.Current().ID == last {
			if i <= gapConfused-1 {
				t.Fatalf("confused card returned after only %d serves — massed, not spaced", i)
			}
			return
		}
		answerCurrent(e, true)
		e.Next()
	}
	t.Fatalf("confused card not served within %d serves of the miss", gapConfused+3)
}

// TestNoIdeaIsAMissWithoutConfusion: declining to answer (choice mode's
// opt-out for a blind guess) scores as a plain miss on the current card —
// lapsed, requeued — but names no other card and pulls nothing forward.
func TestNoIdeaIsAMissWithoutConfusion(t *testing.T) {
	d := confusableDeck(4)
	e := NewEngine(d, nil, nil)

	cur := e.Current().ID
	r := e.AnswerNoIdea()
	if r == nil {
		t.Fatal("AnswerNoIdea returned nil at a question")
	}
	if r.Correct || !r.NoIdea || r.Chosen != -1 {
		t.Errorf("result = {Correct:%v NoIdea:%v Chosen:%d}, want a wrong no-idea result", r.Correct, r.NoIdea, r.Chosen)
	}
	if r.ConfusedWith != nil {
		t.Errorf("ConfusedWith = %q, want nil — declining names no other card", r.ConfusedWith.ID)
	}
	if e.TotalSeen != 1 || e.TotalWrong != 1 {
		t.Errorf("TotalSeen/TotalWrong = %d/%d, want 1/1", e.TotalSeen, e.TotalWrong)
	}
	if !e.lapsed[cur] {
		t.Error("card not marked lapsed — no idea must count as a miss")
	}
	if e.AnswerNoIdea() != nil {
		t.Error("AnswerNoIdea at ShowResult returned a result, want nil")
	}

	// The missed card comes back like any other miss: soon, but not massed.
	e.Next()
	for i := 1; i <= gapAfterMiss+3; i++ {
		if e.Current().ID == cur {
			if i == 1 {
				t.Fatal("missed card returned immediately — massed, not spaced")
			}
			return
		}
		answerCurrent(e, true)
		e.Next()
	}
	t.Fatalf("missed card not served within %d serves", gapAfterMiss+3)
}

// TestConfusionDetectedOutsideSession: the confused-with card may be excluded
// from the session entirely (not due, not weak) — it is still named on the
// result screen, but never pulled in: its review schedule is not the session's
// business.
func TestConfusionDetectedOutsideSession(t *testing.T) {
	full := confusableDeck(4)
	d := *full
	d.Cards = full.Cards[:2] // session sees only ans0, ans1
	e := NewEngine(&d, full.Cards, nil)

	r := e.AnswerTyped("ans3")
	if r.ConfusedWith == nil || r.ConfusedWith.ID != "ans3" {
		t.Fatalf("ConfusedWith = %v, want out-of-session ans3", r.ConfusedWith)
	}
	e.Next()

	for i := 0; i < 100 && e.State() != Done; i++ {
		if e.Current().ID == "ans3" {
			t.Fatal("out-of-session card was served")
		}
		answerCurrent(e, true)
		e.Next()
	}
	if e.State() != Done {
		t.Fatal("session never completed")
	}
}

// TestCurrentIsNew: the "new this session" label comes from history at session
// start and sticks for the whole session, even after answers are recorded.
func TestCurrentIsNew(t *testing.T) {
	store, err := progress.NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	store.RecordCorrect("ans1") // ans1 has history, ans0/ans2 are new
	e := NewEngine(confusableDeck(3), nil, store)

	for i := 0; i < 20 && e.State() != Done; i++ {
		want := e.Current().ID != "ans1"
		if got := e.CurrentIsNew(); got != want {
			t.Fatalf("serve %d (%s): CurrentIsNew = %v, want %v", i, e.Current().ID, got, want)
		}
		answerCurrent(e, true)
		e.Next()
	}
	if e.State() != Done {
		t.Fatal("session never completed")
	}
}

// TestConfusionDoesNotReaddCompletedCard: a confused-with card that already
// met its session criterion is named on the result screen but NOT pulled back
// into the session — that would collapse its spacing.
func TestConfusionDoesNotReaddCompletedCard(t *testing.T) {
	store, err := progress.NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	d := confusableDeck(3)
	// ans1 has history, so it's a review card needing a single recall.
	store.RecordCorrect("ans1")
	e := NewEngine(d, nil, store)

	// Serve ans0 correct, then ans1 correct — ans1 meets its criterion of 1
	// and leaves the session.
	answerCurrent(e, true)
	e.Next()
	if e.Current().ID != "ans1" {
		t.Fatalf("expected ans1 second, got %s", e.Current().ID)
	}
	answerCurrent(e, true)
	e.Next()

	// Confuse the next card with the completed ans1.
	r := e.AnswerTyped("ans1")
	if r.ConfusedWith == nil || r.ConfusedWith.ID != "ans1" {
		t.Fatalf("ConfusedWith = %v, want ans1", r.ConfusedWith)
	}
	e.Next()

	// ans1 must never be served again this session.
	for i := 0; i < 100 && e.State() != Done; i++ {
		if e.Current().ID == "ans1" {
			t.Fatal("completed card was dragged back into the session")
		}
		answerCurrent(e, true)
		e.Next()
	}
	if e.State() != Done {
		t.Fatal("session never completed")
	}
}
