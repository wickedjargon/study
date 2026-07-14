// Package web serves the quiz in a browser: the friends-facing demo at
// study.fftp.io. Visitors are guests — a random ID in a long-lived cookie —
// and each guest gets their own progress directory, so the spaced-repetition
// schedule genuinely works across visits without any account. The engine,
// deck format, and progress files are exactly the desktop app's; this package
// is only the presentation layer, the browser standing in for gui.
package web

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
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

	"study/deck"
	"study/progress"
	"study/quiz"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Server is the HTTP frontend. It owns the deck catalog (scanned once at
// startup) and the live quiz sessions, one per guest per deck.
type Server struct {
	decks   []*deckInfo
	bySlug  map[string]*deckInfo
	dataDir string

	tmpl *template.Template
	mux  *http.ServeMux

	mu       sync.Mutex
	sessions map[string]*session
}

// deckInfo is a catalog entry: the parse of a deck at startup, kept for the
// picker page and the media table. Sessions re-parse the file so each gets
// its own mutable card list.
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

// session is one guest's live quiz over one deck.
type session struct {
	mu      sync.Mutex
	deck    *deck.Deck
	engine  *quiz.Engine
	store   *progress.Store
	last    *quiz.Result
	touched time.Time
}

// sessionIdleLimit is how long an untouched session survives; pruned lazily
// when new sessions are created. Progress is saved after every answer, so an
// evicted session loses nothing durable — a returning guest just gets a
// freshly composed session.
const sessionIdleLimit = 6 * time.Hour

// New builds a server serving every deck found in decksDir (files or pack
// directories), keeping per-guest progress under dataDir/users/<id>/.
func New(decksDir, dataDir string) (*Server, error) {
	s := &Server{
		bySlug:   make(map[string]*deckInfo),
		dataDir:  dataDir,
		sessions: make(map[string]*session),
	}

	if err := s.scanDecks(decksDir); err != nil {
		return nil, err
	}
	if len(s.decks) == 0 {
		return nil, fmt.Errorf("no decks found in %s", decksDir)
	}

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
	mux.HandleFunc("GET /q/{slug}", s.handleQuiz)
	mux.HandleFunc("POST /q/{slug}/start", s.handleStart)
	mux.HandleFunc("POST /q/{slug}/{action}", s.handleAction)
	mux.HandleFunc("GET /media/{slug}/{name}", s.handleMedia)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	s.mux = mux
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// scanDecks catalogs every deck under dir: *.deck files, and directories
// (packs — possibly themselves named *.deck/) containing deck files.
func (s *Server) scanDecks(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading decks dir: %w", err)
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if m, _ := filepath.Glob(filepath.Join(path, "*.deck")); len(m) == 0 {
				continue
			}
		} else if !strings.HasSuffix(e.Name(), ".deck") {
			continue
		}

		d, err := deck.Parse(path)
		if err != nil {
			log.Printf("skipping %s: %v", path, err)
			continue
		}
		for _, w := range d.Warnings {
			log.Printf("%s: %s", e.Name(), w)
		}

		info := &deckInfo{
			Slug:  s.uniqueSlug(e.Name()),
			Name:  d.Name,
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
		s.decks = append(s.decks, info)
		s.bySlug[info.Slug] = info
	}
	sort.Slice(s.decks, func(i, j int) bool { return s.decks[i].Name < s.decks[j].Name })
	return nil
}

// uniqueSlug turns a deck's file name into a URL path segment, deduplicating
// collisions with a numeric suffix.
func (s *Server) uniqueSlug(name string) string {
	base := strings.TrimSuffix(name, ".deck")
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
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
	candidate := slug
	for i := 2; ; i++ {
		if _, taken := s.bySlug[candidate]; !taken {
			return candidate
		}
		candidate = slug + "-" + strconv.Itoa(i)
	}
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

// getSession returns the guest's live session for a deck, composing a fresh
// one (from their saved progress) if none is live. forceNew recomposes even
// when one exists — the "study again" path.
func (s *Server) getSession(guest string, info *deckInfo, forceNew bool) (*session, error) {
	key := guest + "|" + info.Slug

	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[key]; ok && !forceNew {
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
	store, err := progress.NewStoreIn(filepath.Join(s.dataDir, "users", guest), d.Path)
	if err != nil {
		return nil, err
	}
	quiz.Compose(d, store, time.Now())
	sess := &session{
		deck:    d,
		engine:  quiz.NewEngine(d, info.Cards, store),
		store:   store,
		touched: time.Now(),
	}
	s.sessions[key] = sess
	return sess, nil
}

// deckOr404 resolves the slug path segment, writing the 404 itself so
// handlers can just bail on nil.
func (s *Server) deckOr404(w http.ResponseWriter, r *http.Request) *deckInfo {
	info := s.bySlug[r.PathValue("slug")]
	if info == nil {
		http.NotFound(w, r)
	}
	return info
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	info := s.deckOr404(w, r)
	if info == nil {
		return
	}
	guest := s.guestID(w, r)
	if _, err := s.getSession(guest, info, true); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/q/"+info.Slug, http.StatusSeeOther)
}

// handleAction applies one quiz transition and redirects back to the quiz
// page (POST-redirect-GET, so a refresh never re-submits an answer).
func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
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
	e := sess.engine
	switch r.PathValue("action") {
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

	http.Redirect(w, r, "/q/"+info.Slug, http.StatusSeeOther)
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	info := s.deckOr404(w, r)
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
