# study

A quiz tool inspired by suckless sent. Plain-text deck files, X11 GUI, spaced repetition.

## Install

Requires Go. Installs to `~/.local/bin`; override with
`PREFIX=/usr/local sudo make clean install`.

```bash
make clean install
```

## Features

- **Plain-text decks** — cards are just lines in a file; any editor is the deck editor
- **Active recall first** — type-in answers with lenient matching (case, accents, punctuation forgiven); multiple choice is opt-in
- **Evidence-based scheduling** — spaced retrieval with a 3-recall learning criterion and expanding review intervals (1 → 120 days); sessions serve only what's due and complete themselves (successive relearning: Rawson & Dunlosky 2011; spaced retrieval: Karpicke & Bauernschmidt 2011)
- **Reverse mode** — flip a language deck into production practice: see the English, produce the target language; each direction tracks its own progress
- **Cram, rote, and reading modes** — `weak-only`, `sequential`, `flip-through`
- **Cloze deletions** — `{{...}}` fill-in-the-blank cards
- **Images and audio** — with on-the-fly playback speed, pitch preserved
- **Right-to-left scripts** — Arabic/Persian/Urdu shaped and laid out correctly, ZWNJ included
- **Packs** — a directory of `.deck` files studies as one merged session
- **First-viewing preview** — optionally show a brand-new card's answer once before quizzing it
- **Durable progress** — saved after every answer, keyed to question text (renaming media keeps a card's history)
- **Time limits, per-card overrides, auto dark/light theme**

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

- A directory is a **pack**: every `*.deck` inside merges into one session.
- A flag overrides the deck-header setting of the same name for that session
  (`answer-mode`, `answer-case`, and `choice-count` are file-only).
- Progress lives in `~/.local/share/study/`.

### Card order

Set with the `--order` flag or the `# order:` deck header:

| Mode | Behavior |
|------|----------|
| `adaptive` | **Default — "what's due?"** Reviews whose date arrived (most overdue first) plus a batch of new cards. A new card needs 3 correct recalls, a review 1; repetitions are spaced, never back-to-back. A clean session moves a card up the review ladder (1, 3, 7, 14, 30, 60, 120 days); a miss resets it. Completes when every card is done; nothing due → says so and exits. |
| `sequential` | **"In order."** Deck order, wrapping forever; misses get the immediate-repeat drill. For material where the sequence is the content — verse, digits, procedures. |
| `flip-through` | **"Just show me."** Answers visible, enter advances, wraps at the end. Nothing recorded. |
| `weak-only` | **"What am I bad at?"** Cram mode: only weak or never-studied cards, ignoring review dates. |

### Controls

| Key | Action |
|-----|--------|
| `1`–`9` | Select answer (choice mode) |
| Type + `Enter` | Submit answer (type mode) |
| `Backspace` | Delete character (type mode) |
| `Ctrl`+`V` / middle-click | Paste clipboard / primary selection (type mode) |
| `Enter` / `Space` | Continue after result / preview |
| `Ctrl`+`R` | Replay audio (in reverse mode, the reveal's clip on the result screen) |
| `Ctrl`+`,` / `Ctrl`+`.` | Slow down / speed up audio and replay (0.25 steps, 0.25–4x; needs `mpv`) |
| `Ctrl`+`/` | Reset audio speed |
| `Ctrl`+`=` / `Ctrl`+`-` / `Ctrl`+`0` | Grow / shrink / reset font size |
| `Escape` | End session (summary screen; `Escape` again exits) |

## Deck format

Plain text. Blank lines separate cards; `---` or `===` (any length ≥ 3)
separates question from answer. See [examples/basic.deck](examples/basic.deck)
for a beginner deck, and the
[language packs](https://github.com/wickedjargon/study-language-packages) for
full-size decks with audio, script, and pack directories.

### Minimal type-in example

```
2 + 2
---
4
```

### Minimal multiple choice example

```
# answer-mode: choice
What is 2 + 2?
---
~ 3
4
~ 5
~ 6
```

### Accepted answers

`=` after the answer adds an extra accepted answer (type mode):

```
bonjour
---
hello
= hi
```

### Alternative prompt wordings

`=` on the question side is an alternative wording of the prompt, accepted
when the prompt is what you type (`--reverse`); it's never displayed, and
adding one doesn't re-key the card:

```
¿Prefiere ventanilla o pasillo?
= prefieres ventanilla o pasillo
---
do you prefer window or aisle
```

### Media

`@img` and `@audio` ride on the side they're written on; paths are relative
to the deck file, audio plays automatically (needs `mpv` or `aplay`):

```
@img flags/japan.png
@audio audio/konnichiwa.mp3
How do you say "hello"?
---
こんにちは
```

### Cloze

A card with no separator and a `{{...}}` deletion blanks the braced text
(`____`) and makes it the answer; multiple deletions join in order:

```
The capital of France is {{Paris}}.
```

### Per-card overrides

`# answer-mode:`, `# choice-count:`, and `# time-limit:` inside a card block
apply to that card only (`# time-limit: none` exempts it):

```
# choice-count: 2
# time-limit: none
What is 1 + 1?
---
2
```

### Header directives

| Header | Values | Default |
|--------|--------|---------|
| `# answer-mode:` | `choice`, `type` | `type` |
| `# choice-count:` | any integer ≥ 2 | `4` |
| `# answer-case:` | `sensitive`, `insensitive` | `insensitive` |
| `# time-limit:` | seconds (e.g. `20`, `20s`), or `none`; expiry counts as wrong | `none` |
| `# order:` | see [Card order](#card-order) | `adaptive` |
| `# preview-new:` | `on`, `off` | `off` |
| `# new-per-session:` | integer ≥ 0, or `all` | `20` |
| `# font-size:` | 8–48, or `small`/`medium`/`large`/`x-large` | `14` |
| `# audio-speed:` | `0.25`–`4.0` (e.g. `0.75`, `1.5x`) | `1.0` |
