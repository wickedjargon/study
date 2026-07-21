#!/bin/sh
# Deploy study-web to study.fftp.io (see RUNNING.md for local use).
#
# Builds a static linux binary, rsyncs it and every deck pack to the server,
# and restarts the service. First-time server setup (systemd unit, nginx
# site, certbot) was done by hand and lives in /etc on the server; this
# script only refreshes what changes: code and content.
#
# Login mail (one-time, already done): production magic links go out through
# Resend (resend.com) — domain verified there, RESEND_API_KEY in
# /opt/study-web/env (EnvironmentFile in the systemd unit), and
# -base-url https://study.fftp.io on ExecStart. Without the key the server
# still runs; links land in `journalctl -u study-web`. Locally there is
# never email — the link prints to the server log.
#
# Run from the repo root, on the host (it enters archbox for the build).
set -e

SERVER=deploy@45.63.7.246
P=$HOME/d/projects

echo "== build =="
distrobox enter archbox -- env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -trimpath -ldflags "-s -w" -o study-web-linux ./cmd/study-web

echo "== sync decks =="
# Staged through a directory of symlinks so one rsync source covers the whole
# list: --delete can then prune decks dropped from it, which a multi-source
# rsync never does (it only deletes inside each transferred tree).
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
# mktemp makes the dir 0700 and rsync -a would stamp that onto the server's
# decks/ itself, locking the service user out of every pack.
chmod 755 "$STAGE"
for d in \
	"$P/language-packs" \
	"$P/study-mexican-spanish.deck" \
	"$P/study-japanese-numbers.deck" \
	"$P/study-mahjong.deck" \
	"$P/study-farsi-numbers.deck" \
	"$P/study-chinese-numbers.deck" \
	"$P/study-chinese-mahjong-terms.deck" \
	"$P/study-chinese-mahjong-tiles.deck" \
	"$P/study-dog-breeds.deck" \
	"$P/study-decks/study-bc-driving.deck" \
	"$P/study-decks/study-world-flags.deck" \
	"$P/study-decks/study-country-silhouettes.deck" \
	"$P/study-decks/study-world-capitals.deck" \
	"$P/study-decks/study-bc-birds.deck" \
	"$P/study-decks/study-animal-tracks.deck" \
	"$P/study-decks/study-speed-trivia.deck" \
	"$P/study-decks/study-which-is-bigger.deck" \
	"$P/study-decks/study-world-landmarks.deck" \
	"$P/study-decks/study-locator-maps.deck" \
	"$P/study-decks/study-flags-by-region.deck" \
	"$P/study-decks/study-borders.deck" \
	"$P/study-decks/study-waters.deck" \
	"$P/study-decks/study-us-presidents.deck" \
	"$P/study-decks/study-canada.deck" \
	"$P/study-decks/study-comptia-aplus.deck"; do
	ln -s "$d" "$STAGE/"
done
rsync -aL --delete --exclude '__pycache__' --exclude '.git' --exclude '*.geojson' \
	"$STAGE/" "$SERVER:/opt/study-web/decks/"

echo "== install binary & restart =="
rsync -a study-web-linux "$SERVER:/opt/study-web/study-web"
rm study-web-linux
ssh "$SERVER" 'sudo systemctl restart study-web && sleep 1 && sudo systemctl is-active study-web'

echo "== verify =="
# -f: a 5xx after the restart must fail the deploy, not print and exit 0.
curl -fsS -o /dev/null -w "https://study.fftp.io/ -> %{http_code}\n" https://study.fftp.io/
