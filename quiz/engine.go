// Package quiz implements the quiz session logic.
//
// The default scheduler is evidence-based (see the README's "Scheduling"
// section): each card must be correctly recalled a criterion number
// of times per session, repetitions are spaced by intervening cards rather
// than massed back-to-back, and the session completes when every card meets
// its criterion. Sequential mode instead keeps Byki-style repeat-on-wrong
// drilling and cycles its laps forever.
package quiz

import (
	"math/rand"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"study/deck"
	"study/progress"
)

// State represents the current phase of the quiz for a single card.
type State int

const (
	ShowQuestion State = iota
	ShowResult
	Done
	// ShowPreview is the first-viewing reveal (deck "# preview-new: on" or
	// --preview-new): a card that has never been answered is presented with its
	// answer visible, to be studied once; ConfirmPreview then quizzes the very
	// same card.
	ShowPreview
)

// Result records the outcome of answering a single card.
type Result struct {
	Card     *deck.Card
	Chosen   int // index of chosen answer (0-based, -1 for type mode/timeout)
	Correct  bool
	Answer   string // the correct answer text
	Typed    string // what the user typed (type mode only)
	TimedOut bool   // true if the card's time limit expired before answering
}

// Engine drives the quiz session.
type Engine struct {
	deck          *deck.Deck
	choices       int
	mode          deck.QuizMode
	caseSensitive bool
	store         *progress.Store

	// Card queues. main is kept sorted by ascending due tick; the earliest-due
	// card is always served next. retry holds cards answered wrong that owe
	// consecutive correct repetitions.
	main  []queuedCard
	retry []*retryCard

	// step is a monotonic logical clock incremented each time a card is shown
	// from the main queue. A re-queued card's due tick is step+delay, so weak
	// cards (small delay) recur sooner than strong ones — yet because due ticks
	// are absolute and step only grows, every card is eventually served and
	// none can be starved.
	step int

	// All cards for generating distractors.
	allCards []*deck.Card

	// Current state.
	current       *deck.Card
	currentOpts   []string // current answer choices (choice mode only)
	correctIdx    int      // index of correct answer in currentOpts
	fromRetry     bool     // is current card from retry queue?
	repeatCurrent bool     // repeat this card immediately (wrong answer)
	state         State

	// preview enables the first-viewing reveal (ShowPreview). previewed marks
	// cards already revealed this session, so a card is never revealed twice —
	// which the store alone can't guarantee when it's nil or when the reveal was
	// confirmed but the card not yet answered.
	preview   bool
	previewed map[string]bool

	// Evidence-scheduler session state. need is each card's remaining correct
	// recalls before it meets this session's criterion and leaves the queue;
	// lapsed marks cards missed at least once this session, whose
	// between-session schedule therefore resets when they complete. Unused by
	// sequential mode's laps.
	need   map[string]int
	lapsed map[string]bool

	// Session stats.
	TotalSeen    int
	TotalCorrect int
	TotalWrong   int
}

// queuedCard is a card in the main queue tagged with the logical tick at which
// it becomes due to be shown.
type queuedCard struct {
	card *deck.Card
	due  int
}

// retryCard tracks a card in the retry queue.
type retryCard struct {
	card      *deck.Card
	remaining int // how many more times to show this card
}

// minRepeats is the minimum number of times a wrong card is repeated in
// sequential mode's drill.
const minRepeats = 3

// Evidence-scheduler session criterion, in correct recalls per card. Rawson &
// Dunlosky (2011): three recalls for new material in its first session, one
// recall per later relearning session. A card missed mid-session owes at
// least two, so a lapse is re-established rather than one-shot re-tested.
const (
	needNew       = 3
	needReview    = 1
	needAfterMiss = 2
)

// Evidence-scheduler within-session spacing, in serves. Karpicke &
// Bauernschmidt (2011): any nonzero gap between repeated retrievals of an
// item vastly outperforms back-to-back repetition (which only exercises
// short-term memory), while the exact gap pattern matters little. A missed
// card returns soon — but not immediately — and a correctly recalled one
// waits longer.
const (
	gapAfterMiss    = 3
	gapAfterCorrect = 8
)

