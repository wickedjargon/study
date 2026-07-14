package web

import (
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"study/deck"
	"study/quiz"
)

// mediaView is one element of a card side, ready for the template: exactly
// one of Text, ImageURL, or AudioURL is set. The first text line of a side is
// Primary — the hero line; later lines (romanization, glosses) render smaller.
type mediaView struct {
	Text     string
	Primary  bool
	ImageURL string
	AudioURL string
}

// homeGroup is one row of the home page: a language (or subject) plus this
// guest's schedule against it, so a returning friend sees their reviews
// waiting.
type homeGroup struct {
	URL     string // the group page, or straight into the quiz for a single topic
	Name    string
	Initial string
	Hue     int
	Topics  int
	Cards   int
	Due     int
	Fresh   int
	Studied bool
}

// homeSection is one headed block of the home page; an empty Name renders
// its groups without a heading.
type homeSection struct {
	Name   string
	Groups []homeGroup
}

type homeView struct {
	Sections []homeSection
}

// groupDeck is one topic row on a group's page.
type groupDeck struct {
	URL     string
	Name    string
	All     bool // the merged "Everything" entry
	Cards   int
	Due     int
	Fresh   int
	Studied bool
}

type groupView struct {
	Name    string
	Initial string
	Hue     int
	Decks   []groupDeck
}

// quizView carries every screen of the session; Screen selects the template
// block ("question", "preview", "result", "caughtup", "summary").
type quizView struct {
	Screen    string
	Base      string // action/URL prefix: /q/{group}/{deck}
	GroupURL  string
	GroupName string
	DeckName  string
	Hue       int
	Reviewing bool
	Intros    bool   // the guest's introduction preference
	ModeLabel string // the session's answering mode: "type" or "choice"
	// Position is the desktop's [seen/total] session counter, formatted
	// per screen exactly as gui does.
	Position string

	// Header counters. Progress is the session's completion percentage:
	// cards that have met their criterion over cards in play.
	Remaining, Seen, Correct, Wrong int
	Progress                        int

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
	// Confused renders the confused-with card's question — media included,
	// since an image-only card (a flag, a dog) has no text to name it by.
	Confused []mediaView
	WrongPause     int

	// Caught up / summary.
	NextDue     string
	CanContinue bool

	// Summary all-time numbers.
	AllCorrect, AllWrong, CardsStudied int
}

// quizURL returns the quiz page for a group's deck.
func quizURL(g *group, info *deckInfo) string {
	return "/q/" + g.Slug + "/" + info.Slug
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	guest := s.guestID(w, r)
	view := homeView{}
	now := time.Now()
	bySection := make(map[string][]homeGroup)
	for _, g := range s.groups {
		row := homeGroup{
			URL:     "/g/" + g.Slug,
			Name:    g.Name,
			Initial: string([]rune(g.Name)[:1]),
			Hue:     g.Hue,
			Topics:  len(g.Decks),
		}
		if g.single() {
			row.URL = quizURL(g, g.Decks[0])
		}
		// The picker shows the guest's own schedule, totalled over the
		// group's topics. A card can appear in more than one topic (the
		// levels revisit vocab), so the union is taken by card ID; a store
		// is cheap to open (one JSON read) and this stays read-only.
		store, err := s.guestStore(guest, g)
		seen := make(map[string]bool)
		var union []deck.Card
		for _, info := range g.Decks {
			for _, c := range info.Cards {
				if !seen[c.ID] {
					seen[c.ID] = true
					union = append(union, c)
				}
			}
		}
		row.Cards = len(union)
		if err == nil {
			reviews, fresh, _, _ := quiz.SplitDue(union, store, now)
			row.Due = len(reviews)
			row.Fresh = len(fresh)
			row.Studied = row.Fresh < row.Cards
		}
		bySection[g.Section] = append(bySection[g.Section], row)
	}
	for _, name := range s.sections {
		view.Sections = append(view.Sections, homeSection{Name: name, Groups: bySection[name]})
	}
	s.render(w, "home", view)
}

func (s *Server) handleGroup(w http.ResponseWriter, r *http.Request) {
	g := s.groupOr404(w, r)
	if g == nil {
		return
	}
	guest := s.guestID(w, r)
	if g.single() {
		http.Redirect(w, r, quizURL(g, g.Decks[0]), http.StatusSeeOther)
		return
	}

	view := groupView{
		Name:    g.Name,
		Initial: string([]rune(g.Name)[:1]),
		Hue:     g.Hue,
	}
	store, err := s.guestStore(guest, g)
	if err != nil {
		s.fail(w, err)
		return
	}
	now := time.Now()
	row := func(info *deckInfo, all bool) groupDeck {
		reviews, fresh, _, _ := quiz.SplitDue(info.Cards, store, now)
		return groupDeck{
			URL:     quizURL(g, info),
			Name:    info.Name,
			All:     all,
			Cards:   len(info.Cards),
			Due:     len(reviews),
			Fresh:   len(fresh),
			Studied: len(fresh) < len(info.Cards),
		}
	}
	for _, info := range g.Decks {
		view.Decks = append(view.Decks, row(info, false))
	}
	if g.All != nil {
		view.Decks = append(view.Decks, row(g.All, true))
	}
	s.render(w, "group", view)
}

