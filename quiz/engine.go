// Package quiz implements the quiz session logic.
//
// The default scheduler is evidence-based (see the README's "Card order"
// table): each card must be correctly recalled a criterion number
// of times per session, repetitions are spaced by intervening cards rather
// than massed back-to-back, and the session completes when every card meets
// its criterion. Sequential mode instead keeps Byki-style repeat-on-wrong
// drilling and cycles its laps forever.
package quiz

import (
	"math/rand"
	"sort"
	"strings"
	"time"
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
	// CaughtUp is where an adaptive session starts when it has nothing to
	// serve — no reviews due, no new cards to introduce. The schedule's job is
	// done for the day, but that's announced, never enforced: ContinueAll
	// starts a full ahead-of-schedule pass, so the user is never prevented
	// from studying.
	CaughtUp
)

// Result records the outcome of answering a single card.
type Result struct {
	Card     *deck.Card
	Chosen   int // index of chosen answer (0-based, -1 for type mode/timeout)
	Correct  bool
	Answer   string // the correct answer text
	Typed    string // what the user typed (type mode only)
	TimedOut bool   // true if the card's time limit expired before answering
	// ConfusedWith is set on a wrong answer that is itself the answer to
	// another card in the deck — the user confused the two cards, not merely
	// forgot this one. The GUI names that card so the pair can be told apart.
	ConfusedWith *deck.Card
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

	// All session cards for generating distractors.
	allCards []*deck.Card

	// pool is the complete deck, for confusion detection: the session may be a
	// filtered subset (due cards, weak cards), but a wrong answer can belong to
	// any card in the file.
	pool []*deck.Card

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
	// between-session schedule therefore drops down the ladder when they
	// complete. Unused by sequential mode's laps.
	need   map[string]int
	lapsed map[string]bool

	// newCards marks cards that entered this session never studied, decided
	// once at session start (a card answered mid-session stays "new" for the
	// session even though the store now has history for it).
	newCards map[string]bool

	// ahead marks studied cards that entered this session before their review
	// date (--ahead, or a weak-only cram of a not-yet-due card). Early
	// retrieval practice is fine — it's merely lower-yield — but its easy
	// successes are weak evidence: a clean ahead completion must not advance
	// the review ladder, or intervals inflate on recalls that never proved
	// anything. A miss is the opposite — forgetting before the due date is
	// strong evidence the interval was too long — so lapses count normally.
	ahead map[string]bool

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

// gapConfused is where a confused-with card is pulled forward to: just after
// the missed card's own return (gapAfterMiss), so the confusable pair is
// retrieved near — but not next to — each other while the confusion is fresh.
// Juxtaposing confusable items is what teaches the distinction (interleaving:
// Kornell & Bjork 2008).
const gapConfused = 5

// NewEngine creates a quiz engine from a parsed deck. Cards are presented in
// the deck's existing order (the caller shuffles/prioritizes beforehand).
// pool is the deck's complete card list for confusion detection, of which
// d.Cards may be a filtered subset; nil means the session is the whole deck.
func NewEngine(d *deck.Deck, pool []deck.Card, store *progress.Store) *Engine {
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
		pool:          allCards,
		state:         ShowQuestion,
		preview:       d.Preview,
		previewed:     make(map[string]bool),
		newCards:      make(map[string]bool),
	}
	for i := range d.Cards {
		id := d.Cards[i].ID
		if store == nil {
			e.newCards[id] = true
			continue
		}
		if cp := store.Get(id); cp.TimesCorrect+cp.TimesWrong == 0 {
			e.newCards[id] = true
		}
	}
	if pool != nil {
		e.pool = make([]*deck.Card, len(pool))
		for i := range pool {
			e.pool[i] = &pool[i]
		}
	}

	// Seed each card's session criterion for the evidence scheduler: new
	// material owes three spaced recalls in its first session, previously
	// learned material one relearning recall.
	if e.evidenceScheduled() {
		e.need = make(map[string]int, len(d.Cards))
		e.lapsed = make(map[string]bool)
		e.ahead = make(map[string]bool)
		now := time.Now()
		for i := range d.Cards {
			n := needNew
			if store != nil {
				cp := store.Get(d.Cards[i].ID)
				if cp.TimesCorrect+cp.TimesWrong > 0 {
					n = needReview
					if !cp.DueNow(now) {
						e.ahead[d.Cards[i].ID] = true
					}
				}
			}
			e.need[d.Cards[i].ID] = n
		}
	}

	e.advance()
	// An adaptive session that opens with nothing to serve isn't over — it
	// never began: the user is caught up. Land on the caught-up screen (which
	// offers a full pass via ContinueAll) rather than an empty summary.
	if e.state == Done && d.Order == deck.OrderAdaptive {
		e.state = CaughtUp
	}
	return e
}

