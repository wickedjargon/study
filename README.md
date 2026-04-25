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
| `--choices N`    | Number of answer choices (overrides deck) |
| `--sequential`   | Present cards in deck order (default: shuffled) |
| `--reset`        | Clear progress for this deck             |
| `--help`         | Show help                                |

## Deck Format

A `.deck` file is plain text. Cards are separated by blank lines. Each card has a question and answer divided by `---`.

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
```

| Header           | Values                  | Default       |
|------------------|-------------------------|---------------|
| `# mode:`        | `choice`, `type`        | `choice`      |
| `# choices:`     | any integer ≥ 2         | `4`           |
| `# case:`        | `sensitive`, `insensitive` | `sensitive` |

## Features

### Multiple choice mode

The default. The user picks from numbered options.

```
# mode: choice
# choices: 4

What is 3 + 5?
---
8
```

The quiz generates 4 options using answers from other cards in the deck as distractors.

### Type-in mode

The user types the answer.

```
# mode: type
# case: insensitive

What is 10 - 3?
---
7
```

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

### Images

Show an image as part of the question with `@img`:

```
@img flags/france.png
What country is this?
---
France
```

Paths are relative to the directory containing the `.deck` file.

### Audio

Play a pronunciation clip with `@audio`:

```
@audio audio/ni-hao.mp3
---
hello
```

Requires `mpv` or `aplay` on the system.

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

### Multiline answers

Answers can span multiple lines:

```
Name the 4 tones in Mandarin
---
1. flat
2. rising
3. dipping
4. falling

```

## Controls

| Key              | Action                          |
|------------------|---------------------------------|
| `1`-`9`          | Select answer (choice mode)     |
| Type + `Enter`   | Submit answer (type mode)       |
| `Backspace`      | Delete character (type mode)    |
| `Enter` / `Space`| Continue after result           |
| `Escape`         | Quit                            |

## How It Works

- Wrong answers are repeated immediately, then re-queued for 3 additional correct attempts before graduating.
- Confidence scores are tracked per card. High-confidence cards appear less often.
- Progress is saved to `~/.local/share/study/` and persists between sessions.
- Theme is detected automatically (dark/light) via gsettings or `~/.config/theme-mode`.
