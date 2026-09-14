#!/bin/sh
# Serial desktop check; requires screenshot access and Python Pillow.
set -eu
cd "$(dirname "$0")/.."
probe_dir=$(mktemp -d "$PWD/work/selection-appearance-XXXXXX")
probe_pid=
trap 'if [ -n "$probe_pid" ]; then kill "$probe_pid" 2>/dev/null || true; wait "$probe_pid" 2>/dev/null || true; fi; rm -rf "$probe_dir"' EXIT
clang -fblocks -framework AppKit -framework NaturalLanguage -framework ApplicationServices \
  scripts/selection-appearance.m selection_toolbar_darwin.m -o "$probe_dir/preview"
"$probe_dir/preview" "$probe_dir/window-id" &
probe_pid=$!
i=0
while [ ! -s "$probe_dir/window-id" ]; do
  kill -0 "$probe_pid"
  i=$((i+1)); [ "$i" -lt 50 ] || exit 1
  sleep 0.1
done
sleep 0.3
mkdir -p outputs
screencapture -x -l "$(cat "$probe_dir/window-id")" outputs/selection-appearance.png
python3 - <<'PY'
from PIL import Image
im=Image.open('outputs/selection-appearance.png').convert('RGBA')
# Locate the actual body within the OS-added shadow, independent of pixel scale.
x=next(x for x in range(im.width//2) if im.getpixel((x,im.height//2))[3]>200)
y=next(y for y in range(im.height//2) if im.getpixel((im.width//2,y))[3]>200)
r,g,b,a=im.getpixel((x,y))
assert max(r,g,b)*a/255 < 5, f'White square outline outside rounded corner: {(r,g,b,a)}'
print('PASS: production toolbar keeps full height and has no white square outline')
PY
