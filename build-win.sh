#!/bin/sh
# Build the Windows installer: dist/study-setup.exe.
#
# One command over the split toolchains. study.exe cross-compiles in
# archbox (pure Go, no CGO) and makensis packs the installer on the host
# (Debian: apt install nsis). The make targets stay the source of truth,
# this just runs each in the right place.
#
# Run from the repo root, on the host.
set -e

echo "== exe (archbox) =="
distrobox enter archbox -- make study-win

echo "== installer (host) =="
make study-setup
