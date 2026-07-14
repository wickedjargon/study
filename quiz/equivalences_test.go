package quiz

import "testing"

func TestEquivalences(t *testing.T) {
	yes := [][2]string{
		// Card answer, typed answer.
		// Digit–letter splitting.
		{"5 m", "5m"},
		{"30 km", "30km"},
		// Numbers as words, both directions.
		{"2 seconds", "two seconds"},
		{"four", "4"},
		{"4", "four"},
		{"fifty", "50"},
		// Ordinals, both directions.
		{"first", "1st"},
		{"3rd", "third"},
		{"20th", "twentieth"},
		// Unit abbreviations expand (both spellings, both numbers).
		{"5 metres", "5 m"},
		{"5 metres", "5m"},
		{"5 miles", "5 m"},
		{"2 seconds", "2 s"},
		{"2 seconds", "2s"},
		{"2 seconds", "2 sec"},
		{"50 kilometers per hour", "50 km/h"},
		{"50 kilometers per hour", "50 kph"},
		{"6 feet", "6 ft"},
		// Compositions.
		{"five metres", "5 m"},
		{"two seconds", "2s"},
		// Symbols.
		{"rock and roll", "rock & roll"},
		{"rock & roll", "rock and roll"},
		{"rock & roll", "rock roll"}, // symbols stay droppable
		{"50 percent", "50 %"},
		{"90 degrees", "90°"},
		{"2 plus 2", "2 + 2"},
		// Word pairs.
		{"okay", "ok"},
		{"ok", "okay"},
		{"brazil versus argentina", "brazil vs argentina"},
	}
	for _, c := range yes {
		if !acceptsCase(t, c[0], nil, c[1]) {
			t.Errorf("card %q should accept %q", c[0], c[1])
		}
	}

	no := [][2]string{
		// Full unit words never turn into each other via the abbreviation.
		{"5 metres", "5 miles"},
		{"5 miles", "5 metres"},
		// "in" is a preposition, not inches.
		{"5 inches", "5 in"},
		// Unrelated numbers and words stay apart.
		{"4", "5"},
		{"four", "for"},
		{"1st", "21st"},
	}
	for _, c := range no {
		if acceptsCase(t, c[0], nil, c[1]) {
			t.Errorf("card %q should NOT accept %q", c[0], c[1])
		}
	}
}

func TestSpellings(t *testing.T) {
	yes := [][2]string{
		{"5 metres", "5 meters"},
		{"5 meters", "5 metres"},
		{"5 metres", "5 m"}, // unit expansion still meets the folded spelling
		{"colour", "color"},
		{"gray", "grey"},
		{"recognise", "recognize"},
		{"recognized", "recognised"},
		{"50 kilometres per hour", "50 km/h"},
		{"theatre", "theater"},
	}
	for _, c := range yes {
		if !acceptsCase(t, c[0], nil, c[1]) {
			t.Errorf("card %q should accept %q", c[0], c[1])
		}
	}

	no := [][2]string{
		{"four", "for"}, // no suffix rules: real words stay themselves
		{"hour", "hor"},
		{"tour", "tor"},
	}
	for _, c := range no {
		if acceptsCase(t, c[0], nil, c[1]) {
			t.Errorf("card %q should NOT accept %q", c[0], c[1])
		}
	}
}
