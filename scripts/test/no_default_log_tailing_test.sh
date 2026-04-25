#!/usr/bin/env bash
#
# scripts/test/no_default_log_tailing_test.sh
#
# Phase 2 invariant guard: no bundled default addon may depend on
# ``tail -F`` / ``server-output.log`` / ``server.log`` /
# ``fallback_to_server_log`` as an active runtime event source. The
# supervisor's process-output-only event bus is the only supported
# runtime input. References inside ``bundled-addons/examples/deprecated/``
# and inside doc/comment text that explicitly marks the pattern as
# legacy are tolerated; everything else fails the test.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEFAULTS_DIR="${REPO_ROOT}/bundled-addons/defaults"

PASSED=0
FAILED=0

pass() { PASSED=$((PASSED + 1)); printf '[ OK ] %s\n' "$1"; }
fail() { FAILED=$((FAILED + 1)); printf '[FAIL] %s\n' "$1" >&2; }

# The legacy 40-chatlogger.py at the top of defaults/ retains the
# tail/server.log code paths because operator tooling still calls
# ``--stop`` on it during Phase 2 upgrades. It is exempt from the
# scan but must be marked DEPRECATED in its module docstring.
exempt_legacy() {
  local path="$1"
  case "$path" in
    "${DEFAULTS_DIR}/40-chatlogger.py") return 0 ;;
    *) return 1 ;;
  esac
}

# Per-pattern allowlist for files that legitimately reference a
# pattern in a non-event-source context (e.g. a one-shot status
# helper that *prints* the path to ``server.log`` for diagnostic
# output without ever reading it).
allowlisted_for_pattern() {
  local file="$1"
  local label="$2"
  case "${label}::${file}" in
    "server.log (active read)::${DEFAULTS_DIR}/30-checkserverstatus.sh") return 0 ;;
    *) return 1 ;;
  esac
}

assert_no_pattern() {
  local label="$1"
  local pattern="$2"

  local hits=()
  while IFS= read -r -d '' file; do
    if exempt_legacy "$file"; then
      continue
    fi
    if allowlisted_for_pattern "$file" "$label"; then
      continue
    fi
    # Pre-process the file so docstring / comment references do not
    # count as active runtime use:
    #   * Python triple-quoted docstring blocks at module level are
    #     stripped (heuristic: lines between matched ``"""`` markers).
    #   * Lines whose first non-whitespace character is ``#`` are
    #     dropped (shell comments and Python comments alike).
    # The remaining text is what would actually run; only matches in
    # that text fail the test.
    local code
    code=$(awk '
      BEGIN { in_doc = 0 }
      {
        line = $0
        # Drop pure comment lines.
        ws_stripped = line
        sub(/^[[:space:]]+/, "", ws_stripped)
        if (substr(ws_stripped, 1, 1) == "#") { next }
        # Track triple-quoted Python docstring blocks.
        if (in_doc) {
          if (line ~ /"""/) { in_doc = 0 }
          next
        }
        if (line ~ /^[[:space:]]*"""/) {
          # Same-line open+close docstring (e.g. one-line).
          rest = line
          sub(/^[[:space:]]*"""/, "", rest)
          if (rest ~ /"""/) { next }
          in_doc = 1
          next
        }
        print line
      }
    ' "$file")

    if printf '%s\n' "$code" | grep -qE "$pattern"; then
      hits+=("$file")
    fi
  done < <(find "$DEFAULTS_DIR" -type f \( -name '*.py' -o -name '*.sh' \) -print0)

  if [[ ${#hits[@]} -eq 0 ]]; then
    pass "no '${label}' active references in non-exempt bundled defaults"
  else
    fail "'${label}' still actively referenced in: ${hits[*]}"
  fi
}

assert_no_pattern "tail -F"                  'tail[[:space:]]+-[A-Za-z]*[Ff]'
assert_no_pattern "server-output.log"        'server-output\.log'
assert_no_pattern "server.log (active read)" 'server\.log'
assert_no_pattern "fallback_to_server_log"   'fallback_to_server_log'

# The legacy 40-chatlogger.py must still carry its DEPRECATED marker
# so future maintainers do not re-enable it by accident.
LEGACY="${DEFAULTS_DIR}/40-chatlogger.py"
if [[ -f "$LEGACY" ]] && grep -q 'DEPRECATED' "$LEGACY"; then
  pass "legacy 40-chatlogger.py carries DEPRECATED marker"
else
  fail "legacy 40-chatlogger.py is missing the DEPRECATED marker"
fi

# The Phase 2 event-driven chatlogger must exist and must NOT mention
# any of the forbidden source patterns above.
EVENT_CHATLOGGER="${DEFAULTS_DIR}/events/40-chatlogger.py"
if [[ -f "$EVENT_CHATLOGGER" ]]; then
  pass "event-driven chatlogger present at ${EVENT_CHATLOGGER}"
else
  fail "event-driven chatlogger missing at ${EVENT_CHATLOGGER}"
fi

echo
printf 'Passed: %d   Failed: %d\n' "$PASSED" "$FAILED"
if [[ $FAILED -gt 0 ]]; then
  exit 1
fi
