#!/usr/bin/env bash
set -u

# Codex SessionStart hook: inject only the current binding or a few local
# candidates. The full attention dashboard remains available through `atm now`.
payload=$(command cat)
session_id=${CODEX_THREAD_ID:-}
hook_cwd=""

if command -v jq >/dev/null 2>&1; then
	parsed_session=$(printf '%s' "$payload" | jq -r '.session_id // .sessionId // .conversation_id // empty' 2>/dev/null || true)
	parsed_cwd=$(printf '%s' "$payload" | jq -r '.cwd // .data.cwd // empty' 2>/dev/null || true)
	if [[ -n "$parsed_session" ]]; then session_id=$parsed_session; fi
	if [[ -n "$parsed_cwd" ]]; then hook_cwd=$parsed_cwd; fi
fi

atm_bin=${ATM_BIN:-/usr/local/bin/atm}
args=(todo match --prompt --limit 3)
if [[ -n "$session_id" ]]; then args+=(--agent-session "$session_id"); fi

if [[ -n "$hook_cwd" && -d "$hook_cwd" ]]; then
	(cd "$hook_cwd" && "$atm_bin" "${args[@]}" 2>/dev/null) || true
else
	"$atm_bin" "${args[@]}" 2>/dev/null || true
fi
