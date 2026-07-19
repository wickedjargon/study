// Package library is the registry behind the library screen: which decks the
// user shelves for long-term study, and what state each is in.
//
// Membership is explicit, never inferred from having studied something — a
// one-off `study /tmp/test.deck` run must not shelve the file. The user
// watches directories (their real deck collections); the library is whatever
// a scan of those finds. The registry itself is one JSON file in the study
// data directory, beside the progress files.
package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"study/deck"
	"study/progress"
	"study/quiz"
)

const registryName = "library.json"

// Registry is the persistent membership list: the watched directories scanned
// for decks.
type Registry struct {
	Dirs []string `json:"dirs"`

	path string // where Save writes; set by Open
}

// Open loads the registry from dir (the study data directory). A missing file
// is an empty library, not an error — the registry is created on first Save.
func Open(dir string) (*Registry, error) {
	r := &Registry{path: filepath.Join(dir, registryName)}
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading library registry: %w", err)
	}
	if err := json.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", r.path, err)
	}
	return r, nil
}

// Save writes the registry back to its file.
func (r *Registry) Save() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling registry: %w", err)
	}
	if err := os.WriteFile(r.path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing registry: %w", err)
	}
	return nil
}

// Empty reports whether nothing is watched.
func (r *Registry) Empty() bool {
	return len(r.Dirs) == 0
}

// Watch adds a directory to the scan list. The path must be an existing
// directory; it is stored absolute so the library doesn't depend on where
// study is later run from.
func (r *Registry) Watch(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", abs)
	}
	for _, d := range r.Dirs {
		if d == abs {
			return fmt.Errorf("already watching %s", abs)
		}
	}
	r.Dirs = append(r.Dirs, abs)
	return nil
}

