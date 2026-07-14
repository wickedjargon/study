# Running study-web

Run from the repo root.

```sh
make run
```

Open http://127.0.0.1:8091.

Progress lands in `./data/`.

## Phone access

Bind to the network.

```sh
make run ADDR=0.0.0.0:8091
```

Same Wi-Fi: http://10.163.186.41:8091.

Tailscale: http://100.107.125.11:8091.

## After code changes

Go lives in archbox. Rebuild first.

```sh
distrobox enter archbox -- make study-web
```

Then `make run` again.

## Stop

Ctrl-C.

## Decks

`make run` serves the deck list in the Makefile (`WEB_DECKS`).

`group=path` nests a pack under a language.

## Test playground

```sh
make test-run
```

Tiny decks on http://127.0.0.1:8095 reaching every screen fast.

Progress is throwaway; restart for a clean state.
