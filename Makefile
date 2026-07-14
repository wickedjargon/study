PREFIX = $(HOME)/.local

all: study study-web

study:
	go build -o study ./cmd/study

study-web:
	go build -o study-web ./cmd/study-web

# The local demo decks. `make run` needs the binary built first (go lives in
# the archbox container; running the binary doesn't).
WEB_DECKS = \
	$(HOME)/d/projects/language-packs \
	$(HOME)/d/projects/study-mexican-spanish.deck \
	japanese=$(HOME)/d/projects/study-japanese-numbers.deck \
	japanese=$(HOME)/d/projects/study-mahjong.deck \
	farsi=$(HOME)/d/projects/study-farsi-numbers.deck \
	mandarin-chinese=$(HOME)/d/projects/study-chinese-numbers.deck \
	$(HOME)/d/projects/study-dog-breeds.deck

# Bind localhost by default; `make run ADDR=0.0.0.0:8091` opens it to the
# LAN (e.g. to try it from a phone).
ADDR = 127.0.0.1:8091

run:
	@test -x study-web || { echo "study-web is not built — run: distrobox enter archbox -- make study-web"; exit 1; }
	./study-web -addr $(ADDR) -data ./data $(WEB_DECKS)

# install also rebuilds study-web so a `make clean install` doesn't leave
# `make run` without its binary.
install: study study-web
	mkdir -p $(PREFIX)/bin
	cp -f study $(PREFIX)/bin/
	@echo "installed to $(PREFIX)/bin/study"

uninstall:
	rm -f $(PREFIX)/bin/study

clean:
	rm -f study study-web

# The binaries are .PHONY so `make` always delegates to `go build`, which does
# its own up-to-date checking. Without this, make sees the existing binary
# (the target has no prerequisites) and skips rebuilding after source changes,
# silently shipping a stale binary.
.PHONY: all study study-web run install uninstall clean
