#!/bin/zsh
set -euo pipefail

SIGN_IDENTITY="${ATM_CODESIGN_IDENTITY:-}"
if [[ -z "$SIGN_IDENTITY" ]]; then
    SIGN_IDENTITY="$(/usr/bin/security find-identity -v -p codesigning 2>/dev/null | /usr/bin/awk '/Apple Development:/{print $2; exit}')"
fi
if [[ -z "$SIGN_IDENTITY" ]]; then
    SIGN_IDENTITY="-"
fi

TARGET="${@: -1}"
if [[ -f "$TARGET" && ! -L "$TARGET" ]]; then
    # Sign a fresh inode and atomically install it. Some managed macOS hosts
    # continue to quarantine an inode created directly by the Go linker even
    # after it receives a valid local signature.
    SIGNING_COPY="$(/usr/bin/mktemp "${TARGET}.signing.XXXXXX")"
    trap '/bin/rm -f "$SIGNING_COPY"' EXIT
    /bin/cp -p "$TARGET" "$SIGNING_COPY"
    PREFIX_ARGS=()
    if (( $# > 1 )); then
        PREFIX_ARGS=("${@:1:$#-1}")
    fi
    /usr/bin/codesign --force --options runtime --identifier "${TARGET:t}" --sign "$SIGN_IDENTITY" "${PREFIX_ARGS[@]}" "$SIGNING_COPY"
    /bin/mv -f "$SIGNING_COPY" "$TARGET"
    trap - EXIT
else
    /usr/bin/codesign --force --options runtime --sign "$SIGN_IDENTITY" "$@"
fi
