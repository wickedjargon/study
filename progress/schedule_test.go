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

	// Each clean session climbs one rung: 1, 3, 7, ... days.
	for i, days := range reviewLadder {
		s.Schedule("c", false)
		cp := s.Get("c")
		if cp.Level != i+1 {
			t.Fatalf("session %d: Level = %d, want %d", i+1, cp.Level, i+1)
		}
		want := now.Add(time.Duration(days) * 24 * time.Hour)
		if d := cp.Due.Sub(want); d < -time.Minute || d > time.Minute {
			t.Fatalf("session %d: Due = %v, want ~%v", i+1, cp.Due, want)
		}
	}

	// The top rung is a ceiling, not an overflow.
	s.Schedule("c", false)
	if got := s.Get("c").Level; got != len(reviewLadder) {
		t.Errorf("Level past ladder top = %d, want %d", got, len(reviewLadder))
	}
}

func TestScheduleLapseResets(t *testing.T) {
	s := newTestStore(t)

	s.Schedule("c", false)
	s.Schedule("c", false)
	s.Schedule("c", false) // level 3 (7 days)

	s.Schedule("c", true) // lapsed session: back to the bottom
	cp := s.Get("c")
	if cp.Level != 1 {
		t.Errorf("Level after lapse = %d, want 1", cp.Level)
	}
	want := time.Now().Add(24 * time.Hour)
	if d := cp.Due.Sub(want); d < -time.Minute || d > time.Minute {
		t.Errorf("Due after lapse = %v, want ~1 day out", cp.Due)
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
