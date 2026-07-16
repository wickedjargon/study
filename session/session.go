// Package session assembles a runnable quiz session from a deck path: parse,
// optional reverse, saved progress, compose, engine. It is the shared spine of
// launching a session — the CLI entry point layers its flag overrides between
// Load and Start, the GUI library screen calls them back to back.
package session

import (
	"errors"
	"fmt"
	"io"
	"time"

	"study/deck"
	"study/progress"
	"study/quiz"
)

// Sentinel errors callers branch on: both mean "this launch has nothing to
// quiz", not that anything is broken, so the CLI and the library screen turn
// them into friendly messages rather than failures. They are wrapped with the
// deck's name (fmt.Errorf "%s %w"), so errors.Is is the way to test for them.
var (
	ErrNoReversibleCards = errors.New("has no reversible cards")
	ErrNothingWeak       = errors.New("has no weak cards to cram")
)

// Ahead pulls not-yet-due cards into an adaptive session (--ahead): all of
// them, or those due within Days. The zero value is off.
type Ahead struct {
	Days int
	All  bool
}

func (a Ahead) active() bool { return a.All || a.Days > 0 }

// Load parses the deck at path and pairs it with its saved progress store.
// Non-fatal parse warnings are written to warn (nil discards them). With
// reverse set the deck is flipped before progress is loaded, so everything
// downstream — stats, composition, the quiz itself — operates on the reversed
// cards, whose "r:"-prefixed IDs track separately from forward.
func Load(path string, reverse bool, warn io.Writer) (*deck.Deck, *progress.Store, error) {
	d, err := deck.Parse(path)
	if err != nil {
		return nil, nil, err
	}

	if warn != nil {
		for _, w := range d.Warnings {
			fmt.Fprintf(warn, "study: %s\n", w)
		}
	}

	if reverse {
		d = d.Reversed()
		// Cards that can't be reversed (cloze, media-only prompts, answers with
		// no typeable Latin text) are dropped; a deck of nothing else can't run.
		if len(d.Cards) == 0 {
			return nil, nil, fmt.Errorf("%s %w", d.Name, ErrNoReversibleCards)
		}
	}

	store, err := progress.NewStore(d.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("progress: %w", err)
	}

	// One-time migration: progress saved under a card's legacy ID (the old
	// hash included @audio/@img lines, so renaming a media file orphaned the
	// card's history) is moved to its current ID.
	if store.MigrateIDs(d.Cards) {
		if err := store.Save(); err != nil {
			return nil, nil, fmt.Errorf("saving migrated progress: %w", err)
		}
	}

	return d, store, nil
}

// Start composes the session in place under the deck's order mode and returns
// the engine that will run it. Callers apply their per-session overrides to d
// (order, answer mode, timing) before calling. A weak-only order with nothing
// weak returns ErrNothingWeak; an empty adaptive session is not an error — the
// engine opens in the CaughtUp state and the frontend offers to study ahead.
func Start(d *deck.Deck, store *progress.Store, ahead Ahead, now time.Time) (*quiz.Engine, error) {
	// The session may be a filtered subset of the deck (due cards, weak
	// cards), but a confused answer can belong to any card in the file — the
	// engine keeps the full list for confusion detection.
	full := d.Cards

	quiz.Compose(d, store, now)
	switch d.Order {
	case deck.OrderAdaptive:
		// Cards scheduled further out are excluded by Compose — distributing
		// practice across days is the point, and re-drilling tomorrow's cards
		// today would just collapse the spacing. Ahead pulls them in anyway,
		// soonest-due first (closest to forgetting, so highest-yield); the
		// engine then keeps their schedules honest — a clean early review
		// doesn't advance the ladder, only a miss moves it.
		if ahead.active() {
			_, _, future, _ := quiz.SplitDue(full, store, now)
			cutoff := now.Add(time.Duration(ahead.Days) * 24 * time.Hour)
			for _, c := range future {
				if ahead.All || !store.Get(c.ID).Due.After(cutoff) {
					d.Cards = append(d.Cards, c)
				}
			}
		}
	case deck.OrderWeakOnly:
		if len(d.Cards) == 0 {
			return nil, fmt.Errorf("%s %w", d.Name, ErrNothingWeak)
		}
	}

	return quiz.NewEngine(d, full, store), nil
}
