#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export MACOSX_DEPLOYMENT_TARGET=15.0
export CGO_ENABLED=1
export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=15.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=15.0"
if [ "$(uname -s)" != Darwin ]; then echo '此构建仅支持 macOS' >&2; exit 1; fi
# Keep the local signing identity outside generated output and the repository.
# A configured certificate must fail visibly if unavailable; never fall back to
# ad-hoc signing and silently invalidate existing Accessibility authorization.
sign_config="$HOME/Library/Application Support/Pi Popchat/build-signing-identity"
if [ "${PI_POPCHAT_SIGN_IDENTITY+x}" = x ]; then
  sign_identity="$PI_POPCHAT_SIGN_IDENTITY"
elif [ -f "$sign_config" ]; then
  sign_identity=$(cat "$sign_config")
else
  sign_identity=-
fi
if [ -z "$sign_identity" ]; then echo '签名身份不能为空' >&2; exit 1; fi
npm --prefix frontend ci
npm --prefix frontend run build
app_dir="${PI_POPCHAT_BUILD_DIR:-dist}/Pi Popchat.app"
mkdir -p "$app_dir/Contents/MacOS" "$app_dir/Contents/Resources"
go build -tags production -trimpath -ldflags='-s -w' -o "$app_dir/Contents/MacOS/pi-popchat" .
cp build/Info.plist "$app_dir/Contents/Info.plist"
cp LICENSE "$app_dir/Contents/Resources/LICENSE"
cp build/icons/AppIcon.icns "$app_dir/Contents/Resources/AppIcon.icns"
codesign --force --deep --sign "$sign_identity" "$app_dir"
codesign --verify --deep --strict "$app_dir"
echo "已生成本机签名应用：${app_dir}（未公证）"
if [ "$sign_identity" = - ]; then
  echo '注意：临时签名随构建内容变化，旧辅助功能授权可能失效。发布/持续验收请通过 PI_POPCHAT_SIGN_IDENTITY 使用固定签名证书。'
fi
