package main

import (
	"testing"
	"time"

	"study/deck"
	"study/progress"
)

func newTestStore(t *testing.T) *progress.Store {
	t.Helper()
	s, err := progress.NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return s
}

func TestSplitDue(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	cards := []deck.Card{{ID: "new"}, {ID: "overdue"}, {ID: "verylate"}, {ID: "ahead"}}

	// overdue: due an hour ago; verylate: due a day ago; ahead: due tomorrow.
	store.RecordCorrect("overdue")
	store.Get("overdue").Due = now.Add(-time.Hour)
	store.RecordCorrect("verylate")
	store.Get("verylate").Due = now.Add(-24 * time.Hour)
	store.RecordCorrect("ahead")
	store.Get("ahead").Due = now.Add(24 * time.Hour)

	reviews, fresh, nextDue := splitDue(cards, store, now)

	// Reviews: both due cards, most overdue first. The future card is out.
	if len(reviews) != 2 || reviews[0].ID != "verylate" || reviews[1].ID != "overdue" {
		t.Errorf("reviews = %v, want [verylate overdue]", reviews)
	}
	if len(fresh) != 1 || fresh[0].ID != "new" {
		t.Errorf("fresh = %v, want [new]", fresh)
	}
	if nextDue.IsZero() || !nextDue.Equal(store.Get("ahead").Due) {
		t.Errorf("nextDue = %v, want ahead's due time", nextDue)
	}
}

func TestCardLabel(t *testing.T) {
	tests := []struct {
		name string
		card deck.Card
		want string
	}{
		{
			name: "question text preferred",
			card: deck.Card{
				Question: []deck.Media{{Type: deck.Text, Content: "What is 2 + 2?"}},
				Answer:   []deck.Media{{Type: deck.Text, Content: "4"}},
			},
			want: "What is 2 + 2?",
		},
		{
			name: "media question falls back to answer text",
			card: deck.Card{
				Question: []deck.Media{{Type: deck.Image, Content: "/decks/tiles/3bamboo.png"}},
				Answer:   []deck.Media{{Type: deck.Text, Content: "3 Bamboos"}},
			},
			want: "→ 3 Bamboos",
		},
		{
			name: "multiline question collapsed to one line",
			card: deck.Card{
				Question: []deck.Media{
					{Type: deck.Text, Content: "line one"},
					{Type: deck.Text, Content: "line two"},
				},
				Answer: []deck.Media{{Type: deck.Text, Content: "answer"}},
			},
			want: "line one line two",
		},
		{
			name: "media on both sides falls back to file name",
			card: deck.Card{
				Question: []deck.Media{{Type: deck.Image, Content: "/decks/flags/france.png"}},
				Answer:   []deck.Media{{Type: deck.Image, Content: "/decks/flags/answer.png"}},
			},
			want: "[france.png]",
		},
		{
			name: "no text or media",
			card: deck.Card{
				Question: []deck.Media{{Type: deck.Text, Content: ""}},
				Answer:   []deck.Media{{Type: deck.Text, Content: ""}},
			},
			want: "(media card)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cardLabel(&tt.card); got != tt.want {
				t.Errorf("cardLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClipLabel(t *testing.T) {
	long := "this is a rather long answer that should be truncated at the limit"
	got := clipLabel(long)
	if r := []rune(got); len(r) != 48 {
		t.Errorf("clipLabel length = %d, want 48", len(r))
	}
	if got[len(got)-len("…"):] != "…" {
		t.Errorf("clipLabel should end with ellipsis, got %q", got)
	}
}
