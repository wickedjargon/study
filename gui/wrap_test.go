package gui

import "testing"

// runeWidth measures a line as one pixel per rune, so widths in these tests
// read as rune counts.
func runeWidth(s string) int { return len([]rune(s)) }

func TestWrapLines(t *testing.T) {
	tests := []struct {
		name string
		text string
		maxW int
		want []string
	}{
		{
			name: "fits untouched",
			text: "short line",
			maxW: 20,
			want: []string{"short line"},
		},
		{
			name: "soft wrap at spaces",
			text: "you must use headlights after sunset",
			maxW: 13,
			want: []string{"you must use", "headlights", "after sunset"},
		},
		{
			name: "single monster word hard-breaks",
			text: "supercalifragilistic",
			maxW: 8,
			want: []string{"supercal", "ifragili", "stic"},
		},
		{
			name: "monster word mid-sentence",
			text: "a supercalifragilistic day",
			maxW: 8,
			want: []string{"a", "supercal", "ifragili", "stic day"},
		},
		{
			name: "whitespace-only text is left alone",
			text: "   ",
			maxW: 1,
			want: []string{"   "},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapLines(tt.text, tt.maxW, runeWidth)
			if len(got) != len(tt.want) {
				t.Fatalf("wrapLines(%q, %d) = %q, want %q", tt.text, tt.maxW, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
