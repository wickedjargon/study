package quiz

import (
	"strings"

	"study/deck"
)

// Near-miss detection: a wrong typed answer whose spelling is close enough
// to an accepted answer that the miss reads as finger trouble, not absent
// knowledge. A near miss is still a miss — the grade, the session criterion,
// and the review ladder are untouched — but the result screen demands
// transcription practice (practiceReps correct retypes of the exposed
// answer) before it lets go, so the fingers rehearse the spelling while the
// card is in front of the user.
//
// Precedence: confusion wins. An input that is another card's answer is a
// discrimination failure between cards, handled by noteConfusion, and must
// not be softened into a typo (AnswerTyped checks in that order).

// practiceReps is how many correct transcriptions a near miss owes before
// the result screen may advance.
const practiceReps = 3

// editBudget is how many edits (Damerau-Levenshtein: insert, delete,
// substitute, or swap two adjacent runes) an answer of the given rune length
// may absorb and still count as a near miss. Short answers get almost no
// room — one edit on a four-letter word is a quarter of the word.
func editBudget(n int) int {
	switch {
	case n < 4:
		return 0
	case n <= 6:
		return 1
	default:
		return 2
	}
}

// totalBudget caps edits across a *sentence* answer (4+ words): two slips
// anywhere is finger trouble, three or more is not knowing the answer. Short
// answers — names, places, up to 3 words — rely on per-word budgets alone:
// "theadoore rosevelt" is 3 edits, but both words are within budget, and
// nearly-right-everywhere is exactly what a spelling mistake looks like.
const (
	totalBudget    = 2
	totalCapAtWords = 4
)

// nearMiss reports whether the (already failed) typed answer is a spelling
// near miss of one of the card's accepted answers. Comparison happens on
// normalized text, so case, accents, and punctuation never count as edits —
// the grader already forgives those entirely.
func (e *Engine) nearMiss(c *deck.Card, got string) bool {
	gotN := normalizeAnswer(got)
	if gotN == "" {
		return false
	}
	candidates := make([]string, 0, 1+len(c.Accept))
	candidates = append(candidates, c.AnswerText)
	candidates = append(candidates, c.Accept...)
	for _, cand := range candidates {
		candN := normalizeAnswer(cand)
		if candN == "" {
			continue
		}
		// Whole string: catches in-place misspellings.
		if d := damerau(gotN, candN); d > 0 && d <= editBudget(len([]rune(candN))) {
			return true
		}
		// Word bag: catches scrambled word order ("rosavelt d franklin"),
		// where the whole-string distance explodes but every word matches
		// its counterpart within budget.
		if wordBagMatch(gotN, candN) {
			return true
		}
	}
	return false
}

// wordBagMatch reports whether two multi-word strings pair up word-for-word
// ignoring order, each pair within its per-word edit budget — and, for
// sentence-length answers, the whole within totalBudget. A perfect pairing
// with zero edits (pure scramble) is also a near miss: the knowledge is
// there, the convention isn't. Answers longer than a handful of words are
// left to the whole-string check — sentence answers are order-sensitive
// content, and a scrambled sentence is simply wrong.
func wordBagMatch(got, cand string) bool {
	gw, cw := strings.Fields(got), strings.Fields(cand)
	if len(gw) != len(cw) || len(cw) < 2 || len(cw) > 5 {
		return false
	}
	capTotal := -1 // short answers: per-word budgets only
	if len(cw) >= totalCapAtWords {
		capTotal = totalBudget
	}
	best := -1
	permute(gw, func(p []string) {
		total := 0
		for i, w := range p {
			d := damerau(w, cw[i])
			if d > editBudget(len([]rune(cw[i]))) {
				return // this pairing breaks a per-word budget
			}
			total += d
		}
		if best < 0 || total < best {
			best = total
		}
	})
	return best >= 0 && (capTotal < 0 || best <= capTotal)
}

// permute calls f with every permutation of s (Heap's algorithm, in place;
// callers must not retain the slice).
func permute(s []string, f func([]string)) {
	var rec func(k int)
	rec = func(k int) {
		if k == 1 {
			f(s)
			return
		}
		for i := 0; i < k; i++ {
			rec(k - 1)
			if k%2 == 0 {
				s[i], s[k-1] = s[k-1], s[i]
			} else {
				s[0], s[k-1] = s[k-1], s[0]
			}
		}
	}
	if len(s) > 0 {
		rec(len(s))
	}
}

// damerau returns the optimal-string-alignment distance between two strings:
// Levenshtein plus adjacent transposition as a single edit, the classic typo
// metric ("madird" is one edit from "madrid", not two).
func damerau(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	prev2 := make([]int, lb+1) // row i-2
	prev := make([]int, lb+1)  // row i-1
	cur := make([]int, lb+1)   // row i
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			m := min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				m = min(m, prev2[j-2]+1)
			}
			cur[j] = m
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[lb]
}

// PracticeOwed returns how many correct transcriptions the current near-miss
// result still requires before Next will advance (0: none owed).
func (e *Engine) PracticeOwed() int {
	return e.practiceOwed
}

// PracticeTyped submits one transcription attempt on the result screen and
// reports whether it counted. Compared with the same leniency as grading;
// a wrong transcription costs nothing but doesn't count.
func (e *Engine) PracticeTyped(input string) bool {
	if e.state != ShowResult || e.practiceOwed <= 0 || e.current == nil {
		return false
	}
	if !e.accepts(e.current, input) {
		return false
	}
	e.practiceOwed--
	return true
}
