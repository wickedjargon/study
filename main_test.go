package main

import (
	"testing"

	"study/deck"
)

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
