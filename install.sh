#!/bin/sh
set -e
go build -o study
cp study ~/.local/bin/
echo "installed to ~/.local/bin/study"
