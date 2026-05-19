#!/usr/bin/env bash
#
# scripts/test/addon_layout_test.sh
#
# Verifies the new TaystJK modern64 addon folder layout invariants:
#
#   * bundled-addons/defaults has no events/ subdirectory
#   * bundled-addons/defaults has no numeric-prefixed addon files
#   * bundled-addons/defaults has no per-addon *.config.json files
#   * the canonical default scripts exist with their flat names
#   * no active source/runtime/script/doc references to:
#       - ADDON_EVENT_ADDONS_DIR
#       - /addons/events
#       - defaults/events
#       - jka-runtime.example.json
#   * scripts/common/jka_runtime_config.sh does not write
#     jka-runtime.example.json
#   * the Go addon runner uses ConfigPath / DefaultsDir instead of
#     scanning a separate event-addons dir
#
# Run from the repository root:
#   bash scripts/test/addon_layout_test.sh
set -u

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

PASS=0
FAIL=0

pass() { printf '[ OK ] %s\n' "$1"; PASS=$((PASS + 1)); }
fail() { printf '[FAIL] %s\n' "$1" >&2; FAIL=$((FAIL + 1)); }

# ---- bundled-addons/defaults layout ---------------------------------------

if [[ -d bundled-addons/defaults/events ]]; then
  fail "bundled-addons/defaults/events still exists"
else
  pass "bundled-addons/defaults has no events subdirectory"
fi

numeric_hits="$(find bundled-addons/defaults -maxdepth 2 -type f \
  \( -name '[0-9][0-9]-*' \) -print 2>/dev/null || true)"
if [[ -n "$numeric_hits" ]]; then
  fail "bundled-addons/defaults still contains numeric-prefixed files: ${numeric_hits//$'\n'/, }"
else
  pass "bundled-addons/defaults has no numeric-prefixed addon files"
fi

config_json_hits="$(find bundled-addons/defaults -maxdepth 2 -type f \
  -name '*.config.json' -print 2>/dev/null || true)"
if [[ -n "$config_json_hits" ]]; then
  fail "bundled-addons/defaults still contains per-addon *.config.json files: ${config_json_hits//$'\n'/, }"
else
  pass "bundled-addons/defaults has no per-addon *.config.json files"
fi

for expected in \
  bundled-addons/defaults/announcer.py \
  bundled-addons/defaults/live-team-announcer.py \
  bundled-addons/defaults/chatlogger.py; do
  if [[ -f "$expected" ]]; then
    pass "expected default file present: $expected"
  else
    fail "expected default file missing: $expected"
  fi
done

# announcer.messages.txt was removed; verify it no longer exists in the image
if [[ -f "bundled-addons/defaults/announcer.messages.txt" ]]; then
  fail "announcer.messages.txt must not exist in bundled-addons/defaults/ (messages live in jka-addons.json)"
else
  pass "bundled-addons/defaults/announcer.messages.txt does not exist (correct)"
fi

# ---- docs layout ----------------------------------------------------------

doc_count="$(find docs/addons -maxdepth 1 -type f -name '*.md' 2>/dev/null | wc -l | tr -d ' ')"
if [[ "$doc_count" == "1" && -f docs/addons/ADDON_README.md ]]; then
  pass "docs/addons contains exactly one file (ADDON_README.md)"
else
  fail "docs/addons does not contain exactly one ADDON_README.md (count=${doc_count})"
fi

# ---- runtime / Go references ----------------------------------------------

# Check there are no *active* references to the obsolete strings. We
# scan source files only, excluding the test that defines these
# invariants and known historical/changelog material.
scan_paths=(
  scripts/entrypoint.sh
  scripts/install_taystjk.sh
  scripts/common
  internal
  cmd
  egg
  bundled-addons/defaults
  docs/addons
  docs/anti-vpn.md
  docs/operator-sheet.md
  docs/panel-testing.md
)

check_no_active() {
  local needle="$1"
  local label="$2"
  local hit
  hit="$(grep -rIn --exclude='addon_layout_test.sh' "$needle" "${scan_paths[@]}" 2>/dev/null || true)"
  # The runtime ships a one-shot legacy cleanup path that
  # intentionally references the obsolete strings. Filter those
  # out so the invariant only fails on *active* uses.
  hit="$(printf '%s\n' "$hit" \
    | grep -v 'cleanup_legacy_addon_paths' \
    | grep -v 'legacy event-addons' \
    | grep -v 'legacy defaults/events' \
    | grep -v 'former event-addon' \
    | grep -v 'former numeric-prefixed' \
    | grep -v 'stale jka-runtime.example' \
    | grep -v 'stale_example' \
    | grep -v 'jka-runtime.example.json that an' \
    | grep -v 'is created next to it' \
    | grep -v 'ADDON_DEFAULTS_DIR}/20-python-announcer' \
    | grep -v 'ADDON_DEFAULTS_DIR}/30-live-team-announcer' \
    | grep -v 'ADDON_DEFAULTS_DIR}/40-chatlogger' \
    | grep -v 'There is no .*/addons/events/' \
    | grep -v 'no .addons/events. symlink layer' \
    | grep -v 'addons/events.,$' \
    | grep -v '\`defaults/events/\` subdirectory' \
    | grep -v 'addons/defaults/events.,' \
    | grep -v '20-python-announcer\.\*' \
    | grep -v '30-live-team-announcer\.\*' \
    | grep -v '40-chatlogger\.\*' \
    || true)"
  hit="$(printf '%s\n' "$hit" | sed '/^$/d')"
  if [[ -n "$hit" ]]; then
    fail "found active references to ${label}:"
    printf '%s\n' "$hit" | sed 's/^/        /' >&2
  else
    pass "no active references to ${label}"
  fi
}

check_no_active 'ADDON_EVENT_ADDONS_DIR' 'ADDON_EVENT_ADDONS_DIR'
check_no_active '/addons/events' '/addons/events'
check_no_active 'defaults/events' 'defaults/events'
check_no_active 'jka-runtime.example' 'jka-runtime.example.json'
check_no_active '20-python-announcer' '20-python-announcer'
check_no_active '30-live-team-announcer' '30-live-team-announcer'
check_no_active '40-chatlogger' '40-chatlogger'

# ---- jka_runtime_config.sh must not write jka-runtime.example.json --------

if grep -q 'jka_runtime_config_template > .*\.example' scripts/common/jka_runtime_config.sh; then
  fail "scripts/common/jka_runtime_config.sh still writes a jka-runtime.example.json file"
else
  pass "scripts/common/jka_runtime_config.sh does not write jka-runtime.example.json"
fi

# ---- AddonRunnerConfig must use ConfigPath/DefaultsDir, not AddonsDir -----

if grep -q 'AddonsDir ' internal/antivpn/addonrunner.go; then
  fail "AddonRunnerConfig still has an AddonsDir field (should be DefaultsDir + ConfigPath)"
else
  pass "AddonRunnerConfig uses DefaultsDir + ConfigPath"
fi

printf '\nPassed: %d   Failed: %d\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
