package gui

import (
	"testing"
	"time"
)

func TestClampSpeed(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{minSpeed - 1, minSpeed},
		{maxSpeed + 1, maxSpeed},
		{0, minSpeed}, // below the floor
		{1.0, 1.0},
		{0.75, 0.75},
	}
	for _, c := range cases {
		if got := clampSpeed(c.in); got != c.want {
			t.Errorf("clampSpeed(%g) = %g, want %g", c.in, got, c.want)
		}
	}
}

func TestWindowScale(t *testing.T) {
	cases := []struct {
		name   string
		height int
		want   float64
	}{
		{"default height is unity", defaultHeight, 1.0},
		{"tiny window clamps to floor", 100, 0.8},
		{"huge window clamps to ceiling", 100000, 2.0},
		{"double height scales linearly", defaultHeight * 3 / 2, 1.5},
	}
	for _, c := range cases {
		a := &App{height: c.height}
		if got := a.windowScale(); got != c.want {
			t.Errorf("%s: windowScale(height=%d) = %g, want %g", c.name, c.height, got, c.want)
		}
	}
}

func TestTextScaleAndScaled(t *testing.T) {
	a := &App{height: defaultHeight, dpi: 96, baseFontPt: defaultFontPt}
	if ts := a.textScale(); ts <= 0 {
		t.Fatalf("textScale = %g, want > 0", ts)
	}
	// scaled() must grow with the base font size: bigger fonts → bigger gaps.
	small := a.scaled(20)
	a.baseFontPt = maxFontPt
	big := a.scaled(20)
	if big <= small {
		t.Errorf("scaled gap did not grow with font size: small=%d big=%d", small, big)
	}
	// A zero spacer always scales to zero, regardless of size.
	if got := a.scaled(0); got != 0 {
		t.Errorf("scaled(0) = %d, want 0", got)
	}
}

func TestHasArabic(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want bool
	}{
		{"latin", "hello world", false},
		{"digits", "12345", false},
		{"cjk is not arabic", "日本語", false},
		{"arabic word", "سلام", true},
		{"persian word", "خداحافظ", true},
		{"mixed latin + arabic", "hello سلام", true},
		{"presentation form", "ﭐ", true},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := hasArabic(c.s); got != c.want {
			t.Errorf("hasArabic(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestSecondsUntil(t *testing.T) {
	if got := secondsUntil(time.Time{}); got != -1 {
		t.Errorf("zero time = %d, want -1 (not armed)", got)
	}
	if got := secondsUntil(time.Now().Add(-time.Second)); got != 0 {
		t.Errorf("past deadline = %d, want 0", got)
	}
	// Rounded up: 2.5s remaining displays as 3.
	if got := secondsUntil(time.Now().Add(2500 * time.Millisecond)); got != 3 {
		t.Errorf("2.5s remaining = %d, want 3", got)
	}
}
