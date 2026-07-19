// Package web serves the quiz in a browser: the friends-facing demo at
// study.fftp.io. Visitors are guests — a random ID in a long-lived cookie —
// and each guest gets their own progress directory, so the spaced-repetition
// schedule genuinely works across visits without any account. The engine,
// deck format, and progress files are exactly the desktop app's; this package
// is only the presentation layer, the browser standing in for gui.
//
// The catalog is two-level: a pack directory (study-japanese.deck/) is a
// group whose inner *.deck files are its topic decks, studied one at a time;
// an "Everything" entry offers the merged pack, the way the desktop serves
// it. A guest's progress is kept per group, so cards cleared in one topic
// are known to every other session in that language.
package web

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"study/deck"
	"study/progress"
	"study/quiz"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Server is the HTTP frontend. It owns the catalog (scanned once at startup)
// and the live quiz sessions, one per guest per group.
type Server struct {
	groups []*group
	bySlug map[string]*group
	// sections is home-page heading order: encounter order of the
	// "[Section]" marker arguments, "" (unlabeled) first when present.
	sections []string
	section  string // current section while parsing arguments
	dataDir  string

	ids     *identity
	mailer  Mailer
	baseURL string
	secure  bool // https base URL: session cookies get the Secure flag

	// Local is single-user desktop mode (the Windows app wraps the server
	// in a WebView2 window): one fixed identity instead of guest cookies,
	// login UI hidden, auth routes redirect home. Set after New, before
	// serving.
	Local bool

	tmpl *template.Template
	mux  *http.ServeMux

	mu       sync.Mutex
	sessions map[string]*session

	mailMu   sync.Mutex
	lastMail map[string]time.Time
}

// group is one catalog entry on the home page: a language or subject whose
// topics are studied separately against one shared progress store.
type group struct {
	Slug string
	Name string
	// Section is the home-page heading this group files under, set by a
	// "[Section Name]" marker argument; "" is the unlabeled default.
	Section string
	Hue     int // identity color (0-359), from the slug
	// Path keys the guest's progress store for every deck in the group, so
	// the same card studied under two topics shares one history.
	Path  string
	Decks []*deckInfo // topic decks, sorted by name
	// All is the merged pack — the whole group in one session — present only
	// when the group has more than one topic.
	All *deckInfo
}

// deck returns the group's deck with the given slug, or nil.
func (g *group) deck(slug string) *deckInfo {
	if g.All != nil && g.All.Slug == slug {
		return g.All
	}
	for _, d := range g.Decks {
		if d.Slug == slug {
			return d
		}
	}
	return nil
}

// single reports whether the group is just one topic — its pages then skip
// the topic list and link straight into the quiz.
func (g *group) single() bool {
	return g.All == nil && len(g.Decks) == 1
}

// deckInfo is one quizzable deck: a topic file, or a group's merged pack.
// The startup parse is kept for the picker pages and the media table;
// sessions re-parse so each gets its own mutable card list.
type deckInfo struct {
	Slug  string
	Name  string
	Path  string
	Mode  deck.QuizMode // the deck's authored answer mode
	Cards []deck.Card
	// media maps a file's base name to its absolute path. Media URLs carry
	// only the base name, so a request can never name a path outside the
	// deck's own media set.
	media map[string]string
}

// session is one guest's live quiz within one group. A guest has one session
// per group — switching topics recomposes against the shared store, so the
// engine never races another engine over the same progress file.
type session struct {
	mu      sync.Mutex
	info    *deckInfo
	deck    *deck.Deck
	engine  *quiz.Engine
	store   *progress.Store
	last    *quiz.Result
	review  bool // flip-through: answers visible, nothing recorded
	touched time.Time
}

// sessionMode says what getSession should do with an existing session.
type sessionMode int

const (
	modeKeep   sessionMode = iota // reuse whatever is live, else start a quiz
	modeQuiz                      // force a fresh quiz session
	modeReview                    // force a fresh flip-through session
)

