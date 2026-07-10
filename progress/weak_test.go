package progress

import (
	"study/deck"
	"testing"
)

func TestFilterWeak(t *testing.T) {
	s, err := NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	cards := []deck.Card{{ID: "new"}, {ID: "weak"}, {ID: "strong"}}
	// weak: mostly missed. strong: a long clean streak.
	s.RecordCorrect("weak")
	s.RecordWrong("weak")
	s.RecordWrong("weak")
	s.RecordWrong("weak")
	for i := 0; i < 8; i++ {
		s.RecordCorrect("strong")
	}

	got := s.FilterWeak(cards)
	if len(got) != 2 || got[0].ID != "new" || got[1].ID != "weak" {
		t.Errorf("FilterWeak = %v, want [new weak]", got)
	}
}