// Unwatch removes a directory from the scan list.
func (r *Registry) Unwatch(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for i, d := range r.Dirs {
		if d == abs {
			r.Dirs = append(r.Dirs[:i], r.Dirs[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not watching %s", abs)
}

// Entry is one launchable deck the scan found: exactly what `study <path>`
// would take, a .deck file or a pack directory.
type Entry struct {
	Path string
	Name string // display name: the base name without the .deck suffix
	Pack bool   // Path is a pack directory, studied as one merged deck
}

// Group is the scan result for one watched directory. A directory that can't
// be read reports Err instead of silently vanishing from the library.
type Group struct {
	Dir     string
	Err     error
	Entries []Entry
}

// Scan walks the watched directories for launchable entries. Inside a watched
// directory, *.deck files are individual decks and each immediate
// subdirectory containing *.deck files is a pack; anything else is ignored.
// Groups come back in registry order (the order the user watched them),
// entries sorted by name.
func (r *Registry) Scan() []Group {
	var groups []Group
	for _, dir := range r.Dirs {
		g := Group{Dir: dir}
		entries, err := os.ReadDir(dir)
		if err != nil {
			g.Err = err
			groups = append(groups, g)
			continue
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			// Stat rather than e.IsDir so a symlinked pack directory counts.
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			switch {
			case info.IsDir() && isPack(path):
				g.Entries = append(g.Entries, Entry{Path: path, Name: entryName(path), Pack: true})
			case !info.IsDir() && strings.HasSuffix(e.Name(), ".deck"):
				g.Entries = append(g.Entries, Entry{Path: path, Name: entryName(path)})
			}
		}
		sort.Slice(g.Entries, func(i, j int) bool { return g.Entries[i].Name < g.Entries[j].Name })
		groups = append(groups, g)
	}
	return groups
}

// PackEntries lists the individual decks inside a pack directory — the units
// `study <pack>/<file>.deck` would take. The library screen shows them when a
// pack is expanded, the way the web's group page lists its topics. Sorted by
// file name, the order a merged pack session concatenates them in.
func PackEntries(dir string) []Entry {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.deck"))
	sort.Strings(matches)
	var entries []Entry
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && !info.IsDir() {
			entries = append(entries, Entry{Path: m, Name: entryName(m)})
		}
	}
	return entries
}

// isPack reports whether a directory holds at least one *.deck file, i.e.
// whether deck.Parse would accept it.
func isPack(dir string) bool {
	matches, err := filepath.Glob(filepath.Join(dir, "*.deck"))
	return err == nil && len(matches) > 0
}

// entryName is the display name for a deck path: the pack's own .deck-info
// name when it has one, else the base name with the .deck suffix dropped
// (packs conventionally carry it too, e.g. study-farsi.deck/).
func entryName(path string) string {
	if n := deck.PackInfoName(path); n != "" {
		return n
	}
	return strings.TrimSuffix(filepath.Base(path), ".deck")
}

// Info is a deck's studyable state, the numbers a library row shows. The
// forward and reverse directions are reported separately because they are
// separate skills with separate progress; Reversible is false when the deck
// has no cards that survive Reversed().
type Info struct {
	Cards       int // cards in the deck (forward direction)
	DueReviews  int // studied cards due now
	DueNew      int // never-studied cards
	Reversible  bool
	RevCards    int // cards that survive reversal
	RevReviews  int // reverse direction, as above
	RevNew      int
	LastStudied time.Time // zero when never studied (either direction)
	Err         error     // the deck or its progress couldn't be loaded
}

// Part is one piece of a direction's status on a library row; Kind tells the
// GUI how to color it. The states mirror the web app's deck rows.
type Part struct {
	Text string
	Kind PartKind
}

type PartKind int

const (
	KindUnseen PartKind = iota // quiet: new material waiting
	KindDue                    // demands attention: reviews due now
	KindDone                   // green check: nothing due, nothing unseen
)

// DirParts describes one direction's state the way the web app badges it:
// "N to review" when reviews are due; "nothing due" — the day's scheduled
// work is done — while new material still waits, alongside its "N unseen";
// and "caught up ✓" only when the direction is truly exhausted for now. A
// direction never studied at all reports nothing — the row says "never
// studied" elsewhere, and its unseen count would just repeat the deck size.
func DirParts(due, fresh, cards int) []Part {
	if cards == 0 || fresh >= cards {
		return nil
	}
	var parts []Part
	if due > 0 {
		parts = append(parts, Part{fmt.Sprintf("%d to review", due), KindDue})
	} else if fresh > 0 {
		parts = append(parts, Part{"nothing due", KindDone})
	}
	if fresh > 0 {
		parts = append(parts, Part{fmt.Sprintf("%d unseen", fresh), KindUnseen})
	}
	if len(parts) == 0 {
		parts = append(parts, Part{"caught up ✓", KindDone})
	}
	return parts
}

// AgoLabel formats when a deck was last studied, for a library row.
func AgoLabel(last, now time.Time) string {
	if last.IsZero() {
		return "never studied"
	}
	days := int(now.Sub(last).Hours() / 24)
	switch {
	case days < 1:
		return "studied today"
	case days == 1:
		return "studied yesterday"
	default:
		return fmt.Sprintf("studied %dd ago", days)
	}
}

// Describe parses the deck at path and joins it with its saved progress.
// Errors come back inside the Info — one unparsable deck grays out its row,
// it doesn't take down the library.
func Describe(path string, now time.Time) Info {
	d, err := deck.Parse(path)
	if err != nil {
		return Info{Err: err}
	}
	store, err := progress.NewStore(d.Path)
	if err != nil {
		return Info{Err: err}
	}

	info := Info{Cards: len(d.Cards), LastStudied: store.LastStudied()}
	reviews, fresh, _, _ := quiz.SplitDue(d.Cards, store, now)
	info.DueReviews, info.DueNew = len(reviews), len(fresh)

	if rd := d.Reversed(); len(rd.Cards) > 0 {
		info.Reversible = true
		info.RevCards = len(rd.Cards)
		reviews, fresh, _, _ = quiz.SplitDue(rd.Cards, store, now)
		info.RevReviews, info.RevNew = len(reviews), len(fresh)
	}
	return info
}
