# study

A terminal-based study and quiz application built in Go. Features support for custom `.deck` files, multiple choice and type-in questions, and spaced repetition progress tracking.

## Building and Running

Ensure you have Go installed on your system.

### Build
To build the project into an executable binary:
```bash
go build -o study
```

### Run
To run the study application, provide it with the path to a `.deck` file:
```bash
./study path/to/your/deck.deck
```

## Creating a `.deck` File

Deck files are plain-text files that define your study cards. The format is designed to be simple and easy to write.

### Basic Structure

Cards are separated by blank lines. Each card has a **Question** side and an **Answer** side, separated by `---`.

```text
Question text goes here
---
Answer text goes here
```

### Configuration Comments
You can set deck-wide configuration at the top of the file using `# key: value` syntax.

```text
# choices: 4       (number of multiple choice options)
# mode: choice     (can be 'choice' or 'type')
# case: insensitive (can be 'sensitive' or 'insensitive' for 'type' mode)
```

You can also override the `mode` for a specific card by placing `# mode: type` above the card. Comments starting with `#` are ignored otherwise.

### Media (Images and Audio)
You can include media in your questions or answers using the `@img` or `@audio` tags. Paths are relative to the directory containing the `.deck` file.

```text
@img images/apple.png
What is this fruit?
---
Apple
```

### Custom Distractors
For multiple-choice questions, the engine will automatically select other answers from the deck as distractors. If you want to specify custom incorrect answers for a specific card, use the `~` prefix on the answer side.

```text
What is 2 + 2?
---
4
~ 3
~ 5
~ 22
```
