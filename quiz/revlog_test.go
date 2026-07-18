package quiz

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"study/progress"
)

// readLog decodes every line of the single review log in dir.
func readLog(t *testing.T, dir string) []progress.ReviewEvent {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("expected one review log in %s, got %v (%v)", dir, logs, err)
	}
	raw, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	var events []progress.ReviewEvent
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var ev progress.ReviewEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		events = append(events, ev)
	}
	return events
}

// TestReviewLog: every graded answer appends one event carrying the
// scheduler's view at ask time — badge state, owed recalls, and the ladder
// outcome on the completing answer.
func TestReviewLog(t *testing.T) {
	dir := t.TempDir()
	store, err := progress.NewStoreIn(dir, "d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	e := NewEngine(confusableDeck(2), nil, store)

	answers := 0
	for i := 0; i < 20 && e.State() != Done; i++ {
		answerCurrent(e, true)
		answers++
		e.Next()
	}
	if e.State() != Done {
		t.Fatal("session never completed")
	}

	events := readLog(t, dir)
	if len(events) != answers {
		t.Fatalf("logged %d events, want %d (one per graded answer)", len(events), answers)
	}

	perCard := make(map[string][]progress.ReviewEvent)
	for _, ev := range events {
		if !ev.Correct {
			t.Errorf("all answers were correct, event says otherwise: %+v", ev)
		}
		if ev.Mode != "type" {
			t.Errorf("mode = %q, want type", ev.Mode)
		}
		perCard[ev.Card] = append(perCard[ev.Card], ev)
	}
	for card, evs := range perCard {
		if len(evs) != needNew {
			t.Fatalf("%s: %d events, want %d recalls", card, len(evs), needNew)
		}
		if evs[0].State != "new" || evs[0].Owed != needNew {
			t.Errorf("%s first ask: state %q owed %d, want new/%d", card, evs[0].State, evs[0].Owed, needNew)
		}
		for _, ev := range evs[1:] {
			if ev.State != "learning" {
				t.Errorf("%s later ask: state %q, want learning", card, ev.State)
			}
		}
		last := evs[len(evs)-1]
		if last.Sched != "advance" || last.Owed != 1 {
			t.Errorf("%s completing answer: sched %q owed %d, want advance/1", card, last.Sched, last.Owed)
		}
	}
}

// TestReviewLogLapse: a miss logs correct=false, and the completing answer
// of a lapsed card records sched=lapse.
func TestReviewLogLapse(t *testing.T) {
	dir := t.TempDir()
	store, err := progress.NewStoreIn(dir, "d.deck")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	store.RecordCorrect("ans0")
	store.RecordCorrect("ans1")
	store.Schedule("ans0", false)
	store.Schedule("ans1", false)
	e := NewEngine(confusableDeck(2), nil, store)

	missed := e.Current().ID
	answerCurrent(e, false)
	e.Next()
	for i := 0; i < 20 && e.State() != Done; i++ {
		answerCurrent(e, true)
		e.Next()
	}

	events := readLog(t, dir)
	var lapseCompleted bool
	for _, ev := range events {
		if ev.Card == missed && ev.Correct && ev.Sched == "lapse" {
			lapseCompleted = true
		}
	}
	if !lapseCompleted {
		t.Errorf("no lapse-completing event for %s in %+v", missed, events)
	}
}
