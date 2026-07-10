# study

A quiz tool inspired by suckless sent. Plain-text deck files, X11 GUI,
evidence-based spaced repetition.

## Install

Requires Go. Installs to `~/.local/bin`; override with
`PREFIX=/usr/local sudo make clean install`.

```bash
make clean install
```

## Usage

```
study [flags] <deck-file | pack-directory>
```

| Flag | Description |
|------|-------------|
| `--reverse` | Flip the deck: see the English, produce the target language |
| `--order <mode>` | Override the deck's card order for this session — see [Card order](#card-order) |
| `--time-limit <N\|none>` | Override the per-question time limit, uniformly for every card |
| `--preview-new` | Reveal a never-studied card's answer once before quizzing it |
| `--new-per-session <N\|all>` | How many never-studied cards enter an adaptive session (default 20) |
| `--font-size <N>` | Override the base font size (8–48, or `small`/`medium`/`large`/`x-large`) |
| `--audio-speed <X>` | Override audio playback speed (0.25–4.0) |
| `--stats` | Print progress summary (incl. what's due) and exit |
| `--forget` | Clear saved progress — the studied direction only (combine with `--reverse`) |
| `--help` | Show help |

- A directory is a **pack**: every `*.deck` inside (sorted by name) merges into
  one session. Settings come from the first file, duplicate cards are included
  once, and pack progress is saved separately from the individual decks'.
- A flag overrides the deck-header setting of the same name for that session.
  `answer-mode`, `answer-case`, and `choice-count` are deliberately file-only:
  overriding them per session could record misses for answers the deck
  considers right.

## Deck format

Plain text. Blank lines separate cards; `---` or `===` (any length ≥ 3)
separates question from answer.

```
# Farsi phrases
# answer-mode: type
# time-limit: 20

@img img/greeting.png
@audio audio/salam.mp3
سلام
salâm
---
hello
= hi
```

### Card syntax

| Line | Meaning |
|------|---------|
| `= text` (answer side) | Extra accepted answer (type mode) |
| `= text` (question side) | Alternative prompt wording — accepted in `--reverse`, never displayed, doesn't re-key the card |
| `~ text` | Custom wrong answer (choice-mode distractor) |
| `@img path` | Image, shown with the question (path relative to the deck file) |
| `@audio path` | Audio clip, plays automatically (needs `mpv` or `aplay`) |
| `{{...}}` and no separator | Cloze card: the braced text is blanked (`____`) and becomes the answer; multiple deletions join in order |
| `# answer-mode:` / `# choice-count:` / `# time-limit:` inside a card block | Per-card override (`# time-limit: none` exempts one card) |

- Type-mode matching is lenient by default: case, punctuation, accents, and
  extra spaces are ignored (`salam` matches `salâm`); `# answer-case:
  sensitive` requires exact matches.
- Choice mode fills missing distractors with other cards' answers.
- A missing media file is skipped with a warning; the card still runs.
- Arabic-script text (Persian, Arabic, Urdu, …) is shaped and laid out RTL,
  ZWNJ included — needs any Arabic-capable font (Noto Naskh/Sans Arabic,
  Vazirmatn); without one it falls back to unshaped rendering.

### Header directives

| Header | Values | Default |
|--------|--------|---------|
| `# answer-mode:` | `choice`, `type` | `type` |
| `# choice-count:` | any integer ≥ 2 | `4` |
| `# answer-case:` | `sensitive`, `insensitive` | `insensitive` |
| `# time-limit:` | seconds (e.g. `20`, `20s`), or `none` | `none` |
| `# order:` | see [Card order](#card-order) | `adaptive` |
| `# preview-new:` | `on`, `off` | `off` |
| `# new-per-session:` | integer ≥ 0, or `all` | `20` |
| `# font-size:` | 8–48, or `small`/`medium`/`large`/`x-large` | `14` |
| `# audio-speed:` | `0.25`–`4.0` (e.g. `0.75`, `1.5x`) | `1.0` |

## Card order

`# order:` selects what a session serves and how it schedules the cards:

| Mode | Behavior |
|------|----------|
| `adaptive` | **Default — "what's due?"** Cards due for review (most overdue first) plus a batch of new cards; each leaves once it meets its recall criterion; the session completes when all have. See [Scheduling](#scheduling). |
| `sequential` | **"In order."** Deck order, wrapping forever; misses get the immediate-repeat drill. For material where the sequence is the content — verse, digits, procedures. |
| `flip-through` | **"Just show me."** Answers visible, enter advances, wraps at the end. No quizzing, nothing recorded. |
| `weak-only` | **"What am I bad at?"** Cram mode: only weak or never-studied cards, ignoring review dates. Exits up front when there's nothing to cram. |

## Scheduling

The `adaptive` default implements what the learning research supports
(successive relearning: Rawson & Dunlosky 2011; spaced retrieval: Karpicke &
Bauernschmidt 2011):

- **Sessions contain what's due**: reviews whose date has arrived plus up to
  `# new-per-session:` new cards. Nothing due → `study` says so and exits.
- **Session criterion**: a new card needs **3 correct recalls**, a review
  card **1**; meeting it removes the card, and the session completes when
  every card has.
- **Spaced, never massed**: a missed card returns after a few other cards —
  not immediately — and owes at least 2 more spaced recalls.
- **Expanding intervals between sessions**: a clean session moves a card up
  the review ladder — 1, 3, 7, 14, 30, 60, 120 days; any miss sends it back
  to the bottom (due tomorrow).

`weak-only` reuses the in-session criterion scheduling with its own card pick;
`sequential` is deliberate rote drill (endless laps, immediate repeats).

## Reverse mode

`study --reverse deck.deck` flips a language deck for production practice:

- Prompts with the English; you type the target language. Native script and
  audio are held back until the answer is revealed.
- The expected answer is the last text line of the original prompt (the
  romanization); the native script and question-side `=` lines are accepted
  too.
- Always type-in, and progress is tracked separately from the forward
  direction — they're different skills.
- Skipped as unreversible: cloze cards, media-only prompts, answers with no
  Latin characters (no IME on this X stack). A deck with none errors out.

## First-viewing preview

With `# preview-new: on` (or `--preview-new`), a card you've never answered is
first shown with its answer visible — study it, press enter, then the same
card is quizzed for real. Happens exactly once per card per direction; any
recorded history skips it. Off by default: quizzing before study is a
worthwhile struggle (the pretesting effect), and review decks don't need it.

## Controls

| Key | Action |
|-----|--------|
| `1`–`9` | Select answer (choice mode) |
| Type + `Enter` | Submit answer (type mode) |
| `Backspace` | Delete character (type mode) |
| `Ctrl`+`V` / middle-click | Paste clipboard / primary selection (type mode) |
| `Enter` / `Space` | Continue after result / preview |
| `Ctrl`+`R` | Replay audio (in reverse mode, the reveal's clip on the result screen) |
| `Ctrl`+`,` / `Ctrl`+`.` | Slow down / speed up audio and replay (0.25 steps, 0.25–4x; needs `mpv`, pitch preserved) |
| `Ctrl`+`/` | Reset audio speed |
| `Ctrl`+`=` / `Ctrl`+`-` / `Ctrl`+`0` | Grow / shrink / reset font size |
| `Escape` | End session (summary screen; `Escape` again exits) |

## Notes

- Timed-out questions count as wrong.
- Progress lives in `~/.local/share/study/`, saved after every answer. Cards
  are keyed by a hash of their question **text**: renaming media keeps a
  card's history, editing the text re-keys it.
- Dark/light theme is auto-detected via gsettings or `~/.config/theme-mode`.
