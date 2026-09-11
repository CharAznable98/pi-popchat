#!/bin/sh
# Run desktop probes serially. Opens and closes one synthetic test window.
set -eu
cd "$(dirname "$0")/.."
mkdir -p work outputs
probe_dir=$(mktemp -d "$PWD/work/selection-probe-XXXXXX")
clang -fobjc-arc -fblocks -mmacosx-version-min=15.0 -framework AppKit -framework ApplicationServices -framework WebKit \
  scripts/probe-selection.m -o "$probe_dir/probe-selection"
"$probe_dir/probe-selection" probe "$probe_dir/ready.json" > outputs/selection-probe-results.json
cat outputs/selection-probe-results.json
"$probe_dir/probe-selection" probe "$probe_dir/web-ready.json" web > outputs/selection-web-probe-results.json
cat outputs/selection-web-probe-results.json
