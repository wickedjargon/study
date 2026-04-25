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
	Card    *deck.Card
	Chosen  int    // index of chosen answer (0-based, -1 for type mode)
	Correct bool
	Answer  string // the correct answer text
	Typed   string // what the user typed (type mode only)
}

// Engine drives the quiz session.
type Engine struct {
	deck          *deck.Deck
	choices       int
	mode          deck.QuizMode
	caseSensitive bool
	store         *progress.Store

	// Card queues.
	main  []*deck.Card // primary queue (shuffled or sequential)
	retry []*retryCard // cards answered wrong, need consecutive correct

	// All cards for generating distractors.
	allCards []*deck.Card

	// Current state.
	current        *deck.Card
	currentOpts    []string // current answer choices (choice mode only)
	correctIdx     int      // index of correct answer in currentOpts
	fromRetry      bool     // is current card from retry queue?
	repeatCurrent  bool     // repeat this card immediately (wrong answer)
	state          State

	// Session stats.
	TotalSeen    int
	TotalCorrect int
	TotalWrong   int
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
		main:          cards,
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

// TotalCards returns the total number of cards in the deck.
func (e *Engine) TotalCards() int {
	return len(e.allCards)
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

// Next advances to the next card after viewing the result.
// Transitions from ShowResult to ShowQuestion (or Done).
func (e *Engine) Next() {
	if e.state != ShowResult {
		return
	}
	e.advance()
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
			e.current = e.main[0]
			e.main = e.main[1:]
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

// requeueCard re-inserts a card into the main queue at a position
// based on confidence. High confidence = further back = seen less often.
func (e *Engine) requeueCard(card *deck.Card) {
	conf := 0.0
	if e.store != nil {
		conf = e.store.Get(card.ID).Confidence()
	}

	// Delay: minimum 1, scales with confidence and deck size.
	deckSize := len(e.allCards)
	if deckSize < 2 {
		deckSize = 2
	}

	// Low confidence (0): re-insert after ~2 cards.
	// High confidence (100): re-insert after deckSize*3 cards.
	delay := 2 + int(conf/100.0*float64(deckSize*3))

	// Clamp to queue length.
	if delay > len(e.main) {
		e.main = append(e.main, card)
	} else {
		// Insert at position.
		e.main = append(e.main, nil)
		copy(e.main[delay+1:], e.main[delay:])
		e.main[delay] = card
	}
}

// generateChoices builds the multiple choice options for a card.
func (e *Engine) generateChoices(card *deck.Card) ([]string, int) {
	choices := make([]string, 0, e.choices)
	choices = append(choices, card.AnswerText)
	used := map[string]bool{card.AnswerText: true}

	// Use custom distractors first.
	for _, d := range card.Distractors {
		if len(choices) >= e.choices {
			break
		}
		if !used[d] {
			choices = append(choices, d)
			used[d] = true
		}
	}

	// Fill remaining from other cards' answers.
	if len(choices) < e.choices {
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
			if len(choices) >= e.choices {
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
