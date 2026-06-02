package quiz

import (
	"study/deck"
	"testing"
)

func testDeck(n int) *deck.Deck {
	cards := make([]deck.Card, n)
	answers := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	for i := 0; i < n; i++ {
		ans := answers[i%len(answers)]
		cards[i] = deck.Card{
			ID: ans,
			Question: []deck.Media{
				{Type: deck.Text, Content: "What is " + ans + "?"},
			},
			Answer: []deck.Media{
				{Type: deck.Text, Content: ans},
			},
			AnswerText: ans,
		}
	}
	return &deck.Deck{
		Name:    "test",
		Choices: 4,
		Cards:   cards,
	}
}

func TestEngineBasicFlow(t *testing.T) {
	d := testDeck(4)
	e := NewEngine(d, false, 0, nil)

	if e.State() != ShowQuestion {
		t.Fatal("expected ShowQuestion state")
	}
	if e.Current() == nil {
		t.Fatal("expected a current card")
	}

	opts := e.Options()
	if len(opts) != 4 {
		t.Fatalf("expected 4 options, got %d", len(opts))
	}
}

func TestEngineCorrectAnswer(t *testing.T) {
	d := testDeck(2)
	e := NewEngine(d, false, 0, nil)

	// Find the correct answer index.
	opts := e.Options()
	correct := -1
	for i, o := range opts {
		if o == e.Current().AnswerText {
			correct = i
			break
		}
	}
	if correct == -1 {
		t.Fatal("correct answer not in options")
	}

	result := e.Answer(correct)
	if result == nil {
		t.Fatal("expected a result")
	}
	if !result.Correct {
		t.Error("expected correct result")
	}
	if e.State() != ShowResult {
		t.Error("expected ShowResult state")
	}
	if e.TotalCorrect != 1 {
		t.Errorf("expected 1 correct, got %d", e.TotalCorrect)
	}
}

func TestEngineWrongAnswerCreatesRetry(t *testing.T) {
	d := testDeck(4)
	e := NewEngine(d, false, 0, nil)

	// Find a wrong answer index.
	opts := e.Options()
	wrong := -1
	for i, o := range opts {
		if o != e.Current().AnswerText {
			wrong = i
			break
		}
	}

	result := e.Answer(wrong)
	if result.Correct {
		t.Error("expected wrong result")
	}
	if len(e.retry) != 1 {
		t.Errorf("expected 1 card in retry queue, got %d", len(e.retry))
	}
}

func TestEngineCorrectAnswerCycle(t *testing.T) {
	d := testDeck(3)
	e := NewEngine(d, false, 0, nil)

	// Answer each card correctly once (3 cards).
	for i := 0; i < 3; i++ {
		if e.State() != ShowQuestion {
			t.Fatalf("round %d: expected ShowQuestion, got %d", i, e.State())
		}
		opts := e.Options()
		for j, o := range opts {
			if o == e.Current().AnswerText {
				e.Answer(j)
				break
			}
		}
		e.Next()
	}

	if e.TotalSeen != 3 {
		t.Errorf("expected 3 seen, got %d", e.TotalSeen)
	}
	if e.TotalCorrect != 3 {
		t.Errorf("expected 3 correct, got %d", e.TotalCorrect)
	}
	// Session should still be active (continuous mode).
	if e.State() == Done {
		t.Error("session should not be Done in continuous mode")
	}
}

func TestEngineRetryGraduation(t *testing.T) {
	d := testDeck(5)
	e := NewEngine(d, false, 0, nil)

	// Answer first card wrong.
	firstCard := e.Current()
	opts := e.Options()
	for i, o := range opts {
		if o != firstCard.AnswerText {
			e.Answer(i)
			break
		}
	}
	e.Next()

	// The card should be in the retry queue.
	if len(e.retry) != 1 {
		t.Fatalf("expected 1 retry card, got %d", len(e.retry))
	}
	if e.retry[0].card.ID != firstCard.ID {
		t.Error("wrong card in retry queue")
	}
	if e.retry[0].remaining != minRepeats {
		t.Errorf("expected %d remaining, got %d", minRepeats, e.retry[0].remaining)
	}
}

