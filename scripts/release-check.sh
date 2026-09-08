#!/bin/sh
set -eu

usage() {
  printf '%s\n' \
    'Usage: scripts/release-check.sh [go|web|dev|native|release]' \
    '  go       Go tests, vet, CLI build, and release configuration contracts' \
    '  web      Frontend tests, production assets, and embedded Go build' \
    '  dev      Development supervisor lifecycle tests' \
    '  native   macOS Swift tests and signed .app packaging checks' \
    '  release  All portable checks and six cross-builds; native checks on macOS (default)'
}

STAGE="${1:-release}"
if [ "$#" -gt 1 ]; then usage >&2; exit 2; fi
case "$STAGE" in
  go|web|dev|native|release) ;;
  -h|--help) usage; exit 0 ;;
  *) usage >&2; exit 2 ;;
esac

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/atm-release-check.XXXXXX")"
BUILD_ROOT=""
trap 'rm -rf "$TMP_ROOT" "$BUILD_ROOT"' EXIT HUP INT TERM
cd "$ROOT_DIR"

# Keep executable artifacts in the checkout: managed macOS execution policies
# can reject binaries under /tmp. Never replace the daily CLI or native dist apps.
mkdir -p "$ROOT_DIR/bin"
BUILD_ROOT="$(mktemp -d "$ROOT_DIR/bin/atm-release-check.XXXXXX")"
# Respect the caller's GOCACHE or Go's normal cache (including setup-go's cache).

check_version() {
  if [ "$(uname -s)" = Darwin ]; then
    ./scripts/codesign-local.sh "$1"
  fi
  test "$("$1" version)" = 'atm 0.0.0-contract'
}

check_release_configuration() {
  grep -Fq 'main: ./cmd/atm' .goreleaser.yaml
  grep -Fq 'binary: atm' .goreleaser.yaml
  grep -Fq -- '- webui' .goreleaser.yaml
  grep -Fq -- '- npm ci --prefix app/web' .goreleaser.yaml
  grep -Fq -- '- npm run build --prefix app/web' .goreleaser.yaml
  grep -Fq 'https://github.com/${REPO}/releases/latest' install.sh
  if grep -Fq 'api.github.com/repos/${REPO}/releases/latest' install.sh; then
    printf '%s\n' 'install.sh must not depend on the anonymous GitHub API rate limit' >&2
    exit 1
  fi
  grep -Fq 'asset="${BIN}_${version}_${os}_${arch}.tar.gz"' install.sh
  grep -Fq 'checksums_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"' install.sh
  grep -Fq 'actual_checksum" = "$expected_checksum' install.sh
}

check_go() {
  go test -p 1 ./...
  go vet ./...
  mkdir -p "$BUILD_ROOT/cli"
  go build -trimpath -ldflags '-X main.version=0.0.0-contract' \
    -o "$BUILD_ROOT/cli/atm" ./cmd/atm
  check_version "$BUILD_ROOT/cli/atm"
  check_release_configuration
}

check_web() {
  npm ci --prefix app/web
  npm run test --prefix app/web
  # The build script includes tsc --noEmit before compiling production assets.
  npm run build --prefix app/web
  test -s app/web/dist/index.html
  mkdir -p "$BUILD_ROOT/web"
  go build -trimpath -tags webui -ldflags '-X main.version=0.0.0-contract' \
    -o "$BUILD_ROOT/web/atm" ./cmd/atm
  check_version "$BUILD_ROOT/web/atm"
}

check_dev() {
  node --test scripts/dev.test.mjs
}

check_cross_builds() {
  for target in \
    darwin/amd64 darwin/arm64 \
    linux/amd64 linux/arm64 \
    windows/amd64 windows/arm64
  do
    target_os="${target%/*}"
    target_arch="${target#*/}"
    suffix=""
    if [ "$target_os" = windows ]; then suffix='.exe'; fi
    printf 'Cross-building %s\n' "$target"
    GOOS="$target_os" GOARCH="$target_arch" CGO_ENABLED=0 \
      go build -trimpath -tags webui \
        -o "$BUILD_ROOT/atm-${target_os}-${target_arch}${suffix}" ./cmd/atm
  done
}

verify_app() {
  app_path="$1"
  app_executable="$2"
  app_identifier="$3"
  app_plist="$app_path/Contents/Info.plist"
  /usr/bin/plutil -lint "$app_plist"
  test "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$app_plist")" = "$app_executable"
  test "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app_plist")" = "$app_identifier"
  test "$(/usr/libexec/PlistBuddy -c 'Print :CFBundlePackageType' "$app_plist")" = APPL
  test -x "$app_path/Contents/MacOS/$app_executable"
  /usr/bin/codesign --verify --deep --strict "$app_path"
  /usr/bin/codesign -d --verbose=4 "$app_path" 2> "$TMP_ROOT/signature.txt"
  grep -Eq 'flags=.*runtime' "$TMP_ROOT/signature.txt"
}