// ContinueAll re-seeds the adaptive session so running out of due cards never
// blocks studying: reviews due now first (most overdue first), then one batch
// of never-studied cards (shuffled, capped at the deck's new-per-session
// setting — the same pacing as the launch composition), then the rest ahead
// of schedule, soonest-due first (closest to forgetting, so highest-yield).
// Ahead cards keep their honest semantics: a clean early completion leaves
// the review ladder alone, only a miss moves it. Each call is one more pass —
// the next call brings the next batch of new cards — so studying can continue
// indefinitely without ever flooding new material.
func (e *Engine) ContinueAll() {
	if e.deck.Order != deck.OrderAdaptive {
		return
	}

	now := time.Now()
	var reviews, fresh, future []*deck.Card
	for _, c := range e.pool {
		var cp *progress.CardProgress
		if e.store != nil {
			cp = e.store.Get(c.ID)
		}
		switch {
		case cp == nil || cp.TimesCorrect+cp.TimesWrong == 0:
			fresh = append(fresh, c)
		case cp.DueNow(now):
			reviews = append(reviews, c)
		default:
			future = append(future, c)
		}
	}
	sort.SliceStable(reviews, func(i, j int) bool {
		return e.store.Get(reviews[i].ID).Due.Before(e.store.Get(reviews[j].ID).Due)
	})
	sort.SliceStable(future, func(i, j int) bool {
		return e.store.Get(future[i].ID).Due.Before(e.store.Get(future[j].ID).Due)
	})
	rand.Shuffle(len(fresh), func(i, j int) {
		fresh[i], fresh[j] = fresh[j], fresh[i]
	})
	if e.deck.NewPerSession >= 0 && len(fresh) > e.deck.NewPerSession {
		fresh = fresh[:e.deck.NewPerSession]
	}
	ordered := append(append(reviews, fresh...), future...)

	// A fresh pass: rebuild the queue (End() can leave cards behind) and
	// re-seed each card's criterion and flags against the current schedule.
	// The session is now the whole deck, so the distractor pool and stats
	// scope (allCards) follow.
	e.main = e.main[:0]
	e.retry = nil
	e.repeatCurrent = false
	e.fromRetry = false
	e.current = nil
	e.lapsed = make(map[string]bool)
	e.ahead = make(map[string]bool)
	for i, c := range ordered {
		e.main = append(e.main, queuedCard{card: c, due: e.step + i})
	}
	for _, c := range fresh {
		e.need[c.ID] = needNew
		e.newCards[c.ID] = true
	}
	for _, c := range reviews {
		e.need[c.ID] = needReview
	}
	for _, c := range future {
		e.need[c.ID] = needReview
		e.ahead[c.ID] = true
	}
	e.allCards = ordered
	e.advance()
}

// Name returns the deck's display name.
func (e *Engine) Name() string {
	return e.deck.Name
}

