#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p work
probe_dir=$(mktemp -d "$PWD/work/selection-events-XXXXXX")
trap 'rm -rf "$probe_dir"' EXIT
clang -fblocks -mmacosx-version-min=15.0 \
  -framework AppKit -framework ApplicationServices -framework NaturalLanguage \
  scripts/selection-events.m selection_darwin.m selection_toolbar_darwin.m -o "$probe_dir/selection-events"
"$probe_dir/selection-events"
