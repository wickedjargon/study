package quiz

import (
	"strings"

	"study/deck"
)

// Set-answer cards: the user enumerates the card's "+" items one entry at a
// time, any order, until SetTarget distinct items are named (the quota, or
// all of them). The card stays in ShowQuestion for the whole loop; the
// completing entry (or giving up) produces the card's single Result, so the
// scheduler still sees one binary verdict per card: a clean enumeration is
// correct, any wrong guess or a give-up is a miss.

// SetVerdict classifies one entry of a set card.
type SetVerdict int

const (
	// SetHit: a not-yet-named item, now checked off.
	SetHit SetVerdict = iota
	// SetDuplicate: an item already named this serve; costs nothing.
	SetDuplicate
	// SetClose: within near-miss distance of an unnamed item — neither
	// checked off nor penalized, retype it (pre-result automatic leniency,
	// same stance as the practice loop).
	SetClose
	// SetMiss: not one of the items; recorded, and the card's eventual
	// verdict is now wrong.
	SetMiss
)

// SetOutcome is the engine's response to one set-card entry. Result is
// non-nil when this entry completed the card (the target was reached) —
// the session has moved to ShowResult.
type SetOutcome struct {
	Verdict SetVerdict
	Item    int // index into SetItems for Hit/Duplicate, -1 otherwise
	Result  *Result
}

// setState is the per-serve progress of the current set card. Reset by
// advance() whenever a card is served.
type setState struct {
	named map[int]bool
	wrong int
	log   []SetLogEntry
}

// SetLogEntry is one counted entry of the current serve, in the order it
// was typed: a named item (canonical text, Hit) or a wrong guess (as
// typed). The costless outcomes (duplicates, near spellings) don't log.
type SetLogEntry struct {
	Text string
	Hit  bool
}

// SetLog returns the serve's counted entries in order — the transcript both
// frontends print above the input.
func (e *Engine) SetLog() []SetLogEntry {
	return e.set.log
}

// SetNamed reports which of the current card's items have been named this
// serve, aligned with Card.SetItems. Valid through the result screen (the
// reveal marks what was and wasn't named).
func (e *Engine) SetNamed() []bool {
	if e.current == nil || !e.current.IsSet() {
		return nil
	}
	out := make([]bool, len(e.current.SetItems))
	for i := range out {
		out[i] = e.set.named[i]
	}
	return out
}

// SetNamedCount returns how many distinct items have been named this serve.
func (e *Engine) SetNamedCount() int { return len(e.set.named) }

// AnswerSetEntry submits one entry of the current set card. Nil when the
// current card isn't an unanswered set card.
func (e *Engine) AnswerSetEntry(input string) *SetOutcome {
	if e.state != ShowQuestion || e.current == nil || !e.current.IsSet() {
		return nil
	}
	if e.set.named == nil {
		e.set.named = make(map[int]bool)
	}
	items := e.current.SetItems

	// A hit on an unnamed item, or a duplicate of a named one.
	for i := range items {
		if !itemMatches(&items[i], input, e.caseSensitive) {
			continue
		}
		if e.set.named[i] {
			return &SetOutcome{Verdict: SetDuplicate, Item: i}
		}
		e.set.named[i] = true
		e.set.log = append(e.set.log, SetLogEntry{Text: items[i].Text, Hit: true})
		out := &SetOutcome{Verdict: SetHit, Item: i}
		if len(e.set.named) >= e.current.SetTarget() {
			out.Result = e.finishSet(false)
		}
		return out
	}

	// Near an unnamed item: no check-off, no penalty — retype it.
	for i := range items {
		if e.set.named[i] {
			continue
		}
		if itemNear(&items[i], input) {
			return &SetOutcome{Verdict: SetClose, Item: -1}
		}
	}

	e.set.wrong++
	e.set.log = append(e.set.log, SetLogEntry{Text: strings.TrimSpace(input), Hit: false})
	out := &SetOutcome{Verdict: SetMiss, Item: -1}
	// An attempts cap counts exactly the logged entries. The card ends the
	// moment the target is out of reach — playing out dead attempts can't
	// change a verdict that only misses can have tainted.
	if left := e.SetAttemptsLeft(); left >= 0 && left < e.current.SetTarget()-len(e.set.named) {
		out.Result = e.finishSet(false)
	}
	return out
}

// SetAttemptsLeft returns how many counted entries the current set card
// still allows, or -1 when it has no attempts cap.
func (e *Engine) SetAttemptsLeft() int {
	if e.current == nil || !e.current.IsSet() || e.current.Attempts <= 0 {
		return -1
	}
	if left := e.current.Attempts - len(e.set.log); left > 0 {
		return left
	}
	return 0
}

// AnswerSetGiveUp ends the current set card without reaching the target: a
// miss, with the reveal marking what was named. Nil outside a set card.
func (e *Engine) AnswerSetGiveUp() *Result {
	if e.state != ShowQuestion || e.current == nil || !e.current.IsSet() {
		return nil
	}
	return e.finishSet(true)
}

// finishSet produces the set card's single result and runs it through the
// standard answer path: one graded outcome, one log line, one scheduling
// decision.
func (e *Engine) finishSet(gaveUp bool) *Result {
	correct := !gaveUp && e.set.wrong == 0
	result := &Result{
		Card:    e.current,
		Chosen:  -1,
		Correct: correct,
		Answer:  e.current.AnswerText,
		NoIdea:  gaveUp && len(e.set.named) == 0,
	}
	e.TotalSeen++
	e.recordAnswer(correct)
	if correct {
		e.TotalCorrect++
		e.handleCorrect()
	} else {
		e.TotalWrong++
		e.handleWrong()
	}
	e.state = ShowResult
	return result
}

// itemMatches reports whether an entry names the given item, with the same
// leniency as single-answer grading.
func itemMatches(it *deck.SetItem, got string, caseSensitive bool) bool {
	c := deck.Card{AnswerText: it.Text, Accept: it.Accept}
	return cardAccepts(&c, got, caseSensitive)
}

// itemNear reports whether an entry is a spelling near miss of the item —
// the same tiered edit budgets as single answers, whole-string layer only
// (items are short; scrambling a one-word entry isn't a thing).
func itemNear(it *deck.SetItem, got string) bool {
	gotN := normalizeAnswer(got)
	if gotN == "" {
		return false
	}
	for _, cand := range append([]string{it.Text}, it.Accept...) {
		candN := normalizeAnswer(cand)
		if d := damerau(gotN, candN); d > 0 && d <= editBudget(len([]rune(candN))) {
			return true
		}
	}
	return false
}
