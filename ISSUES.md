# study — known issues & missing features

An analysis of the codebase as of 2026-06-01. Grouped by severity. Line
references are approximate and may drift as the code changes.

## Bugs & dead code

### 1. The "Session Complete" summary screen is unreachable
`quiz.handleCorrect()` always calls `requeueCard()` for non-retry cards, which
*always* re-appends the card to the main queue (`quiz/engine.go:342-367`). So
`main` never drains, `advance()` never reaches the `Done` branch
(`quiz/engine.go:330`), and `renderSummary()` (`gui/app.go:632`) is dead code.
The only exit is `Escape`, which calls `quit()` and tears down the X loop
*without* rendering the summary. There is no way to end a session and see the
in-app stats.

**Fix direction:** either let the loop end naturally, or add an "end session"
key that transitions the engine to `Done`.

*Partially mitigated:* `study --stats <deck>` now prints the same numbers from
the command line, but the live summary screen is still unreachable.

### 2. Confidence-based prioritization is discarded in the default mode
`main.go` calls `store.PrioritizeCards(...)` (weak-cards-first) and *then*
`NewEngine(d, shuffle=true, ...)` re-shuffles the slice (`quiz/engine.go:85`).
Because shuffle is the default, the prioritization only takes effect under
`--sequential`. The confidence/streak machinery in `progress/store.go` is
mostly wasted in normal use.

### 3. Answer-side media is parsed but never shown
`deck/parser.go` parses `@img`/`@audio` on the answer side into `Card.Answer`,
but the GUI only ever loads question media (`loadQuestionImage`,
`playQuestionAudio` at `gui/app.go:423,439`). `renderResult` never displays
answer images/audio, so e.g. a pronunciation clip on the answer side is
silently dropped — a real loss for language decks.

### 4. README/`main.go` claim images need `sxiv`/`feh`, but they don't
Images are rendered *in-window* via `loadImage`/`renderImage`. The external
image-viewer path in `media/viewer.go` (`showImage` via sxiv/feh) is never
invoked for questions, and `CheckRequiredViewers` is never called from
`main.go`. The README install note and the `main.go` header comment
("Requires: sxiv or feh for image decks") are misleading — only audio needs an
external tool.

## Missing features

### 5. Type-in mode can't accept non-ASCII — breaks the headline use case — MOSTLY DONE
~~`handleTypeKey` only accepts `key[0] >= 32 && key[0] <= 126`.~~ The old
filter rejected every multi-byte string, and `keybind.LookupString` doesn't
return the character for non-ASCII keys anyway (it returns the keysym *name*,
e.g. `"aacute"`, or `""`). Type input now resolves the rune from the keysym
directly (`keysymToRune` in `gui/app.go`): ASCII and full Latin-1 (é, ñ, ü, ç,
…) work, as does anything delivered as an X11 *Unicode keysym* — which is how
`xdotool type` and many non-Latin layouts send characters, so CJK works on
those paths.

*Remaining limitation:* legacy national keysyms (`Cyrillic_*`, `Greek_*`, …
from a hardware layout that doesn't emit Unicode keysyms) are not mapped, and
interactive IME composition (fcitx/ibus over XIM) is never received by this
xgbutil-based X stack at all — so live romaji→kanji conversion still won't
work. Closing those needs a keysym→UCS table and/or XIM/IBus client support.
As a practical workaround, composed text can now be pasted in (see #11).

### 6. No text wrapping
`drawTextCentered` clamps the left edge to `padding` but does nothing about the
right (`gui/app.go:721`). Long questions, answers, or choice options run off
the right edge of the window. There is no word-wrap anywhere.

### 7. No audio replay key
Audio fires once on card load and that's it. For pronunciation practice, a key
to replay the clip is essential.

### 8. No self-graded / reveal mode
Only `choice` and `type` modes exist. The classic flashcard flow (show
question → reveal answer → rate yourself easy/hard) isn't supported, which is
the natural mode for production recall in language learning.

### 9. No stats-only view — DONE
~~No way to inspect progress/confidence without starting a quiz.~~
Implemented as `study --stats <deck>`, which prints a progress summary
(cards studied, mastered, all-time accuracy, weakest cards) and exits.

### 10. No pause
The countdown can't be paused; stepping away counts the card as wrong.

### 11. No clipboard paste in type mode — DONE
~~Type mode only read individual key presses, so there was no way to paste an
answer, and `Ctrl+V` actually inserted a literal `v` (LookupString ignores the
Control modifier).~~ Type mode now supports paste: `Ctrl+V` pastes the
`CLIPBOARD` selection and middle-click pastes the `PRIMARY` selection (only
printable runes are kept, so multi-line pastes collapse into the field), and
non-paste `Ctrl` combos are swallowed instead of leaking characters. This is
also the practical workaround for the IME gap in #5: compose CJK elsewhere,
then paste it in. *Note:* the X INCR protocol (very large selections) is not
handled — fine for short answers.

## Smaller notes

- **Progress keyed by absolute file path** (`deckHash` in `progress/store.go:231`)
  — moving or renaming a deck silently orphans all its progress.
- **`--reset` has no confirmation** and wipes progress immediately.
- **X11 only** (xgb/xgbutil) — no Wayland-native path, though it works under
  XWayland.
- **`# mode:` / `# case:` headers** are only read from the leading contiguous
  comment block; this constraint is undocumented.

## Suggested priority

1. ~~**#5** non-ASCII typing~~ — mostly done (Latin-1 + Unicode keysyms);
   legacy national keysyms and interactive IME remain.
2. **#1** unreachable summary / no session end.
3. **#2** prioritization defeated by shuffle — quietly undermines the
   spaced-repetition selling point.
