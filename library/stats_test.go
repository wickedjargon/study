package library

import (
	"path/filepath"
	"testing"
	"time"

	"study/deck"
	"study/progress"
)

func TestStats(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // progress goes to a scratch store

	path := filepath.Join(t.TempDir(), "farsi.deck")
	writeDeck(t, path) // two cards: salâm→hello, xodâhâfez→goodbye

	fresh, err := Stats(path, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Cards != 2 || fresh.Studied != 0 || fresh.DueNew != 2 {
		t.Errorf("fresh deck: %+v, want 2 cards, 0 studied, 2 new", fresh)
	}
	if !fresh.Reversible {
		t.Error("deck should report a reversed direction")
	}
	if len(fresh.Weakest) != 0 {
		t.Errorf("fresh deck has weakest list: %+v", fresh.Weakest)
	}

	// Record one answered card and re-read: studied and the weakest listing
	// follow, and the numbers stay direction-scoped (reverse still fresh).
	d, err := deck.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := progress.NewStore(d.Path)
	if err != nil {
		t.Fatal(err)
	}
	store.RecordCorrect(d.Cards[0].ID)
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	info, err := Stats(path, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if info.Studied != 1 || info.Correct != 1 || info.Wrong != 0 {
		t.Errorf("after one correct: %+v, want 1 studied, 1 correct", info)
	}
	if len(info.Weakest) != 1 || info.Weakest[0].Accuracy != 100 {
		t.Errorf("weakest = %+v, want the one studied card at 100%%", info.Weakest)
	}
	if acc := info.Accuracy(); acc != 100 {
		t.Errorf("Accuracy = %.0f, want 100", acc)
	}

	rev, err := Stats(path, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rev.Studied != 0 || rev.DueNew != 2 {
		t.Errorf("reverse direction: %+v, want untouched (0 studied, 2 new)", rev)
	}
}
