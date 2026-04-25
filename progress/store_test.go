package progress

import (
	"study/deck"
	"testing"
)

func TestStoreRecordAndGet(t *testing.T) {
	dir := t.TempDir()
	s := &Store{
		dir:  dir,
		path: dir + "/test.json",
		data: &DeckProgress{
			DeckPath: "/test/deck",
			Cards:    make(map[string]*CardProgress),
		},
	}

	s.RecordCorrect("card1")
	s.RecordCorrect("card1")
	s.RecordWrong("card1")

	cp := s.Get("card1")
	if cp.TimesCorrect != 2 {
		t.Errorf("expected 2 correct, got %d", cp.TimesCorrect)
	}
	if cp.TimesWrong != 1 {
		t.Errorf("expected 1 wrong, got %d", cp.TimesWrong)
	}
	if cp.Streak != 0 {
		t.Errorf("expected streak 0 after wrong, got %d", cp.Streak)
	}
}

func TestStoreSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := &Store{
		dir:  dir,
		path: dir + "/test.json",
		data: &DeckProgress{
			DeckPath: "/test/deck",
			Cards:    make(map[string]*CardProgress),
		},
	}

	s.RecordCorrect("card1")
	s.RecordWrong("card2")

	if err := s.Save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Load into new store.
	s2 := &Store{
		dir:  dir,
		path: dir + "/test.json",
	}
	data, err := s2.load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	s2.data = data

	cp1 := s2.Get("card1")
	if cp1.TimesCorrect != 1 {
		t.Errorf("expected 1 correct after reload, got %d", cp1.TimesCorrect)
	}

	cp2 := s2.Get("card2")
	if cp2.TimesWrong != 1 {
		t.Errorf("expected 1 wrong after reload, got %d", cp2.TimesWrong)
	}
}

func TestStoreReset(t *testing.T) {
	dir := t.TempDir()
	s := &Store{
		dir:  dir,
		path: dir + "/test.json",
		data: &DeckProgress{
			DeckPath: "/test/deck",
			Cards:    make(map[string]*CardProgress),
		},
	}

	s.RecordCorrect("card1")
	s.Reset()

	if s.HasProgress() {
		t.Error("expected no progress after reset")
	}
}

func TestStorePrioritize(t *testing.T) {
	dir := t.TempDir()
	s := &Store{
		dir:  dir,
		path: dir + "/test.json",
		data: &DeckProgress{
			DeckPath: "/test/deck",
			Cards:    make(map[string]*CardProgress),
		},
	}

	// card1: seen, high accuracy
	s.RecordCorrect("card1")
	s.RecordCorrect("card1")
	s.RecordCorrect("card1")

	// card2: seen, low accuracy
	s.RecordWrong("card2")
	s.RecordWrong("card2")
	s.RecordCorrect("card2")

	// card3: unseen

	cards := []deck.Card{
		{ID: "card1", AnswerText: "a"},
		{ID: "card2", AnswerText: "b"},
		{ID: "card3", AnswerText: "c"},
	}

	sorted := s.PrioritizeCards(cards)

	// Unseen (card3) should be first.
	if sorted[0].ID != "card3" {
		t.Errorf("expected unseen card first, got %s", sorted[0].ID)
	}
	// Low accuracy (card2) before high accuracy (card1).
	if sorted[1].ID != "card2" {
		t.Errorf("expected low accuracy card second, got %s", sorted[1].ID)
	}
	if sorted[2].ID != "card1" {
		t.Errorf("expected high accuracy card last, got %s", sorted[2].ID)
	}
}

func TestAccuracy(t *testing.T) {
	cp := &CardProgress{TimesCorrect: 3, TimesWrong: 1}
	acc := cp.Accuracy()
	if acc != 75.0 {
		t.Errorf("expected 75%%, got %.1f%%", acc)
	}

	empty := &CardProgress{}
	if empty.Accuracy() != 0 {
		t.Error("expected 0% for unseen card")
	}
}
