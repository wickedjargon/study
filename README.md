# study

A quiz tool inspired by suckless sent. Plain-text deck files, X11 GUI, spaced repetition.

## Install

Requires Go.

```bash
make clean install
```

Installs to `~/.local/bin` by default. Override with `PREFIX=/usr/local sudo make clean install`.

## Usage

```
study [flags] <deck-file>
```

| Flag             | Description                              |
|------------------|------------------------------------------|
| `--stats`        | Print saved progress summary for the deck and exit |
| `--forget`       | Clear saved progress for this deck       |
| `--help`         | Show help                                |

All per-deck settings (choices, time, order, …) live in the deck file header — see below.

## Deck Format

A `.deck` file is plain text. Cards are separated by blank lines. Each card has a question and answer divided by `---` or `===`.

### Minimal card

```
What is 2 + 2?
---
4
```

### Deck header

Comments at the top of the file configure deck-wide settings:

```
# Math Quiz
# mode: choice
# choices: 4
# case: insensitive
# time: 20
# order: sequential
```

| Header           | Values                  | Default       |
|------------------|-------------------------|---------------|
| `# mode:`        | `choice`, `type`        | `type`        |
| `# choices:`     | any integer ≥ 2         | `4`           |
| `# case:`        | `sensitive`, `insensitive` | `sensitive` |
| `# time:`        | seconds (e.g. `20`, `20s`), or `none` | `none` |
| `# order:`       | `sequential`, `shuffled` | `shuffled`   |
| `# speed:`       | audio speed `0.25`–`4.0` (e.g. `0.75`, `1.5x`) | `1.0` |

## Features

### Type-in mode

The default. The user types the answer.

```
What is 10 - 3?
---
7
```

Type-in is active recall — you produce the answer rather than recognizing it —
so it's the default.

By default (`# case: insensitive`) matching is lenient, so a right answer isn't
marked wrong over trivia: case, surrounding/embedded punctuation, accents, and
extra spaces are ignored — `Hello!`, `hello`, and `HELLO` all match `hello`, and
`salam` matches `salâm`. Set `# case: sensitive` for exact, character-for-character
matching instead.

### Accepted alternatives

When more than one answer should count as correct, list extras with `=` lines:

```
hello
= hi
= hey
```

All of `hello`, `hi`, and `hey` are accepted. The first line stays the canonical
answer (it's what's shown on the result screen and used as the correct option in
choice mode). `=` accepted answers and `~` distractors can be mixed on one card.

### Fill-in-the-blank (cloze)

A card with **no separator** but a `{{...}}` deletion is a fill-in-the-blank
card. The braced text is blanked out in the question and becomes the answer:

```
The capital of France is {{Paris}}.
```

shows `The capital of France is ____.` and accepts `Paris`. Multiple deletions in
one card are all blanked, and their contents join (in order) to form the answer:

```
{{Romeo}} and {{Juliet}}
```

Cloze cards honour the card's mode — type-in by default, or multiple choice under
`# mode: choice` (distractors are drawn from other cards as usual) — and accept
`=` alternatives and `~` distractors like any other card.

### Multiple choice mode

The user picks from numbered options. Opt in with `# mode: choice`.

```
# mode: choice
# choices: 4

What is 3 + 5?
---
8
```

The quiz generates 4 options using answers from other cards in the deck as distractors.

### Custom distractors

Control exactly which wrong answers appear with `~` lines:

```
What is 6 × 7?
---
42
~ 36
~ 48
~ 54
```

The quiz shows these specific wrong answers instead of pulling from other cards.

### Per-card mode override

Mix choice and type-in cards in the same deck:

```
# mode: choice
What is 1 + 1?
---
2

# mode: type
What is 100 / 10?
---
10
```

The first card is multiple choice (deck default). The second card overrides to type-in.

### Per-card choice count

Override how many options appear for a specific card:

```
# choices: 3
What is 5 - 1?
---
4

# choices: 6
What is 9 + 9?
---
18
```

The first card shows 3 options (deck default). The second card shows 6.

### Time limits

Give every question a countdown with the deck-level `# time:` header (seconds).
When the timer runs out, the card is counted as wrong and queued for retry — just
like an incorrect answer. A live countdown is shown in the top-right corner.

```
# time: 15

What is the capital of France?
---
Paris
```

Any card can override the deck-wide limit with its own `# time:` line. Use a
number to set a different limit, or `none` (or `0`) to remove the limit for that
one card:

```
# time: 10

Quick: 2 + 2?
---
4

# time: 30
Take your time — explain photosynthesis in one word.
---
light

# time: none
No rush on this one.
---
ok
```

The first card uses the deck default (10s), the second allows 30s, and the third
has no limit.

### Card order

By default cards are shuffled each session so the deck's authored order can't
become a memorization crutch. Set `# order: sequential` in the header to present cards in deck order instead:

```
# order: sequential

First card
---
1

Second card
---
2
```

### Images

Show an image as part of the question with `@img`:

```
@img flags/france.png
What country is this?
---
France
```

Paths are relative to the directory containing the `.deck` file.

### Right-to-left scripts (Arabic, Persian, …)

Arabic-script text is rendered with full contextual shaping (letters join into
their isolated/initial/medial/final forms) and right-to-left layout, including
ZWNJ (zero-width non-joiner) handling — so Persian, Arabic, Urdu, etc. display
correctly:

```
سلام
salâm
---
hello
```

This needs an Arabic-capable font installed (e.g. Noto Naskh Arabic, Noto Sans
Arabic, or Vazirmatn); one is detected automatically. If none is found, the text
falls back to the plain renderer (unshaped). Latin and CJK text are unaffected.

### Audio

Play a pronunciation clip with `@audio`:

```
@audio audio/ni-hao.mp3
---
hello
```

Requires `mpv` or `aplay` on the system.

#### Playback speed

Audio plays at normal speed by default. While a question is showing, adjust the
speed on the fly — handy for hearing a tricky phrase slowly:

- `Ctrl`+`,` — slow down and replay
- `Ctrl`+`.` — speed up and replay
- `Ctrl`+`/` — reset to normal (1.00x)

Speed steps by `0.25`, clamped to `0.25x`–`4.00x`, and the current value is shown
in the footer. The setting carries across cards for the rest of the session. Set
a different starting speed per deck with the `# speed:` header (e.g. `# speed: 0.75`
for a beginner deck). Speed changes require `mpv`; pitch is preserved so slowed
speech stays clear. (`aplay` always plays at normal speed.)

### Combined media

A single card can have text, image, and audio together:

```
@img flags/japan.png
@audio audio/konnichiwa.mp3
How do you say "hello"?
---
こんにちは
```

The image is displayed, the audio plays automatically, and the text is shown below.

## Controls

| Key              | Action                          |
|------------------|---------------------------------|
| `1`-`9`          | Select answer (choice mode)     |
| Type + `Enter`   | Submit answer (type mode)       |
| `Backspace`      | Delete character (type mode)    |
| `Ctrl`+`R`       | Replay the question's audio     |
| `Ctrl`+`,` / `Ctrl`+`.` | Slow down / speed up audio (and replay) |
| `Ctrl`+`/`       | Reset audio speed to 1.00x      |
| `Enter` / `Space`| Continue after result           |
| `Escape`         | Quit                            |

## How It Works

- Wrong answers (including questions that hit their time limit) are repeated immediately, then re-queued for 3 additional correct attempts before graduating.
- Confidence scores are tracked per card. High-confidence cards appear less often.
- Progress is saved to `~/.local/share/study/` and persists between sessions.
- Theme is detected automatically (dark/light) via gsettings or `~/.config/theme-mode`.
