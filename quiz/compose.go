package quiz

import (
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
	}
}

// SplitDue partitions deck cards for an adaptive session: reviews are studied
// cards due now, sorted most overdue first (so an interrupted session spends
// its time where forgetting is most advanced); fresh are never-studied cards,
// shuffled; future are studied cards scheduled past now, sorted soonest-due
// first — normally excluded from the session, --ahead pulls them in. nextDue
// reports the earliest future review time (zero when none are scheduled).
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
		return store.Get(reviews[i].ID).Due.Before(store.Get(reviews[j].ID).Due)
	})
	sort.SliceStable(future, func(i, j int) bool {
		return store.Get(future[i].ID).Due.Before(store.Get(future[j].ID).Due)
	})
	shuffleCards(fresh)
	return reviews, fresh, future, nextDue
}

// shuffleCards randomizes card order in place.
func shuffleCards(cards []deck.Card) {
	rand.Shuffle(len(cards), func(i, j int) {
		cards[i], cards[j] = cards[j], cards[i]
	})
}
