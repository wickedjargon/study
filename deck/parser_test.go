package deck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempDeck(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.deck")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseBasicDeck(t *testing.T) {
	content := `# Test Deck

hello
---
world

foo
---
bar
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(d.Cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(d.Cards))
	}
	if d.Choices != 4 {
		t.Errorf("expected default 4 choices, got %d", d.Choices)
	}
	if d.Cards[0].AnswerText != "world" {
		t.Errorf("expected answer 'world', got %q", d.Cards[0].AnswerText)
	}
	if d.Cards[1].AnswerText != "bar" {
		t.Errorf("expected answer 'bar', got %q", d.Cards[1].AnswerText)
	}
}

func TestParseDefaultModeIsType(t *testing.T) {
	// A deck with no # answer-mode: header defaults to type-in (active recall).
	d, err := Parse(writeTempDeck(t, "2 + 2\n---\n4\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Mode != ModeType {
		t.Errorf("expected deck default mode ModeType, got %v", d.Mode)
	}
	if d.Cards[0].Mode != ModeType {
		t.Errorf("expected card default mode ModeType, got %v", d.Cards[0].Mode)
	}

	// # answer-mode: choice still opts a deck into multiple choice.
	d, err = Parse(writeTempDeck(t, "# answer-mode: choice\n\n2 + 2\n---\n4\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Mode != ModeChoice {
		t.Errorf("expected ModeChoice with '# answer-mode: choice', got %v", d.Mode)
	}
}

func TestParseSeparatorVariants(t *testing.T) {
	// --- and ===, any length ≥ 3, are all accepted and mixable in one deck.
	content := "a\n===\n1\n\nb\n----\n2\n\nc\n========\n3\n\nd\n---\n4\n"
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"1", "2", "3", "4"}
	if len(d.Cards) != len(want) {
		t.Fatalf("expected %d cards, got %d", len(want), len(d.Cards))
	}
	for i, w := range want {
		if d.Cards[i].AnswerText != w {
			t.Errorf("card %d: expected answer %q, got %q", i, w, d.Cards[i].AnswerText)
		}
	}
}

func TestIsSeparator(t *testing.T) {
	yes := []string{"---", "----", "===", "========", "  ---  ", "-----------"}
	no := []string{"--", "==", "-", "", "-=-", "- - -", "===x", "a---"}
	for _, s := range yes {
		if !isSeparator(s) {
			t.Errorf("isSeparator(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isSeparator(s) {
			t.Errorf("isSeparator(%q) = true, want false", s)
		}
	}
}

func TestParseChoicesMetadata(t *testing.T) {
	content := `# choice-count: 6

q1
---
a1
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Choices != 6 {
		t.Errorf("expected 6 choices, got %d", d.Choices)
	}
}

