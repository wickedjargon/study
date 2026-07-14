package web

import (
	"net/http"
	"path/filepath"
	"time"

	"study/deck"
	"study/progress"
	"study/quiz"
)

// mediaView is one element of a card side, ready for the template: exactly
// one of Text, ImageURL, or AudioURL is set.
type mediaView struct {
	Text     string
	ImageURL string
	AudioURL string
}

// homeDeck is one row of the picker: the deck plus this guest's schedule
// against it, so a returning friend sees their reviews waiting.
type homeDeck struct {
	Slug    string
	Name    string
	Cards   int
	Due     int
	Fresh   int
	Studied bool
}

type homeView struct {
	Decks []homeDeck
}

// quizView carries every screen of the session; Screen selects the template
// block ("question", "preview", "result", "caughtup", "summary").
type quizView struct {
	Screen   string
	Slug     string
	DeckName string

	// Header counters.
	Remaining, Seen, Correct, Wrong int

	// Question / preview.
	Question   []mediaView
	AnswerSide []mediaView
	Choice     bool
	Options    []string
	TimeLimit  int
	AudioSpeed float64
	IsNew      bool
	IsRetry    bool
	IsAhead    bool
	FlipMode   bool

	// Result.
	ResultCorrect  bool
	ResultTimedOut bool
	ResultTyped    string
	ResultAnswer   string
	ConfusedWith   string
	WrongPause     int

	// Caught up / summary.
	NextDue     string
	CanContinue bool

	// Summary all-time numbers.
	AllCorrect, AllWrong, CardsStudied int
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	guest := s.guestID(w, r)
	view := homeView{}
	now := time.Now()
	for _, info := range s.decks {
		row := homeDeck{Slug: info.Slug, Name: info.Name, Cards: len(info.Cards)}
		// The picker shows the guest's own schedule; a store is cheap to open
		// (one JSON read) and this stays read-only.
		if store, err := progress.NewStoreIn(filepath.Join(s.dataDir, "users", guest), info.Path); err == nil {
			reviews, fresh, _, _ := quiz.SplitDue(info.Cards, store, now)
			row.Due = len(reviews)
			row.Fresh = len(fresh)
			row.Studied = len(fresh) < len(info.Cards)
		}
		view.Decks = append(view.Decks, row)
	}
	s.render(w, "home", view)
}

func (s *Server) handleQuiz(w http.ResponseWriter, r *http.Request) {
	info := s.deckOr404(w, r)
	if info == nil {
		return
	}
	guest := s.guestID(w, r)
	sess, err := s.getSession(guest, info, false)
	if err != nil {
		s.fail(w, err)
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	e := sess.engine
	view := quizView{
		Slug:      info.Slug,
		DeckName:  e.Name(),
		Remaining: e.Remaining(),
		Seen:      e.TotalSeen,
		Correct:   e.TotalCorrect,
		Wrong:     e.TotalWrong,
	}
	speed := sess.deck.Speed
	if speed == 0 {
		speed = 1.0
	}
	view.AudioSpeed = speed

	switch e.State() {
	case quiz.ShowQuestion:
		card := e.Current()
		view.Screen = "question"
		view.Question = mediaViews(info.Slug, card.Question)
		view.Choice = card.Mode == deck.ModeChoice
		view.Options = e.Options()
		view.TimeLimit = e.TimeLimit()
		view.IsNew = e.CurrentIsNew()
		view.IsRetry = e.IsRetry()
		view.IsAhead = e.CurrentIsAhead()

	case quiz.ShowPreview:
		card := e.Current()
		view.Screen = "preview"
		view.Question = mediaViews(info.Slug, card.Question)
		view.AnswerSide = mediaViews(info.Slug, card.Answer)
		view.FlipMode = e.Order() == deck.OrderFlipThrough
		view.IsNew = e.CurrentIsNew()

	case quiz.ShowResult:
		res := sess.last
		view.Screen = "result"
		if res == nil {
			// A session composed fresh can't be in ShowResult without a
			// result; guard anyway rather than render a hole.
			view.Screen = "question"
			break
		}
		view.Question = mediaViews(info.Slug, res.Card.Question)
		view.AnswerSide = mediaViews(info.Slug, res.Card.Answer)
		view.ResultCorrect = res.Correct
		view.ResultTimedOut = res.TimedOut
		view.ResultTyped = res.Typed
		view.ResultAnswer = res.Answer
		if res.ConfusedWith != nil {
			view.ConfusedWith = deck.JoinText(res.ConfusedWith.Question)
		}
		if !res.Correct {
			view.WrongPause = e.WrongPause()
		}

	case quiz.CaughtUp:
		view.Screen = "caughtup"
		view.CanContinue = true
		if due, _ := e.NextDue(); !due.IsZero() {
			view.NextDue = due.Local().Format("Mon Jan 2 15:04")
		}

	case quiz.Done:
		view.Screen = "summary"
		view.CanContinue = e.Order() == deck.OrderAdaptive
		view.AllCorrect, view.AllWrong, view.CardsStudied = sess.store.SummaryFor(e.CardIDs())
		if due, caughtUp := e.NextDue(); !due.IsZero() && caughtUp {
			view.NextDue = due.Local().Format("Mon Jan 2 15:04")
		}
	}

	s.render(w, "quiz", view)
}

// mediaViews converts a card side for the template, routing file media
// through the /media handler by base name.
func mediaViews(slug string, side []deck.Media) []mediaView {
	var out []mediaView
	for _, m := range side {
		switch m.Type {
		case deck.Text:
			if m.Content != "" {
				out = append(out, mediaView{Text: m.Content})
			}
		case deck.Image:
			out = append(out, mediaView{ImageURL: mediaURL(slug, m.Content)})
		case deck.Audio:
			out = append(out, mediaView{AudioURL: mediaURL(slug, m.Content)})
		}
	}
	return out
}

func mediaURL(slug, path string) string {
	return "/media/" + slug + "/" + filepath.Base(path)
}
