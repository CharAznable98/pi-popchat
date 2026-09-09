#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ ! -x "dist/Pi Popchat.app/Contents/MacOS/pi-popchat" ]; then sh scripts/build-app.sh; fi
open "dist/Pi Popchat.app"
