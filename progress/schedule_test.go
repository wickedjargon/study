package progress

import (
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir() + "/d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return s
}

func TestScheduleClimbsLadder(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	// Each clean session climbs one rung: 1, 3, 7, ... days. Due lands at
	// the start of the local day the interval reaches, not N*24h out.
	for i, days := range reviewLadder {
		s.Schedule("c", false)
		cp := s.Get("c")
		if cp.Level != i+1 {
			t.Fatalf("session %d: Level = %d, want %d", i+1, cp.Level, i+1)
		}
		want := dayStart(now.AddDate(0, 0, days))
		if !cp.Due.Equal(want) {
			t.Fatalf("session %d: Due = %v, want %v", i+1, cp.Due, want)
		}
	}

	// The top rung is a ceiling, not an overflow.
	s.Schedule("c", false)
	if got := s.Get("c").Level; got != len(reviewLadder) {
		t.Errorf("Level past ladder top = %d, want %d", got, len(reviewLadder))
	}
}

func TestScheduleLapseHalvesLevel(t *testing.T) {
	cases := []struct {
		level, want int
	}{
		{0, 1}, // first-ever session with a miss: bottom rung
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 2},
		{7, 3},
	}
	for _, tc := range cases {
		s := newTestStore(t)
		for i := 0; i < tc.level; i++ {
			s.Schedule("c", false)
		}

		s.Schedule("c", true) // lapsed session: halfway down, not the bottom
		cp := s.Get("c")
		if cp.Level != tc.want {
			t.Errorf("Level after lapse at %d = %d, want %d", tc.level, cp.Level, tc.want)
		}
		want := dayStart(time.Now().AddDate(0, 0, reviewLadder[tc.want-1]))
		if !cp.Due.Equal(want) {
			t.Errorf("Due after lapse at %d = %v, want %v", tc.level, cp.Due, want)
		}
	}
}

// TestScheduleCreditsOverdueSuccess: a clean success on an overdue card
// advances to at least the first rung covering the days actually survived
// (scheduled interval + overdue days), not just one rung up. On-time reviews
// climb exactly one rung as before, and a lapse gets no credit — halving
// stays halving no matter how late the miss came.
func TestScheduleCreditsOverdueSuccess(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 2; i++ {
		s.Schedule("c", false) // Level 2: the 3-day rung
	}

	// Answered 40 days late: 3 + 40 = 43 days survived, and the first rung
	// covering 43 days is 60 (Level 6).
	s.Get("c").Due = time.Now().Add(-40 * 24 * time.Hour)
	s.Schedule("c", false)
	if got := s.Get("c").Level; got != 6 {
		t.Errorf("Level after 40-days-late success at rung 2 = %d, want 6", got)
	}

	// Survival beyond the top rung clamps to the top.
	s.Get("c").Due = time.Now().Add(-500 * 24 * time.Hour)
	s.Schedule("c", false)
	if got, top := s.Get("c").Level, len(reviewLadder); got != top {
		t.Errorf("Level after 500-days-late success = %d, want ladder top %d", got, top)
	}
}

func TestScheduleOnTimeAdvancesOneRung(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 2; i++ {
		s.Schedule("c", false)
	}
	// Due an hour ago: on time in whole-day terms, so exactly one rung up.
	s.Get("c").Due = time.Now().Add(-time.Hour)
	s.Schedule("c", false)
	if got := s.Get("c").Level; got != 3 {
		t.Errorf("Level after on-time success at rung 2 = %d, want 3", got)
	}
}

func TestScheduleLateLapseGetsNoCredit(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 4; i++ {
		s.Schedule("c", false)
	}
	s.Get("c").Due = time.Now().Add(-40 * 24 * time.Hour)
	s.Schedule("c", true)
	if got := s.Get("c").Level; got != 2 {
		t.Errorf("Level after late lapse at rung 4 = %d, want 2 (halved)", got)
	}
}

func TestDueNow(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		cp   CardProgress
		want bool
	}{
		{"no schedule (new or pre-scheduler progress)", CardProgress{}, true},
		{"overdue", CardProgress{Due: now.Add(-time.Hour)}, true},
		{"scheduled ahead", CardProgress{Due: now.Add(time.Hour)}, false},
	}
	for _, tc := range cases {
		if got := tc.cp.DueNow(now); got != tc.want {
			t.Errorf("%s: DueNow = %v, want %v", tc.name, got, tc.want)
		}
	}
}
