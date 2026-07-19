package quiz

import (
	"testing"

	"study/deck"
)

// chinaCard builds a quota-5 set card of China's neighbors (abridged list).
func chinaDeck(quota int) *deck.Deck {
	d := testDeck(1)
	c := &d.Cards[0]
	c.Mode = deck.ModeType
	c.SetItems = []deck.SetItem{
		{Text: "Russia"},
		{Text: "Mongolia"},
		{Text: "Kazakhstan"},
		{Text: "India"},
		{Text: "Pakistan"},
		{Text: "Nepal"},
		{Text: "Myanmar", Accept: []string{"Burma"}},
	}
	c.Quota = quota
	c.AnswerText = "Russia, Mongolia, Kazakhstan, India, Pakistan, Nepal, Myanmar"
	return d
}

// TestSetQuotaCleanRun: five distinct hits complete the card correct; the
// scheduler sees one correct answer.
func TestSetQuotaCleanRun(t *testing.T) {
	e := NewEngine(chinaDeck(5), nil, nil)
	entries := []string{"russia", "mongolia", "kazakhstan", "india", "burma"}
	for i, s := range entries {
		out := e.AnswerSetEntry(s)
		if out == nil || out.Verdict != SetHit {
			t.Fatalf("entry %q: verdict = %+v, want hit", s, out)
		}
		if i < len(entries)-1 && out.Result != nil {
			t.Fatalf("entry %q completed early", s)
		}
		if i == len(entries)-1 {
			if out.Result == nil {
				t.Fatal("fifth hit did not complete the card")
			}
			if !out.Result.Correct {
				t.Fatal("clean quota run graded wrong")
			}
		}
	}
	if e.State() != ShowResult {
		t.Fatalf("State = %v, want ShowResult", e.State())
	}
	if e.TotalCorrect != 1 || e.TotalSeen != 1 {
		t.Errorf("TotalCorrect/Seen = %d/%d, want 1/1 — one card, one verdict", e.TotalCorrect, e.TotalSeen)
	}
}

// TestSetWrongGuessTaintsVerdict: reaching the quota after a wrong guess
// still completes, but the card grades wrong.
func TestSetWrongGuessTaintsVerdict(t *testing.T) {
	e := NewEngine(chinaDeck(2), nil, nil)
	if out := e.AnswerSetEntry("japan"); out == nil || out.Verdict != SetMiss {
		t.Fatalf("japan: verdict = %+v, want miss", out)
	}
	e.AnswerSetEntry("russia")
	out := e.AnswerSetEntry("nepal")
	if out.Result == nil {
		t.Fatal("quota reached but no result")
	}
	if out.Result.Correct {
		t.Fatal("tainted run graded correct")
	}
}

// TestSetDuplicateAndClose: duplicates don't advance the count, near misses
// neither check off nor penalize.
func TestSetDuplicateAndClose(t *testing.T) {
	e := NewEngine(chinaDeck(2), nil, nil)
	e.AnswerSetEntry("russia")
	if out := e.AnswerSetEntry("Russia"); out.Verdict != SetDuplicate {
		t.Fatalf("repeat: verdict = %v, want duplicate", out.Verdict)
	}
	if out := e.AnswerSetEntry("mongolla"); out.Verdict != SetClose {
		t.Fatalf("mongolla: verdict = %v, want close", out.Verdict)
	}
	if e.SetNamedCount() != 1 {
		t.Fatalf("named = %d, want 1", e.SetNamedCount())
	}
	// The near miss cost nothing: completing cleanly still grades correct.
	out := e.AnswerSetEntry("mongolia")
	if out.Result == nil || !out.Result.Correct {
		t.Fatal("run with only a near-miss entry graded wrong")
	}
}

// TestSetGiveUp: giving up is a miss, and with nothing named it reads as a
// declined answer rather than a wrong one.
func TestSetGiveUp(t *testing.T) {
	e := NewEngine(chinaDeck(0), nil, nil)
	res := e.AnswerSetGiveUp()
	if res == nil || res.Correct {
		t.Fatal("give-up graded correct")
	}
	if !res.NoIdea {
		t.Error("give-up with nothing named should read as declined")
	}
}

// TestSetFullEnumeration: quota 0 requires every item.
func TestSetFullEnumeration(t *testing.T) {
	e := NewEngine(chinaDeck(0), nil, nil)
	all := []string{"russia", "mongolia", "kazakhstan", "india", "pakistan", "nepal", "myanmar"}
	for i, s := range all {
		out := e.AnswerSetEntry(s)
		if (out.Result != nil) != (i == len(all)-1) {
			t.Fatalf("completion at entry %d (%q)", i, s)
		}
	}
	if e.State() != ShowResult {
		t.Fatal("full enumeration did not complete")
	}
}

// TestSetServeResetsProgress: a set card served again (sequential drill)
// starts from zero named.
func TestSetServeResetsProgress(t *testing.T) {
	d := chinaDeck(2)
	d.Order = deck.OrderSequential
	e := NewEngine(d, nil, nil)
	e.AnswerSetEntry("japan") // taint
	e.AnswerSetEntry("russia")
	out := e.AnswerSetEntry("nepal")
	if out.Result == nil || out.Result.Correct {
		t.Fatal("setup: tainted completion expected")
	}
	e.Next() // sequential: the missed card repeats
	if e.SetNamedCount() != 0 {
		t.Fatalf("named after re-serve = %d, want 0", e.SetNamedCount())
	}
}

// TestSetIgnoresSingleAnswerPath: AnswerTyped has no meaning on a set card.
func TestSetIgnoresSingleAnswerPath(t *testing.T) {
	e := NewEngine(chinaDeck(2), nil, nil)
	if res := e.AnswerTyped("russia"); res != nil {
		t.Fatal("AnswerTyped answered a set card")
	}
	if e.State() != ShowQuestion {
		t.Fatal("state moved")
	}
}

// TestSetLogTranscript: counted entries log in typed order — hits as
// canonical text, wrong guesses as typed — while duplicates and near
// spellings stay out.
func TestSetLogTranscript(t *testing.T) {
	e := NewEngine(chinaDeck(3), nil, nil)
	e.AnswerSetEntry("burma")    // hit, logs as Myanmar
	e.AnswerSetEntry("Japan")    // miss, logs as typed
	e.AnswerSetEntry("burma")    // duplicate: no log
	e.AnswerSetEntry("mongolla") // close: no log
	log := e.SetLog()
	want := []SetLogEntry{{"Myanmar", true}, {"Japan", false}}
	if len(log) != len(want) {
		t.Fatalf("log has %d entries, want %d: %v", len(log), len(want), log)
	}
	for i, w := range want {
		if log[i] != w {
			t.Errorf("log[%d] = %v, want %v", i, log[i], w)
		}
	}
}