func TestParseOrderMetadata(t *testing.T) {
	cases := []struct {
		name string
		line string // "" = no directive
		want OrderMode
	}{
		{"default", "", OrderAdaptive},
		{"adaptive", "# order: adaptive", OrderAdaptive},
		{"sequential", "# order: sequential", OrderSequential},
		{"flip-through", "# order: flip-through", OrderFlipThrough},
		{"weak-only", "# order: weak-only", OrderWeakOnly},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := "q1\n---\na1\n"
			if tc.line != "" {
				content = tc.line + "\n\n" + content
			}
			d, err := Parse(writeTempDeck(t, content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Order != tc.want {
				t.Errorf("Order = %v, want %v", d.Order, tc.want)
			}
		})
	}

	// Malformed values — including the names of removed modes — are ignored
	// with a warning.
	for _, v := range []string{"random", "shuffled", "sequential-strict", "stale-first", "new-first"} {
		d, err := Parse(writeTempDeck(t, "# order: "+v+"\n\nq1\n---\na1\n"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Order != OrderAdaptive {
			t.Errorf("%q: expected default order, got %v", v, d.Order)
		}
		if len(d.Warnings) != 1 {
			t.Errorf("%q: expected 1 warning, got %v", v, d.Warnings)
		}
	}
}

func TestLegacyDirectiveNamesWarn(t *testing.T) {
	// The pre-rename directive names were removed, not aliased: they take no
	// effect, and each produces a warning naming its replacement so an old deck
	// fails loudly rather than silently running on defaults.
	content := "# mode: choice\n# time: 20\n\nq1\n---\na1\n\n# choices: 6\nq2\n---\na2\n"
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Mode != ModeType {
		t.Errorf("legacy # mode: took effect: got %v, want ModeType", d.Mode)
	}
	if d.TimeLimit != 0 {
		t.Errorf("legacy # time: took effect: got %d, want 0", d.TimeLimit)
	}
	if d.Cards[1].Choices != 0 {
		t.Errorf("legacy per-card # choices: took effect: got %d, want 0", d.Cards[1].Choices)
	}
	if len(d.Warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %v", d.Warnings)
	}
	for i, want := range []string{"answer-mode", "time-limit", "choice-count"} {
		if !strings.Contains(d.Warnings[i], want) {
			t.Errorf("warning %d = %q, want mention of %q", i, d.Warnings[i], want)
		}
	}
}

func TestParseRenamedDirectives(t *testing.T) {
	// Every renamed directive, deck-level and per-card, lands on its field.
	content := `# answer-mode: choice
# choice-count: 6
# answer-case: sensitive
# time-limit: 20
# audio-speed: 0.75
# preview-new: on

q1
---
a1

# answer-mode: type
# time-limit: none
q2
---
a2
`
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", d.Warnings)
	}
	if d.Mode != ModeChoice {
		t.Errorf("answer-mode: got %v, want ModeChoice", d.Mode)
	}
	if d.Choices != 6 {
		t.Errorf("choice-count: got %d, want 6", d.Choices)
	}
	if !d.CaseSensitive {
		t.Errorf("answer-case: got insensitive, want sensitive")
	}
	if d.TimeLimit != 20 {
		t.Errorf("time-limit: got %d, want 20", d.TimeLimit)
	}
	if d.Speed != 0.75 {
		t.Errorf("audio-speed: got %v, want 0.75", d.Speed)
	}
	if !d.Preview {
		t.Errorf("preview-new: got off, want on")
	}
	// Per-card renames on the second card.
	if len(d.Cards) != 2 {
		t.Fatalf("got %d cards, want 2", len(d.Cards))
	}
	if d.Cards[1].Mode != ModeType {
		t.Errorf("per-card answer-mode: got %v, want ModeType", d.Cards[1].Mode)
	}
	if d.Cards[1].TimeLimit != -1 {
		t.Errorf("per-card time-limit none: got %d, want -1", d.Cards[1].TimeLimit)
	}
}

func TestParseNewPerSessionMetadata(t *testing.T) {
	cases := []struct {
		name string
		line string // "" = no directive
		want int
	}{
		{"default", "", 10},
		{"explicit", "# new-per-session: 5", 5},
		{"zero (reviews only)", "# new-per-session: 0", 0},
		{"all", "# new-per-session: all", -1},
		{"malformed", "# new-per-session: lots", 10}, // rejected → default
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := "q1\n---\na1\n"
			if tc.line != "" {
				content = tc.line + "\n\n" + content
			}
			d, err := Parse(writeTempDeck(t, content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.NewPerSession != tc.want {
				t.Errorf("NewPerSession = %d, want %d", d.NewPerSession, tc.want)
			}
		})
	}
}

func TestParseWrongPauseMetadata(t *testing.T) {
	cases := []struct {
		name string
		line string // "" = no directive
		want int
	}{
		{"default", "", 5},
		{"explicit", "# wrong-pause: 10", 10},
		{"none disables", "# wrong-pause: none", 0},
		{"malformed", "# wrong-pause: forever", 5}, // rejected → default
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := "q1\n---\na1\n"
			if tc.line != "" {
				content = tc.line + "\n\n" + content
			}
			d, err := Parse(writeTempDeck(t, content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.WrongPause != tc.want {
				t.Errorf("WrongPause = %d, want %d", d.WrongPause, tc.want)
			}
		})
	}
}

func TestParsePreviewMetadata(t *testing.T) {
	// Default (no header) is off.
	d, err := Parse(writeTempDeck(t, "q1\n---\na1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Preview {
		t.Errorf("expected Preview=false by default")
	}

	// # preview-new: on opts into the first-viewing reveal.
	d, err = Parse(writeTempDeck(t, "# preview-new: on\n\nq1\n---\na1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.Preview {
		t.Errorf("expected Preview=true with '# preview-new: on'")
	}

	// A malformed value is ignored with a warning.
	d, err = Parse(writeTempDeck(t, "# preview-new: yes\n\nq1\n---\na1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Preview {
		t.Errorf("expected Preview=false with malformed '# preview-new: yes'")
	}
	if len(d.Warnings) != 1 {
		t.Errorf("expected 1 warning for malformed value, got %v", d.Warnings)
	}
}

func TestParseImgTintMetadata(t *testing.T) {
	// Default (no header) is off.
	d, err := Parse(writeTempDeck(t, "q1\n---\na1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ImgTint {
		t.Errorf("expected ImgTint=false by default")
	}

	// # img-tint: fg marks the deck's images as recolorable alpha masks.
	d, err = Parse(writeTempDeck(t, "# img-tint: fg\n\nq1\n---\na1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.ImgTint {
		t.Errorf("expected ImgTint=true with '# img-tint: fg'")
	}

	// A malformed value is ignored with a warning.
	d, err = Parse(writeTempDeck(t, "# img-tint: white\n\nq1\n---\na1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ImgTint {
		t.Errorf("expected ImgTint=false with malformed '# img-tint: white'")
	}
	if len(d.Warnings) != 1 {
		t.Errorf("expected 1 warning for malformed value, got %v", d.Warnings)
	}
}

func TestDistractorsImplyChoiceMode(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    QuizMode
	}{
		{"distractors alone imply choice", "q\n---\na\n~ b\n~ c\n", ModeChoice},
		{"no distractors stays type", "q\n---\na\n", ModeType},
		{"explicit per-card type wins", "# answer-mode: type\nq\n---\na\n~ b\n", ModeType},
		// A deck-header answer-mode also beats the inference: a typed deck
		// may author "~" wrong answers purely for choice-mode sessions.
		{"deck-header type beats inference", "# answer-mode: type\n\nq\n---\na\n~ b\n", ModeType},
		{"cloze with distractors", "the answer is {{a}}\n~ b\n~ c\n", ModeChoice},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Parse(writeTempDeck(t, tc.content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := d.Cards[0].Mode; got != tc.want {
				t.Errorf("Mode = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseCustomDistractors(t *testing.T) {
	content := `question
---
correct answer
~ wrong1
~ wrong2
~ wrong3
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	card := d.Cards[0]
	if card.AnswerText != "correct answer" {
		t.Errorf("expected 'correct answer', got %q", card.AnswerText)
	}
	if len(card.Distractors) != 3 {
		t.Fatalf("expected 3 distractors, got %d", len(card.Distractors))
	}
	if card.Distractors[0] != "wrong1" {
		t.Errorf("expected distractor 'wrong1', got %q", card.Distractors[0])
	}
}

func TestParseAcceptedAlternatives(t *testing.T) {
	content := `question
---
hello
= hi
=hey
~ goodbye
`
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	card := d.Cards[0]
	if card.AnswerText != "hello" {
		t.Errorf("AnswerText = %q, want \"hello\"", card.AnswerText)
	}
	if len(card.Accept) != 2 || card.Accept[0] != "hi" || card.Accept[1] != "hey" {
		t.Errorf("Accept = %v, want [hi hey]", card.Accept)
	}
	// The = lines must not leak into the distractor set.
	if len(card.Distractors) != 1 || card.Distractors[0] != "goodbye" {
		t.Errorf("Distractors = %v, want [goodbye]", card.Distractors)
	}
}

func TestParseQuestionSideAlternatives(t *testing.T) {
	content := `¿Prefiere ventanilla o pasillo?
= prefieres ventanilla o pasillo
=prefiere usted ventanilla o pasillo
---
do you prefer window or aisle
= would you prefer window or aisle
`
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	card := d.Cards[0]

	// The = lines are stored for reverse mode, never displayed.
	want := []string{"prefieres ventanilla o pasillo", "prefiere usted ventanilla o pasillo"}
	if len(card.QuestionAccept) != 2 || card.QuestionAccept[0] != want[0] || card.QuestionAccept[1] != want[1] {
		t.Errorf("QuestionAccept = %v, want %v", card.QuestionAccept, want)
	}
	if len(card.Question) != 1 || card.Question[0].Content != "¿Prefiere ventanilla o pasillo?" {
		t.Errorf("Question = %+v, want only the prompt line", card.Question)
	}

	// They must not leak into the answer-side accept list, and vice versa.
	if len(card.Accept) != 1 || card.Accept[0] != "would you prefer window or aisle" {
		t.Errorf("Accept = %v, want the answer-side alternative only", card.Accept)
	}

	// Adding a = line must not re-key the card (that would orphan progress).
	plain, err := Parse(writeTempDeck(t, "¿Prefiere ventanilla o pasillo?\n---\ndo you prefer window or aisle\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if card.ID != plain.Cards[0].ID {
		t.Errorf("ID changed when = alternatives were added: %q vs %q", card.ID, plain.Cards[0].ID)
	}
}

func TestParseQuestionOnlyAlternativesIsError(t *testing.T) {
	_, err := Parse(writeTempDeck(t, "= no prompt here\n---\nanswer\n"))
	if err == nil {
		t.Fatal("expected error for a card whose question is only = lines")
	}
}

func TestParseWithImage(t *testing.T) {
	dir := t.TempDir()

	// Create a fake image file.
	imgDir := filepath.Join(dir, "tiles")
	os.MkdirAll(imgDir, 0755)
	os.WriteFile(filepath.Join(imgDir, "test.png"), []byte("fake"), 0644)

	content := `@img tiles/test.png
---
test answer
`
	path := filepath.Join(dir, "test.deck")
	os.WriteFile(path, []byte(content), 0644)

	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	card := d.Cards[0]
	if len(card.Question) != 1 {
		t.Fatalf("expected 1 question element, got %d", len(card.Question))
	}
	if card.Question[0].Type != Image {
		t.Errorf("expected Image type, got %d", card.Question[0].Type)
	}
	expected := filepath.Join(imgDir, "test.png")
	if card.Question[0].Content != expected {
		t.Errorf("expected path %q, got %q", expected, card.Question[0].Content)
	}
}

func TestParseMissingSeparator(t *testing.T) {
	content := `question without separator
answer
`
	path := writeTempDeck(t, content)
	_, err := Parse(path)
	if err == nil {
		t.Fatal("expected error for missing separator")
	}
}

func TestParseEmptyDeck(t *testing.T) {
	content := `# Just comments
# nothing else
`
	path := writeTempDeck(t, content)
	_, err := Parse(path)
	if err == nil {
		t.Fatal("expected error for empty deck")
	}
}

func TestParseMissingImageOnlyQuestionErrors(t *testing.T) {
	// A missing media file is skipped with a warning, but if that leaves the
	// question side empty the card is unusable and the deck must not load.
	content := `@img nonexistent.png
---
answer
`
	path := writeTempDeck(t, content)
	_, err := Parse(path)
	if err == nil {
		t.Fatal("expected error for question that is only a missing image")
	}
}

func TestParseMissingAudioSkippedWithWarning(t *testing.T) {
	// A card that still has text works without its missing clip; the deck
	// loads and the problem is surfaced as a warning, not a fatal error.
	content := `@audio nonexistent.mp3
salam
---
hello
`
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := d.Cards[0]
	if len(c.Question) != 1 || c.Question[0].Type != Text {
		t.Errorf("question = %+v, want just the text line (audio skipped)", c.Question)
	}
	if len(d.Warnings) != 1 || !strings.Contains(d.Warnings[0], "missing audio") {
		t.Errorf("warnings = %v, want one about the missing audio", d.Warnings)
	}
}

func TestCardIDIgnoresMediaLines(t *testing.T) {
	// The ID hashes only the question's text, so renaming a media file must
	// not re-key the card (which would orphan its saved progress). The old
	// media-inclusive hash is kept as LegacyID for one-time migration.
	withAudio, err := Parse(writeTempDeck(t, "@audio a.mp3\nsalam\n---\nhello\n"))
	if err != nil {
		t.Fatal(err)
	}
	textOnly, err := Parse(writeTempDeck(t, "salam\n---\nhello\n"))
	if err != nil {
		t.Fatal(err)
	}

	if withAudio.Cards[0].ID != textOnly.Cards[0].ID {
		t.Errorf("IDs differ with/without media line: %q vs %q",
			withAudio.Cards[0].ID, textOnly.Cards[0].ID)
	}
	if withAudio.Cards[0].LegacyID == "" {
		t.Error("card with media has no LegacyID for migration")
	}
	if textOnly.Cards[0].LegacyID != "" {
		t.Errorf("text-only card has LegacyID %q, want none (ID never changed)", textOnly.Cards[0].LegacyID)
	}
}

func TestCardIDMediaOnlyQuestionStillUnique(t *testing.T) {
	// With no text to hash, media-only questions fall back to hashing the
	// media lines — two image cards must not collapse into one ID.
	id1, legacy1 := stableCardID([]string{"@img a.png"})
	id2, _ := stableCardID([]string{"@img b.png"})
	if id1 == id2 {
		t.Error("two different media-only questions share an ID")
	}
	if legacy1 != "" {
		t.Errorf("media-only question has LegacyID %q, want none (hash unchanged)", legacy1)
	}
}

func TestParseDirPack(t *testing.T) {
	dir := t.TempDir()
	a := "# answer-mode: type\n\nsalam\n---\nhello\n\nkhodahafez\n---\ngoodbye\n"
	// b repeats a card from a (cross-deck reuse) and adds one of its own.
	b := "# answer-mode: type\n\nsalam\n---\nhello\n\nmamnun\n---\nthanks\n"
	if err := os.WriteFile(filepath.Join(dir, "a.deck"), []byte(a), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.deck"), []byte(b), 0644); err != nil {
		t.Fatal(err)
	}

	d, err := Parse(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d.Cards) != 3 {
		t.Fatalf("merged pack has %d cards, want 3 (duplicate deduped)", len(d.Cards))
	}
	if d.Path != dir {
		t.Errorf("pack path = %q, want %q", d.Path, dir)
	}
	if d.Name != filepath.Base(dir) {
		t.Errorf("pack name = %q, want %q", d.Name, filepath.Base(dir))
	}
}

func TestParseDirConflictingHeadersWarn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.deck"), []byte("# answer-mode: type\n\nq1\n---\na1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.deck"), []byte("# answer-mode: choice\n\nq2\n---\na2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	d, err := Parse(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Mode != ModeType {
		t.Errorf("pack mode = %v, want the first file's ModeType", d.Mode)
	}
	found := false
	for _, w := range d.Warnings {
		if strings.Contains(w, "header settings differ") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one about conflicting headers", d.Warnings)
	}
	// Per-card modes still honor each card's own file.
	if d.Cards[1].Mode != ModeChoice {
		t.Errorf("second file's card mode = %v, want ModeChoice", d.Cards[1].Mode)
	}
}

func TestParseEmptyDirErrors(t *testing.T) {
	if _, err := Parse(t.TempDir()); err == nil {
		t.Fatal("expected error for a directory with no .deck files")
	}
}

func TestParseMultilineAnswerRejected(t *testing.T) {
	content := `question
---
line one
line two
`
	path := writeTempDeck(t, content)
	if _, err := Parse(path); err == nil {
		t.Fatal("expected error for multi-line answer, got nil")
	}
}

func TestCardIDStable(t *testing.T) {
	lines1 := []string{"hello", "world"}
	lines2 := []string{"hello", "world"}
	lines3 := []string{"different"}

	id1 := cardID(lines1)
	id2 := cardID(lines2)
	id3 := cardID(lines3)

	if id1 != id2 {
		t.Errorf("same content should produce same ID: %s != %s", id1, id2)
	}
	if id1 == id3 {
		t.Error("different content should produce different IDs")
	}
}

func TestParseTimeLimitMetadata(t *testing.T) {
	content := `# time-limit: 15

q1
---
a1

# time-limit: 30s
q2
---
a2

# time-limit: none
q3
---
a3

q4
---
a4
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if d.TimeLimit != 15 {
		t.Errorf("expected deck TimeLimit=15, got %d", d.TimeLimit)
	}
	if len(d.Cards) != 4 {
		t.Fatalf("expected 4 cards, got %d", len(d.Cards))
	}

	// Card 0: no per-card line → inherits deck default (0 = inherit).
	if d.Cards[0].TimeLimit != 0 {
		t.Errorf("card 0: expected TimeLimit=0 (inherit), got %d", d.Cards[0].TimeLimit)
	}
	if got := d.Cards[0].EffectiveTimeLimit(d.TimeLimit); got != 15 {
		t.Errorf("card 0: expected effective 15, got %d", got)
	}

	// Card 1: per-card override "30s".
	if d.Cards[1].TimeLimit != 30 {
		t.Errorf("card 1: expected TimeLimit=30, got %d", d.Cards[1].TimeLimit)
	}
	if got := d.Cards[1].EffectiveTimeLimit(d.TimeLimit); got != 30 {
		t.Errorf("card 1: expected effective 30, got %d", got)
	}

	// Card 2: "none" → explicitly unlimited (-1), effective 0 despite deck default.
	if d.Cards[2].TimeLimit != -1 {
		t.Errorf("card 2: expected TimeLimit=-1 (none), got %d", d.Cards[2].TimeLimit)
	}
	if got := d.Cards[2].EffectiveTimeLimit(d.TimeLimit); got != 0 {
		t.Errorf("card 2: expected effective 0 (unlimited), got %d", got)
	}

	// Card 3: no line, inherits deck default.
	if got := d.Cards[3].EffectiveTimeLimit(d.TimeLimit); got != 15 {
		t.Errorf("card 3: expected effective 15, got %d", got)
	}
}

func TestParseFontSizeMetadata(t *testing.T) {
	cases := []struct {
		name string
		line string
		want int
	}{
		{"numeric", "# font-size: 24", 24},
		{"named small", "# font-size: small", 14},
		{"named medium", "# font-size: medium", 18},
		{"named large", "# font-size: large", 22},
		{"named x-large", "# font-size: x-large", 26},
		{"out of range", "# font-size: 200", 0}, // rejected → unset
		{"unparseable", "# font-size: huge", 0}, // rejected → unset
		{"absent", "# choice-count: 4", 0},      // no directive → unset
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := tc.line + "\n\nq1\n---\na1\n"
			d, err := Parse(writeTempDeck(t, content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.FontSize != tc.want {
				t.Errorf("FontSize = %d, want %d", d.FontSize, tc.want)
			}
		})
	}
}

func TestParseSpeedMetadata(t *testing.T) {
	cases := []struct {
		name string
		line string
		want float64
	}{
		{"numeric", "# audio-speed: 0.75", 0.75},
		{"with x suffix", "# audio-speed: 1.5x", 1.5},
		{"min bound", "# audio-speed: 0.25", 0.25},
		{"max bound", "# audio-speed: 4.0", 4.0},
		{"below min", "# audio-speed: 0.1", 0},    // rejected → unset
		{"above max", "# audio-speed: 8", 0},      // rejected → unset
		{"unparseable", "# audio-speed: fast", 0}, // rejected → unset
		{"absent", "# choice-count: 4", 0},        // no directive → unset
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := tc.line + "\n\nq1\n---\na1\n"
			d, err := Parse(writeTempDeck(t, content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Speed != tc.want {
				t.Errorf("Speed = %v, want %v", d.Speed, tc.want)
			}
		})
	}
}

func TestEffectiveTimeLimitNoDeckDefault(t *testing.T) {
	// With no deck-global limit, an un-annotated card has no limit.
	c := Card{TimeLimit: 0}
	if got := c.EffectiveTimeLimit(0); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
	// A per-card limit still applies even without a deck default.
	c2 := Card{TimeLimit: 20}
	if got := c2.EffectiveTimeLimit(0); got != 20 {
		t.Errorf("expected 20, got %d", got)
	}
}

func TestParseHeaderDirectivesAcrossBlankLines(t *testing.T) {
	// Deck-level directives are read from every leading comment-only block, so a
	// blank line separating header directives must not drop the ones after it.
	content := `# choice-count: 6

# answer-mode: choice
# order: sequential

q1
---
a1
`
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Choices != 6 {
		t.Errorf("expected 6 choices, got %d", d.Choices)
	}
	if d.Mode != ModeChoice {
		t.Errorf("expected ModeChoice from directive after a blank line, got %v", d.Mode)
	}
	if d.Order != OrderSequential {
		t.Errorf("expected sequential order from directive after a blank line, got %v", d.Order)
	}
}

func TestParsePerCardChoices(t *testing.T) {
	content := `# choice-count: 4

q1
---
a1

# choice-count: 6
q2
---
a2
`
	path := writeTempDeck(t, content)
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(d.Cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(d.Cards))
	}
	if d.Cards[0].Choices != 0 {
		t.Errorf("expected card 0 choices=0 (deck default), got %d", d.Cards[0].Choices)
	}
	if d.Cards[1].Choices != 6 {
		t.Errorf("expected card 1 choices=6, got %d", d.Cards[1].Choices)
	}
}

// questionText joins a card's question-side text Media into one string,
// matching how the GUI lays them out line by line.
func questionText(c *Card) string {
	var parts []string
	for _, m := range c.Question {
		if m.Type == Text {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func TestParseClozeBasic(t *testing.T) {
	// A separator-less card with a {{...}} deletion is a fill-in-the-blank card:
	// the braced text is blanked in the question and becomes the answer.
	d, err := Parse(writeTempDeck(t, "The capital of France is {{Paris}}.\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(d.Cards))
	}
	c := d.Cards[0]
	if c.AnswerText != "Paris" {
		t.Errorf("expected answer %q, got %q", "Paris", c.AnswerText)
	}
	if got := questionText(&c); got != "The capital of France is ____." {
		t.Errorf("expected blanked question, got %q", got)
	}
	if !c.Cloze {
		t.Error("expected Cloze flag set (reverse mode skips cloze cards)")
	}
}

func TestParseClozeMultiple(t *testing.T) {
	d, err := Parse(writeTempDeck(t, "{{Romeo}} and {{Juliet}}\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := d.Cards[0]
	if c.AnswerText != "Romeo Juliet" {
		t.Errorf("expected joined answer %q, got %q", "Romeo Juliet", c.AnswerText)
	}
	if got := questionText(&c); got != "____ and ____" {
		t.Errorf("expected both deletions blanked, got %q", got)
	}
}

func TestParseClozeWithAcceptAndDistractors(t *testing.T) {
	content := `Water is H2{{O}}.
= oxygen
~ hydrogen
`
	d, err := Parse(writeTempDeck(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := d.Cards[0]
	if c.AnswerText != "O" {
		t.Errorf("expected answer %q, got %q", "O", c.AnswerText)
	}
	if len(c.Accept) != 1 || c.Accept[0] != "oxygen" {
		t.Errorf("expected accept [oxygen], got %v", c.Accept)
	}
	if len(c.Distractors) != 1 || c.Distractors[0] != "hydrogen" {
		t.Errorf("expected distractors [hydrogen], got %v", c.Distractors)
	}
}

func TestParseClozeEmptyDeletionErrors(t *testing.T) {
	if _, err := Parse(writeTempDeck(t, "an empty {{}} blank\n")); err == nil {
		t.Fatal("expected error for empty {{}} deletion")
	}
}

func TestParseClozeStillRequiresSeparatorWhenNoBraces(t *testing.T) {
	// Without braces, a separator-less card is still the existing error, not a
	// silently-accepted cloze.
	if _, err := Parse(writeTempDeck(t, "no separator and no cloze\n")); err == nil {
		t.Fatal("expected missing-separator error")
	}
}

func TestParseTimeLimitUpperBound(t *testing.T) {
	// An absurd per-question limit is rejected (and warned) rather than honored.
	d, err := Parse(writeTempDeck(t, "# time-limit: 999999\n\nq\n---\na\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.TimeLimit != 0 {
		t.Errorf("expected out-of-range time limit ignored (0), got %d", d.TimeLimit)
	}
	if len(d.Warnings) == 0 {
		t.Error("expected a warning for the out-of-range # time-limit:")
	}
}

func TestParseWarnsOnInvalidDirective(t *testing.T) {
	d, err := Parse(writeTempDeck(t, "# choice-count: banana\n# answer-mode: sideways\n\nq\n---\na\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d: %v", len(d.Warnings), d.Warnings)
	}
	// The bad directives are ignored: defaults stand.
	if d.Choices != 4 {
		t.Errorf("expected default 4 choices after invalid value, got %d", d.Choices)
	}
}

func TestParseValidDirectivesNoWarnings(t *testing.T) {
	// A well-formed deck must not emit spurious warnings.
	d, err := Parse(writeTempDeck(t, "# choice-count: 3\n# time-limit: 30\n# answer-mode: choice\n\nq\n---\na\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", d.Warnings)
	}
}

// TestDeckModeBeatsDistractorInference: an explicit deck-level answer-mode
// wins over distractor-implied choice, so a typed deck can author "~" wrong
// answers purely for choice-mode sessions. Without the header, and with a
// per-card directive, behavior is unchanged.
func TestDeckModeBeatsDistractorInference(t *testing.T) {
	card := "q1\n---\na1\n~ w1\n~ w2\n"

	d, err := Parse(writeTempDeck(t, "# answer-mode: type\n\n"+card))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := d.Cards[0].Mode; got != ModeType {
		t.Errorf("deck-level type: card mode = %v, want ModeType", got)
	}
	if len(d.Cards[0].Distractors) != 2 {
		t.Errorf("distractors should be kept for choice sessions, got %v", d.Cards[0].Distractors)
	}

	d, err = Parse(writeTempDeck(t, card))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := d.Cards[0].Mode; got != ModeChoice {
		t.Errorf("no header: card mode = %v, want inferred ModeChoice", got)
	}

	d, err = Parse(writeTempDeck(t, "# answer-mode: type\n\n# answer-mode: choice\n"+card))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := d.Cards[0].Mode; got != ModeChoice {
		t.Errorf("per-card choice under typed deck: card mode = %v, want ModeChoice", got)
	}
}

// TestNoteSection: a third --- section is the card's note. It parses into
// Note, leaves the answer intact, and — critically — does not change the
// card ID, so annotating an existing card keeps its progress.
func TestNoteSection(t *testing.T) {
	plain, err := Parse(writeTempDeck(t, "hello\n---\nworld\n"))
	if err != nil {
		t.Fatalf("plain: %v", err)
	}
	noted, err := Parse(writeTempDeck(t, "hello\n---\nworld\n---\nA note line.\nAnother note line.\n"))
	if err != nil {
		t.Fatalf("noted: %v", err)
	}
	c := noted.Cards[0]
	if c.AnswerText != "world" {
		t.Errorf("answer = %q, want world", c.AnswerText)
	}
	if len(c.Note) != 2 || c.Note[0].Content != "A note line." {
		t.Errorf("note = %+v, want two text lines", c.Note)
	}
	if c.ID != plain.Cards[0].ID {
		t.Error("adding a note must not change the card ID")
	}
	if len(noted.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", noted.Warnings)
	}
}

// TestNoteSectionImage: notes carry images.
func TestNoteSectionImage(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(img, []byte("not-really-png"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "test.deck")
	content := "q\n---\na\n---\nSee:\n@img pic.png\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	note := d.Cards[0].Note
	if len(note) != 2 || note[1].Type != Image {
		t.Fatalf("note = %+v, want text + image", note)
	}
}

// TestNoteSectionRejectsAudio: audio in a note is dropped with a warning —
// its playback ordering against the card's own clip isn't designed yet.
func TestNoteSectionRejectsAudio(t *testing.T) {
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mp3")
	if err := os.WriteFile(clip, []byte("mp3"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "test.deck")
	if err := os.WriteFile(path, []byte("q\n---\na\n---\nWhy:\n@audio clip.mp3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	d, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	note := d.Cards[0].Note
	if len(note) != 1 || note[0].Type != Text {
		t.Fatalf("note = %+v, want the text line only", note)
	}
	if len(d.Warnings) != 1 || !strings.Contains(d.Warnings[0], "note audio") {
		t.Errorf("expected one note-audio warning, got %v", d.Warnings)
	}
}

// TestNoteSectionTooManySections: a fourth section is malformed.
func TestNoteSectionTooManySections(t *testing.T) {
	_, err := Parse(writeTempDeck(t, "q\n---\na\n---\nnote\n---\nwhat\n"))
	if err == nil || !strings.Contains(err.Error(), "too many sections") {
		t.Fatalf("expected too-many-sections error, got %v", err)
	}
}

// TestNoteSurvivesReverse: the note explains the pairing, so the reversed
// card carries it unchanged.
func TestNoteSurvivesReverse(t *testing.T) {
	d, err := Parse(writeTempDeck(t, "chien\n---\ndog\n---\nFrom Latin canis.\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rev := d.Reversed()
	if len(rev.Cards) != 1 {
		t.Fatalf("expected 1 reversed card, got %d", len(rev.Cards))
	}
	if len(rev.Cards[0].Note) != 1 || rev.Cards[0].Note[0].Content != "From Latin canis." {
		t.Errorf("reversed note = %+v, want the original note", rev.Cards[0].Note)
	}
}