// sessionIdleLimit is how long an untouched session survives; pruned lazily
// when new sessions are created. Progress is saved after every answer, so an
// evicted session loses nothing durable — a returning guest just gets a
// freshly composed session.
const sessionIdleLimit = 6 * time.Hour

// New builds a server serving the given deck paths, keeping per-visitor
// progress under dataDir/users/<id>/ and identity in dataDir/identity.db.
// Login links are minted against baseURL (scheme and host as visitors reach
// the site, no trailing slash) and delivered by the mailer. Each path is
// either a pack directory or single *.deck file (one group), or a directory
// whose deck entries each become a group. A "group=path" argument instead
// nests the pack as one topic inside an existing group, whatever the
// argument order — so Mahjong can live under Japanese rather than clutter
// the top level. A "@Display Name" suffix on a deck path overrides the name
// derived from the file name, e.g. …/study-mexican-spanish.deck@Spanish
// (Mexican).
func New(deckPaths []string, dataDir, baseURL string, mailer Mailer) (*Server, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}
	ids, err := openIdentity(filepath.Join(dataDir, "identity.db"))
	if err != nil {
		return nil, err
	}
	s := &Server{
		bySlug:   make(map[string]*group),
		dataDir:  dataDir,
		ids:      ids,
		mailer:   mailer,
		baseURL:  baseURL,
		secure:   strings.HasPrefix(baseURL, "https://"),
		sessions: make(map[string]*session),
		lastMail: make(map[string]time.Time),
	}

	var nested [][3]string
	for _, p := range deckPaths {
		// "[Section Name]" files every following group under that heading.
		if inner, ok := strings.CutPrefix(p, "["); ok && strings.HasSuffix(inner, "]") {
			s.setSection(strings.TrimSuffix(inner, "]"))
			continue
		}
		var display string
		if path, name, ok := strings.Cut(p, "@"); ok {
			p, display = path, name
		}
		if name, path, ok := strings.Cut(p, "="); ok && !strings.Contains(name, "/") {
			nested = append(nested, [3]string{name, filepath.Clean(path), display})
			continue
		}
		if err := s.scanPath(filepath.Clean(p), display); err != nil {
			return nil, err
		}
	}
	for _, n := range nested {
		if err := s.nestPack(n[0], n[1], n[2]); err != nil {
			return nil, err
		}
	}
	if len(s.groups) == 0 {
		return nil, fmt.Errorf("no decks found in %s", strings.Join(deckPaths, ", "))
	}
	sort.Slice(s.groups, func(i, j int) bool { return s.groups[i].Name < s.groups[j].Name })
	// Unlabeled groups list before the named sections.
	for _, g := range s.groups {
		if g.Section == "" {
			s.sections = append([]string{""}, s.sections...)
			break
		}
	}

	tmpl, err := template.New("").Funcs(template.FuncMap{
		// keynum labels a choice with its 1-based keyboard shortcut.
		"keynum": func(i int) int { return i + 1 },
		// local reports single-user desktop mode (the Windows app): no
		// guests, no login UI. Read at render time so New's caller can set
		// s.Local after construction.
		"local": func() bool { return s.Local },
	}).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}
	s.tmpl = tmpl

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("GET /g/{group}", s.handleGroup)
	mux.HandleFunc("GET /q/{group}/{deck}", s.handleQuiz)
	mux.HandleFunc("POST /q/{group}/{deck}/{action}", s.handleAction)
	mux.HandleFunc("GET /media/{group}/{deck}/{name}", s.handleMedia)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLoginSend)
	mux.HandleFunc("GET /auth/{token}", s.handleAuthPage)
	mux.HandleFunc("POST /auth/{token}", s.handleAuthRedeem)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	s.mux = mux
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// setSection switches the section that subsequently scanned groups file
// under, recording heading order.
func (s *Server) setSection(name string) {
	s.section = name
	for _, have := range s.sections {
		if have == name {
			return
		}
	}
	if name != "" {
		s.sections = append(s.sections, name)
	}
}