check_native() {
  if [ "$(uname -s)" != Darwin ]; then
    printf '%s\n' 'The native stage requires macOS and Xcode.' >&2
    exit 1
  fi
  # Keep compiler caches writable in restricted local checkouts as well as CI.
  CLANG_MODULE_CACHE_PATH="${CLANG_MODULE_CACHE_PATH:-$ROOT_DIR/.build/native-module-cache}"
  SWIFTPM_MODULECACHE_OVERRIDE="${SWIFTPM_MODULECACHE_OVERRIDE:-$CLANG_MODULE_CACHE_PATH}"
  export CLANG_MODULE_CACHE_PATH SWIFTPM_MODULECACHE_OVERRIDE
  mkdir -p "$CLANG_MODULE_CACHE_PATH" "$SWIFTPM_MODULECACHE_OVERRIDE"
  for native_package in menubar voice; do
    swift test --package-path "app/$native_package" \
      --scratch-path "app/$native_package/.build" \
      --cache-path "app/$native_package/.cache" --disable-sandbox
  done

  # Ad hoc signing is sufficient to validate packaging without CI certificates.
  # Explicit local signing identities remain available to release maintainers.
  ATM_MENU_OUTPUT_DIR="$BUILD_ROOT/native" ATM_BUILD_CONFIGURATION=release \
    ATM_CODESIGN_IDENTITY="${ATM_CODESIGN_IDENTITY:--}" \
    app/menubar/Scripts/build-app.sh
  VOXCARET_OUTPUT_DIR="$BUILD_ROOT/native" VOXCARET_BUILD_CONFIGURATION=release \
    VOXCARET_CODESIGN_IDENTITY="${VOXCARET_CODESIGN_IDENTITY:-${ATM_CODESIGN_IDENTITY:--}}" \
    app/voice/Scripts/build-app.sh

  menu_app="$BUILD_ROOT/native/ATM Menu.app"
  voice_app="$BUILD_ROOT/native/VoxCaret.app"
  verify_app "$menu_app" ATMCompanion dev.zanebyte.atm.companion
  verify_app "$voice_app" VoxCaret dev.zanebyte.voxcaret
  test "$(/usr/libexec/PlistBuddy -c 'Print :LSUIElement' "$menu_app/Contents/Info.plist")" = true

  # SwiftPM flattens processed resources; verify the actual packaged copies.
  for menu_resource in \
    app/menubar/Sources/ATMCompanion/Resources/Sounds/*.wav \
    app/menubar/Sources/ATMCompanion/Resources/ThirdParty/*
  do
    cmp "$menu_resource" "$menu_app/Contents/Resources/ATMCompanion_ATMCompanion.bundle/$(basename "$menu_resource")"
  done
  test -s "$voice_app/Contents/Resources/Assets.car"
  test -s "$voice_app/Contents/Resources/AppIcon.icns"
  test "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIconFile' "$voice_app/Contents/Info.plist")" = AppIcon
  for usage_key in NSMicrophoneUsageDescription NSSpeechRecognitionUsageDescription; do
    test -n "$(/usr/libexec/PlistBuddy -c "Print :$usage_key" "$voice_app/Contents/Info.plist")"
  done
  cmp app/voice/Resources/zh-Hans.lproj/InfoPlist.strings \
    "$voice_app/Contents/Resources/zh-Hans.lproj/InfoPlist.strings"
  /usr/bin/codesign -d --entitlements :- "$voice_app" \
    > "$TMP_ROOT/voice-entitlements.plist" 2> "$TMP_ROOT/voice-signature.txt"
  /usr/bin/plutil -lint "$TMP_ROOT/voice-entitlements.plist"
  test "$(/usr/libexec/PlistBuddy -c 'Print :com.apple.security.device.audio-input' "$TMP_ROOT/voice-entitlements.plist")" = true
  printf '%s\n' 'Packaged apps verified. Microphone, system permissions, hotkeys, and target-app insertion require manual acceptance.'
}

case "$STAGE" in
  go) check_go ;;
  web) check_web ;;
  dev) check_dev ;;
  native) check_native ;;
  release)
    check_go
    check_web
    check_dev
    check_cross_builds
    if [ "$(uname -s)" = Darwin ]; then check_native; fi
    ;;
esac
printf 'Checks passed: %s\n' "$STAGE"
