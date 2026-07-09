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

	if len(s.data.Cards) != 0 {
		t.Error("expected no progress after reset")
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return &Store{
		dir:  dir,
		path: dir + "/test.json",
		data: &DeckProgress{
			DeckPath: "/test/deck",
			Cards:    make(map[string]*CardProgress),
		},
	}
}

func TestStoreResetDirection(t *testing.T) {
	s := testStore(t)
	s.RecordCorrect("fwd1")
	s.RecordCorrect("r:rev1")

	s.ResetDirection(true) // clear reverse only
	if _, ok := s.data.Cards["r:rev1"]; ok {
		t.Error("reverse entry survived ResetDirection(true)")
	}
	if _, ok := s.data.Cards["fwd1"]; !ok {
		t.Error("forward entry was destroyed by ResetDirection(true)")
	}

	s.RecordCorrect("r:rev1")
	s.ResetDirection(false) // clear forward only
	if _, ok := s.data.Cards["fwd1"]; ok {
		t.Error("forward entry survived ResetDirection(false)")
	}
	if _, ok := s.data.Cards["r:rev1"]; !ok {
		t.Error("reverse entry was destroyed by ResetDirection(false)")
	}
}

func TestStoreMigrateIDs(t *testing.T) {
	s := testStore(t)
	// History saved under the old (media-inclusive) hash, both directions.
	s.RecordCorrect("old1")
	s.RecordCorrect("old1")
	s.RecordWrong("r:old1")

	cards := []deck.Card{
		{ID: "new1", LegacyID: "old1"},
		{ID: "new2"}, // no legacy — untouched
	}
	if !s.MigrateIDs(cards) {
		t.Fatal("MigrateIDs reported nothing moved")
	}

	if cp := s.Get("new1"); cp.TimesCorrect != 2 {
		t.Errorf("forward history not migrated: %+v", cp)
	}
	if cp := s.Get("r:new1"); cp.TimesWrong != 1 {
		t.Errorf("reverse history not migrated: %+v", cp)
	}
	if _, ok := s.data.Cards["old1"]; ok {
		t.Error("legacy forward entry not removed")
	}
	if _, ok := s.data.Cards["r:old1"]; ok {
		t.Error("legacy reverse entry not removed")
	}
	if s.MigrateIDs(cards) {
		t.Error("second MigrateIDs still reports movement")
	}
}

func TestStoreMigrateIDsKeepsNewerEntry(t *testing.T) {
	s := testStore(t)
	s.RecordWrong("old1") // stale history under the legacy ID
	s.RecordCorrect("new1")
	s.RecordCorrect("new1") // real progress already under the new ID

	s.MigrateIDs([]deck.Card{{ID: "new1", LegacyID: "old1"}})

	cp := s.Get("new1")
	if cp.TimesCorrect != 2 || cp.TimesWrong != 0 {
		t.Errorf("existing entry was overwritten by legacy history: %+v", cp)
	}
	if _, ok := s.data.Cards["old1"]; ok {
		t.Error("legacy entry not cleaned up")
	}
}

func TestSummaryFor(t *testing.T) {
	s := testStore(t)
	s.RecordCorrect("card1")
	s.RecordWrong("card1")
	s.RecordCorrect("r:card1") // other direction — out of scope
	s.RecordCorrect("orphan")  // removed card — out of scope

	correct, wrong, studied := s.SummaryFor([]string{"card1", "card2"})
	if correct != 1 || wrong != 1 || studied != 1 {
		t.Errorf("SummaryFor = (%d, %d, %d), want (1, 1, 1)", correct, wrong, studied)
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
