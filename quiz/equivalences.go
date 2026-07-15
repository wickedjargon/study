package quiz

import "strings"

// Beyond contractions, lenient matching folds in other true-synonym
// notations, under the same set-based rules (see contractions.go): every
// token contributes its stripped original plus expansions, and only short
// forms expand — "5 m" reads as metres or miles, but "5 miles" never turns
// into "5 metres" via the shared abbreviation.

// numberWords maps single-word numbers to digits; the inverse (digits to
// words) is derived below. Only numbers a single word can say are mapped —
// "twenty one" spans tokens and stays out.
var numberWords = map[string]string{
	"zero": "0", "one": "1", "two": "2", "three": "3", "four": "4",
	"five": "5", "six": "6", "seven": "7", "eight": "8", "nine": "9",
	"ten": "10", "eleven": "11", "twelve": "12", "thirteen": "13",
	"fourteen": "14", "fifteen": "15", "sixteen": "16", "seventeen": "17",
	"eighteen": "18", "nineteen": "19", "twenty": "20", "thirty": "30",
	"forty": "40", "fifty": "50", "sixty": "60", "seventy": "70",
	"eighty": "80", "ninety": "90", "hundred": "100", "thousand": "1000",
}

var wordNumbers = func() map[string]string {
	m := make(map[string]string, len(numberWords))
	for w, d := range numberWords {
		m[d] = w
	}
	return m
}()

// ordinalWords maps a digit ordinal ("1st", "22nd" → by its number part) to
// the word. Tokens keep their ordinal suffix through splitting so these can
// apply; the word side needs no table — the digit side's readings include
// the word, and both sides of a comparison expand.
var ordinalWords = map[string]string{
	"1": "first", "2": "second", "3": "third", "4": "fourth", "5": "fifth",
	"6": "sixth", "7": "seventh", "8": "eighth", "9": "ninth", "10": "tenth",
	"11": "eleventh", "12": "twelfth", "13": "thirteenth", "14": "fourteenth",
	"15": "fifteenth", "16": "sixteenth", "17": "seventeenth",
	"18": "eighteenth", "19": "nineteenth", "20": "twentieth",
	"30": "thirtieth", "40": "fortieth", "50": "fiftieth", "60": "sixtieth",
	"70": "seventieth", "80": "eightieth", "90": "ninetieth", "100": "hundredth",
}

// unitReadings expands unit abbreviations, both spellings and both numbers,
// since the deck may use any. Deliberately excluded: "in" (a preposition
// before it is ever inches), "a", "t", "no" — the contraction rule again: a
// token that is a real word must not expand.
var unitReadings = map[string][]string{
	"m":   {"metre", "metres", "meter", "meters", "mile", "miles"},
	"km":  {"kilometre", "kilometres", "kilometer", "kilometers"},
	"cm":  {"centimetre", "centimetres", "centimeter", "centimeters"},
	"mm":  {"millimetre", "millimetres", "millimeter", "millimeters"},
	"kg":  {"kilogram", "kilograms"},
	"g":   {"gram", "grams"},
	"s":   {"second", "seconds"},
	"sec": {"second", "seconds"},
	"min": {"minute", "minutes"},
	"h":   {"hour", "hours"},
	"hr":  {"hour", "hours"},
	"mi":  {"mile", "miles"},
	"ft":  {"foot", "feet"},
	"lb":  {"pound", "pounds"},
	"lbs": {"pound", "pounds"},
	"oz":  {"ounce", "ounces"},
	"yd":  {"yard", "yards"},
	"l":   {"litre", "litres", "liter", "liters"},
	"ml":  {"millilitre", "millilitres", "milliliter", "milliliters"},
	"kmh": {"kilometres per hour", "kilometers per hour"},
	"kph": {"kilometres per hour", "kilometers per hour"},
	"mph": {"miles per hour"},
}

// wordPairs are small symmetric synonyms.
var wordPairs = map[string][]string{
	"ok": {"okay"}, "okay": {"ok"},
	"vs": {"versus"}, "versus": {"vs"},
	"etc": {"et cetera"},
}

// droppableWords read as themselves or nothing, like droppable symbols:
// "3 of dots" matches "3 dots" without the deck authoring an "=" variant.
var droppableWords = map[string]bool{
	"of": true,
}

