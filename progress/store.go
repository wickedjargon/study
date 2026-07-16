// Package progress handles cross-session persistence of quiz results.
package progress

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"study/deck"
	"time"
)

// CardProgress tracks per-card performance across sessions.
type CardProgress struct {
	TimesCorrect int       `json:"times_correct"`
	TimesWrong   int       `json:"times_wrong"`
	Streak       int       `json:"streak"` // consecutive correct
	LastSeen     time.Time `json:"last_seen"`

	// Between-session scheduling state (successive relearning). Level counts
	// completed successful sessions and indexes reviewLadder; Due is when the
	// card should next be reviewed. Zero values (including progress saved by
	// older versions) mean "due now".
	Level int       `json:"level,omitempty"`
	Due   time.Time `json:"due,omitempty"`
}

// reviewLadder is the between-session interval schedule, in days: each
// successful session moves a card one rung up, so reviews spread out as the
// card is retained across sessions. Distributing retrieval practice across
// days is the most robust effect in the learning literature (successive
// relearning: Rawson & Dunlosky); expanding the gaps keeps it efficient.
var reviewLadder = []int{1, 3, 7, 14, 30, 60, 120}

// Schedule records that a card met its session criterion and sets its next
// review. A clean session moves the card one rung up the ladder; a session
// with a lapse (any miss) drops it to half its level — not the bottom.
// Relearning a once-known card is far faster than initial learning (savings,
// Ebbinghaus 1885), so a miss means the interval outran this card's memory,
// not that the memory is gone; halving backs off geometrically without
// discarding the history, the way stability-based schedulers (e.g. FSRS)
// treat a lapse.
func (s *Store) Schedule(cardID string, lapsed bool) {
	cp := s.ensure(cardID)
	if lapsed {
		cp.Level = cp.Level / 2
		if cp.Level < 1 {
			cp.Level = 1
		}
	} else if cp.Level < len(reviewLadder) {
		cp.Level++
	}
	days := reviewLadder[cp.Level-1]
	cp.Due = time.Now().Add(time.Duration(days) * 24 * time.Hour)
}

// DueNow reports whether the card should be reviewed now. Cards with no
// recorded schedule (new cards, or progress from older versions) are due.
func (cp *CardProgress) DueNow(now time.Time) bool {
	return cp.Due.IsZero() || !cp.Due.After(now)
}

// Accuracy returns the percentage of correct answers (0-100).
func (cp *CardProgress) Accuracy() float64 {
	total := cp.TimesCorrect + cp.TimesWrong
	if total == 0 {
		return 0
	}
	return float64(cp.TimesCorrect) / float64(total) * 100
}

// DeckProgress stores progress for an entire deck.
type DeckProgress struct {
	DeckPath string                   `json:"deck_path"`
	Cards    map[string]*CardProgress `json:"cards"` // keyed by card ID
}

// Store manages loading and saving progress data.
type Store struct {
	dir  string // directory for progress files
	data *DeckProgress
	path string // path to the progress file
}

// NewStore creates a progress store for a given deck in the default
// per-user directory (~/.local/share/study).
func NewStore(deckPath string) (*Store, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return NewStoreIn(dir, deckPath)
}

// NewStoreIn creates a progress store for a given deck inside an explicit
// directory. The web server keeps one directory per user this way; the file
// layout inside is identical to the desktop default.
func NewStoreIn(dir, deckPath string) (*Store, error) {
	hash := deckHash(deckPath)
	path := filepath.Join(dir, hash+".json")

	s := &Store{
		dir:  dir,
		path: path,
	}

	// Try to load existing progress.
	if data, err := s.load(); err == nil {
		s.data = data
	} else {
		s.data = &DeckProgress{
			DeckPath: deckPath,
			Cards:    make(map[string]*CardProgress),
		}
	}

	return s, nil
}

// Get returns the progress for a specific card.
func (s *Store) Get(cardID string) *CardProgress {
	if cp, ok := s.data.Cards[cardID]; ok {
		return cp
	}
	return &CardProgress{}
}

