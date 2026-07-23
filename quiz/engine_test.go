package quiz

import (
	"study/deck"
	"study/progress"
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

// typedDeck builds a single-card type-mode deck for answer-matching tests.
func typedDeck(answer string, accept []string, caseSensitive bool) *deck.Deck {
	return &deck.Deck{
		Name:          "t",
		Choices:       4,
		CaseSensitive: caseSensitive,
		Cards: []deck.Card{{
			ID:         "c1",
			Question:   []deck.Media{{Type: deck.Text, Content: "q"}},
			Answer:     []deck.Media{{Type: deck.Text, Content: answer}},
			AnswerText: answer,
			Accept:     accept,
			Mode:       deck.ModeType,
		}},
	}
}

func TestAnswerTypedMatching(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		accept []string
		input  string
		want   bool
	}{
		{"exact", "hello", nil, "hello", true},
		{"case insensitive", "hello", nil, "HeLLo", true},
		{"surrounding space", "hello", nil, "  hello ", true},
		{"trailing punctuation", "hello", nil, "hello!", true},
		{"diacritic folded", "salâm", nil, "salam", true},
		{"apostrophe dropped", "i'm fine", nil, "im fine", true},
		{"collapsed whitespace", "good morning", nil, "good   morning", true},
		{"accepted alternative", "hello", []string{"hi", "hey"}, "hey", true},
		{"alternative normalized", "hello", []string{"salâm"}, "SALAM", true},
		{"genuinely wrong", "hello", []string{"hi"}, "goodbye", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngine(typedDeck(tc.answer, tc.accept, false), nil, nil)
			r := e.AnswerTyped(tc.input)
			if r == nil {
				t.Fatal("nil result")
			}
			if r.Correct != tc.want {
				t.Errorf("AnswerTyped(%q) correct=%v, want %v", tc.input, r.Correct, tc.want)
			}
		})
	}
}

func TestAnswerTypedCaseSensitive(t *testing.T) {
	// A case-sensitive deck still accepts alternatives but compares exactly:
	// no case folding, no punctuation/diacritic leniency.
	e := NewEngine(typedDeck("Hello", []string{"Hi"}, true), nil, nil)
	if !e.AnswerTyped("Hello").Correct {
		t.Error("exact match should be correct")
	}

	e = NewEngine(typedDeck("Hello", []string{"Hi"}, true), nil, nil)
	if e.AnswerTyped("hello").Correct {
		t.Error("case-sensitive deck should reject differing case")
	}

	e = NewEngine(typedDeck("Hello", []string{"Hi"}, true), nil, nil)
	if !e.AnswerTyped("Hi").Correct {
		t.Error("accepted alternative should be correct even when case-sensitive")
	}
}

