package gio

import (
	_ "embed"
	"log"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/text"
)

// Noto Sans, embedded: the user prefers a plain sans-serif over the Go
// font's humanist flare. The Go fonts stay in the collection behind it as
// fallback for glyphs Noto's Latin build lacks.
//
//go:embed fonts/NotoSans-Regular.ttf
var notoRegular []byte

//go:embed fonts/NotoSans-Bold.ttf
var notoBold []byte

// collection returns the UI's font faces, Noto Sans first so the shaper's
// default resolution lands on it.
func collection() []text.FontFace {
	var faces []text.FontFace
	for _, f := range []struct {
		data   []byte
		weight font.Weight
	}{
		{notoRegular, font.Normal},
		{notoBold, font.Bold},
	} {
		face, err := opentype.Parse(f.data)
		if err != nil {
			log.Printf("study-gio: parsing embedded font: %v", err)
			continue
		}
		faces = append(faces, text.FontFace{
			Font: font.Font{Typeface: "Noto Sans", Weight: f.weight},
			Face: face,
		})
	}
	return append(faces, gofont.Collection()...)
}