// scanPath catalogs one command-line path: a *.deck name (file or pack
// directory) becomes a group; anything else is a directory whose *.deck
// entries each become one (display overrides only apply to deck paths).
func (s *Server) scanPath(path, display string) error {
	if strings.HasSuffix(path, ".deck") {
		s.addGroup(path, display)
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("reading decks dir: %w", err)
	}
	for _, e := range entries {
		sub := filepath.Join(path, e.Name())
		if e.IsDir() {
			if m, _ := filepath.Glob(filepath.Join(sub, "*.deck")); len(m) == 0 {
				continue
			}
		} else if !strings.HasSuffix(e.Name(), ".deck") {
			continue
		}
		s.addGroup(sub, "")
	}
	return nil
}

// addGroup catalogs one group from a pack directory or single deck file,
// displayed under the given name (or one prettified from the file name).
// A group (or topic within it) that fails to parse is skipped with a log
// line rather than sinking the whole server.
func (s *Server) addGroup(path, display string) {
	if display == "" {
		display = prettyName(filepath.Base(path))
	}
	slug := s.uniqueGroupSlug(display)
	g := &group{
		Slug:    slug,
		Name:    display,
		Section: s.section,
		Hue:     deckHue(slug),
		Path:    path,
	}

	fi, err := os.Stat(path)
	if err != nil {
		log.Printf("skipping %s: %v", path, err)
		return
	}

	taken := map[string]bool{"all": true}
	if !fi.IsDir() {
		d := parseDeck(path)
		if d == nil {
			return
		}
		// A fresh taken set: this group's one deck may claim "all" itself.
		g.Decks = []*deckInfo{newDeckInfo(d, g.Name, "all", map[string]bool{})}
	} else {
		inner, _ := filepath.Glob(filepath.Join(path, "*.deck"))
		sort.Strings(inner)
		for _, p := range inner {
			if d := parseDeck(p); d != nil {
				base := strings.TrimSuffix(filepath.Base(p), ".deck")
				g.Decks = append(g.Decks, newDeckInfo(d, prettyName(base), slugify(base), taken))
			}
		}
		if len(g.Decks) == 0 {
			log.Printf("skipping %s: no parseable decks", path)
			return
		}
		sort.Slice(g.Decks, func(i, j int) bool { return g.Decks[i].Name < g.Decks[j].Name })
		if len(g.Decks) > 1 {
			if d := parseDeck(path); d != nil {
				g.All = newDeckInfo(d, "Everything", "", map[string]bool{})
				g.All.Slug = "all"
			}
		}
	}

	s.groups = append(s.groups, g)
	s.bySlug[g.Slug] = g
}

// nestPack adds a pack (or single deck file) as one topic of an existing
// group, named without the group's own name ("Japanese Numbers" under
// Japanese is just "Numbers") unless a display override is given. Its cards
// share the group's progress store; they are not part of the group's merged
// Everything session.
func (s *Server) nestPack(groupSlug, path, display string) error {
	g := s.bySlug[groupSlug]
	if g == nil {
		known := make([]string, 0, len(s.groups))
		for _, gg := range s.groups {
			known = append(known, gg.Slug)
		}
		return fmt.Errorf("cannot nest %s: no group %q (groups: %s)", path, groupSlug, strings.Join(known, ", "))
	}
	d := parseDeck(path)
	if d == nil {
		return fmt.Errorf("nesting %s into %s: deck failed to parse", path, groupSlug)
	}

	taken := map[string]bool{"all": true}
	for _, di := range g.Decks {
		taken[di.Slug] = true
	}
	base := filepath.Base(path)
	name := display
	if name == "" {
		name = strings.TrimPrefix(prettyName(base), g.Name+" ")
	}
	g.Decks = append(g.Decks, newDeckInfo(d, name, slugify(strings.TrimPrefix(base, "study-")), taken))
	sort.Slice(g.Decks, func(i, j int) bool { return g.Decks[i].Name < g.Decks[j].Name })
	return nil
}

// parseDeck parses one deck path, logging and returning nil on failure.
func parseDeck(path string) *deck.Deck {
	d, err := deck.Parse(path)
	if err != nil {
		log.Printf("skipping %s: %v", path, err)
		return nil
	}
	for _, w := range d.Warnings {
		log.Printf("%s: %s", filepath.Base(path), w)
	}
	return d
}

