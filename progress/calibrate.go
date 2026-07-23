package progress

import (
	"encoding/json"
	"os"
	"strings"
)

// ReadLog returns every event in the deck's review log, oldest first. A
// missing log is an empty history, not an error. Malformed lines (a torn
// write from a kill mid-append) are skipped: the log is an instrument, and
// one bad line shouldn't hide the rest.
func (s *Store) ReadLog() ([]ReviewEvent, error) {
	data, err := os.ReadFile(s.logPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var events []ReviewEvent
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev ReviewEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events, nil
}

// CalBucket accumulates graded answers and reports their recall rate.
type CalBucket struct {
	Asks    int
	Correct int
}

// Recall returns the bucket's percentage of correct answers (0-100).
func (b *CalBucket) Recall() float64 {
	if b.Asks == 0 {
		return 0
	}
	return float64(b.Correct) / float64(b.Asks) * 100
}

// Calibration is the review log aggregated into recall rates: the ladder's
// measured forgetting curve, the instrument every scheduler change is judged
// against. Rungs and Modes count only scheduled review asks — a due card's
// first ask of a session (logged with an empty State and a rung), the one
// moment that tests whether the between-session interval outran memory.
// States counts every graded answer, bucketed by the badge the card wore.
type Calibration struct {
	Events  int                   // graded answers considered
	Reviews int                   // scheduled review asks among them
	Rungs   map[int]*CalBucket    // review asks by ladder rung at ask time
	States  map[string]*CalBucket // all answers: new, learning, retry, review, ahead
	Modes   map[string]*CalBucket // review asks by answer mode: type, choice
}

// Calibrate aggregates log events into recall-rate buckets. Only events for
// cards in ids count — the deck in the direction being reported, so the other
// direction, pack siblings, and removed cards' history stay out, the same
// scoping SummaryFor uses. A nil ids accepts everything.
func Calibrate(events []ReviewEvent, ids map[string]bool) Calibration {
	cal := Calibration{
		Rungs:  make(map[int]*CalBucket),
		States: make(map[string]*CalBucket),
		Modes:  make(map[string]*CalBucket),
	}
	tally := func(m map[string]*CalBucket, key string, correct bool) {
		b := m[key]
		if b == nil {
			b = &CalBucket{}
			m[key] = b
		}
		b.Asks++
		if correct {
			b.Correct++
		}
	}
	for _, ev := range events {
		if ids != nil && !ids[ev.Card] {
			continue
		}
		cal.Events++
		state := ev.State
		if state == "" {
			state = "review"
		}
		tally(cal.States, state, ev.Correct)
		// A scheduled review ask: no session badge (not new, not mid-session,
		// not a retry, not ahead of schedule) and a rung to attribute it to.
		if ev.State == "" && ev.Level >= 1 {
			cal.Reviews++
			b := cal.Rungs[ev.Level]
			if b == nil {
				b = &CalBucket{}
				cal.Rungs[ev.Level] = b
			}
			b.Asks++
			if ev.Correct {
				b.Correct++
			}
			if ev.Mode != "" {
				tally(cal.Modes, ev.Mode, ev.Correct)
			}
		}
	}
	return cal
}

// LadderDays returns the review interval in days at the given ladder rung,
// or 0 for a rung outside the ladder. Rungs are 1-based (Level semantics).
func LadderDays(rung int) int {
	if rung < 1 || rung > len(reviewLadder) {
		return 0
	}
	return reviewLadder[rung-1]
}

// IntervalDays returns the card's scheduled between-session interval in days
// (its rung's span, before fuzz), or 0 for a card the ladder hasn't
// scheduled.
func (cp *CardProgress) IntervalDays() int { return LadderDays(cp.Level) }
