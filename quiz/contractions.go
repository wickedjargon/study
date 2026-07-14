package quiz

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// English contractions are folded into lenient answer matching, so a card
// saying "I do not understand" accepts "I don't understand" (and vice versa)
// without every deck authoring "=" variants for them.
//
// Matching is set-based: each side of the comparison renders into the set of
// its plausible readings — every token contributes its punctuation-stripped
// original plus any contraction expansions — and the answer is accepted when
// the sets intersect. Keeping the original in the set is what makes this
// safe: expansions only ever add ways to match, never remove one.
//
// Apostrophe-carrying tokens ("don't", "she's") expand unconditionally;
// ambiguous ones ('s, 'd) expand both ways, so "she's" matches "she is" and
// "she has" alike. Bare forms expand only from a vetted list ("dont", "im",
// "youre") that excludes tokens which are real words in their own right —
// "ill", "well", "were", "wont", "id", "its" — since a card whose answer is
// the plain word must not accept the contraction's expansion.

// bareContractions maps apostrophe-less contraction spellings that are not
// themselves English words. Values are the possible expansions.
var bareContractions = map[string][]string{
	"im": {"i am"}, "ive": {"i have"},
	"youre": {"you are"}, "youve": {"you have"}, "youll": {"you will"},
	"theyre": {"they are"}, "theyve": {"they have"}, "theyll": {"they will"},
	"weve": {"we have"},
	"hes":  {"he is", "he has"}, "shes": {"she is", "she has"},
	"isnt": {"is not"}, "arent": {"are not"}, "wasnt": {"was not"}, "werent": {"were not"},
	"dont": {"do not"}, "doesnt": {"does not"}, "didnt": {"did not"},
	"cant": {"cannot", "can not"}, "couldnt": {"could not"},
	"shouldnt": {"should not"}, "wouldnt": {"would not"}, "mustnt": {"must not"},
	"hasnt": {"has not"}, "havent": {"have not"}, "hadnt": {"had not"},
	"lets": {"let us"}, "thats": {"that is", "that has"},
	"whats": {"what is", "what has"}, "whos": {"who is", "who has"},
	"heres": {"here is"}, "theres": {"there is", "there has"},
	"wheres": {"where is"}, "hows": {"how is"},
	"aint": {"is not", "am not", "are not", "has not", "have not"},
}

// suffixContractions expand apostrophe-carrying tokens by their ending; the
// apostrophe makes the intent unambiguous, so these apply to any stem
// ("bird'll" → "bird will").
var suffixContractions = []struct {
	suffix     string
	expansions []string
}{
	{"n't", []string{" not"}}, // don't, isn't, … (won't/can't/shan't special-cased below)
	{"'ll", []string{" will"}},
	{"'re", []string{" are"}},
	{"'ve", []string{" have"}},
	{"'m", []string{" am"}},
	{"'s", []string{" is", " has"}},
	{"'d", []string{" would", " had"}},
}

// irregularContractions are apostrophe forms whose stem changes.
var irregularContractions = map[string][]string{
	"won't": {"will not"}, "can't": {"cannot", "can not"}, "shan't": {"shall not"},
	"let's": {"let us"}, "ain't": {"is not", "am not", "are not", "has not", "have not"},
	"y'all": {"you all"},
}

// maxReadings caps the expansion set; an answer with many ambiguous tokens
// falls back to plain comparison rather than a combinatorial blow-up.
const maxReadings = 64

// tokenReadings returns every rendering of one lowercased token that still
// carries its apostrophes: the apostrophe-stripped original, plus any
// contraction expansions.
func tokenReadings(tok string) []string {
	readings := []string{strings.ReplaceAll(tok, "'", "")}
	if exp, ok := irregularContractions[tok]; ok {
		return append(readings, exp...)
	}
	if strings.Contains(tok, "'") {
		for _, s := range suffixContractions {
			if stem, ok := strings.CutSuffix(tok, s.suffix); ok && stem != "" {
				stem = strings.ReplaceAll(stem, "'", "")
				for _, e := range s.expansions {
					readings = append(readings, stem+e)
				}
				return readings
			}
		}
		return readings
	}
	return append(readings, bareContractions[tok]...)
}

// contractionTokens lowercases and strips an answer like normalizeAnswer,
// but keeps apostrophes (folding the typographic ’) so contractions are
// still recognizable, and returns the words.
func contractionTokens(s string) []string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case r == '\'' || r == '’':
			b.WriteRune('\'')
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}

// readings renders an answer into the set of its normalized readings under
// contraction expansion. A nil return means the expansion set was too large
// to enumerate.
func readings(s string) map[string]bool {
	variants := []string{""}
	for _, tok := range contractionTokens(s) {
		renderings := tokenReadings(tok)
		if len(variants)*len(renderings) > maxReadings {
			return nil
		}
		next := make([]string, 0, len(variants)*len(renderings))
		for _, v := range variants {
			for _, r := range renderings {
				if v == "" {
					next = append(next, r)
				} else {
					next = append(next, v+" "+r)
				}
			}
		}
		variants = next
	}
	set := make(map[string]bool, len(variants))
	for _, v := range variants {
		set[v] = true
	}
	return set
}

// matchesContracted reports whether two answers agree under some reading of
// their contractions.
func matchesContracted(got, cand string) bool {
	gs, cs := readings(got), readings(cand)
	if gs == nil || cs == nil {
		return false
	}
	for g := range gs {
		if cs[g] {
			return true
		}
	}
	return false
}
