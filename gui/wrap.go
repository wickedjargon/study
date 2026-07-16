package gui

import "strings"

// wrapLines breaks text into lines no wider than maxW pixels, as reported by
// measure: soft-wrapped at spaces, greedily filled; a single word wider than
// the whole line is broken rune-by-rune rather than run off the window. Text
// that fits comes back unchanged as one line.
func wrapLines(text string, maxW int, measure func(string) int) []string {
	if measure(text) <= maxW {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	var lines []string
	cur := ""
	for _, word := range words {
		cand := word
		if cur != "" {
			cand = cur + " " + word
		}
		if measure(cand) <= maxW {
			cur = cand
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		// The word alone may still be too wide: hard-break it. n never goes
		// below one rune, so even an impossibly narrow window makes progress.
		cur = word
		for measure(cur) > maxW {
			runes := []rune(cur)
			if len(runes) == 1 {
				break
			}
			n := len(runes) - 1
			for n > 1 && measure(string(runes[:n])) > maxW {
				n--
			}
			lines = append(lines, string(runes[:n]))
			cur = string(runes[n:])
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
