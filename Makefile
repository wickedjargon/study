PREFIX = $(HOME)/.local

all: study

study:
	go build -o study

install: study
	mkdir -p $(PREFIX)/bin
	cp -f study $(PREFIX)/bin/
	@echo "installed to $(PREFIX)/bin/study"

uninstall:
	rm -f $(PREFIX)/bin/study

clean:
	rm -f study

.PHONY: all install uninstall clean
