package quiz

import (
	"testing"

	"study/deck"
)

func TestDamerau(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"tehran", "tehran", 0},
		{"tehrran", "tehran", 1},
		{"madird", "madrid", 1}, // adjacent swap is one edit
		{"linkon", "lincoln", 2},
		{"rosavelt", "roosevelt", 2},
		{"rosevlt", "roosevelt", 2},
		{"car", "cat", 1},
		{"", "abc", 3},
	}
	for _, c := range cases {
		if got := damerau(c.a, c.b); got != c.want {
			t.Errorf("damerau(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// fdrDeck is a one-card deck shaped like the presidents pack: full name
// answer with accepted variants.
func fdrDeck() *deck.Deck {
	d := testDeck(1)
	d.Cards[0].AnswerText = "Franklin D. Roosevelt"
	d.Cards[0].Accept = []string{"FDR", "Franklin Roosevelt"}
	d.Cards[0].Mode = deck.ModeType
	return d
}

// TestNearMissSpecimens: the four real-world misspellings that motivated the
// feature, straight from the user's screenshots, plus boundary cases.
func TestNearMissSpecimens(t *testing.T) {
	d := fdrDeck()
	e := NewEngine(d, nil, nil)
	card := &d.Cards[0]

	near := []string{
		"franklin d. rosavelt", // in-place misspelling, distance 2
		"franklin d rosevlt",   // two dropped letters
		"rosavelt d franklin",  // scrambled words + misspelling
		"franklin d roosevelt", // sanity: distance 0 never reaches nearMiss in practice, but scramble layer tolerates exact words
		"roosevelt franklin d", // pure scramble, zero edits
		"franklin rosevelt",    // near the accepted "Franklin Roosevelt"
		"franklen d rosavelt",  // 3 edits, but every word within its budget
	}
	for _, s := range near {
		if !e.nearMiss(card, s) {
			t.Errorf("nearMiss(%q) = false, want true", s)
		}
	}

	far := []string{
		"abraham lincoln", // different president entirely
		"fdrr x yz",       // word count mismatch with every candidate
		"",                // empty input
	}
	for _, s := range far {
		if e.nearMiss(card, s) {
			t.Errorf("nearMiss(%q) = true, want false", s)
		}
	}
}

// TestNearMissBothWordsMisspelled: a short answer with every word inside
// its own budget is a spelling mistake even when the edits sum past the
// sentence cap — the user's "Theadoore rosevelt" screenshot.
func TestNearMissBothWordsMisspelled(t *testing.T) {
	d := testDeck(1)
	d.Cards[0].AnswerText = "Theodore Roosevelt"
	d.Cards[0].Accept = []string{"Teddy Roosevelt"}
	e := NewEngine(d, nil, nil)
	if !e.nearMiss(&d.Cards[0], "Theadoore rosevelt") {
		t.Error("theadoore rosevelt (2+1 edits, both words in budget) must flag")
	}
	// A sentence keeps the strict total cap: four words, each within its own
	// per-word budget, three edits overall — still no flag.
	d2 := testDeck(1)
	d2.Cards[0].AnswerText = "please speak more slowly"
	e2 := NewEngine(d2, nil, nil)
	if e2.nearMiss(&d2.Cards[0], "pleese spek mre slowly") {
		t.Error("3 edits across a 4-word sentence must not flag")
	}
}

// TestNearMissShortAnswers: short answers get almost no tolerance.
func TestNearMissShortAnswers(t *testing.T) {
	d := testDeck(1)
	d.Cards[0].AnswerText = "cat"
	e := NewEngine(d, nil, nil)
	if e.nearMiss(&d.Cards[0], "car") {
		t.Error("3-letter answers must never flag a near miss")
	}

	d2 := testDeck(1)
	d2.Cards[0].AnswerText = "Paris"
	e2 := NewEngine(d2, nil, nil)
	if !e2.nearMiss(&d2.Cards[0], "pari") {
		t.Error("one edit on a 5-letter answer should flag")
	}
	if e2.nearMiss(&d2.Cards[0], "pans") {
		// substitution + deletion: 2 edits on a 5-letter word is over budget
		t.Error("two edits on a 5-letter answer must not flag")
	}
}

// TestConfusionBeatsNearMiss: typing another card's answer — even one edit
// away from this card's — is a confusion, not a typo.
func TestConfusionBeatsNearMiss(t *testing.T) {
	d := testDeck(2)
	d.Cards[0].AnswerText = "Iraq"
	d.Cards[0].Mode = deck.ModeType
	d.Cards[1].AnswerText = "Iran"
	d.Cards[1].Mode = deck.ModeType
	e := NewEngine(d, nil, nil)

	// Serve until card 0 (Iraq) is current, then type Iran's answer.
	if e.Current().AnswerText != "Iraq" {
		e.AnswerTyped(e.Current().AnswerText) // clear the other card first
		e.Next()
	}
	res := e.AnswerTyped("Iran")
	if res.Correct {
		t.Fatal("Iran for Iraq graded correct")
	}
	if res.ConfusedWith == nil {
		t.Fatal("expected confusion with the Iran card")
	}
	if res.NearMiss {
		t.Error("confusion must not also flag a near miss")
	}
	if e.PracticeOwed() != 0 {
		t.Error("confusion must not owe practice")
	}
}

// TestPracticeFlow: a near miss owes three correct transcriptions; Next is
// inert until they are paid; wrong transcriptions don't count.
func TestPracticeFlow(t *testing.T) {
	d := fdrDeck()
	d.Order = deck.OrderAdaptive
	e := NewEngine(d, nil, nil)

	res := e.AnswerTyped("franklin d rosavelt")
	if res.Correct {
		t.Fatal("misspelling graded correct")
	}
	if !res.NearMiss {
		t.Fatal("expected a near miss")
	}
	if e.PracticeOwed() != 3 {
		t.Fatalf("PracticeOwed = %d, want 3", e.PracticeOwed())
	}

	e.Next()
	if e.State() != ShowResult {
		t.Fatal("Next advanced past an unpaid practice debt")
	}

	if e.PracticeTyped("still wrong") {
		t.Error("wrong transcription counted")
	}
	if e.PracticeOwed() != 3 {
		t.Errorf("PracticeOwed after wrong transcription = %d, want 3", e.PracticeOwed())
	}

	for i := 0; i < 3; i++ {
		if !e.PracticeTyped("Franklin D. Roosevelt") {
			t.Fatalf("correct transcription %d rejected", i+1)
		}
	}
	if e.PracticeOwed() != 0 {
		t.Fatalf("PracticeOwed after 3 = %d, want 0", e.PracticeOwed())
	}

	e.Next()
	if e.State() == ShowResult {
		t.Fatal("Next still inert after practice paid")
	}
}

// TestNoPracticeOnPlainMiss: an ordinary wrong answer owes nothing.
func TestNoPracticeOnPlainMiss(t *testing.T) {
	d := fdrDeck()
	e := NewEngine(d, nil, nil)
	res := e.AnswerTyped("abraham lincoln")
	if res.NearMiss {
		t.Fatal("plain miss flagged as near miss")
	}
	if e.PracticeOwed() != 0 {
		t.Fatal("plain miss owes practice")
	}
	e.Next()
	if e.State() == ShowResult {
		t.Fatal("Next inert after a plain miss")
	}
}