// NextDue reports the review schedule across the full deck: due is the
// earliest upcoming review time (zero when none is scheduled) and caughtUp is
// true when no studied card is currently due — the state the caught-up screen
// and the adaptive summary announce.
func (e *Engine) NextDue() (due time.Time, caughtUp bool) {
	if e.store == nil {
		return time.Time{}, true
	}
	now := time.Now()
	caughtUp = true
	for _, c := range e.pool {
		cp := e.store.Get(c.ID)
		if cp.TimesCorrect+cp.TimesWrong == 0 {
			continue
		}
		if cp.DueNow(now) {
			caughtUp = false
			continue
		}
		if due.IsZero() || cp.Due.Before(due) {
			due = cp.Due
		}
	}
	return due, caughtUp
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

// WrongPause returns how many seconds the result screen of a wrong answer
// should refuse to advance (0 = no pause).
func (e *Engine) WrongPause() int {
	return e.deck.WrongPause
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

// CurrentIsNew reports whether the current card entered this session never
// studied. Decided at session start: a new card keeps the label for the whole
// session, even after its first answers are recorded.
func (e *Engine) CurrentIsNew() bool {
	return e.current != nil && e.newCards[e.current.ID]
}

// CurrentIsAhead reports whether the current card is being reviewed ahead of
// its schedule — its clean completion won't advance the review ladder.
func (e *Engine) CurrentIsAhead() bool {
	return e.current != nil && e.ahead[e.current.ID]
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
		// Picking a distractor that is another card's answer is the same
		// confusion signal as typing it — distractors are drawn from the deck.
		if choice >= 0 && choice < len(e.currentOpts) {
			result.ConfusedWith = e.noteConfusion(e.currentOpts[choice])
		}
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
		result.ConfusedWith = e.noteConfusion(got)
		e.handleWrong()
	}

	e.state = ShowResult
	return result
}

// matchesAnswer reports whether a typed answer counts as correct for the
// current card.
func (e *Engine) matchesAnswer(got string) bool {
	return e.accepts(e.current, got)
}

// accepts reports whether an answer counts as correct for the given card. It
// is checked against the primary answer and every accepted alternative ("= "
// lines). A case-sensitive deck requires an exact match (after trimming);
// otherwise answers are compared leniently via normalizeAnswer, so case,
// surrounding/embedded punctuation, and accents don't cause a right answer to
// be marked wrong.
func (e *Engine) accepts(c *deck.Card, got string) bool {
	candidates := make([]string, 0, 1+len(c.Accept))
	candidates = append(candidates, c.AnswerText)
	candidates = append(candidates, c.Accept...)

	got = strings.TrimSpace(got)
	for _, cand := range candidates {
		if e.caseSensitive {
			if got == strings.TrimSpace(cand) {
				return true
			}
		} else if normalizeAnswer(got) == normalizeAnswer(cand) {
			return true
		}
	}
	return false
}

// noteConfusion handles a wrong answer that is itself the answer to another
// card: a discrimination failure between two cards, not ordinary forgetting
// of one. The confused-with card is returned for the result screen's contrast
// line, and — when it still owes recalls this session — pulled forward so the
// pair is retrieved near each other while the confusion is fresh. Its
// between-session schedule is untouched: the user producing its answer to the
// wrong cue is no evidence against that card itself, and a card past its
// criterion is not dragged back in (that would collapse its spacing).
func (e *Engine) noteConfusion(got string) *deck.Card {
	if strings.TrimSpace(got) == "" {
		return nil
	}
	var confused *deck.Card
	for _, c := range e.pool {
		if c.ID == e.current.ID || !e.accepts(c, got) {
			continue
		}
		if confused == nil {
			confused = c
		}
		// Among several cards sharing this answer, prefer one still active in
		// the session — that one can actually be juxtaposed.
		if e.evidenceScheduled() && e.need[c.ID] > 0 {
			confused = c
			break
		}
	}
	if confused == nil {
		return nil
	}
	if e.evidenceScheduled() && e.need[confused.ID] > 0 {
		e.pullForward(confused, gapConfused)
	}
	return confused
}

// pullForward moves a queued card's due tick up to step+gap so it is served
// soon. A card already due sooner keeps its earlier tick, and one not in the
// main queue is left alone rather than re-added.
func (e *Engine) pullForward(card *deck.Card, gap int) {
	due := e.step + gap
	for i := range e.main {
		if e.main[i].card.ID == card.ID {
			if e.main[i].due <= due {
				return
			}
			e.main = append(e.main[:i], e.main[i+1:]...)
			e.requeueGap(card, gap)
			return
		}
	}
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
			// A clean ahead-of-schedule completion leaves the ladder alone:
			// the card's next review stays where the evidence put it. A lapsed
			// one reschedules normally — the early miss is real evidence.
			if e.store != nil && (!e.ahead[id] || e.lapsed[id]) {
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
	// recalls; the lapse also drops its between-session schedule down the
	// ladder when it completes.
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
