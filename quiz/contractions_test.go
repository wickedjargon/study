package quiz

import (
	"testing"

	"study/deck"
)

// acceptsCase runs one accepts() check against a single-card engine.
func acceptsCase(t *testing.T, answer string, extra []string, typed string) bool {
	t.Helper()
	d := &deck.Deck{
		Order: deck.OrderSequential,
		Mode:  deck.ModeType,
		Cards: []deck.Card{{ID: "c", AnswerText: answer, Accept: extra, Mode: deck.ModeType}},
	}
	e := NewEngine(d, nil, nil)
	return e.accepts(e.Current(), typed)
}

func TestContractionsExpand(t *testing.T) {
	yes := [][2]string{
		// Card answer, typed answer.
		{"I do not understand", "i don't understand"},
		{"I do not understand", "i dont understand"},
		{"I don't understand", "i do not understand"},
		{"I'll", "ill"},    // punctuation-stripped original
		{"I'll", "i will"}, // expansion
		{"He is", "he's"},
		{"He is", "hes"},
		{"She's gone", "she is gone"},
		{"She's gone", "she has gone"},
		{"won't", "will not"},
		{"can't", "cannot"},
		{"You are welcome", "you're welcome"},
		{"we're here", "we are here"},
		{"let's go", "let us go"},
	}
	for _, c := range yes {
		if !acceptsCase(t, c[0], nil, c[1]) {
			t.Errorf("card %q should accept %q", c[0], c[1])
		}
	}

	no := [][2]string{
		// Bare real words must not act as contractions.
		{"ill", "i will"},        // sick ≠ I will
		{"were here", "we are here"},
		{"well", "we will"},
		{"i had", "i would"},     // 'd is ambiguous but these two never meet
	}
	for _, c := range no {
		if acceptsCase(t, c[0], nil, c[1]) {
			t.Errorf("card %q should NOT accept %q", c[0], c[1])
		}
	}
}

func TestContractionsCaseSensitiveUntouched(t *testing.T) {
	d := &deck.Deck{
		Order:         deck.OrderSequential,
		Mode:          deck.ModeType,
		CaseSensitive: true,
		Cards:         []deck.Card{{ID: "c", AnswerText: "I don't", Mode: deck.ModeType}},
	}
	e := NewEngine(d, nil, nil)
	if e.accepts(e.Current(), "I do not") {
		t.Error("case-sensitive decks must keep exact matching")
	}
}
