#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
sh scripts/build-app.sh
exec sh scripts/check-native-desktop.sh
