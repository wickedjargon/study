package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeDeck drops a minimal two-card deck at path.
func writeDeck(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	deck := "salâm\n---\nhello\n\nxodâhâfez\n---\ngoodbye\n"
	if err := os.WriteFile(path, []byte(deck), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	data := t.TempDir()
	decks := t.TempDir()

	r, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Empty() {
		t.Fatal("fresh registry should be empty")
	}
	if err := r.Watch(decks); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}

	r2, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Dirs) != 1 || r2.Dirs[0] != decks {
		t.Errorf("Dirs = %v, want [%s]", r2.Dirs, decks)
	}

	if err := r2.Unwatch(decks); err != nil {
		t.Fatal(err)
	}
	if !r2.Empty() {
		t.Error("registry should be empty after unwatch")
	}
}

func TestWatchRejectsDuplicatesAndFiles(t *testing.T) {
	data := t.TempDir()
	decks := t.TempDir()
	r, _ := Open(data)

	if err := r.Watch(decks); err != nil {
		t.Fatal(err)
	}
	if err := r.Watch(decks); err == nil {
		t.Error("watching the same dir twice should fail")
	}
	f := filepath.Join(decks, "a.deck")
	writeDeck(t, f)
	if err := r.Watch(f); err == nil {
		t.Error("watching a file should fail")
	}
	if err := r.Unwatch(t.TempDir()); err == nil {
		t.Error("unwatching a never-watched dir should fail")
	}
}

func TestScan(t *testing.T) {
	data := t.TempDir()
	decks := t.TempDir()

	// Two loose decks, a pack subdirectory, a deckless subdirectory, and a
	// stray non-deck file.
	writeDeck(t, filepath.Join(decks, "b.deck"))
	writeDeck(t, filepath.Join(decks, "a.deck"))
	writeDeck(t, filepath.Join(decks, "farsi.deck", "one.deck"))
	if err := os.MkdirAll(filepath.Join(decks, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decks, "README.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	r, _ := Open(data)
	if err := r.Watch(decks); err != nil {
		t.Fatal(err)
	}

	groups := r.Scan()
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1: %+v", len(groups), groups)
	}

	g := groups[0]
	if g.Dir != decks || g.Err != nil {
		t.Fatalf("group = {%s %v}, want the watched dir", g.Dir, g.Err)
	}
	var got []string
	for _, e := range g.Entries {
		got = append(got, e.Name)
	}
	want := []string{"a", "b", "farsi"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %s, want %s (sorted by name)", i, got[i], want[i])
		}
	}
	if !g.Entries[2].Pack {
		t.Error("farsi.deck/ should be a pack")
	}
}

func TestScanUnreadableDir(t *testing.T) {
	data := t.TempDir()
	r, _ := Open(data)
	dir := t.TempDir()
	if err := r.Watch(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	groups := r.Scan()
	if len(groups) != 1 || groups[0].Err == nil {
		t.Fatalf("a vanished watched dir should surface its error: %+v", groups)
	}
}

func TestDescribe(t *testing.T) {
	// Describe opens progress via the default per-user store; point HOME at a
	// scratch dir so the test never touches real progress.
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "farsi.deck")
	writeDeck(t, path)

	info := Describe(path, time.Now())
	if info.Err != nil {
		t.Fatal(info.Err)
	}
	if info.Cards != 2 || info.DueReviews != 0 || info.DueNew != 2 {
		t.Errorf("fresh deck: %+v, want 2 cards, 0 reviews, 2 new", info)
	}
	if !info.Reversible || info.RevNew != 2 {
		t.Errorf("deck should be reversible with 2 new: %+v", info)
	}
	if !info.LastStudied.IsZero() {
		t.Errorf("never-studied deck has LastStudied %v", info.LastStudied)
	}

	bad := Describe(filepath.Join(t.TempDir(), "missing.deck"), time.Now())
	if bad.Err == nil {
		t.Error("missing deck should report Err")
	}
}
