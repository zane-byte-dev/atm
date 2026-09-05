#!/bin/zsh
set -euo pipefail
NATIVE_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NATIVE_CONFIGURATION="${VOXCARET_BUILD_CONFIGURATION:-release}"
NATIVE_ENTITLEMENTS="$NATIVE_ROOT/Resources/VoxCaret.entitlements"
NATIVE_ICON_SOURCE="$NATIVE_ROOT/Resources/AppIcon.png"
swift build --package-path "$NATIVE_ROOT" --scratch-path "$NATIVE_ROOT/.build" --cache-path "$NATIVE_ROOT/.cache" --disable-sandbox -c "$NATIVE_CONFIGURATION"
mkdir -p "$NATIVE_ROOT/dist"
NATIVE_STAGING="$(mktemp -d "$NATIVE_ROOT/dist/.build.XXXXXX")"
trap 'rm -rf "$NATIVE_STAGING"' EXIT
NATIVE_APP="$NATIVE_STAGING/VoxCaret.app"
NATIVE_ASSETS="$NATIVE_STAGING/Assets.xcassets"
NATIVE_ICONSET="$NATIVE_ASSETS/AppIcon.appiconset"
mkdir -p "$NATIVE_APP/Contents/MacOS" "$NATIVE_APP/Contents/Resources"
mkdir -p "$NATIVE_ICONSET"
cp "$NATIVE_ROOT/Resources/Assets.xcassets/Contents.json" "$NATIVE_ASSETS/Contents.json"
cp "$NATIVE_ROOT/Resources/Assets.xcassets/AppIcon.appiconset/Contents.json" "$NATIVE_ICONSET/Contents.json"
sips -z 16 16 "$NATIVE_ICON_SOURCE" --out "$NATIVE_ICONSET/icon_16x16.png" >/dev/null
sips -z 32 32 "$NATIVE_ICON_SOURCE" --out "$NATIVE_ICONSET/icon_16x16@2x.png" >/dev/null
sips -z 32 32 "$NATIVE_ICON_SOURCE" --out "$NATIVE_ICONSET/icon_32x32.png" >/dev/null
sips -z 64 64 "$NATIVE_ICON_SOURCE" --out "$NATIVE_ICONSET/icon_32x32@2x.png" >/dev/null
sips -z 128 128 "$NATIVE_ICON_SOURCE" --out "$NATIVE_ICONSET/icon_128x128.png" >/dev/null
sips -z 256 256 "$NATIVE_ICON_SOURCE" --out "$NATIVE_ICONSET/icon_128x128@2x.png" >/dev/null
sips -z 256 256 "$NATIVE_ICON_SOURCE" --out "$NATIVE_ICONSET/icon_256x256.png" >/dev/null
sips -z 512 512 "$NATIVE_ICON_SOURCE" --out "$NATIVE_ICONSET/icon_256x256@2x.png" >/dev/null
sips -z 512 512 "$NATIVE_ICON_SOURCE" --out "$NATIVE_ICONSET/icon_512x512.png" >/dev/null
sips -z 1024 1024 "$NATIVE_ICON_SOURCE" --out "$NATIVE_ICONSET/icon_512x512@2x.png" >/dev/null
xcrun actool "$NATIVE_ASSETS" \
    --compile "$NATIVE_APP/Contents/Resources" \
    --platform macosx \
    --minimum-deployment-target 13.4 \
    --app-icon AppIcon \
    --output-partial-info-plist "$NATIVE_STAGING/AssetInfo.plist" \
    >/dev/null
cp "$NATIVE_ROOT/.build/$NATIVE_CONFIGURATION/VoxCaret" "$NATIVE_APP/Contents/MacOS/VoxCaret"
cp "$NATIVE_ROOT/Resources/Info.plist" "$NATIVE_APP/Contents/Info.plist"
cp -R "$NATIVE_ROOT/Resources/zh-Hans.lproj" "$NATIVE_APP/Contents/Resources/"
chmod +x "$NATIVE_APP/Contents/MacOS/VoxCaret"
if [[ -n "${VOXCARET_CODESIGN_IDENTITY:-}" ]]; then
    ATM_CODESIGN_IDENTITY="$VOXCARET_CODESIGN_IDENTITY" \
        "$NATIVE_ROOT/../../scripts/codesign-local.sh" --entitlements "$NATIVE_ENTITLEMENTS" "$NATIVE_APP"
else
    "$NATIVE_ROOT/../../scripts/codesign-local.sh" --entitlements "$NATIVE_ENTITLEMENTS" "$NATIVE_APP"
fi
codesign --verify --strict "$NATIVE_APP"
rm -rf "$NATIVE_ROOT/dist/VoxCaret.app"
mv "$NATIVE_APP" "$NATIVE_ROOT/dist/VoxCaret.app"
print "$NATIVE_ROOT/dist/VoxCaret.app"