// newDeckInfo builds a catalog deck from a parse, claiming a unique slug
// from taken.
func newDeckInfo(d *deck.Deck, name, slug string, taken map[string]bool) *deckInfo {
	candidate := slug
	for i := 2; taken[candidate]; i++ {
		candidate = slug + "-" + strconv.Itoa(i)
	}
	taken[candidate] = true

	info := &deckInfo{
		Slug:  candidate,
		Name:  name,
		Path:  d.Path,
		Mode:  d.Mode,
		Cards: d.Cards,
		media: make(map[string]string),
	}
	for _, c := range d.Cards {
		for _, side := range [][]deck.Media{c.Question, c.Answer} {
			for _, m := range side {
				if m.Type == deck.Image || m.Type == deck.Audio {
					info.media[filepath.Base(m.Content)] = m.Content
				}
			}
		}
	}
	return info
}

// prettyName turns a deck or pack file name into a display title:
// "study-farsi.deck" → "Farsi", "level1-foundations" → "Level 1 Foundations".
func prettyName(name string) string {
	base := strings.TrimSuffix(name, ".deck")
	base = strings.TrimPrefix(base, "study-")
	words := strings.FieldsFunc(base, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		r := []rune(w)
		// Space a trailing digit run off its word: level1 → Level 1.
		for j := 1; j < len(r); j++ {
			if unicode.IsDigit(r[j]) && !unicode.IsDigit(r[j-1]) {
				r = append(r[:j], append([]rune{' '}, r[j:]...)...)
				break
			}
		}
		words[i] = strings.ToUpper(string(r[:1])) + string(r[1:])
	}
	return strings.Join(words, " ")
}

// slugify turns a name into a URL path segment.
func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSuffix(name, ".deck")) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			// Runs of separators collapse: "Spanish (Mexican)" →
			// "spanish-mexican", not "spanish--mexican-".
			if s := b.String(); s != "" && !strings.HasSuffix(s, "-") {
				b.WriteRune('-')
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "deck"
	}
	return slug
}

// uniqueGroupSlug slugifies a group's file name, deduplicating collisions
// with a numeric suffix.
func (s *Server) uniqueGroupSlug(name string) string {
	slug := slugify(strings.TrimPrefix(name, "study-"))
	candidate := slug
	for i := 2; ; i++ {
		if _, taken := s.bySlug[candidate]; !taken {
			return candidate
		}
		candidate = slug + "-" + strconv.Itoa(i)
	}
}

// deckHue derives an identity hue from a slug, so every group gets a stable
// accent color without anyone choosing one.
func deckHue(slug string) int {
	h := fnv.New32a()
	h.Write([]byte(slug))
	return int(h.Sum32() % 360)
}

