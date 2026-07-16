package deck

import "path/filepath"

// CardLabel returns a short, single-line identifier for a card, used in
// progress listings (--stats, the library's stats screen) so the user can
// tell cards apart. It tries, in order: the question text (what the user
// authored and sees while studying); the answer text, marked with "→" so it's
// clear the label is the answer side (this is what makes media-only question
// cards — e.g. an image flashcard whose answer is a word — distinguishable);
// the file name of the card's first media element; and finally "(media card)"
// only when a card carries no text or media name at all.
func CardLabel(c *Card) string {
	if s := JoinText(c.Question); s != "" {
		return clipLabel(s)
	}
	if s := JoinText(c.Answer); s != "" {
		return clipLabel("→ " + s)
	}
	if s := firstMediaName(c.Question); s != "" {
		return clipLabel("[" + s + "]")
	}
	return "(media card)"
}

// firstMediaName returns the base file name of the first image or audio
// element on a card side, or "" if there is none.
func firstMediaName(media []Media) string {
	for _, m := range media {
		if m.Type == Image || m.Type == Audio {
			return filepath.Base(m.Content)
		}
	}
	return ""
}

// clipLabel truncates a label to a fixed width for the listing.
func clipLabel(s string) string {
	const max = 48
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}