func TestEngineTimeLimit(t *testing.T) {
	d := testDeck(3)
	d.TimeLimit = 12          // deck-global default
	d.Cards[1].TimeLimit = 5  // per-card override
	d.Cards[2].TimeLimit = -1 // explicitly unlimited

	e := NewEngine(d, false, 0, nil) // sequential order preserved

	answerCorrectly := func() {
		opts := e.Options()
		for i, o := range opts {
			if o == e.Current().AnswerText {
				e.Answer(i)
				return
			}
		}
		t.Fatal("correct answer not in options")
	}

	// Card 0 inherits the deck default.
	if got := e.TimeLimit(); got != 12 {
		t.Errorf("card 0: expected 12, got %d", got)
	}

	// Advance to card 1 (override) and card 2 (unlimited). Answering
	// correctly re-queues each card to the back, so order is preserved.
	answerCorrectly()
	e.Next()
	if got := e.TimeLimit(); got != 5 {
		t.Errorf("card 1: expected 5, got %d", got)
	}
	answerCorrectly()
	e.Next()
	if got := e.TimeLimit(); got != 0 {
		t.Errorf("card 2: expected 0 (unlimited), got %d", got)
	}
}

func TestEngineAnswerTimeout(t *testing.T) {
	d := testDeck(4)
	e := NewEngine(d, false, 0, nil)

	card := e.Current()
	result := e.AnswerTimeout()
	if result == nil {
		t.Fatal("expected a result")
	}
	if result.Correct {
		t.Error("timeout should never be correct")
	}
	if !result.TimedOut {
		t.Error("expected TimedOut to be true")
	}
	if e.State() != ShowResult {
		t.Error("expected ShowResult state")
	}
	if e.TotalWrong != 1 {
		t.Errorf("expected 1 wrong, got %d", e.TotalWrong)
	}
	// A timed-out card is queued for retry, like any wrong answer.
	if len(e.retry) != 1 || e.retry[0].card.ID != card.ID {
		t.Error("expected timed-out card in retry queue")
	}
}

func TestEngineChoiceCountClamped(t *testing.T) {
	d := testDeck(2)
	e := NewEngine(d, false, 10, nil) // request 10 choices but only 2 cards

	opts := e.Options()
	if len(opts) != 2 {
		t.Errorf("expected 2 options (clamped), got %d", len(opts))
	}
}

func TestEngineCustomDistractors(t *testing.T) {
	d := &deck.Deck{
		Name:    "test",
		Choices: 4,
		Cards: []deck.Card{
			{
				ID:          "card1",
				Question:    []deck.Media{{Type: deck.Text, Content: "Q1"}},
				Answer:      []deck.Media{{Type: deck.Text, Content: "correct"}},
				AnswerText:  "correct",
				Distractors: []string{"wrong1", "wrong2", "wrong3"},
			},
		},
	}

	e := NewEngine(d, false, 0, nil)
	opts := e.Options()

	if len(opts) != 4 {
		t.Fatalf("expected 4 options, got %d", len(opts))
	}

	// Verify correct answer is present.
	found := false
	for _, o := range opts {
		if o == "correct" {
			found = true
			break
		}
	}
	if !found {
		t.Error("correct answer not in options")
	}

	// Verify all distractors are present.
	for _, d := range []string{"wrong1", "wrong2", "wrong3"} {
		found := false
		for _, o := range opts {
			if o == d {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("distractor %q not in options", d)
		}
	}
}

// TestEngineIsRetryTimingInvariant locks in the contract the gui relies on to
// keep drill repetitions invisible to persisted stats: right after an answer
// is submitted, IsRetry() must report whether the card just answered came from
// the retry queue. The first (cold) miss must read false so it is recorded;
// every subsequent drill rep must read true so it is skipped.
func TestEngineIsRetryTimingInvariant(t *testing.T) {
	d := testDeck(4)
	e := NewEngine(d, false, 0, nil)

	if e.IsRetry() {
		t.Fatal("first card comes from the main queue; IsRetry must be false")
	}

	// Answer the first card wrong. IsRetry must still be false immediately
	// after Answer() returns — this is when the gui records the cold miss.
	for i, o := range e.Options() {
		if o != e.Current().AnswerText {
			e.Answer(i)
			break
		}
	}
	if e.IsRetry() {
		t.Error("IsRetry must stay false right after the first cold miss")
	}

	// Advancing replays the same card, now as a retry drill rep.
	e.Next()
	if !e.IsRetry() {
		t.Error("the replayed card after a wrong answer must be a retry rep")
	}

	// A correct drill rep stays a retry rep (still invisible to stats) until
	// the card graduates.
	for i, o := range e.Options() {
		if o == e.Current().AnswerText {
			e.Answer(i)
			break
		}
	}
	if !e.IsRetry() {
		t.Error("a correct drill rep must still report IsRetry true")
	}
}
