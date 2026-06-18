// Package quiz implements the core quiz logic with Byki-style repeat-on-wrong.
package quiz

import (
	"math/rand"
	"strings"
	"study/deck"
	"study/progress"
)

// State represents the current phase of the quiz for a single card.
type State int

const (
	ShowQuestion State = iota
	ShowResult
	Done
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

// minRepeats is the minimum number of times a wrong card is repeated.
const minRepeats = 3

// NewEngine creates a quiz engine from a parsed deck.
// If shuffle is true, the card order is randomized.
func NewEngine(d *deck.Deck, shuffle bool, choicesOverride int, store *progress.Store) *Engine {
	choices := d.Choices
	if choicesOverride > 0 {
		choices = choicesOverride
	}
	if choices < 2 {
		choices = 2
	}

	// Build card pointer slice.
	cards := make([]*deck.Card, len(d.Cards))
	for i := range d.Cards {
		cards[i] = &d.Cards[i]
	}

	if shuffle {
		rand.Shuffle(len(cards), func(i, j int) {
			cards[i], cards[j] = cards[j], cards[i]
		})
	}

	// Seed the main queue with due ticks matching the incoming order, so the
	// first pass presents cards in their (already shuffled/prioritized) order.
	main := make([]queuedCard, len(cards))
	for i, c := range cards {
		main[i] = queuedCard{card: c, due: i}
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
	}

	e.advance()
	return e
}

// State returns the current quiz state.
func (e *Engine) State() State {
	return e.state
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

	correct := false
	if e.caseSensitive {
		correct = got == expected
	} else {
		correct = strings.EqualFold(got, expected)
	}

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

// recordAnswer persists the outcome of the current card to the store. It must
// run BEFORE handleCorrect/handleWrong, because requeueCard reads the freshly
// updated streak to schedule the card's next appearance — recording afterwards
// would schedule off a stale, one-answer-old streak.
//
// Retry-queue reps are drill repetitions, not cold recall, so they stay out of
// persisted stats; only the original main-queue showing counts.
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
// Re-queues the card at a delay proportional to confidence.
func (e *Engine) handleCorrect() {
	if e.fromRetry {
		// Handle retry queue graduation.
		for i, rc := range e.retry {
			if rc.card.ID == e.current.ID {
				rc.remaining--
				if rc.remaining <= 0 {
					e.retry = append(e.retry[:i], e.retry[i+1:]...)
					// A card the user originally missed must not vanish for
					// the rest of the session while cards they answered right
					// keep recurring. Re-queue it: its low stored confidence
					// yields a short delay, so it returns sooner than
					// well-known cards — i.e. wrong cards are seen more often.
					e.requeueCard(e.current)
				} else {
					e.repeatCurrent = true
				}
				return
			}
		}
		return
	}

	// Re-queue based on confidence.
	e.requeueCard(e.current)
}

// handleWrong processes a wrong answer by adding/resetting in retry queue.
func (e *Engine) handleWrong() {
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
		// No cards left — should not happen in continuous mode.
		e.current = nil
		e.state = Done
		return
	}

	if e.current.Mode == deck.ModeChoice {
		e.currentOpts, e.correctIdx = e.generateChoices(e.current)
	}
	e.state = ShowQuestion
}

// maxStreak caps how far a correct streak can push a card's next appearance
// out. Beyond this the card is effectively "known well enough"; spacing it any
// further would just hide it.
const maxStreak = 10

// requeueCard re-inserts a card into the main queue, scheduling it to come due
// after a delay driven by the card's current correct streak — NOT its lifetime
// confidence. Streak is a recency signal: a wrong answer resets it to 0, so a
// just-missed card returns soonest (appears most often) regardless of how good
// its history is, and only spaces back out as the user rebuilds consecutive
// correct answers. The due tick is absolute (step + delay), so the queue stays
// ordered by due time and no card can be starved — every card's due tick is
// eventually reached as the clock advances.
func (e *Engine) requeueCard(card *deck.Card) {
	streak := 0
	if e.store != nil {
		streak = e.store.Get(card.ID).Streak
	}
	if streak > maxStreak {
		streak = maxStreak
	}

	deckSize := len(e.allCards)
	if deckSize < 2 {
		deckSize = 2
	}

	// Streak 0 (just missed, or never answered correctly): due again after ~2
	// steps. Full streak: due after ~deckSize*3 steps. Each correct answer in
	// between nudges the next appearance a little further out — a gradual
	// recovery, exactly inverse to a miss snapping it back to the front.
	delay := 2 + int(float64(streak)/float64(maxStreak)*float64(deckSize*3))
	due := e.step + delay

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
