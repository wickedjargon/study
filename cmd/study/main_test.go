package main

import "testing"

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