// guestID returns the visitor's stable guest identity, minting one (and
// setting the cookie) on first contact. The ID doubles as a directory name
// under dataDir, so anything not shaped like our own hex tokens is discarded.
func (s *Server) guestID(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie("guest"); err == nil && validGuestID(c.Value) {
		return c.Value
	}
	buf := make([]byte, 16)
	rand.Read(buf)
	id := hex.EncodeToString(buf)
	http.SetCookie(w, &http.Cookie{
		Name:     "guest",
		Value:    id,
		Path:     "/",
		MaxAge:   10 * 365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return id
}

func validGuestID(v string) bool {
	if len(v) != 32 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

// forcedMode reports the guest's answering-mode override for a group:
// "type" or "choice" force every card, "" (or anything else) leaves the deck
// author's per-card modes alone. The web's equivalent of the desktop's
// --answer-mode flag, kept per group so drilling one deck as production
// doesn't flip every other deck too.
func forcedMode(r *http.Request, g *group) string {
	c, err := r.Cookie("mode-" + g.Slug)
	if err != nil || (c.Value != "type" && c.Value != "choice") {
		return ""
	}
	return c.Value
}

// modeName renders a quiz mode for the toggle label.
func modeName(m deck.QuizMode) string {
	if m == deck.ModeChoice {
		return "choice"
	}
	return "type"
}

// effectiveMode is what the toggle shows and flips from: the forced override
// when set, else the deck's authored mode.
func effectiveMode(forced string, info *deckInfo) string {
	if forced != "" {
		return forced
	}
	return modeName(info.Mode)
}

// setForcedMode persists the answering-mode override for a group; "" clears.
func setForcedMode(w http.ResponseWriter, g *group, v string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "mode-" + g.Slug,
		Value:    v,
		Path:     "/",
		MaxAge:   10 * 365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// introsOn reports the guest's introduction preference: unseen cards are
// shown answer-visible once before being quizzed. On unless opted out — a
// guest who already knows a deck (say, after clearing cookies) can skip
// straight to being tested.
func introsOn(r *http.Request) bool {
	c, err := r.Cookie("intros")
	return err != nil || c.Value != "off"
}

// setIntros persists the introduction preference.
func setIntros(w http.ResponseWriter, on bool) {
	v := "on"
	if !on {
		v = "off"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "intros",
		Value:    v,
		Path:     "/",
		MaxAge:   10 * 365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// visitorStore opens a visitor's progress store for a group. Guests and
// logged-in users share the layout: one directory per identity.
func (s *Server) visitorStore(visitor string, g *group) (*progress.Store, error) {
	return progress.NewStoreIn(filepath.Join(s.dataDir, "users", visitor), g.Path)
}

// getSession returns the visitor's live session for a deck, composing a
// fresh one (from their saved group progress) if none is live or a different
// topic of the group is. modeQuiz and modeReview recompose even when the
// same topic is live — the "start over" and "review" paths. A review session
// is the deck in flip-through order: every card answer-visible, in authored
// order, nothing recorded.
func (s *Server) getSession(visitor string, g *group, info *deckInfo, mode sessionMode, intros bool, forced string) (*session, error) {
	key := visitor + "|" + g.Slug

	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[key]; ok && sess.info == info && mode == modeKeep {
		sess.touched = time.Now()
		return sess, nil
	}

	for k, sess := range s.sessions {
		if time.Since(sess.touched) > sessionIdleLimit {
			delete(s.sessions, k)
		}
	}

	d, err := deck.Parse(info.Path)
	if err != nil {
		return nil, err
	}
	store, err := s.visitorStore(visitor, g)
	if err != nil {
		return nil, err
	}
	review := mode == modeReview
	pool := info.Cards
	if review {
		d.Order = deck.OrderFlipThrough
	} else {
		// The guest's introduction preference overrides the deck header in
		// both directions: it is their call, not the author's.
		d.Preview = intros
		// Forced answering mode outranks per-card directives and the
		// distractor-implied choice inference, like the desktop's flag. The
		// engine's pool must carry the override too — ContinueAll re-seeds
		// from the pool, and serving catalog-mode cards there would quietly
		// revert the session (a copy: the catalog is shared across guests).
		if forced == "type" || forced == "choice" {
			m := deck.ModeType
			if forced == "choice" {
				m = deck.ModeChoice
			}
			d.Mode = m
			for i := range d.Cards {
				d.Cards[i].Mode = m
			}
			pool = make([]deck.Card, len(info.Cards))
			copy(pool, info.Cards)
			for i := range pool {
				pool[i].Mode = m
			}
		}
	}
	quiz.Compose(d, store, time.Now())
	sess := &session{
		info:    info,
		deck:    d,
		engine:  quiz.NewEngine(d, pool, store),
		store:   store,
		review:  review,
		touched: time.Now(),
	}
	s.sessions[key] = sess
	return sess, nil
}

// groupOr404 resolves the group path segment, writing the 404 itself so
// handlers can just bail on nil.
func (s *Server) groupOr404(w http.ResponseWriter, r *http.Request) *group {
	g := s.bySlug[r.PathValue("group")]
	if g == nil {
		http.NotFound(w, r)
	}
	return g
}

// deckOr404 resolves the group and deck path segments.
func (s *Server) deckOr404(w http.ResponseWriter, r *http.Request) (*group, *deckInfo) {
	g := s.groupOr404(w, r)
	if g == nil {
		return nil, nil
	}
	info := g.deck(r.PathValue("deck"))
	if info == nil {
		http.NotFound(w, r)
		return nil, nil
	}
	return g, info
}

// handleAction applies one quiz transition and redirects back to the quiz
// page (POST-redirect-GET, so a refresh never re-submits an answer).
func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	g, info := s.deckOr404(w, r)
	if info == nil {
		return
	}
	visitor := s.visitorID(w, r)
	action := r.PathValue("action")
	intros := introsOn(r)
	forced := forcedMode(r, g)
	mode := modeKeep
	switch action {
	case "start":
		mode = modeQuiz
	case "review":
		mode = modeReview
	case "intros":
		// Flip the preference and recompose the quiz under it.
		intros = !intros
		setIntros(w, intros)
		mode = modeQuiz
	case "mode":
		// Flip between typed and choice — from whatever the session is
		// currently doing, the deck's authored mode being the start.
		if effectiveMode(forced, info) == "type" {
			forced = "choice"
		} else {
			forced = "type"
		}
		setForcedMode(w, g, forced)
		mode = modeQuiz
	}
	sess, err := s.getSession(visitor, g, info, mode, intros, forced)
	if err != nil {
		s.fail(w, err)
		return
	}

	sess.mu.Lock()
	e := sess.engine
	flash := ""
	switch action {
	case "start", "review", "intros", "mode":
		// getSession already recomposed.
	case "answer":
		var res *quiz.Result
		switch {
		case r.FormValue("timeout") == "1":
			res = e.AnswerTimeout()
		case e.Mode() == deck.ModeChoice:
			if r.FormValue("noidea") == "1" {
				res = e.AnswerNoIdea()
			} else if idx, err := strconv.Atoi(r.FormValue("choice")); err == nil {
				res = e.Answer(idx)
			}
		default:
			res = e.AnswerTyped(r.FormValue("answer"))
		}
		// nil means the engine wasn't at a question (a double submit) —
		// keep the previous result and let the redirect re-render.
		if res != nil {
			sess.last = res
			if err := sess.store.Save(); err != nil {
				log.Printf("saving progress: %v", err)
			}
		}
	case "next":
		e.Next()
	case "practice":
		// One transcription attempt on a near-miss result. The engine keeps
		// the count; Next stays inert until the debt is paid.
		e.PracticeTyped(r.FormValue("practice"))
	case "entry":
		// One entry of a set card's enumeration. The verdict rides the
		// redirect as ?f= so the next render can flash it; the completing
		// entry produces the card's single result.
		if out := e.AnswerSetEntry(r.FormValue("entry")); out != nil {
			if out.Result != nil {
				sess.last = out.Result
				if err := sess.store.Save(); err != nil {
					log.Printf("saving progress: %v", err)
				}
			} else {
				// Counted entries land in the engine's transcript; only
				// the costless outcomes need a flash across the redirect.
				switch out.Verdict {
				case quiz.SetDuplicate:
					flash = "dup"
				case quiz.SetClose:
					flash = "close"
				}
			}
		}
	case "giveup":
		if res := e.AnswerSetGiveUp(); res != nil {
			sess.last = res
			if err := sess.store.Save(); err != nil {
				log.Printf("saving progress: %v", err)
			}
		}
	case "preview":
		e.ConfirmPreview()
	case "continue":
		e.ContinueAll()
	default:
		sess.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	sess.mu.Unlock()

	dest := "/q/" + g.Slug + "/" + info.Slug
	if flash != "" {
		dest += "?f=" + flash
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	_, info := s.deckOr404(w, r)
	if info == nil {
		return
	}
	path, ok := info.media[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	http.Error(w, "something went wrong", http.StatusInternalServerError)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	var b strings.Builder
	if err := s.tmpl.ExecuteTemplate(&b, name, data); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, b.String())
}