func TestEngineBasicFlow(t *testing.T) {
	d := testDeck(4)
	e := NewEngine(d, nil, nil)

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
	e := NewEngine(d, nil, nil)

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

func TestSequentialMissRejoinsLap(t *testing.T) {
	// Sequential is a reading order, not a scheduler: a miss is recorded and
	// the card rejoins the lap at the tail — no immediate repeat, no drill.
	d := testDeck(4)
	d.Order = deck.OrderSequential
	e := NewEngine(d, nil, nil)

	missed := e.Current()
	opts := e.Options()
	for i, o := range opts {
		if o != missed.AnswerText {
			if res := e.Answer(i); res.Correct {
				t.Fatal("expected wrong result")
			}
			break
		}
	}
	e.Next()
	if e.Current().ID == missed.ID {
		t.Fatal("missed card repeated immediately — the drill is gone")
	}
	// It comes around at the end of the lap, wearing the retry badge.
	for lap := 0; lap < 3 && e.Current().ID != missed.ID; lap++ {
		for i, o := range e.Options() {
			if o == e.Current().AnswerText {
				e.Answer(i)
				break
			}
		}
		e.Next()
	}
	if e.Current().ID != missed.ID {
		t.Fatal("missed card never came around the lap")
	}
	if !e.IsRetry() {
		t.Error("the come-around miss must read retry")
	}
}

func TestEngineCorrectAnswerCycle(t *testing.T) {
	d := testDeck(3)
	e := NewEngine(d, nil, nil)

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
	// Session should still be active: each new card owes 3 recalls.
	if e.State() == Done {
		t.Error("session should not be Done after one recall per new card")
	}
}

func TestSequentialLapOrderSurvivesMiss(t *testing.T) {
	// After a miss, the lap continues in authored order; the missed card
	// waits its turn at the tail rather than jumping the queue.
	d := testDeck(5)
	d.Order = deck.OrderSequential
	e := NewEngine(d, nil, nil)

	first := e.Current()
	for i, o := range e.Options() {
		if o != first.AnswerText {
			e.Answer(i)
			break
		}
	}
	e.Next()

	// The next four serves are the rest of the lap, in order, then the miss.
	want := []string{d.Cards[1].ID, d.Cards[2].ID, d.Cards[3].ID, d.Cards[4].ID, first.ID}
	for _, id := range want {
		if e.Current().ID != id {
			t.Fatalf("serve = %s, want %s", e.Current().ID, id)
		}
		for i, o := range e.Options() {
			if o == e.Current().AnswerText {
				e.Answer(i)
				break
			}
		}
		e.Next()
	}
}

func TestEngineTimeLimit(t *testing.T) {
	d := testDeck(3)
	d.TimeLimit = 12          // deck-global default
	d.Cards[1].TimeLimit = 5  // per-card override
	d.Cards[2].TimeLimit = -1 // explicitly unlimited

	e := NewEngine(d, nil, nil) // sequential order preserved

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
	d.Order = deck.OrderSequential // timeouts queue for retry in sequential mode
	e := NewEngine(d, nil, nil)

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
	// A timed-out card rejoins the lap at the tail, like any wrong answer.
	e.Next()
	if e.Current().ID == card.ID {
		t.Error("timed-out card repeated immediately — it should wait its lap turn")
	}
}

func TestEngineChoiceCountClamped(t *testing.T) {
	d := testDeck(2)
	d.Choices = 10 // deck requests 10 choices but has only 2 cards
	e := NewEngine(d, nil, nil)

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

	e := NewEngine(d, nil, nil)
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

// TestAuthoredDistractorsAreWholeOptionSet locks in that a card with "~"
// distractors is never padded from other cards' answers: a binary "A or B?"
// card serves exactly two options even when the deck default asks for four
// and sibling cards could supply filler.
func TestAuthoredDistractorsAreWholeOptionSet(t *testing.T) {
	d := &deck.Deck{
		Name:    "test",
		Choices: 4,
		Cards: []deck.Card{
			{
				ID:          "binary",
				Question:    []deck.Media{{Type: deck.Text, Content: "A or B?"}},
				Answer:      []deck.Media{{Type: deck.Text, Content: "A"}},
				AnswerText:  "A",
				Distractors: []string{"B"},
				Mode:        deck.ModeChoice,
			},
			{
				ID:         "filler1",
				Question:   []deck.Media{{Type: deck.Text, Content: "Q2"}},
				Answer:     []deck.Media{{Type: deck.Text, Content: "C"}},
				AnswerText: "C",
				Mode:       deck.ModeChoice,
			},
			{
				ID:         "filler2",
				Question:   []deck.Media{{Type: deck.Text, Content: "Q3"}},
				Answer:     []deck.Media{{Type: deck.Text, Content: "D"}},
				AnswerText: "D",
				Mode:       deck.ModeChoice,
			},
		},
	}

	e := NewEngine(d, nil, nil)
	opts := e.Options()
	if len(opts) != 2 {
		t.Fatalf("binary card should serve exactly 2 options, got %d: %v", len(opts), opts)
	}
	seen := map[string]bool{opts[0]: true, opts[1]: true}
	if !seen["A"] || !seen["B"] {
		t.Errorf("options should be exactly A and B, got %v", opts)
	}
}

// TestPoolFillWithoutAuthoredDistractors locks in the counterpart: a choice
// card with no "~" lines still fills its options from other cards' answers.
func TestPoolFillWithoutAuthoredDistractors(t *testing.T) {
	d := testDeck(4)
	d.Choices = 4
	for i := range d.Cards {
		d.Cards[i].Mode = deck.ModeChoice
	}
	e := NewEngine(d, nil, nil)
	if got := len(e.Options()); got != 4 {
		t.Fatalf("expected 4 pool-filled options, got %d", got)
	}
}

// TestSequentialStats locks in sequential mode's stats contract: every
// graded answer, hit or miss, lands in persisted stats — a sequential miss
// requeues the card at the lap's tail rather than entering a massed drill,
// so there are no free repetitions to exclude. IsRetry stays a display
// label: "your last attempt at this card was a miss".
func TestSequentialStats(t *testing.T) {
	store, err := progress.NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	d := testDeck(4)
	d.Order = deck.OrderSequential
	e := NewEngine(d, nil, store)

	if e.IsRetry() {
		t.Fatal("first card comes fresh; IsRetry must be false")
	}
	missed := e.Current().ID

	// The miss lands in persisted stats once, and the card reads retry from
	// this grading on.
	for i, o := range e.Options() {
		if o != e.Current().AnswerText {
			e.Answer(i)
			break
		}
	}
	if got := store.Get(missed).TimesWrong; got != 1 {
		t.Fatalf("miss recorded %d times, want 1", got)
	}
	if !e.IsRetry() {
		t.Error("a card whose last graded answer was a miss must read retry")
	}

	// No drill: the next serve is a different card, and every graded answer
	// counts in stats.
	e.Next()
	if e.Current().ID == missed {
		t.Fatal("missed card repeated immediately")
	}
	next := e.Current().ID
	for i, o := range e.Options() {
		if o == e.Current().AnswerText {
			e.Answer(i)
			break
		}
	}
	if cp := store.Get(next); cp.TimesCorrect != 1 {
		t.Errorf("correct answer recorded %d times, want 1", cp.TimesCorrect)
	}
}

func TestEngineEnd(t *testing.T) {
	// Sessions are endless (correct cards re-queue forever), so Done is only
	// reachable through End() — the user deciding to stop.
	e := NewEngine(testDeck(3), nil, nil)
	if e.State() != ShowQuestion {
		t.Fatalf("state = %v, want ShowQuestion", e.State())
	}
	e.End()
	if e.State() != Done {
		t.Errorf("state after End = %v, want Done", e.State())
	}
	if e.Current() != nil {
		t.Error("current card should be nil after End")
	}
}

func TestEngineCardIDs(t *testing.T) {
	e := NewEngine(testDeck(3), nil, nil)
	ids := e.CardIDs()
	if len(ids) != 3 {
		t.Fatalf("CardIDs returned %d ids, want 3", len(ids))
	}
	want := map[string]bool{"alpha": true, "beta": true, "gamma": true}
	for _, id := range ids {
		if !want[id] {
			t.Errorf("unexpected card id %q", id)
		}
	}
}

// TestRemainingOnResultScreen: on the result screen the serve is finished and
// a re-queued card is already back in the queue — Remaining must not count the
// current card again, or every result screen's denominator inflates by one.
func TestRemainingOnResultScreen(t *testing.T) {
	full := testDeck(1)
	full.Order = deck.OrderAdaptive
	e := NewEngine(full, full.Cards, newTestStore(t))

	answerCurrent(e, true)
	if e.State() != ShowResult {
		t.Fatalf("State = %v, want ShowResult", e.State())
	}
	resultDenom := e.TotalSeen + e.Remaining()

	e.Next()
	if e.State() != ShowQuestion {
		t.Fatalf("State = %v, want ShowQuestion (card owes more recalls)", e.State())
	}
	questionDenom := e.TotalSeen + e.Remaining()
	if resultDenom != questionDenom {
		t.Errorf("result denominator %d != question denominator %d for the same session shape", resultDenom, questionDenom)
	}
}
