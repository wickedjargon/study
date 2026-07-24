#!/bin/sh
# Deploy the Windows build to the local win11 QEMU VM.
#
# Copies dist/study-setup.exe (build it with ./build-win.sh) into the VM
# over the forwarded ssh port and runs a silent install. /S takes the
# installer defaults: study.exe plus the pre-checked starter geography
# packs. For the opt-in packs, run the installer interactively in the VM.
# It stays in Downloads for that.
#
# The VM must be running (vm start win11).
set -e

VM=farzi@localhost
PORT=2222
SETUP=dist/study-setup.exe

test -f "$SETUP" || { echo "no $SETUP. run ./build-win.sh first"; exit 1; }

echo "== push =="
scp -P "$PORT" "$SETUP" "$VM:C:/Users/farzi/Downloads/study-setup.exe"

echo "== install =="
ssh -p "$PORT" "$VM" 'powershell -c "Start-Process -FilePath \"$env:USERPROFILE\Downloads\study-setup.exe\" -ArgumentList /S -Wait"'

echo "== verify =="
# The installed exe must exist and the pushed installer must match the
# local one byte for byte. One line: the VM's login shell is cmd.exe,
# which mangles newlines inside a quoted argument.
SIZE=$(wc -c <"$SETUP")
ssh -p "$PORT" "$VM" 'powershell -c "if (-not (Test-Path \"$env:LOCALAPPDATA\Programs\study\study.exe\")) { exit 1 }; (Get-Item \"$env:USERPROFILE\Downloads\study-setup.exe\").Length"' \
	| tr -d '\r' | grep -qx "$SIZE"
echo "installed, installer $SIZE bytes on the VM"
