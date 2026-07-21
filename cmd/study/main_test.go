package main

import (
	"reflect"
	"testing"
)

func TestParseAhead(t *testing.T) {
	cases := []struct {
		in   string
		days int
		all  bool
		ok   bool
	}{
		{"all", 0, true, true},
		{"3", 3, false, true},
		{"1", 1, false, true},
		{"0", 0, false, false},
		{"-2", 0, false, false},
		{"soon", 0, false, false},
	}
	for _, tc := range cases {
		days, all, ok := parseAhead(tc.in)
		if days != tc.days || all != tc.all || ok != tc.ok {
			t.Errorf("parseAhead(%q) = (%d, %v, %v), want (%d, %v, %v)",
				tc.in, days, all, ok, tc.days, tc.all, tc.ok)
		}
	}
}

// TestStrayDeckFlags: with no deck argument, deck-scoped flags must be caught
// (so `study --stats` errors instead of opening the library), while the
// standalone flags pass through.
func TestStrayDeckFlags(t *testing.T) {
	cases := []struct {
		name string
		set  []string
		want []string
	}{
		{"nothing set", nil, nil},
		{"stats needs a deck", []string{"stats"}, []string{"stats"}},
		{"forget needs a deck", []string{"forget"}, []string{"forget"}},
		{"reverse needs a deck", []string{"reverse"}, []string{"reverse"}},
		{"session overrides need a deck",
			[]string{"order", "answer-mode", "ahead", "time-limit", "wrong-pause",
				"preview-new", "new-per-session", "font-size", "audio-speed"},
			[]string{"order", "answer-mode", "ahead", "time-limit", "wrong-pause",
				"preview-new", "new-per-session", "font-size", "audio-speed"}},
		{"library maintenance stands alone", []string{"watch", "unwatch", "library", "help"}, nil},
		{"mixed keeps only the deck-scoped ones", []string{"library", "stats"}, []string{"stats"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strayDeckFlags(tc.set); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("strayDeckFlags(%v) = %v, want %v", tc.set, got, tc.want)
			}
		})
	}
}
