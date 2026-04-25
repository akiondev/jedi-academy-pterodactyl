#!/usr/bin/env bash
#
# scripts/test/no_default_log_tailing_test.sh
#
# Phase 3 invariant guard: no bundled default addon may depend on
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

assert_no_pattern() {
  local label="$1"
  local pattern="$2"

  local hits=()
  while IFS= read -r -d '' file; do
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
        ws_stripped = line
        sub(/^[[:space:]]+/, "", ws_stripped)
        if (substr(ws_stripped, 1, 1) == "#") { next }
        if (in_doc) {
          if (line ~ /"""/) { in_doc = 0 }
          next
        }
        if (line ~ /^[[:space:]]*"""/) {
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
    pass "no '${label}' active references in bundled defaults"
  else
    fail "'${label}' still actively referenced in: ${hits[*]}"
  fi
}

assert_no_pattern "tail -F"                  'tail[[:space:]]+-[A-Za-z]*[Ff]'
assert_no_pattern "server-output.log"        'server-output\.log'
assert_no_pattern "server.log (active read)" 'server\.log'
assert_no_pattern "fallback_to_server_log"   'fallback_to_server_log'

# checkserverstatus must be completely gone from bundled defaults.
if grep -RIn -E 'checkserverstatus' "$DEFAULTS_DIR" >/dev/null 2>&1; then
  fail "checkserverstatus references remain in bundled defaults"
else
  pass "no checkserverstatus references in bundled defaults"
fi

# The Phase 2 event-driven chatlogger must exist.
EVENT_CHATLOGGER="${DEFAULTS_DIR}/events/40-chatlogger.py"
if [[ -f "$EVENT_CHATLOGGER" ]]; then
  pass "event-driven chatlogger present at ${EVENT_CHATLOGGER}"
else
  fail "event-driven chatlogger missing at ${EVENT_CHATLOGGER}"
fi

# The Phase 3 event-driven live team announcer must exist.
EVENT_TEAM_ANNOUNCER="${DEFAULTS_DIR}/events/30-live-team-announcer.py"
if [[ -f "$EVENT_TEAM_ANNOUNCER" ]]; then
  pass "event-driven live team announcer present at ${EVENT_TEAM_ANNOUNCER}"
else
  fail "event-driven live team announcer missing at ${EVENT_TEAM_ANNOUNCER}"
fi

echo
printf 'Passed: %d   Failed: %d\n' "$PASSED" "$FAILED"
if [[ $FAILED -gt 0 ]]; then
  exit 1
fi