// NewEngine creates a quiz engine from a parsed deck. Cards are presented in
// the deck's existing order (the caller shuffles/prioritizes beforehand).
func NewEngine(d *deck.Deck, store *progress.Store) *Engine {
	choices := d.Choices
	if choices < 2 {
		choices = 2
	}

	// Seed the main queue with due ticks matching the incoming order, so the
	// first pass presents cards in their (already shuffled/prioritized) order.
	main := make([]queuedCard, len(d.Cards))
	for i := range d.Cards {
		main[i] = queuedCard{card: &d.Cards[i], due: i}
	}

	allCards := make([]*deck.Card, len(d.Cards))
	for i := range d.Cards {
		allCards[i] = &d.Cards[i]
	}

	e := &Engine{
		deck:          d,
		choices:       choices,
		mode:          d.Mode,
		caseSensitive: d.CaseSensitive,
		store:         store,
		main:          main,
		allCards:      allCards,
		state:         ShowQuestion,
		preview:       d.Preview,
		previewed:     make(map[string]bool),
	}

	// Seed each card's session criterion for the evidence scheduler: new
	// material owes three spaced recalls in its first session, previously
	// learned material one relearning recall.
	if e.evidenceScheduled() {
		e.need = make(map[string]int, len(d.Cards))
		e.lapsed = make(map[string]bool)
		for i := range d.Cards {
			n := needNew
			if store != nil {
				cp := store.Get(d.Cards[i].ID)
				if cp.TimesCorrect+cp.TimesWrong > 0 {
					n = needReview
				}
			}
			e.need[d.Cards[i].ID] = n
		}
	}

	e.advance()
	return e
}

// evidenceScheduled reports whether this session runs the evidence-based
// scheduler (criterion learning with spaced repetitions, session completes).
// Sequential and flip-through keep their own lap scheduling.
func (e *Engine) evidenceScheduled() bool {
	switch e.deck.Order {
	case deck.OrderAdaptive, deck.OrderWeakOnly:
		return true
	}
	return false
}

// State returns the current quiz state.
func (e *Engine) State() State {
	return e.state
}

// Order returns the deck's session ordering mode.
func (e *Engine) Order() deck.OrderMode {
	return e.deck.Order
}

// DeckSize returns the number of cards in the session's deck.
func (e *Engine) DeckSize() int {
	return len(e.allCards)
}

// Mode returns the quiz mode for the current card.
func (e *Engine) Mode() deck.QuizMode {
	if e.current != nil {
		return e.current.Mode
	}
	return e.mode
}

// FontSize returns the deck's configured base font size in points,
// or 0 if the deck doesn't set one.
func (e *Engine) FontSize() int {
	return e.deck.FontSize
}

// Speed returns the deck's configured audio playback speed multiplier,
// or 0 if the deck doesn't set one (the GUI then uses its default of 1.0).
func (e *Engine) Speed() float64 {
	return e.deck.Speed
}

// Current returns the current card being quizzed.
func (e *Engine) Current() *deck.Card {
	return e.current
}

// CardIDs returns the IDs of every card in the session's deck, for scoping
// all-time stats to the deck (and direction) actually being studied.
func (e *Engine) CardIDs() []string {
	ids := make([]string, len(e.allCards))
	for i, c := range e.allCards {
		ids[i] = c.ID
	}
	return ids
}

// End finishes the session early: the quiz transitions to Done so the GUI can
// show the summary screen. Evidence-scheduled sessions also reach Done on
// their own when every card meets its criterion; sequential and flip-through
// cycle forever, so there this is the only way out.
func (e *Engine) End() {
	e.current = nil
	e.state = Done
}

// Options returns the current multiple choice answer strings.
func (e *Engine) Options() []string {
	return e.currentOpts
}

