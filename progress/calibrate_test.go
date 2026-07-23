package progress

import (
	"os"
	"testing"
	"time"
)

// TestCalibrate: review asks (empty State, a rung) feed the per-rung and
// per-mode buckets; badged answers only the per-state ones; cards outside
// the id scope (other direction, pack siblings) are ignored entirely.
func TestCalibrate(t *testing.T) {
	events := []ReviewEvent{
		{Card: "a", State: "", Level: 2, Mode: "type", Correct: true},
		{Card: "a", State: "", Level: 2, Mode: "type", Correct: false},
		{Card: "b", State: "", Level: 3, Mode: "choice", Correct: true},
		{Card: "a", State: "retry", Level: 2, Mode: "type", Correct: true},
		{Card: "c", State: "new", Mode: "type", Correct: false},
		{Card: "r:a", State: "", Level: 5, Mode: "type", Correct: true}, // other direction
	}
	ids := map[string]bool{"a": true, "b": true, "c": true}

	cal := Calibrate(events, ids)

	if cal.Events != 5 || cal.Reviews != 3 {
		t.Fatalf("Events, Reviews = %d, %d; want 5, 3", cal.Events, cal.Reviews)
	}
	if b := cal.Rungs[2]; b == nil || b.Asks != 2 || b.Correct != 1 {
		t.Errorf("rung 2 = %+v, want 2 asks 1 correct", cal.Rungs[2])
	}
	if b := cal.Rungs[3]; b == nil || b.Asks != 1 || b.Correct != 1 {
		t.Errorf("rung 3 = %+v, want 1 ask 1 correct", cal.Rungs[3])
	}
	if cal.Rungs[5] != nil {
		t.Error("rung 5 bucket exists; the other direction's events must be out of scope")
	}
	if b := cal.States["review"]; b == nil || b.Asks != 3 {
		t.Errorf("state review = %+v, want 3 asks", cal.States["review"])
	}
	if b := cal.States["retry"]; b == nil || b.Asks != 1 || b.Correct != 1 {
		t.Errorf("state retry = %+v, want 1 ask 1 correct", cal.States["retry"])
	}
	if b := cal.Modes["type"]; b == nil || b.Asks != 2 {
		t.Errorf("mode type = %+v, want 2 asks (review asks only, no retry/new)", cal.Modes["type"])
	}
	if b := cal.Modes["choice"]; b == nil || b.Asks != 1 {
		t.Errorf("mode choice = %+v, want 1 ask", cal.Modes["choice"])
	}
}

// TestReadLog: the log round-trips through LogReview, a missing log is an
// empty history, and a torn line (kill mid-append) hides only itself.
func TestReadLog(t *testing.T) {
	s := newTestStore(t)

	if events, err := s.ReadLog(); err != nil || len(events) != 0 {
		t.Fatalf("missing log: events, err = %v, %v; want none, nil", events, err)
	}

	s.LogReview(ReviewEvent{TS: time.Now(), Card: "a", Correct: true, Level: 1})
	s.LogReview(ReviewEvent{TS: time.Now(), Card: "b", Correct: false})
	f, err := os.OpenFile(s.logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"card":"torn`)
	f.Close()

	events, err := s.ReadLog()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Card != "a" || events[1].Card != "b" {
		t.Fatalf("events = %+v, want the two whole lines in order", events)
	}
}

func TestLadderDays(t *testing.T) {
	for rung, want := range map[int]int{0: 0, 1: 1, 2: 3, 7: 120, 8: 0} {
		if got := LadderDays(rung); got != want {
			t.Errorf("LadderDays(%d) = %d, want %d", rung, got, want)
		}
	}
}
