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

## Logging in

Guests study anonymously; logging in (email magic link) makes progress
portable across devices. Identity lives in `<data>/identity.db`, progress
stays in `<data>/users/<id>/`.

Locally there is no email: the login link is printed to the server log —
copy it into the browser.

In production the link goes out through [Resend](https://resend.com):

- One-time: create a Resend account, verify the `fftp.io` domain (add the
  SPF/DKIM records Resend shows), mint an API key.
- One-time, on the server: put the key in the unit —
  `Environment=RESEND_API_KEY=re_…` in
  `/etc/systemd/system/study-web.service` (or an `EnvironmentFile`), and add
  `-base-url https://study.fftp.io` to `ExecStart`. Then
  `sudo systemctl daemon-reload && sudo systemctl restart study-web`.

Without the key the server still runs — links just land in the journal
(`journalctl -u study-web`), which also works in a pinch.

Login emails come from `-mail-from` (default `study <study@fftp.io>`).
Links are single-use and expire in 15 minutes; sessions last a year.
A new account adopts the requesting guest's progress; logging into an
existing account leaves guest progress behind.

## Test playground

```sh
make test-run
```

Tiny decks on http://127.0.0.1:8095 reaching every screen fast.

Progress is throwaway; restart for a clean state.
