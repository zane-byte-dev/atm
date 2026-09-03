#!/bin/zsh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BUILD_CONFIGURATION="${ATM_BUILD_CONFIGURATION:-debug}"
case "$BUILD_CONFIGURATION" in
    debug|release) ;;
    *)
        echo "ATM_BUILD_CONFIGURATION must be debug or release (received: $BUILD_CONFIGURATION)" >&2
        exit 2
        ;;
esac
APP_DIR="$ROOT_DIR/.build/dev-app/ATM Dev.app"
BUILD_DIR="$ROOT_DIR/.build/$BUILD_CONFIGURATION"
RESOURCE_BUNDLE="$BUILD_DIR/ATMMenuBarApp_ATMMenuBarApp.bundle"
ICON_SOURCE="$ROOT_DIR/Resources/AppIcon.png"
ICONSET_DIR="$ROOT_DIR/.build/dev-app/AppIcon.iconset"
ICON_FILE="$ROOT_DIR/.build/dev-app/AppIcon.icns"

echo "Building ATM Dev ($BUILD_CONFIGURATION)"
swift build --package-path "$ROOT_DIR" -c "$BUILD_CONFIGURATION"

rm -rf "$APP_DIR" "$ICONSET_DIR"
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources" "$ICONSET_DIR"

sips -z 16 16 "$ICON_SOURCE" --out "$ICONSET_DIR/icon_16x16.png" >/dev/null
sips -z 32 32 "$ICON_SOURCE" --out "$ICONSET_DIR/icon_16x16@2x.png" >/dev/null
sips -z 32 32 "$ICON_SOURCE" --out "$ICONSET_DIR/icon_32x32.png" >/dev/null
sips -z 64 64 "$ICON_SOURCE" --out "$ICONSET_DIR/icon_32x32@2x.png" >/dev/null
sips -z 128 128 "$ICON_SOURCE" --out "$ICONSET_DIR/icon_128x128.png" >/dev/null
sips -z 256 256 "$ICON_SOURCE" --out "$ICONSET_DIR/icon_128x128@2x.png" >/dev/null
sips -z 256 256 "$ICON_SOURCE" --out "$ICONSET_DIR/icon_256x256.png" >/dev/null
sips -z 512 512 "$ICON_SOURCE" --out "$ICONSET_DIR/icon_256x256@2x.png" >/dev/null
sips -z 512 512 "$ICON_SOURCE" --out "$ICONSET_DIR/icon_512x512.png" >/dev/null
sips -z 1024 1024 "$ICON_SOURCE" --out "$ICONSET_DIR/icon_512x512@2x.png" >/dev/null
iconutil -c icns "$ICONSET_DIR" -o "$ICON_FILE"

cp "$BUILD_DIR/ATMMenuBarApp" "$APP_DIR/Contents/MacOS/ATMMenuBarApp"
cp -R "$RESOURCE_BUNDLE" "$APP_DIR/Contents/Resources/"
cp "$ICON_FILE" "$APP_DIR/Contents/Resources/AppIcon.icns"
cp "$ROOT_DIR/Resources/DebugInfo.plist" "$APP_DIR/Contents/Info.plist"
chmod +x "$APP_DIR/Contents/MacOS/ATMMenuBarApp"
codesign --force --deep --sign - "$APP_DIR"

echo "Packaged $APP_DIR ($BUILD_CONFIGURATION)"
if [[ "${ATM_BUILD_ONLY:-0}" == "1" ]]; then
    echo "ATM_BUILD_ONLY=1: launch skipped"
    exit 0
fi

echo "Running $APP_DIR ($BUILD_CONFIGURATION)"
exec "$APP_DIR/Contents/MacOS/ATMMenuBarApp"
