// Package progress handles cross-session persistence of quiz results.
package progress

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"study/deck"
	"time"
)

// CardProgress tracks per-card performance across sessions.
type CardProgress struct {
	TimesCorrect int       `json:"times_correct"`
	TimesWrong   int       `json:"times_wrong"`
	Streak       int       `json:"streak"`    // consecutive correct
	LastSeen     time.Time `json:"last_seen"`
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

// NewStore creates a progress store for a given deck.
func NewStore(deckPath string) (*Store, error) {
	dir, err := progressDir()
	if err != nil {
		return nil, err
	}

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

// Save writes progress to disk.
func (s *Store) Save() error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("creating progress dir: %w", err)
	}

	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling progress: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("writing progress: %w", err)
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

// HasProgress returns true if there is any saved progress for this deck.
func (s *Store) HasProgress() bool {
	return len(s.data.Cards) > 0
}

// Summary returns aggregate stats for display.
func (s *Store) Summary() (totalCorrect, totalWrong, cardsStudied int) {
	for _, cp := range s.data.Cards {
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

// progressDir returns the directory for storing progress files.
func progressDir() (string, error) {
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
