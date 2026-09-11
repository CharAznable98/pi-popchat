#!/bin/sh
# Launch a new ad-hoc app identity without requesting or changing TCC permissions.
set -eu
cd "$(dirname "$0")/.."
mkdir -p work outputs
probe_dir=$(mktemp -d "$PWD/work/selection-permission-XXXXXX")
app_dir="$probe_dir/Selection Permission Probe.app"
mkdir -p "$app_dir/Contents/MacOS"
clang -fobjc-arc -fblocks -mmacosx-version-min=15.0 -framework AppKit -framework ApplicationServices -framework WebKit \
  scripts/selection-acceptance.m -o "$app_dir/Contents/MacOS/selection-acceptance"
python3 - "$app_dir" <<'PY'
import pathlib,plistlib,sys,uuid
app=pathlib.Path(sys.argv[1])
with (app/'Contents/Info.plist').open('wb') as f:
    plistlib.dump({'CFBundleIdentifier':'com.pipopchat.selection-permission.'+uuid.uuid4().hex,'CFBundleExecutable':'selection-acceptance','CFBundleName':'Selection Permission Probe','CFBundlePackageType':'APPL','LSUIElement':True,'LSMinimumSystemVersion':'15.0'},f)
PY
codesign --force --sign - "$app_dir"
open -n -W "$app_dir" --args permission native 0 0 "$probe_dir/result.json"
cp "$probe_dir/result.json" outputs/selection-permission-results.json
cat outputs/selection-permission-results.json
