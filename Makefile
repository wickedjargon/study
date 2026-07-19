PREFIX = $(HOME)/.local

all: study study-web

study:
	go build -o study ./cmd/study

study-web:
	go build -o study-web ./cmd/study-web

# The local demo decks. `make run` needs the binary built first (go lives in
# the archbox container; running the binary doesn't).
# Language variants share a "Language (Variant)" display name (the @ suffix)
# so they sort next to each other on the home page.
WEB_DECKS = \
	'[Languages]' \
	$(HOME)/d/projects/language-packs/study-japanese.deck \
	$(HOME)/d/projects/language-packs/study-farsi.deck \
	$(HOME)/d/projects/language-packs/study-mandarin-chinese.deck \
	'$(HOME)/d/projects/language-packs/study-colombian-spanish.deck@Spanish (Colombian)' \
	'$(HOME)/d/projects/study-mexican-spanish.deck@Spanish (Mexican)' \
	'$(HOME)/d/projects/language-packs/study-brazilian-portuguese.deck@Portuguese (Brazilian)' \
	japanese=$(HOME)/d/projects/study-japanese-numbers.deck \
	'japanese=$(HOME)/d/projects/study-mahjong.deck@Mahjong (Tiles)' \
	farsi=$(HOME)/d/projects/study-farsi-numbers.deck \
	mandarin-chinese=$(HOME)/d/projects/study-chinese-numbers.deck \
	'mandarin-chinese=$(HOME)/d/projects/study-chinese-mahjong-tiles.deck@Mahjong (Tiles)' \
	'mandarin-chinese=$(HOME)/d/projects/study-chinese-mahjong-terms.deck@Mahjong (Vocab)' \
	'[More]' \
	$(HOME)/d/projects/study-dog-breeds.deck \
	'$(HOME)/d/projects/study-decks/study-bc-driving.deck@British Columbia Driving' \
	$(HOME)/d/projects/study-decks/study-world-flags.deck \
	$(HOME)/d/projects/study-decks/study-country-silhouettes.deck \
	$(HOME)/d/projects/study-decks/study-world-capitals.deck \
	'$(HOME)/d/projects/study-decks/study-bc-birds.deck@British Columbia Birds' \
	$(HOME)/d/projects/study-decks/study-animal-tracks.deck \
	$(HOME)/d/projects/study-decks/study-world-landmarks.deck \
	$(HOME)/d/projects/study-decks/study-locator-maps.deck \
	'$(HOME)/d/projects/study-decks/study-flags-by-region.deck@Flags by Region' \
	$(HOME)/d/projects/study-decks/study-borders.deck \
	$(HOME)/d/projects/study-decks/study-waters.deck \
	'$(HOME)/d/projects/study-decks/study-us-presidents.deck@US Presidents' \
	$(HOME)/d/projects/study-decks/study-canada.deck \
	'[Trivia]' \
	$(HOME)/d/projects/study-decks/study-speed-trivia.deck \
	'$(HOME)/d/projects/study-decks/study-which-is-bigger.deck@Which Is Bigger?'

# Bind localhost by default; `make run ADDR=0.0.0.0:8091` opens it to the
# LAN (e.g. to try it from a phone).
ADDR = 127.0.0.1:8091

run:
	@test -x study-web || { echo "study-web is not built — run: distrobox enter archbox -- make study-web"; exit 1; }
	./study-web -addr $(ADDR) -data ./data $(WEB_DECKS)

# A local playground on its own port: tiny decks that reach every screen in
# a few keystrokes (one-letter answers, no wrong-pause). Progress goes to a
# throwaway directory, so every start is a fresh, predictable state.
test-run:
	@test -x study-web || { echo "study-web is not built — run: distrobox enter archbox -- make study-web"; exit 1; }
	./study-web -addr 127.0.0.1:8095 -data $$(mktemp -d) 'testdata/webtest.deck@Playground'

# install also rebuilds study-web so a `make clean install` doesn't leave
# `make run` without its binary.
COMPLETIONS = $(HOME)/.local/share/bash-completion/completions

install: study study-web
	mkdir -p $(PREFIX)/bin
	cp -f study $(PREFIX)/bin/
	mkdir -p $(COMPLETIONS)
	cp -f completions/study.bash $(COMPLETIONS)/study
	@echo "installed to $(PREFIX)/bin/study (+ bash completion)"

uninstall:
	rm -f $(PREFIX)/bin/study
	rm -f $(COMPLETIONS)/study

clean:
	rm -f study study-web

# The binaries are .PHONY so `make` always delegates to `go build`, which does
# its own up-to-date checking. Without this, make sees the existing binary
# (the target has no prerequisites) and skips rebuilding after source changes,
# silently shipping a stale binary.
.PHONY: all study study-web run install uninstall clean