// pluralReadings adds an English token's singular: pluralization never
// decides correctness, so "3 dot" matches "3 Dots" and "dogs" matches "dog"
// (both sides expand, so one stripping direction covers both). The guards
// keep real words whole: short tokens and -ss/-us/-is endings ("glass",
// "plus", "this") never strip.
func pluralReadings(tok string) []string {
	var out []string
	n := len(tok)
	switch {
	case n >= 5 && strings.HasSuffix(tok, "ies"):
		out = append(out, tok[:n-3]+"y") // flies → fly
	case n >= 5 && strings.HasSuffix(tok, "es") && esPlural(tok):
		out = append(out, tok[:n-2]) // boxes → box, dishes → dish
	}
	if n >= 4 && strings.HasSuffix(tok, "s") &&
		!strings.HasSuffix(tok, "ss") && !strings.HasSuffix(tok, "us") && !strings.HasSuffix(tok, "is") {
		out = append(out, tok[:n-1]) // dots → dot
	}
	return out
}

// esPlural reports whether a token ending in -es plausibly pluralizes a stem
// that ends in a sibilant (box/boxes, bus/buses, dish/dishes, watch/watches).
func esPlural(tok string) bool {
	switch tok[len(tok)-3] {
	case 'x', 'z', 's':
		return true
	case 'h':
		c := tok[len(tok)-4]
		return c == 'c' || c == 's'
	}
	return false
}

// symbolReadings gives symbol tokens their word forms. The empty reading
// means "droppable": plain normalization discards symbols entirely, so
// "rock & roll" must keep matching "rock roll" as well as gain
// "rock and roll".
var symbolReadings = map[string][]string{
	"&": {"", "and"},
	"%": {"", "percent", "per cent"},
	"°": {"", "degree", "degrees"},
	"+": {"", "plus"},
	// "km/h" → "km per h" → "kilometres per hour", token by token.
	"/": {"", "per"},
}

// ordinalSuffixes keep a digit token's trailing letters attached during
// digit–letter splitting, so "1st" stays one token and can read as "first".
var ordinalSuffixes = []string{"st", "nd", "rd", "th"}

// splitDigitBoundaries inserts spaces where digits meet letters ("5m" →
// "5 m", "covid19" → "covid 19"), except before an ordinal suffix.
func splitDigitBoundaries(tok string) []string {
	var out []string
	runs := splitRuns(tok)
	for i := 0; i < len(runs); i++ {
		// "1st" stays whole only when the suffix ends the token ("1step"
		// has no ordinal reading and splits normally).
		last := i+1 == len(runs)-1
		if last && isDigits(runs[i]) && isOrdinalSuffix(runs[i+1]) {
			out = append(out, runs[i]+runs[i+1])
			i++
			continue
		}
		out = append(out, runs[i])
	}
	return out
}

// splitRuns breaks a token into maximal digit and non-digit runs.
func splitRuns(tok string) []string {
	var runs []string
	start := 0
	for i := 1; i <= len(tok); i++ {
		if i == len(tok) || isDigitByte(tok[i]) != isDigitByte(tok[start]) {
			runs = append(runs, tok[start:i])
			start = i
		}
	}
	return runs
}

func isDigitByte(b byte) bool { return b >= '0' && b <= '9' }

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigitByte(s[i]) {
			return false
		}
	}
	return len(s) > 0
}

func isOrdinalSuffix(s string) bool {
	for _, suf := range ordinalSuffixes {
		if s == suf {
			return true
		}
	}
	return false
}

// equivalenceReadings returns the non-contraction readings of one token:
// numbers as words and vice versa, ordinals, unit abbreviations, and the
// small synonym pairs. Additive only — the caller keeps the original.
func equivalenceReadings(tok string) []string {
	var out []string
	if d, ok := numberWords[tok]; ok {
		out = append(out, d)
	}
	if w, ok := wordNumbers[tok]; ok {
		out = append(out, w)
	}
	if n, found := strings.CutSuffix(tok, "st"); found && isDigits(n) {
		if w, ok := ordinalWords[n]; ok {
			out = append(out, w, n+" st")
		}
	} else if n, found := strings.CutSuffix(tok, "nd"); found && isDigits(n) {
		if w, ok := ordinalWords[n]; ok {
			out = append(out, w, n+" nd")
		}
	} else if n, found := strings.CutSuffix(tok, "rd"); found && isDigits(n) {
		if w, ok := ordinalWords[n]; ok {
			out = append(out, w, n+" rd")
		}
	} else if n, found := strings.CutSuffix(tok, "th"); found && isDigits(n) {
		if w, ok := ordinalWords[n]; ok {
			out = append(out, w, n+" th")
		}
	}
	out = append(out, unitReadings[tok]...)
	out = append(out, wordPairs[tok]...)
	out = append(out, pluralReadings(tok)...)
	return out
}
