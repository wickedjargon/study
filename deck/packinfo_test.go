package deck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackInfoName(t *testing.T) {
	dir := t.TempDir()
	if got := PackInfoName(dir); got != "" {
		t.Fatalf("no .deck-info: got %q, want empty", got)
	}
	content := "# a comment\n# section-like-future-key: ignored\n# pack:  US Presidents \n"
	if err := os.WriteFile(filepath.Join(dir, ".deck-info"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if got := PackInfoName(dir); got != "US Presidents" {
		t.Fatalf("got %q, want US Presidents", got)
	}
	// A deck *file* path has no .deck-info inside it.
	if got := PackInfoName(filepath.Join(dir, "level1.deck")); got != "" {
		t.Fatalf("file path: got %q, want empty", got)
	}
}
