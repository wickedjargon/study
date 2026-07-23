package quiz

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"study/deck"
	"study/progress"
)

// Compose filters and orders d.Cards in place into a session under the deck's
// order mode. It is the shared front half of session setup — every frontend
// (CLI flags, web handlers) calls it before NewEngine, which presents the
// cards in whatever order Compose left them.
//
// Adaptive: reviews due now (most overdue first), then a bounded batch of
// never-studied cards, shuffled. Weak-only: weak cards, shuffled then
// prioritized weakest-first. Sequential and flip-through: authored order,
// untouched.
func Compose(d *deck.Deck, store *progress.Store, now time.Time) {
	switch d.Order {
	case deck.OrderAdaptive:
		reviews, fresh, _, _ := SplitDue(d.Cards, store, now)
		if d.NewPerSession >= 0 && len(fresh) > d.NewPerSession {
			fresh = fresh[:d.NewPerSession]
		}
		d.Cards = append(reviews, fresh...)
	case deck.OrderWeakOnly:
		d.Cards = store.FilterWeak(d.Cards)
		shuffleCards(d.Cards)
		d.Cards = store.PrioritizeCards(d.Cards)
	case deck.OrderSequential:
		// Sequential resumes where the last session left off: the lap is
		// rotated to start just after the most recently answered card.
		// Derived from LastSeen rather than a stored position, so it needs
		// no schema and survives deck edits (a deleted card just stops
		// being the anchor). Authored order is preserved within the lap.
		rotateAfterLastSeen(d.Cards, store)
	}
}

// rotateAfterLastSeen rotates cards in place so the card following the most
// recently answered one comes first. A never-studied deck, or one whose last
// answer was its final card, starts at the top.
func rotateAfterLastSeen(cards []deck.Card, store *progress.Store) {
	if store == nil {
		return
	}
	last := -1
	var lastTime time.Time
	for i := range cards {
		cp := store.Get(cards[i].ID)
		if !cp.LastSeen.IsZero() && !cp.LastSeen.Before(lastTime) {
			lastTime = cp.LastSeen
			last = i
		}
	}
	if last < 0 || last == len(cards)-1 {
		return
	}
	rotated := append(append([]deck.Card{}, cards[last+1:]...), cards[:last+1]...)
	copy(cards, rotated)
}

// SplitDue partitions deck cards for an adaptive session: reviews are studied
// cards due now, sorted most relatively overdue first (so an interrupted
// session spends its time where forgetting is most advanced); fresh are
// never-studied cards, shuffled; future are studied cards scheduled past now,
// sorted soonest-due first — normally excluded from the session, --ahead
// pulls them in. nextDue reports the earliest future review time (zero when
// none are scheduled).
func SplitDue(cards []deck.Card, store *progress.Store, now time.Time) (reviews, fresh, future []deck.Card, nextDue time.Time) {
	for _, c := range cards {
		cp := store.Get(c.ID)
		switch {
		case cp.TimesCorrect+cp.TimesWrong == 0:
			fresh = append(fresh, c)
		case cp.DueNow(now):
			reviews = append(reviews, c)
		default:
			future = append(future, c)
			if nextDue.IsZero() || cp.Due.Before(nextDue) {
				nextDue = cp.Due
			}
		}
	}
	sort.SliceStable(reviews, func(i, j int) bool {
		return relativeOverdue(store.Get(reviews[i].ID), now) > relativeOverdue(store.Get(reviews[j].ID), now)
	})
	sort.SliceStable(future, func(i, j int) bool {
		return store.Get(future[i].ID).Due.Before(store.Get(future[j].ID).Due)
	})
	shuffleCards(fresh)
	return reviews, fresh, future, nextDue
}

// relativeOverdue returns how overdue a card is in units of its own
// scheduled interval: overdue days divided by interval days. Absolute
// overdueness triages a backlog backwards — a card 5 days late on a 3-day
// interval is in far more danger than one 10 days late on a 120-day
// interval — so reviews are served by this ratio instead, the model-free
// version of descending retrievability. Progress from before the scheduler
// (a Due with no rung) counts its interval as one day; a card with no Due
// at all sorts first, having waited the longest.
func relativeOverdue(cp *progress.CardProgress, now time.Time) float64 {
	if cp.Due.IsZero() {
		return math.Inf(1)
	}
	iv := cp.IntervalDays()
	if iv < 1 {
		iv = 1
	}
	return now.Sub(cp.Due).Hours() / 24 / float64(iv)
}

// shuffleCards randomizes card order in place.
func shuffleCards(cards []deck.Card) {
	rand.Shuffle(len(cards), func(i, j int) {
		cards[i], cards[j] = cards[j], cards[i]
	})
}