// RecordCorrect records a correct answer for a card.
func (s *Store) RecordCorrect(cardID string) {
	cp := s.ensure(cardID)
	cp.TimesCorrect++
	cp.Streak++
	cp.LastSeen = time.Now()
}

// RecordWrong records a wrong answer for a card.
func (s *Store) RecordWrong(cardID string) {
	cp := s.ensure(cardID)
	cp.TimesWrong++
	cp.Streak = 0
	cp.LastSeen = time.Now()
}

// Save writes progress to disk atomically and durably. Because callers persist
// after every answer, a plain truncate-then-write (os.WriteFile) would leave the
// file half-written if the process is killed mid-write, corrupting the entire
// progress history. Instead we write to a temp file in the same directory, fsync
// it, then rename it over the real path — rename is atomic on POSIX, so a reader
// always sees either the old complete file or the new complete file. Finally we
// fsync the containing directory: rename is atomic, but the directory entry that
// points at the new file isn't durable until the directory itself is synced, so
// without this a crash right after the rename could revert to the old file.
func (s *Store) Save() error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("creating progress dir: %w", err)
	}

	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling progress: %w", err)
	}

	// Temp file must share a filesystem with the target for rename to be atomic,
	// so create it alongside the destination.
	tmp, err := os.CreateTemp(s.dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp progress file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before the rename succeeds.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing progress: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing progress: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing progress: %w", err)
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replacing progress file: %w", err)
	}

	// Make the rename durable by syncing the directory. Best-effort: not all
	// platforms/filesystems permit fsync on a directory, and a failure here
	// doesn't mean the data is lost (the rename already succeeded), so we don't
	// surface it as an error.
	if dir, err := os.Open(s.dir); err == nil {
		dir.Sync()
		dir.Close()
	}

	return nil
}

// Reset clears all progress data.
func (s *Store) Reset() {
	s.data = &DeckProgress{
		DeckPath: s.data.DeckPath,
		Cards:    make(map[string]*CardProgress),
	}
}

// reversePrefix namespaces the IDs of reversed cards (deck.Reversed) so a
// card's forward and reverse recall accumulate progress independently.
const reversePrefix = "r:"

// ResetDirection clears progress for one direction of the deck only: the
// reversed cards ("r:"-prefixed IDs) when reverse is true, the forward cards
// otherwise. --forget uses this so forgetting one direction of a language deck
// doesn't destroy the other's history.
func (s *Store) ResetDirection(reverse bool) {
	for id := range s.data.Cards {
		if strings.HasPrefix(id, reversePrefix) == reverse {
			delete(s.data.Cards, id)
		}
	}
}

// MigrateIDs moves progress saved under each card's legacy ID (see
// deck.Card.LegacyID: the old hash included media lines, so renaming an audio
// file re-keyed the card) to its current ID, in both the forward and reverse
// namespaces. An entry already present under the new ID wins — real progress
// isn't overwritten by stale history. Reports whether anything moved, so the
// caller knows to save.
func (s *Store) MigrateIDs(cards []deck.Card) bool {
	moved := false
	for i := range cards {
		c := &cards[i]
		if c.LegacyID == "" {
			continue
		}
		for _, pre := range []string{"", reversePrefix} {
			from, to := pre+c.LegacyID, pre+c.ID
			cp, ok := s.data.Cards[from]
			if !ok {
				continue
			}
			if _, exists := s.data.Cards[to]; !exists {
				s.data.Cards[to] = cp
			}
			delete(s.data.Cards, from)
			moved = true
		}
	}
	return moved
}

// Confidence returns a 0-100 score indicating how well the user knows this card.
// Factors: accuracy, streak length, and number of times seen.
func (cp *CardProgress) Confidence() float64 {
	total := cp.TimesCorrect + cp.TimesWrong
	if total == 0 {
		return 0
	}
	acc := cp.Accuracy() / 100.0 // 0..1
	// Streak bonus: each consecutive correct adds confidence, capped at 10.
	streakFactor := float64(cp.Streak) / 10.0
	if streakFactor > 1 {
		streakFactor = 1
	}
	// Volume factor: more attempts = more reliable score, capped at 10.
	volumeFactor := float64(total) / 10.0
	if volumeFactor > 1 {
		volumeFactor = 1
	}
	return (acc*0.5 + streakFactor*0.3 + volumeFactor*0.2) * 100
}

