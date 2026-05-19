#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORK_DIR="$(mktemp -d -t jka-runtime-config-test.XXXXXX)"
trap 'rm -rf "${WORK_DIR}"' EXIT

PASS=0
FAIL=0

pass() { printf '[ OK ] %s\n' "$1"; PASS=$((PASS + 1)); }
fail_assert() { printf '[FAIL] %s\n' "$1" >&2; FAIL=$((FAIL + 1)); }

run_loader() {
  local config_path="$1"
  (
    # shellcheck source=../common/jka_runtime_common.sh
    source "${REPO_ROOT}/scripts/common/jka_runtime_common.sh"
    # shellcheck source=../common/jka_runtime_config.sh
    source "${REPO_ROOT}/scripts/common/jka_runtime_config.sh"

    JKA_RUNTIME_CONFIG_DIR="$(dirname "$config_path")"
    JKA_RUNTIME_CONFIG_PATH="$config_path"
    export JKA_RUNTIME_CONFIG_DIR JKA_RUNTIME_CONFIG_PATH
    load_runtime_json_config >/dev/null
  )
}

assert_jq() {
  local label="$1"
  local path="$2"
  local query="$3"

  if jq -e "$query" "$path" >/dev/null; then
    pass "$label"
  else
    fail_assert "$label"
  fi
}

# Existing user configs from older images should be left intact.
case1="${WORK_DIR}/case1/config/jka-runtime.json"
mkdir -p "$(dirname "$case1")"
cat > "$case1" <<'JSON'
{
  "server": {
    "fs_game": "taystjk",
    "config": "custom.cfg",
    "log_filename": "server.log",
    "port_fallback": 29100
  },
  "supervisor": {
    "enabled": true,
    "debug_startup": true,
    "live_output_mirror_enabled": false
  },
  "anti_vpn": {
    "enabled": true,
    "mode": "block"
  }
}
JSON
run_loader "$case1"
assert_jq "[missing-block] server config preserved" "$case1" '.server.config == "custom.cfg"'
assert_jq "[missing-block] anti-vpn value preserved" "$case1" '.anti_vpn.enabled == true'

printf '\nPassed: %d   Failed: %d\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
