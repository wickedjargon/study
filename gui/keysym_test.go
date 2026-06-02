package gui

import (
	"testing"

	"github.com/BurntSushi/xgb/xproto"
)

func TestKeysymToRune(t *testing.T) {
	cases := []struct {
		name string
		ks   uint32
		want rune
		ok   bool
	}{
		// ASCII (Latin-1 keysyms map directly to the code point).
		{"ascii lowercase a", 0x0061, 'a', true},
		{"ascii uppercase A", 0x0041, 'A', true},
		{"ascii digit", 0x0039, '9', true},
		{"space", 0x0020, ' ', true},
		// Latin-1 accented characters (keysym == code point).
		{"a acute", 0x00e1, 'á', true},
		{"n tilde", 0x00f1, 'ñ', true},
		{"u diaeresis", 0x00fc, 'ü', true},
		{"c cedilla", 0x00e7, 'ç', true},
		// Unicode keysyms: 0x01000000 | code point.
		{"hiragana ko", 0x01000000 | 0x3053, 'こ', true},
		{"cjk ri", 0x01000000 | 0x65e5, '日', true},
		{"cyrillic de via unicode keysym", 0x01000000 | 0x0434, 'д', true},
		{"emoji via unicode keysym", 0x01010000 | 0x1f600, '😀', true},
		// Non-printable / control keysyms must be rejected.
		{"return keysym", 0xff0d, 0, false},
		{"escape keysym", 0xff1b, 0, false},
		{"backspace keysym", 0xff08, 0, false},
		{"del control char", 0x007f, 0, false},
		{"nul", 0x0000, 0, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := keysymToRune(xproto.Keysym(c.ks))
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (rune %U)", ok, c.ok, got)
			}
			if ok && got != c.want {
				t.Fatalf("rune = %U (%q), want %U (%q)", got, got, c.want, c.want)
			}
		})
	}
}
