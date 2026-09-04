#!/bin/zsh
set -euo pipefail
HANDOVER_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
"$HANDOVER_ROOT/Scripts/build-app.sh"
# Package the same guarded Release executable for the existing Dev bundle ID.
# Never touch .build/dev-app, which may still be the user's running application.
HANDOVER_DEV="$HANDOVER_ROOT/dist/ATM Dev.app"
rm -rf "$HANDOVER_DEV"
cp -R "$HANDOVER_ROOT/dist/ATM.app" "$HANDOVER_DEV"
mv "$HANDOVER_DEV/Contents/MacOS/ATM" "$HANDOVER_DEV/Contents/MacOS/ATMMenuBarApp"
cp "$HANDOVER_ROOT/Resources/DebugInfo.plist" "$HANDOVER_DEV/Contents/Info.plist"
"$HANDOVER_ROOT/../../scripts/codesign-local.sh" --deep "$HANDOVER_DEV"
codesign --verify --strict "$HANDOVER_DEV"
print "$HANDOVER_DEV"
