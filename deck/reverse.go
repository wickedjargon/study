package deck

import (
	"strings"
	"unicode"
)

// Reversed returns a copy of the deck flipped for production practice.
//
// A normal ("forward") card shows the target-language prompt and audio and asks
// the user to type the English. A reversed card does the opposite: it prompts
// with the English and asks the user to produce the target language, revealing
// the native script and playing the audio only after they answer. The transform
// is purely structural — no re-authoring is required:
//
//	forward                        reversed
//	  Question: @audio, script,      Question: english             (prompt)
//	            romanization         Answer:   @audio, script,      (reveal)
//	  Answer:   english                        romanization
//
// The last text line of the original prompt (the romanization, or the word
// itself for a Latin-script pack) becomes the canonical answer to type; any
// earlier line — the native script — is accepted too, so a user who can type the
// script isn't marked wrong, as are the card's question-side "=" alternatives
// (variant wordings of the prompt, authored for exactly this direction). Matching stays lenient (accent/case/punctuation-
// insensitive) in the engine, so "salam" is accepted for "salâm" and "ni hao"
// for "nǐ hǎo".
//
// Card IDs are namespaced with an "r:" prefix so a card's forward and reverse
// recall — genuinely different skills — accumulate progress independently within
// the same per-deck store. Producing the target language is inherently active
// recall, so every reversed card is forced to type-in (multiple choice, which
// can't be meaningfully reversed, is dropped).
func (d *Deck) Reversed() *Deck {
	rev := *d // shallow copy: scalar settings (case, time, order, speed, …) carry over
	rev.Mode = ModeType
	rev.Cards = make([]Card, 0, len(d.Cards))
	for i := range d.Cards {
		if c, ok := reverseCard(&d.Cards[i]); ok {
			rev.Cards = append(rev.Cards, c)
		}
	}
	return &rev
}

// reverseCard flips a single card. It returns ok=false for a card that can't
// be meaningfully reversed:
//
//   - a card with no target-language text to produce (a pure image/audio
//     prompt), which would leave the user nothing to type;
//   - a cloze card, whose "question" is the blanked-out sentence — reversing it
//     would prompt with the deleted word and demand the ____-riddled sentence;
//   - a card whose answer-to-type has no Latin letters or digits (e.g. a
//     single-line script drill like "あ → a"): this X stack receives no IME
//     input, so native script can only be entered by pasting, and a whole deck
//     of paste-only cards isn't a usable session.
func reverseCard(c *Card) (Card, bool) {
	if c.Cloze {
		return Card{}, false
	}
	// A set card's answer is an enumeration; prompting with the list and
	// demanding the question back makes no card.
	if c.IsSet() {
		return Card{}, false
	}

	// The original prompt's text lines are the native script and its
	// romanization; its non-text media (audio, image) ride along to the reveal.
	var textLines []string
	for _, m := range c.Question {
		if m.Type == Text && strings.TrimSpace(m.Content) != "" {
			textLines = append(textLines, m.Content)
		}
	}
	if len(textLines) == 0 {
		return Card{}, false
	}

	// Last line = what the learner types (romanization, or the Latin word).
	// Earlier lines = the native script, accepted so typing the script counts.
	primary := textLines[len(textLines)-1]
	if !typeable(primary) {
		return Card{}, false
	}
	var accept []string
	if len(textLines) > 1 {
		accept = append(accept, textLines[:len(textLines)-1]...)
	}
	// Question-side "=" lines are alternative wordings of the target-language
	// prompt (e.g. a tú-form variant); they exist precisely for this direction.
	accept = append(accept, c.QuestionAccept...)

	// English prompt = the forward card's canonical answer. The forward "="
	// alternatives were English synonyms — useful only when typing English — so
	// they don't carry over.
	prompt := []Media{{Type: Text, Content: c.AnswerText}}

	// Reveal = the original prompt verbatim (audio, script, romanization, image),
	// in authored order, so the result screen can show and speak it.
	reveal := make([]Media, len(c.Question))
	copy(reveal, c.Question)

	rc := Card{
		ID:         "r:" + c.ID,
		Question:   prompt,
		Answer:     reveal,
		AnswerText: primary,
		Accept:     accept,
		// The note explains the pairing, not one side — it rides along
		// unchanged in either direction.
		Note:      c.Note,
		Mode:      ModeType,
		TimeLimit: c.TimeLimit,
	}
	for _, legacy := range c.LegacyIDs {
		rc.LegacyIDs = append(rc.LegacyIDs, "r:"+legacy)
	}
	return rc, true
}

// typeable reports whether s can be produced on a plain keyboard: it contains
// at least one Latin letter or digit. Text that is purely non-Latin script
// (kana, hanzi, Arabic script, …) needs an IME, which this GUI never receives.
func typeable(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Latin, r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
