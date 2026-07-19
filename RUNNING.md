# Running study-web

Run from the repo root:

```sh
make run
```

Open http://127.0.0.1:8091. Progress lands in `./data/`.

Go lives in archbox. Rebuild after code changes:

```sh
distrobox enter archbox -- make study-web
```

## Phone access

```sh
make run ADDR=0.0.0.0:8091
```

Same Wi-Fi: http://10.163.186.41:8091. Tailscale: http://100.107.125.11:8091.

## Decks

`make run` serves the Makefile's `WEB_DECKS` list. `group=path` nests a
pack under a language. `path@Name` overrides the display name.

## Test playground

```sh
make test-run
```

Open http://127.0.0.1:8095. Tiny decks, one-letter answers, every screen a
few keystrokes away. Progress is a fresh temp dir per start.