func (s *Server) handleQuiz(w http.ResponseWriter, r *http.Request) {
	g, info := s.deckOr404(w, r)
	if info == nil {
		return
	}
	guest := s.guestID(w, r)
	intros := introsOn(r)
	forced := forcedMode(r, g)
	sess, err := s.getSession(guest, g, info, modeKeep, intros, forced)
	if err != nil {
		s.fail(w, err)
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	e := sess.engine
	view := quizView{
		Base:      quizURL(g, info),
		GroupURL:  "/g/" + g.Slug,
		GroupName: g.Name,
		DeckName:  info.Name,
		Hue:       g.Hue,
		Reviewing: sess.review,
		Intros:    intros,
		ModeLabel: effectiveMode(forced, info),
		Remaining: e.Remaining(),
		Seen:      e.TotalSeen,
		Correct:   e.TotalCorrect,
		Wrong:     e.TotalWrong,
	}
	if g.single() {
		// No topic list to go back to — the breadcrumb points home.
		view.GroupURL = "/"
		view.GroupName = ""
		view.DeckName = g.Name
	}
	if size := e.DeckSize(); size > 0 {
		// On a result screen the just-requeued card is momentarily counted
		// both as current and in the queue, pushing Remaining past size —
		// clamp, or the bar's width goes negative (which CSS renders full).
		view.Progress = max((size-view.Remaining)*100/size, 0)
	}
	speed := sess.deck.Speed
	if speed == 0 {
		speed = 1.0
	}
	view.AudioSpeed = speed

	mediaBase := "/media/" + g.Slug + "/" + info.Slug + "/"

	switch e.State() {
	case quiz.ShowQuestion:
		card := e.Current()
		view.Screen = "question"
		view.Position = fmt.Sprintf("[%d/%d]", e.TotalSeen+1, e.TotalSeen+e.Remaining())
		view.Question = mediaViews(mediaBase, card.Question)
		view.Choice = card.Mode == deck.ModeChoice
		view.Options = e.Options()
		view.TimeLimit = e.TimeLimit()
		view.IsNew = e.CurrentIsNew()
		view.IsRetry = e.IsRetry()
		view.IsAhead = e.CurrentIsAhead()

	case quiz.ShowPreview:
		card := e.Current()
		view.Screen = "preview"
		view.Question = mediaViews(mediaBase, card.Question)
		view.AnswerSide = mediaViews(mediaBase, card.Answer)
		view.FlipMode = e.Order() == deck.OrderFlipThrough
		view.IsNew = e.CurrentIsNew()
		if view.FlipMode {
			// Flip-through wraps forever; the counter is position in the lap.
			view.Position = fmt.Sprintf("[%d/%d]", e.TotalSeen%e.DeckSize()+1, e.DeckSize())
		} else {
			view.Position = fmt.Sprintf("[%d/%d]", e.TotalSeen+1, e.TotalSeen+e.Remaining())
		}

	case quiz.ShowResult:
		res := sess.last
		view.Screen = "result"
		view.Position = fmt.Sprintf("[%d/%d]", e.TotalSeen, e.TotalSeen+e.Remaining())
		if res == nil {
			// A session composed fresh can't be in ShowResult without a
			// result; guard anyway rather than render a hole.
			view.Screen = "question"
			break
		}
		view.Question = mediaViews(mediaBase, res.Card.Question)
		view.AnswerSide = mediaViews(mediaBase, res.Card.Answer)
		view.ResultCorrect = res.Correct
		view.ResultTimedOut = res.TimedOut
		view.ResultTyped = res.Typed
		view.ResultAnswer = res.Answer
		if res.ConfusedWith != nil {
			view.Confused = mediaViews(mediaBase, res.ConfusedWith.Question)
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
func mediaViews(base string, side []deck.Media) []mediaView {
	var out []mediaView
	first := true
	for _, m := range side {
		switch m.Type {
		case deck.Text:
			if m.Content != "" {
				out = append(out, mediaView{Text: m.Content, Primary: first})
				first = false
			}
		case deck.Image:
			out = append(out, mediaView{ImageURL: base + filepath.Base(m.Content)})
		case deck.Audio:
			out = append(out, mediaView{AudioURL: base + filepath.Base(m.Content)})
		}
	}
	return out
}
