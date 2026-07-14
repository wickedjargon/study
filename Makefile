PREFIX = $(HOME)/.local

all: study

study:
	go build -o study ./cmd/study

install: study
	mkdir -p $(PREFIX)/bin
	cp -f study $(PREFIX)/bin/
	@echo "installed to $(PREFIX)/bin/study"

uninstall:
	rm -f $(PREFIX)/bin/study

clean:
	rm -f study

# study is .PHONY so `make` always delegates to `go build`, which does its own
# up-to-date checking. Without this, make sees the existing ./study file (the
# target has no prerequisites) and skips rebuilding after source changes,
# silently shipping a stale binary.
.PHONY: all study install uninstall clean
