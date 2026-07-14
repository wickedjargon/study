PREFIX = $(HOME)/.local

all: study study-web

study:
	go build -o study ./cmd/study

study-web:
	go build -o study-web ./cmd/study-web

install: study
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
.PHONY: all study study-web install uninstall clean
