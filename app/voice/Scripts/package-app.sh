#!/bin/zsh
set -euo pipefail

VOICE_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VOICE_APP="$VOICE_ROOT/dist/VoxCaret.app"

"$VOICE_ROOT/Scripts/build-app.sh" >/dev/null

VOICE_VERSION="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$VOICE_APP/Contents/Info.plist")"
VOICE_ARCH="$(/usr/bin/uname -m)"
VOICE_ARCHIVE="$VOICE_ROOT/dist/VoxCaret-$VOICE_VERSION-macos-$VOICE_ARCH.zip"
VOICE_STAGING="$(/usr/bin/mktemp -d "$VOICE_ROOT/dist/.package.XXXXXX")"
trap '/bin/rm -rf "$VOICE_STAGING"' EXIT

/usr/bin/ditto -c -k --sequesterRsrc --keepParent "$VOICE_APP" "$VOICE_STAGING/archive.zip"
/usr/bin/codesign --verify --deep --strict "$VOICE_APP"
/bin/mv -f "$VOICE_STAGING/archive.zip" "$VOICE_ARCHIVE"

/usr/bin/shasum -a 256 "$VOICE_ARCHIVE"
print "$VOICE_ARCHIVE"
