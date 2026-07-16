package library

import (
	"fmt"
	"sort"
	"time"

	"study/deck"
	"study/progress"
	"study/quiz"
)

// StatsInfo is a deck's full progress report, one direction at a time — the
// numbers --stats prints and the library's stats screen shows. Only cards
// that have actually been answered count as studied; aggregates are computed
// over the deck's current cards (orphaned progress from removed cards is
// ignored).
type StatsInfo struct {
	Name       string
	Cards      int
	Studied    int
	Mastered   int
	DueReviews int
	DueNew     int
	NextDue    time.Time // earliest scheduled review; zero when none
	Correct    int       // all-time, this direction
	Wrong      int
	Reversible bool       // the deck has a reversed direction to report on
	Weakest    []WeakCard // studied cards below the weak threshold, weakest first, capped
}

// WeakCard is one line of the weakest-cards listing.
type WeakCard struct {
	Label      string
	Accuracy   float64 // percent
	Confidence float64 // 0-100 confidence score, the sort key
}

// weakestCap bounds the weakest-cards listing: the things worth reviewing are
// at the top, the rest is noise.
const weakestCap = 10

// Accuracy returns the all-time percentage of correct answers (0-100).
func (s *StatsInfo) Accuracy() float64 {
	if s.Correct+s.Wrong == 0 {
		return 0
	}
	return float64(s.Correct) / float64(s.Correct+s.Wrong) * 100
}

// StatsOf reports on an already-loaded deck and store — the CLI's --stats
// path, which has both in hand.
func StatsOf(d *deck.Deck, store *progress.Store, now time.Time) StatsInfo {
	info := StatsInfo{Name: d.Name, Cards: len(d.Cards)}
	for i := range d.Cards {
		c := &d.Cards[i]
		cp := store.Get(c.ID)
		if cp.TimesCorrect+cp.TimesWrong == 0 {
			continue
		}
		info.Studied++
		info.Correct += cp.TimesCorrect
		info.Wrong += cp.TimesWrong
		if cp.IsMastered() {
			info.Mastered++
		}
		// Only genuinely weak cards make the list — the same absolute cutoff
		// weak-only cram uses. Without it, a deck in good shape would parade
		// its strongest-but-bottom cards as "weakest".
		if cp.Confidence() < progress.WeakThreshold {
			info.Weakest = append(info.Weakest, WeakCard{
				Label:      deck.CardLabel(c),
				Accuracy:   cp.Accuracy(),
				Confidence: cp.Confidence(),
			})
		}
	}

	reviews, fresh, _, nextDue := quiz.SplitDue(d.Cards, store, now)
	info.DueReviews, info.DueNew, info.NextDue = len(reviews), len(fresh), nextDue

	sort.SliceStable(info.Weakest, func(i, j int) bool {
		return info.Weakest[i].Confidence < info.Weakest[j].Confidence
	})
	if len(info.Weakest) > weakestCap {
		info.Weakest = info.Weakest[:weakestCap]
	}
	return info
}

// Stats loads the deck at path with its saved progress and reports on the
// asked-for direction — the library screen's path, which has only the entry.
func Stats(path string, reverse bool, now time.Time) (StatsInfo, error) {
	d, err := deck.Parse(path)
	if err != nil {
		return StatsInfo{}, err
	}
	rd := d.Reversed()
	if reverse {
		if len(rd.Cards) == 0 {
			return StatsInfo{}, fmt.Errorf("%s has no reversible cards", d.Name)
		}
		d = rd
	}
	store, err := progress.NewStore(d.Path)
	if err != nil {
		return StatsInfo{}, err
	}
	info := StatsOf(d, store, now)
	info.Reversible = len(rd.Cards) > 0
	return info, nil
}
