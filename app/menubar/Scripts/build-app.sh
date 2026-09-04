#!/bin/zsh
set -euo pipefail
NATIVE_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NATIVE_CONFIGURATION="${ATM_BUILD_CONFIGURATION:-release}"
swift build --package-path "$NATIVE_ROOT" --scratch-path "$NATIVE_ROOT/.build" --cache-path "$NATIVE_ROOT/.cache" --disable-sandbox -c "$NATIVE_CONFIGURATION"
mkdir -p "$NATIVE_ROOT/dist"
NATIVE_STAGING="$(mktemp -d "$NATIVE_ROOT/dist/.build.XXXXXX")"
trap 'rm -rf "$NATIVE_STAGING"' EXIT
NATIVE_APP="$NATIVE_STAGING/ATM Menu.app"
mkdir -p "$NATIVE_APP/Contents/MacOS" "$NATIVE_APP/Contents/Resources"
cp "$NATIVE_ROOT/.build/$NATIVE_CONFIGURATION/ATMCompanion" "$NATIVE_APP/Contents/MacOS/ATMCompanion"
cp -R "$NATIVE_ROOT/.build/$NATIVE_CONFIGURATION/ATMCompanion_ATMCompanion.bundle" "$NATIVE_APP/Contents/Resources/"
cp "$NATIVE_ROOT/Resources/Info.plist" "$NATIVE_APP/Contents/Info.plist"
chmod +x "$NATIVE_APP/Contents/MacOS/ATMCompanion"
"$NATIVE_ROOT/../../scripts/codesign-local.sh" "$NATIVE_APP"
codesign --verify --strict "$NATIVE_APP"
rm -rf "$NATIVE_ROOT/dist/ATM Menu.app"
mv "$NATIVE_APP" "$NATIVE_ROOT/dist/ATM Menu.app"
print "$NATIVE_ROOT/dist/ATM Menu.app"