// IsRetry returns true if the current card is from the retry queue.
func (e *Engine) IsRetry() bool {
	return e.fromRetry
}

// Remaining returns the number of cards left (main + retry).
func (e *Engine) Remaining() int {
	n := len(e.main) + len(e.retry)
	if e.current != nil && e.state != Done {
		n++
	}
	return n
}

// Answer submits an answer (0-based index) and returns the result.
// Transitions state from ShowQuestion to ShowResult.
func (e *Engine) Answer(choice int) *Result {
	if e.state != ShowQuestion || e.current == nil {
		return nil
	}

	correct := choice == e.correctIdx
	result := &Result{
		Card:    e.current,
		Chosen:  choice,
		Correct: correct,
		Answer:  e.current.AnswerText,
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

// AnswerTyped submits a typed answer (type mode) and returns the result.
func (e *Engine) AnswerTyped(input string) *Result {
	if e.state != ShowQuestion || e.current == nil {
		return nil
	}

	expected := e.current.AnswerText
	got := strings.TrimSpace(input)
	correct := e.matchesAnswer(got)

	result := &Result{
		Card:    e.current,
		Chosen:  -1,
		Correct: correct,
		Answer:  expected,
		Typed:   got,
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

// matchesAnswer reports whether a typed answer counts as correct for the
// current card. It is checked against the primary answer and every accepted
// alternative ("= " lines). A case-sensitive deck requires an exact match
// (after trimming); otherwise answers are compared leniently via
// normalizeAnswer, so case, surrounding/embedded punctuation, and accents don't
// cause a right answer to be marked wrong.
func (e *Engine) matchesAnswer(got string) bool {
	candidates := make([]string, 0, 1+len(e.current.Accept))
	candidates = append(candidates, e.current.AnswerText)
	candidates = append(candidates, e.current.Accept...)

	got = strings.TrimSpace(got)
	for _, c := range candidates {
		if e.caseSensitive {
			if got == strings.TrimSpace(c) {
				return true
			}
		} else if normalizeAnswer(got) == normalizeAnswer(c) {
			return true
		}
	}
	return false
}

// normalizeAnswer canonicalizes a typed answer for lenient comparison: it
// lowercases, strips diacritics (so "salâm" matches "salam"), drops punctuation
// and symbols (so "i'm" matches "im" and "hello!" matches "hello"), and
// collapses runs of whitespace to single spaces. Letters and digits of any
// script are kept, so CJK and Arabic answers are unaffected beyond spacing.
func normalizeAnswer(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r): // combining mark (accent) — drop
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default: // punctuation, symbols — drop
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// TimeLimit returns the time limit in seconds for the current card, taking
// the deck-global limit and any per-card override into account. A return of
// 0 means the current card has no time limit.
func (e *Engine) TimeLimit() int {
	if e.current == nil {
		return 0
	}
	return e.current.EffectiveTimeLimit(e.deck.TimeLimit)
}

// AnswerTimeout records the current card as wrong because its time limit
// expired before the user answered. Transitions to ShowResult.
func (e *Engine) AnswerTimeout() *Result {
	if e.state != ShowQuestion || e.current == nil {
		return nil
	}

	result := &Result{
		Card:     e.current,
		Chosen:   -1,
		Correct:  false,
		Answer:   e.current.AnswerText,
		TimedOut: true,
	}

	e.TotalSeen++
	e.recordAnswer(false)
	e.TotalWrong++
	e.handleWrong()

	e.state = ShowResult
	return result
}

// Next advances to the next card after viewing the result.
// Transitions from ShowResult to ShowQuestion (or Done).
func (e *Engine) Next() {
	if e.state != ShowResult {
		return
	}
	e.advance()
}

// recordAnswer persists the outcome of the current card to the store, before
// handleCorrect/handleWrong schedule its next appearance.
//
// Under the evidence scheduler every attempt is a spaced retrieval and counts.
// In sequential mode, retry-queue reps are massed drill repetitions, not
// cold recall, so they stay out of persisted stats; only the original
// main-queue showing counts there.
func (e *Engine) recordAnswer(correct bool) {
	if e.store == nil || e.fromRetry {
		return
	}
	if correct {
		e.store.RecordCorrect(e.current.ID)
	} else {
		e.store.RecordWrong(e.current.ID)
	}
}

// handleCorrect processes a correct answer.
func (e *Engine) handleCorrect() {
	// Evidence scheduler: one recall down. A card that meets its session
	// criterion is done — its next appearance is a matter of days, scheduled
	// in the store — otherwise it returns later this session, spaced out.
	if e.evidenceScheduled() {
		id := e.current.ID
		e.need[id]--
		if e.need[id] <= 0 {
			if e.store != nil {
				e.store.Schedule(id, e.lapsed[id])
			}
			return // criterion met: leaves the session
		}
		e.requeueGap(e.current, gapAfterCorrect)
		return
	}

	if e.fromRetry {
		// Handle retry queue graduation.
		for i, rc := range e.retry {
			if rc.card.ID == e.current.ID {
				rc.remaining--
				if rc.remaining <= 0 {
					e.retry = append(e.retry[:i], e.retry[i+1:]...)
					// The drilled card rejoins the lap at the tail.
					e.requeueTail(e.current)
				} else {
					e.repeatCurrent = true
				}
				return
			}
		}
		return
	}

	// Sequential mode: to the back of the lap.
	e.requeueTail(e.current)
}

// handleWrong processes a wrong answer.
func (e *Engine) handleWrong() {
	// Evidence scheduler: no massed drill. The card returns after a short —
	// but nonzero — gap (an immediate repeat would be answered from short-term
	// memory and teach nothing durable) and owes at least two more spaced
	// recalls; the lapse also resets its between-session schedule when it
	// completes.
	if e.evidenceScheduled() {
		id := e.current.ID
		e.lapsed[id] = true
		if e.need[id] < needAfterMiss {
			e.need[id] = needAfterMiss
		}
		e.requeueGap(e.current, gapAfterMiss)
		return
	}

	e.repeatCurrent = true

	// Check if already in retry queue — reset to full repeats.
	for _, rc := range e.retry {
		if rc.card.ID == e.current.ID {
			rc.remaining = minRepeats
			return
		}
	}
	// Add to retry queue.
	e.retry = append(e.retry, &retryCard{
		card:      e.current,
		remaining: minRepeats,
	})
}

// advance moves to the next card from main or retry queue.
func (e *Engine) advance() {
	// If the last answer was wrong, repeat the same card immediately.
	if e.repeatCurrent && e.current != nil {
		e.repeatCurrent = false
		e.fromRetry = true
		if e.current.Mode == deck.ModeChoice {
			e.currentOpts, e.correctIdx = e.generateChoices(e.current)
		}
		e.state = ShowQuestion
		return
	}

	// Normal advancement: main queue, then retry queue.
	if len(e.main) > 0 {
		if len(e.retry) > 0 && e.TotalSeen > 0 && e.TotalSeen%3 == 0 {
			e.current = e.retry[0].card
			e.fromRetry = true
		} else {
			// Serve the earliest-due card and advance the logical clock.
			e.current = e.main[0].card
			e.main = e.main[1:]
			e.step++
			e.fromRetry = false
		}
	} else if len(e.retry) > 0 {
		e.current = e.retry[0].card
		e.fromRetry = true
	} else {
		// Every card has met its session criterion — the session is complete.
		// (Only the evidence scheduler gets here; the lap modes always requeue.)
		e.current = nil
		e.state = Done
		return
	}

	// Flip-through never quizzes: every card is presented answer-visible, and
	// ConfirmPreview moves to the next one.
	if e.deck.Order == deck.OrderFlipThrough {
		e.state = ShowPreview
		return
	}

	if e.current.Mode == deck.ModeChoice {
		e.currentOpts, e.correctIdx = e.generateChoices(e.current)
	}

	// A brand-new card is revealed before it's quizzed. Only fresh main-queue
	// serves qualify: a retry/repeat card was necessarily answered already.
	if !e.fromRetry && e.shouldPreview(e.current) {
		e.previewed[e.current.ID] = true
		e.state = ShowPreview
		return
	}
	e.state = ShowQuestion
}

// shouldPreview reports whether the card gets the first-viewing reveal: the
// feature is on and the card has never been answered — not earlier in this
// session (previewed) and not in any prior one (no recorded history).
func (e *Engine) shouldPreview(c *deck.Card) bool {
	if !e.preview || e.previewed[c.ID] {
		return false
	}
	if e.store != nil {
		cp := e.store.Get(c.ID)
		if cp.TimesCorrect+cp.TimesWrong > 0 {
			return false
		}
	}
	return true
}

// ConfirmPreview ends the answer-visible presentation. For a first-viewing
// reveal it quizzes the same card (ShowPreview → ShowQuestion); in flip-through
// mode there is no quiz, so it counts the card as viewed and serves the next
// one (wrapping to the top via the tail requeue). A no-op in any other state.
func (e *Engine) ConfirmPreview() {
	if e.state != ShowPreview {
		return
	}
	if e.deck.Order == deck.OrderFlipThrough {
		e.TotalSeen++
		e.requeueTail(e.current)
		e.advance()
		return
	}
	e.state = ShowQuestion
}

// requeueGap re-inserts a card so it comes due after roughly gap more serves.
// The due tick is absolute (step + gap), so the queue stays ordered by due
// time and no card can be starved; with fewer than gap cards pending, the
// card simply returns when the queue reaches it.
func (e *Engine) requeueGap(card *deck.Card, gap int) {
	due := e.step + gap

	// Insert keeping main sorted by ascending due tick (stable: ties keep the
	// existing card ahead of the newcomer).
	qc := queuedCard{card: card, due: due}
	pos := len(e.main)
	for i := range e.main {
		if e.main[i].due > due {
			pos = i
			break
		}
	}
	e.main = append(e.main, queuedCard{})
	copy(e.main[pos+1:], e.main[pos:])
	e.main[pos] = qc
}

// requeueTail appends a card after everything already queued, preserving the
// lap structure of sequential and flip-through mode.
func (e *Engine) requeueTail(card *deck.Card) {
	due := e.step
	if n := len(e.main); n > 0 && e.main[n-1].due >= due {
		due = e.main[n-1].due
	}
	e.main = append(e.main, queuedCard{card: card, due: due + 1})
}

// generateChoices builds the multiple choice options for a card.
func (e *Engine) generateChoices(card *deck.Card) ([]string, int) {
	numChoices := e.choices
	if card.Choices > 0 {
		numChoices = card.Choices
	}

	choices := make([]string, 0, numChoices)
	choices = append(choices, card.AnswerText)
	used := map[string]bool{card.AnswerText: true}

	// Use custom distractors first.
	for _, d := range card.Distractors {
		if len(choices) >= numChoices {
			break
		}
		if !used[d] {
			choices = append(choices, d)
			used[d] = true
		}
	}

	// Fill remaining from other cards' answers.
	if len(choices) < numChoices {
		// Build pool of other answers, shuffled.
		pool := make([]string, 0)
		for _, c := range e.allCards {
			if !used[c.AnswerText] {
				pool = append(pool, c.AnswerText)
			}
		}
		rand.Shuffle(len(pool), func(i, j int) {
			pool[i], pool[j] = pool[j], pool[i]
		})
		for _, p := range pool {
			if len(choices) >= numChoices {
				break
			}
			choices = append(choices, p)
			used[p] = true
		}
	}

	// Shuffle choices and track where the correct answer ended up.
	rand.Shuffle(len(choices), func(i, j int) {
		choices[i], choices[j] = choices[j], choices[i]
	})

	correctIdx := 0
	for i, c := range choices {
		if c == card.AnswerText {
			correctIdx = i
			break
		}
	}

	return choices, correctIdx
}
