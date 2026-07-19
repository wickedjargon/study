package deck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseString(t *testing.T, content string) (*Deck, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "set.deck")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return Parse(path)
}

func TestParseSetCard(t *testing.T) {
	d, err := parseString(t, `# Set Cards

Name five countries that border China
---
quota: 5
+ Russia
+ Mongolia
+ Kazakhstan
+ Myanmar
= Burma
+ Laos
+ Vietnam
---
Fourteen in reality; this card wants any five.
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(d.Cards))
	}
	c := &d.Cards[0]
	if !c.IsSet() || len(c.SetItems) != 6 {
		t.Fatalf("SetItems = %d, want 6", len(c.SetItems))
	}
	if c.Quota != 5 || c.SetTarget() != 5 {
		t.Fatalf("Quota = %d, target %d, want 5", c.Quota, c.SetTarget())
	}
	if c.Mode != ModeType {
		t.Error("set card not forced to type mode")
	}
	// "= Burma" attaches to Myanmar, not the card.
	if len(c.Accept) != 0 {
		t.Errorf("card-level accepts = %v, want none", c.Accept)
	}
	myanmar := c.SetItems[3]
	if myanmar.Text != "Myanmar" || len(myanmar.Accept) != 1 || myanmar.Accept[0] != "Burma" {
		t.Errorf("Myanmar item = %+v", myanmar)
	}
	if !strings.Contains(c.AnswerText, "Russia, Mongolia") {
		t.Errorf("AnswerText = %q, want joined list", c.AnswerText)
	}
	if len(c.Note) == 0 {
		t.Error("note lost")
	}
	// Set cards don't reverse.
	if rev := d.Reversed(); len(rev.Cards) != 0 {
		t.Errorf("reversed cards = %d, want 0", len(rev.Cards))
	}
}

func TestParseSetCardErrors(t *testing.T) {
	cases := []struct{ name, body string }{
		{"one item", "Q\n---\n+ Only\n"},
		{"quota exceeds items", "Q\n---\nquota: 3\n+ A1\n+ B2\n"},
		{"quota without items", "Q\n---\nquota: 2\nAnswer\n"},
		{"distractors on set card", "Q\n---\n+ Aa\n+ Bb\n~ Cc\n"},
		{"plain answer mixed in", "Q\n---\nAnswer\n+ Aa\n+ Bb\n"},
	}
	for _, c := range cases {
		if _, err := parseString(t, "# T\n\n"+c.body); err == nil {
			t.Errorf("%s: parsed without error", c.name)
		}
	}
}
