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
rsync -a --delete --exclude '__pycache__' --exclude '.git' \
	"$P/language-packs" \
	"$P/study-mexican-spanish.deck" \
	"$P/study-japanese-numbers.deck" \
	"$P/study-mahjong.deck" \
	"$P/study-farsi-numbers.deck" \
	"$P/study-chinese-numbers.deck" \
	"$P/study-chinese-mahjong-terms.deck" \
	"$P/study-chinese-mahjong-tiles.deck" \
	"$P/study-dog-breeds.deck" \
	"$P/study-bc-driving.deck" \
	"$P/study-world-flags.deck" \
	"$P/study-world-capitals.deck" \
	"$P/study-bc-birds.deck" \
	"$SERVER:/opt/study-web/decks/"

echo "== install binary & restart =="
rsync -a study-web-linux "$SERVER:/opt/study-web/study-web"
rm study-web-linux
ssh "$SERVER" 'sudo systemctl restart study-web && sleep 1 && sudo systemctl is-active study-web'

echo "== verify =="
curl -s -o /dev/null -w "https://study.fftp.io/ -> %{http_code}\n" https://study.fftp.io/
