# study

A flashcard quiz tool inspired by suckless sent. Decks are plain text files you write in any editor; sessions run in a minimal X11 window. The default schedule follows evidence-based spaced-repetition: new cards are learned to a three-recall criterion, reviews come due on expanding intervals, and a session ends when everything due is done.

![study showing a mahjong tile card](screenshot.png)

## Getting started

Requires Go. Installs to `~/.local/bin`; override with
`PREFIX=/usr/local sudo make clean install`.

```bash
make clean install
```

Save this as `example.deck` — a type-in card and a multiple choice card
(`~` lines are the wrong options, and their presence is what makes the card
multiple choice):

```
2 + 2
---
4

What is 2 + 2?
---
~ 3
4
~ 5
~ 6
```

Run it:

```bash
study example.deck
```

For more, [examples/basic.deck](examples/basic.deck) is a small beginner deck,
and the [language packs](https://github.com/wickedjargon/study-language-packages)
are full-size decks with audio, native script, and pack directories.

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

## Deck format

Plain text. Blank lines separate cards; `---` or `===` (any length ≥ 3)
separates question from answer.

### Accepted answers

`=` after the answer adds an extra accepted answer (type mode). Matching is
lenient by default — case, accents, punctuation, and extra spaces are
forgiven (`salam` matches `salâm`):

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
| `# answer-mode:` | `choice`, `type` (a card with `~` distractors is `choice` automatically) | `type` |
| `# choice-count:` | any integer ≥ 2 | `4` |
| `# answer-case:` | `sensitive`, `insensitive` | `insensitive` |
| `# time-limit:` | seconds (e.g. `20`, `20s`), or `none`; expiry counts as wrong | `none` |
| `# order:` | see [Card order](#card-order) | `adaptive` |
| `# preview-new:` | `on`, `off` | `off` |
| `# new-per-session:` | integer ≥ 0, or `all` | `20` |
| `# font-size:` | 8–48, or `small`/`medium`/`large`/`x-large` | `14` |
| `# audio-speed:` | `0.25`–`4.0` (e.g. `0.75`, `1.5x`) | `1.0` |

## Controls

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
