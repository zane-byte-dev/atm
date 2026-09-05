#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/atm-release-check.XXXXXX")"
BUILD_ROOT=""
trap 'rm -rf "$TMP_ROOT" "$BUILD_ROOT"' EXIT HUP INT TERM

cd "$ROOT_DIR"
# Keep executable artifacts in the checkout: local macOS execution policy can
# reject binaries under /tmp. A unique directory never replaces the daily CLI.
mkdir -p "$ROOT_DIR/bin"
BUILD_ROOT="$(mktemp -d "$ROOT_DIR/bin/atm-release-check.XXXXXX")"

GOCACHE="${GOCACHE:-$TMP_ROOT/go-cache}" go test -p 1 ./...
GOCACHE="${GOCACHE:-$TMP_ROOT/go-cache}" go vet ./...
GOCACHE="${GOCACHE:-$TMP_ROOT/go-cache}" go build \
  -trimpath -ldflags "-X main.version=0.0.0-contract" \
  -o "$BUILD_ROOT/atm-cli" ./cmd/atm

test "$("$BUILD_ROOT/atm-cli" version)" = "atm 0.0.0-contract"

npm ci --prefix app/web
npm run check --prefix app/web
npm run test --prefix app/web
npm run build --prefix app/web
test -s app/web/dist/index.html

GOCACHE="${GOCACHE:-$TMP_ROOT/go-cache}" go build \
  -trimpath -tags webui -ldflags "-X main.version=0.0.0-contract" \
  -o "$BUILD_ROOT/atm-web" ./cmd/atm
test "$("$BUILD_ROOT/atm-web" version)" = "atm 0.0.0-contract"

for target in \
  darwin/amd64 darwin/arm64 \
  linux/amd64 linux/arm64 \
  windows/amd64 windows/arm64
do
  target_os="${target%/*}"
  target_arch="${target#*/}"
  suffix=""
  if [ "$target_os" = "windows" ]; then
    suffix=".exe"
  fi
  GOOS="$target_os" GOARCH="$target_arch" CGO_ENABLED=0 \
    GOCACHE="${GOCACHE:-$TMP_ROOT/go-cache}" \
    go build -trimpath -tags webui \
      -o "$BUILD_ROOT/atm-${target_os}-${target_arch}${suffix}" ./cmd/atm
done

grep -Fq 'main: ./cmd/atm' .goreleaser.yaml
grep -Fq 'binary: atm' .goreleaser.yaml
grep -Fq -- '- webui' .goreleaser.yaml
grep -Fq -- '- npm ci --prefix app/web' .goreleaser.yaml
grep -Fq -- '- npm run build --prefix app/web' .goreleaser.yaml
grep -Fq 'https://github.com/${REPO}/releases/latest' install.sh
if grep -Fq 'api.github.com/repos/${REPO}/releases/latest' install.sh; then
  printf '%s\n' "install.sh must not depend on the anonymous GitHub API rate limit" >&2
  exit 1
fi
grep -Fq 'asset="${BIN}_${version}_${os}_${arch}.tar.gz"' install.sh
grep -Fq 'checksums_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"' install.sh
grep -Fq 'actual_checksum" = "$expected_checksum' install.sh

if [ "$(uname -s)" = "Darwin" ]; then
  swift test --package-path app/menubar
  swift build --package-path app/menubar -c release
  swift test --package-path app/voice
  swift build --package-path app/voice -c release
fi

printf '%s\n' "release contract check passed"
