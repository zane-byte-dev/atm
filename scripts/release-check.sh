#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/atm-release-check.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT HUP INT TERM

cd "$ROOT_DIR"

GOCACHE="${GOCACHE:-$TMP_ROOT/go-cache}" go test -p 1 ./...
GOCACHE="${GOCACHE:-$TMP_ROOT/go-cache}" go vet ./...
GOCACHE="${GOCACHE:-$TMP_ROOT/go-cache}" go build \
  -trimpath -ldflags "-X main.version=0.0.0-contract" \
  -o "$TMP_ROOT/atm" ./cmd/atm

test "$("$TMP_ROOT/atm" version)" = "atm 0.0.0-contract"

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
    go build -trimpath -o "$TMP_ROOT/atm-${target_os}-${target_arch}${suffix}" ./cmd/atm
done

grep -Fq 'main: ./cmd/atm' .goreleaser.yaml
grep -Fq 'binary: atm' .goreleaser.yaml
grep -Fq 'asset="${BIN}_${version}_${os}_${arch}.tar.gz"' install.sh
grep -Fq 'checksums_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"' install.sh
grep -Fq 'actual_checksum" = "$expected_checksum' install.sh

if [ "$(uname -s)" = "Darwin" ]; then
  ATM_CONTRACT_EXECUTABLE="$TMP_ROOT/atm" swift test --package-path app/macos
  swift build --package-path app/macos -c release
fi

printf '%s\n' "release contract check passed"
