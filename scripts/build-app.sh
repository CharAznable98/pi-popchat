#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export MACOSX_DEPLOYMENT_TARGET=15.0
export CGO_ENABLED=1
export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=15.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=15.0"
if [ "$(uname -s)" != Darwin ]; then echo '此构建仅支持 macOS' >&2; exit 1; fi
npm --prefix frontend ci
npm --prefix frontend run build
app_dir="${PI_POPCHAT_BUILD_DIR:-dist}/Pi Popchat.app"
mkdir -p "$app_dir/Contents/MacOS" "$app_dir/Contents/Resources"
go build -tags production -trimpath -ldflags='-s -w' -o "$app_dir/Contents/MacOS/pi-popchat" .
cp build/Info.plist "$app_dir/Contents/Info.plist"
cp build/icons/AppIcon.icns "$app_dir/Contents/Resources/AppIcon.icns"
codesign --force --deep --sign - "$app_dir"
codesign --verify --deep --strict "$app_dir"
echo "已生成本机签名应用：${app_dir}（未公证）"
