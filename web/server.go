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
	groups  []*group
	bySlug  map[string]*group
	dataDir string

	tmpl *template.Template
	mux  *http.ServeMux

	mu       sync.Mutex
	sessions map[string]*session
}

// group is one catalog entry on the home page: a language or subject whose
// topics are studied separately against one shared progress store.
type group struct {
	Slug string
	Name string
	Hue  int // identity color (0-359), from the slug
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

// New builds a server serving the given deck paths, keeping per-guest
// progress under dataDir/users/<id>/. Each path is either a pack directory
// or single *.deck file (one group), or a directory whose deck entries each
// become a group. A "group=path" argument instead nests the pack as one
// topic inside an existing group, whatever the argument order — so Mahjong
// can live under Japanese rather than clutter the top level.
func New(deckPaths []string, dataDir string) (*Server, error) {
	s := &Server{
		bySlug:   make(map[string]*group),
		dataDir:  dataDir,
		sessions: make(map[string]*session),
	}

	var nested [][2]string
	for _, p := range deckPaths {
		if name, path, ok := strings.Cut(p, "="); ok && !strings.Contains(name, "/") {
			nested = append(nested, [2]string{name, filepath.Clean(path)})
			continue
		}
		if err := s.scanPath(filepath.Clean(p)); err != nil {
			return nil, err
		}
	}
	for _, n := range nested {
		if err := s.nestPack(n[0], n[1]); err != nil {
			return nil, err
		}
	}
	if len(s.groups) == 0 {
		return nil, fmt.Errorf("no decks found in %s", strings.Join(deckPaths, ", "))
	}
	sort.Slice(s.groups, func(i, j int) bool { return s.groups[i].Name < s.groups[j].Name })

	tmpl, err := template.New("").Funcs(template.FuncMap{
		// keynum labels a choice with its 1-based keyboard shortcut.
		"keynum": func(i int) int { return i + 1 },
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
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	s.mux = mux
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// scanPath catalogs one command-line path: a *.deck name (file or pack
// directory) becomes a group; anything else is a directory whose *.deck
// entries each become one.
func (s *Server) scanPath(path string) error {
	if strings.HasSuffix(path, ".deck") {
		s.addGroup(path)
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
		s.addGroup(sub)
	}
	return nil
}

// addGroup catalogs one group from a pack directory or single deck file.
// A group (or topic within it) that fails to parse is skipped with a log
// line rather than sinking the whole server.
func (s *Server) addGroup(path string) {
	slug := s.uniqueGroupSlug(filepath.Base(path))
	g := &group{
		Slug: slug,
		Name: prettyName(filepath.Base(path)),
		Hue:  deckHue(slug),
		Path: path,
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
		g.Decks = []*deckInfo{newDeckInfo(d, g.Name, "all", taken)}
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
// Japanese is just "Numbers"). Its cards share the group's progress store;
// they are not part of the group's merged Everything session.
func (s *Server) nestPack(groupSlug, path string) error {
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
	name := strings.TrimPrefix(prettyName(base), g.Name+" ")
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
			b.WriteRune('-')
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

// guestStore opens the guest's progress store for a group.
func (s *Server) guestStore(guest string, g *group) (*progress.Store, error) {
	return progress.NewStoreIn(filepath.Join(s.dataDir, "users", guest), g.Path)
}

// getSession returns the guest's live session for a deck, composing a fresh
// one (from their saved group progress) if none is live or a different topic
// of the group is. modeQuiz and modeReview recompose even when the same
// topic is live — the "start over" and "review" paths. A review session is
// the deck in flip-through order: every card answer-visible, in authored
// order, nothing recorded.
func (s *Server) getSession(guest string, g *group, info *deckInfo, mode sessionMode) (*session, error) {
	key := guest + "|" + g.Slug

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
	store, err := s.guestStore(guest, g)
	if err != nil {
		return nil, err
	}
	review := mode == modeReview
	if review {
		d.Order = deck.OrderFlipThrough
	}
	quiz.Compose(d, store, time.Now())
	sess := &session{
		info:    info,
		deck:    d,
		engine:  quiz.NewEngine(d, info.Cards, store),
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
	guest := s.guestID(w, r)
	action := r.PathValue("action")
	mode := modeKeep
	switch action {
	case "start":
		mode = modeQuiz
	case "review":
		mode = modeReview
	}
	sess, err := s.getSession(guest, g, info, mode)
	if err != nil {
		s.fail(w, err)
		return
	}

	sess.mu.Lock()
	e := sess.engine
	switch action {
	case "start", "review":
		// getSession already recomposed.
	case "answer":
		var res *quiz.Result
		switch {
		case r.FormValue("timeout") == "1":
			res = e.AnswerTimeout()
		case e.Mode() == deck.ModeChoice:
			if idx, err := strconv.Atoi(r.FormValue("choice")); err == nil {
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
	case "preview":
		e.ConfirmPreview()
	case "continue":
		e.ContinueAll()
	case "end":
		e.End()
	default:
		sess.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	sess.mu.Unlock()

	http.Redirect(w, r, "/q/"+g.Slug+"/"+info.Slug, http.StatusSeeOther)
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
