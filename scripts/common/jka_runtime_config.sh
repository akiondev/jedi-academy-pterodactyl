# shellcheck shell=bash
#
# scripts/common/jka_runtime_config.sh
#
# Loads /home/container/config/jka-runtime.json and exports the legacy
# environment variable names the rest of the runtime scripts already
# read. The user-owned JSON file is the single source of truth for
# runtime behavior in the manual-first egg model. Only four panel
# variables remain (COPYRIGHT_ACKNOWLEDGED, EXTRA_STARTUP_ARGS,
# SERVER_BINARY, TAYSTJK_AUTO_UPDATE_BINARY); everything else lives in
# the JSON file.
#
# Behavior:
#   * Ensures /home/container/config exists.
#   * Creates /home/container/config/jka-runtime.json from the template
#     ONLY when the file is missing. An existing user-owned file is
#     never overwritten. No example file is created next to it: the
#     canonical schema/defaults are documented in
#     /home/container/addons/docs/ADDON_README.md.
#   * Reads values through `jq` and exports legacy env names so the
#     rest of the runtime layer keeps working without per-script
#     rewrites.
#   * Never prints provider API keys.
#
# Requires: jka_runtime_common.sh sourced first (for `info`, `warn`,
# `debug`, `fail`).
# Requires: `jq` available on PATH.

JKA_RUNTIME_CONFIG_DIR="${JKA_RUNTIME_CONFIG_DIR:-/home/container/config}"
JKA_RUNTIME_CONFIG_PATH="${JKA_RUNTIME_CONFIG_PATH:-${JKA_RUNTIME_CONFIG_DIR}/jka-runtime.json}"

# jka_runtime_config_template emits the canonical default JSON config.
# Keep this in sync with docs/addons/ADDON_README.md.
jka_runtime_config_template() {
  cat <<'JSON'
{
  "server": {
    "fs_game": "taystjk",
    "config": "server.cfg",
    "log_filename": "server.log",
    "port_fallback": 29070,
    "sync_managed_taystjk_payload": true
  },
  "supervisor": {
    "enabled": true,
    "debug_startup": false,
    "live_output_mirror_enabled": false
  },
  "anti_vpn": {
    "enabled": false,
    "mode": "block",
    "score_threshold": 90,
    "allowlist": [],
    "timeout_ms": 1500,
    "cache_ttl": "6h",
    "cache_flush_interval": "2s",
    "audit_log_path": "/home/container/logs/anti-vpn-audit.log",
    "audit_allow": true,
    "log_decisions": false,
    "providers": {
      "proxycheck_api_key": "",
      "ipapiis_api_key": "",
      "iphub_api_key": "",
      "vpnapi_io_api_key": "",
      "ipqualityscore_api_key": "",
      "iplocate_api_key": ""
    },
    "broadcast": {
      "mode": "pass-and-block",
      "cooldown": "90s",
      "pass_template": "say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%",
      "block_template": "say [Anti-VPN] VPN BLOCKED: %PLAYER% triggered anti-VPN (%SCORE%/%THRESHOLD%). %SUMMARY%"
    },
    "enforcement": {
      "mode": "kick-only",
      "kick_command": "clientkick %SLOT%",
      "ban_command": ""
    }
  },
  "rcon_guard": {
    "enabled": true,
    "action": "kick",
    "broadcast": true,
    "ignore_hosts": ["127.0.0.1", "::1", "localhost"]
  },
  "addons": {
    "enabled": true,
    "directory": "/home/container/addons",
    "strict": false,
    "timeout_seconds": 30,
    "log_output": true,
    "event_bus": {
      "enabled": true,
      "buffer_size": 1000,
      "drop_policy": "drop-oldest"
    }
  }
}
JSON
}

# jka_runtime_config_query_or wraps `jq -r` with a default fallback. The
# default is returned both when the key is missing and when jq returns
# the literal string "null" or an empty string.
jka_runtime_config_query_or() {
  local query="$1"
  local default_value="$2"
  local value=""

  if [[ -z "${JKA_RUNTIME_CONFIG_LOADED:-}" ]]; then
    printf '%s\n' "$default_value"
    return 0
  fi

  value="$(jq -r "$query // empty" "$JKA_RUNTIME_CONFIG_PATH" 2>/dev/null || true)"
  if [[ -z "$value" || "$value" == "null" ]]; then
    printf '%s\n' "$default_value"
  else
    printf '%s\n' "$value"
  fi
}