// IsMastered returns true if the user has demonstrated consistent mastery.
func (cp *CardProgress) IsMastered() bool {
	total := cp.TimesCorrect + cp.TimesWrong
	return total >= 5 && cp.Streak >= 5 && cp.Accuracy() == 100
}

// PrioritizeCards reorders cards to put weak ones first and optionally
// excludes mastered cards. Cards are ordered by confidence (lowest first).
func (s *Store) PrioritizeCards(cards []deck.Card) []deck.Card {
	var active []deck.Card
	var mastered []deck.Card

	for _, c := range cards {
		cp := s.Get(c.ID)
		if cp.IsMastered() {
			mastered = append(mastered, c)
		} else {
			active = append(active, c)
		}
	}

	// Sort active cards: lowest confidence first.
	sort.SliceStable(active, func(i, j int) bool {
		ci := s.Get(active[i].ID).Confidence()
		cj := s.Get(active[j].ID).Confidence()
		return ci < cj
	})

	// Append mastered cards at the end (they'll rarely be reached).
	active = append(active, mastered...)
	return active
}

// WeakThreshold is the confidence score below which a card counts as weak for
// a "# order: weak-only" cram session. Never-studied cards score 0, so they
// are always included; a mastered card scores well above it.
const WeakThreshold = 50

// FilterWeak returns only the cards worth cramming: those whose confidence is
// below WeakThreshold. May return an empty slice — a deck in good shape has
// nothing to cram.
func (s *Store) FilterWeak(cards []deck.Card) []deck.Card {
	var weak []deck.Card
	for _, c := range cards {
		if s.Get(c.ID).Confidence() < WeakThreshold {
			weak = append(weak, c)
		}
	}
	return weak
}

// Summary returns aggregate stats over every stored entry, including orphaned
// progress from removed/edited cards and the other study direction.
func (s *Store) Summary() (totalCorrect, totalWrong, cardsStudied int) {
	for _, cp := range s.data.Cards {
		totalCorrect += cp.TimesCorrect
		totalWrong += cp.TimesWrong
		cardsStudied++
	}
	return
}

// LastStudied returns the most recent answer time recorded anywhere in the
// store — either direction, current or orphaned cards — or the zero time when
// the deck has never been answered. The library view sorts and labels decks
// with it.
func (s *Store) LastStudied() time.Time {
	var last time.Time
	for _, cp := range s.data.Cards {
		if cp.LastSeen.After(last) {
			last = cp.LastSeen
		}
	}
	return last
}

// SummaryFor returns aggregate stats scoped to the given card IDs — i.e. the
// deck as it exists now, in the direction being studied — so orphaned progress
// and the opposite direction's history don't inflate the numbers.
func (s *Store) SummaryFor(ids []string) (totalCorrect, totalWrong, cardsStudied int) {
	for _, id := range ids {
		cp, ok := s.data.Cards[id]
		if !ok || cp.TimesCorrect+cp.TimesWrong == 0 {
			continue
		}
		totalCorrect += cp.TimesCorrect
		totalWrong += cp.TimesWrong
		cardsStudied++
	}
	return
}

// ensure returns the progress for a card, creating it if needed.
func (s *Store) ensure(cardID string) *CardProgress {
	if cp, ok := s.data.Cards[cardID]; ok {
		return cp
	}
	cp := &CardProgress{}
	s.data.Cards[cardID] = cp
	return cp
}

// load reads progress from disk.
func (s *Store) load() (*DeckProgress, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}

	var dp DeckProgress
	if err := json.Unmarshal(data, &dp); err != nil {
		return nil, err
	}

	if dp.Cards == nil {
		dp.Cards = make(map[string]*CardProgress)
	}

	return &dp, nil
}

// Dir returns the per-user study data directory (~/.local/share/study): home
// of the progress files, and of the library registry, which lives beside them.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "study"), nil
}

// deckHash generates a short hash from the deck path for the filename.
func deckHash(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", h[:8])
}