jka_runtime_config_query_array_csv() {
  local query="$1"

  if [[ -z "${JKA_RUNTIME_CONFIG_LOADED:-}" ]]; then
    return 0
  fi

  jq -r "${query} // [] | join(\",\")" "$JKA_RUNTIME_CONFIG_PATH" 2>/dev/null || true
}

# load_runtime_json_config ensures the config directory and example
# file exist, materialises the user file if it is missing, validates
# the file as JSON, and exports the legacy environment variable names
# read by the rest of the runtime scripts.
#
# The function is safe to call multiple times. It never overwrites an
# existing user-owned jka-runtime.json.
load_runtime_json_config() {
  if ! command -v jq >/dev/null 2>&1; then
    fail "Runtime JSON config loader requires jq, but jq is not available"
  fi

  mkdir -p "$JKA_RUNTIME_CONFIG_DIR"

  # Materialise the user file from the template only when missing so
  # the operator's edits are preserved across container restarts. No
  # ``jka-runtime.example.json`` is created next to it: the canonical
  # schema is documented in /home/container/addons/docs/ADDON_README.md.
  if [[ ! -f "$JKA_RUNTIME_CONFIG_PATH" ]]; then
    info "Creating default runtime config at ${JKA_RUNTIME_CONFIG_PATH}"
    jka_runtime_config_template > "$JKA_RUNTIME_CONFIG_PATH"
  fi

  if ! jq -e . "$JKA_RUNTIME_CONFIG_PATH" >/dev/null 2>&1; then
    fail "Runtime config at ${JKA_RUNTIME_CONFIG_PATH} is not valid JSON"
  fi

  # Best-effort: remove any stale jka-runtime.example.json that an
  # older image revision may have left in the operator volume. We
  # never delete the user-owned file and this branch is a no-op when
  # the example file is absent.
  local stale_example="${JKA_RUNTIME_CONFIG_DIR}/jka-runtime.example.json"
  if [[ -f "$stale_example" ]]; then
    rm -f "$stale_example" 2>/dev/null || true
  fi

  JKA_RUNTIME_CONFIG_LOADED=1
  export JKA_RUNTIME_CONFIG_PATH JKA_RUNTIME_CONFIG_LOADED

  # Server section: these become the legacy env names the rest of the
  # scripts already consume.
  FS_GAME_MOD="$(jka_runtime_config_query_or '.server.fs_game' 'taystjk')"
  SERVER_CONFIG="$(jka_runtime_config_query_or '.server.config' 'server.cfg')"
  SERVER_LOG_FILENAME="$(jka_runtime_config_query_or '.server.log_filename' 'server.log')"
  SERVER_PORT="$(jka_runtime_config_query_or '.server.port_fallback' '29070')"
  JKA_SYNC_MANAGED_TAYSTJK_PAYLOAD="$(jka_runtime_config_query_or '.server.sync_managed_taystjk_payload' 'true')"
  export FS_GAME_MOD SERVER_CONFIG SERVER_LOG_FILENAME SERVER_PORT JKA_SYNC_MANAGED_TAYSTJK_PAYLOAD

  # The new model never writes managed cvars into server.cfg from
  # panel variables. Hardwire the legacy override flag to false so
  # downstream code that still references it stays in user-owned mode.
  SERVER_CFG_OVERRIDES_ENABLED="false"
  SERVER_HOSTNAME=""
  SERVER_MOTD=""
  SERVER_MAXCLIENTS=""
  SERVER_GAMETYPE=""
  SERVER_RCON_PASSWORD=""
  export SERVER_CFG_OVERRIDES_ENABLED SERVER_HOSTNAME SERVER_MOTD SERVER_MAXCLIENTS SERVER_GAMETYPE SERVER_RCON_PASSWORD

  # Supervisor section.
  DEBUG_STARTUP="$(jka_runtime_config_query_or '.supervisor.debug_startup' 'false')"
  local mirror_flag
  mirror_flag="$(jka_runtime_config_query_or '.supervisor.live_output_mirror_enabled' 'false')"
  JKA_LIVE_OUTPUT_MIRROR_ENABLED="$mirror_flag"
  TAYSTJK_LIVE_OUTPUT_ENABLED="$mirror_flag"
  export DEBUG_STARTUP JKA_LIVE_OUTPUT_MIRROR_ENABLED TAYSTJK_LIVE_OUTPUT_ENABLED

  # Anti-VPN section. Provider API keys are exported into the
  # environment so the Go supervisor can pick them up via env fallback,
  # but they MUST NOT be printed to the console anywhere in the shell
  # layer.
  ANTI_VPN_ENABLED="$(jka_runtime_config_query_or '.anti_vpn.enabled' 'false')"
  ANTI_VPN_MODE="$(jka_runtime_config_query_or '.anti_vpn.mode' 'block')"
  ANTI_VPN_SCORE_THRESHOLD="$(jka_runtime_config_query_or '.anti_vpn.score_threshold' '90')"
  ANTI_VPN_ALLOWLIST="$(jka_runtime_config_query_array_csv '.anti_vpn.allowlist')"
  ANTI_VPN_TIMEOUT_MS="$(jka_runtime_config_query_or '.anti_vpn.timeout_ms' '1500')"
  ANTI_VPN_CACHE_TTL="$(jka_runtime_config_query_or '.anti_vpn.cache_ttl' '6h')"
  ANTI_VPN_CACHE_FLUSH_INTERVAL="$(jka_runtime_config_query_or '.anti_vpn.cache_flush_interval' '2s')"
  ANTI_VPN_AUDIT_LOG_PATH="$(jka_runtime_config_query_or '.anti_vpn.audit_log_path' '/home/container/logs/anti-vpn-audit.log')"
  ANTI_VPN_AUDIT_ALLOW="$(jka_runtime_config_query_or '.anti_vpn.audit_allow' 'true')"
  ANTI_VPN_LOG_DECISIONS="$(jka_runtime_config_query_or '.anti_vpn.log_decisions' 'false')"
  ANTI_VPN_PROXYCHECK_API_KEY="$(jka_runtime_config_query_or '.anti_vpn.providers.proxycheck_api_key' '')"
  ANTI_VPN_IPAPIIS_API_KEY="$(jka_runtime_config_query_or '.anti_vpn.providers.ipapiis_api_key' '')"
  ANTI_VPN_IPHUB_API_KEY="$(jka_runtime_config_query_or '.anti_vpn.providers.iphub_api_key' '')"
  ANTI_VPN_VPNAPI_IO_API_KEY="$(jka_runtime_config_query_or '.anti_vpn.providers.vpnapi_io_api_key' '')"
  ANTI_VPN_IPQUALITYSCORE_API_KEY="$(jka_runtime_config_query_or '.anti_vpn.providers.ipqualityscore_api_key' '')"
  ANTI_VPN_IPLOCATE_API_KEY="$(jka_runtime_config_query_or '.anti_vpn.providers.iplocate_api_key' '')"
  ANTI_VPN_BROADCAST_MODE="$(jka_runtime_config_query_or '.anti_vpn.broadcast.mode' 'pass-and-block')"
  ANTI_VPN_BROADCAST_COOLDOWN="$(jka_runtime_config_query_or '.anti_vpn.broadcast.cooldown' '90s')"
  ANTI_VPN_BROADCAST_PASS_TEMPLATE="$(jka_runtime_config_query_or '.anti_vpn.broadcast.pass_template' 'say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%')"
  ANTI_VPN_BROADCAST_BLOCK_TEMPLATE="$(jka_runtime_config_query_or '.anti_vpn.broadcast.block_template' 'say [Anti-VPN] VPN BLOCKED: %PLAYER% triggered anti-VPN (%SCORE%/%THRESHOLD%). %SUMMARY%')"
  ANTI_VPN_ENFORCEMENT_MODE="$(jka_runtime_config_query_or '.anti_vpn.enforcement.mode' 'kick-only')"
  ANTI_VPN_KICK_COMMAND="$(jka_runtime_config_query_or '.anti_vpn.enforcement.kick_command' 'clientkick %SLOT%')"
  ANTI_VPN_BAN_COMMAND="$(jka_runtime_config_query_or '.anti_vpn.enforcement.ban_command' '')"
  export \
    ANTI_VPN_ENABLED ANTI_VPN_MODE ANTI_VPN_SCORE_THRESHOLD ANTI_VPN_ALLOWLIST \
    ANTI_VPN_TIMEOUT_MS ANTI_VPN_CACHE_TTL ANTI_VPN_CACHE_FLUSH_INTERVAL \
    ANTI_VPN_AUDIT_LOG_PATH ANTI_VPN_AUDIT_ALLOW ANTI_VPN_LOG_DECISIONS \
    ANTI_VPN_PROXYCHECK_API_KEY ANTI_VPN_IPAPIIS_API_KEY ANTI_VPN_IPHUB_API_KEY \
    ANTI_VPN_VPNAPI_IO_API_KEY ANTI_VPN_IPQUALITYSCORE_API_KEY ANTI_VPN_IPLOCATE_API_KEY \
    ANTI_VPN_BROADCAST_MODE ANTI_VPN_BROADCAST_COOLDOWN \
    ANTI_VPN_BROADCAST_PASS_TEMPLATE ANTI_VPN_BROADCAST_BLOCK_TEMPLATE \
    ANTI_VPN_ENFORCEMENT_MODE ANTI_VPN_KICK_COMMAND ANTI_VPN_BAN_COMMAND

  # RCON guard.
  RCON_GUARD_ENABLED="$(jka_runtime_config_query_or '.rcon_guard.enabled' 'true')"
  RCON_GUARD_ACTION="$(jka_runtime_config_query_or '.rcon_guard.action' 'kick')"
  RCON_GUARD_BROADCAST="$(jka_runtime_config_query_or '.rcon_guard.broadcast' 'true')"
  RCON_GUARD_IGNORE_HOSTS="$(jka_runtime_config_query_array_csv '.rcon_guard.ignore_hosts')"
  if [[ -z "$RCON_GUARD_IGNORE_HOSTS" ]]; then
    RCON_GUARD_IGNORE_HOSTS="127.0.0.1,::1,localhost"
  fi
  export RCON_GUARD_ENABLED RCON_GUARD_ACTION RCON_GUARD_BROADCAST RCON_GUARD_IGNORE_HOSTS

  # Addons (runtime-wide settings only; per-addon enabled/config lives
  # in /home/container/config/jka-addons.json and is loaded by
  # load_addons_json_config in jka_addon_loader.sh).
  ADDONS_ENABLED="$(jka_runtime_config_query_or '.addons.enabled' 'true')"
  ADDONS_DIR="$(jka_runtime_config_query_or '.addons.directory' '/home/container/addons')"
  ADDONS_STRICT="$(jka_runtime_config_query_or '.addons.strict' 'false')"
  ADDONS_TIMEOUT_SECONDS="$(jka_runtime_config_query_or '.addons.timeout_seconds' '30')"
  ADDONS_LOG_OUTPUT="$(jka_runtime_config_query_or '.addons.log_output' 'true')"
  ADDON_EVENT_BUS_ENABLED="$(jka_runtime_config_query_or '.addons.event_bus.enabled' 'true')"
  ADDON_EVENT_BUS_BUFFER_SIZE="$(jka_runtime_config_query_or '.addons.event_bus.buffer_size' '1000')"
  ADDON_EVENT_BUS_DROP_POLICY="$(jka_runtime_config_query_or '.addons.event_bus.drop_policy' 'drop-oldest')"
  export \
    ADDONS_ENABLED ADDONS_DIR \
    ADDONS_STRICT ADDONS_TIMEOUT_SECONDS ADDONS_LOG_OUTPUT \
    ADDON_EVENT_BUS_ENABLED ADDON_EVENT_BUS_BUFFER_SIZE ADDON_EVENT_BUS_DROP_POLICY
}
