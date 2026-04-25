# shellcheck shell=bash
#
# scripts/common/jka_server_cfg.sh
#
# PR-A skeleton: server.cfg cvar I/O and runtime-state writers.
# Functions are textually copied from scripts/entrypoint.sh and are NOT
# sourced by the runtime yet (see scripts/common/README.md).
#
# Requires: jka_runtime_common.sh sourced first (for `fail`, `warn`,
# `info`), jka_security.sh for `require_no_newlines` /
# `normalize_optional_boolean`.
#
# Note: the TaystJK-specific names below (function name
# `ensure_managed_taystjk_server_config`, env var prefix `TAYSTJK_`,
# defaults like "TaystJK Pterodactyl Server") are intentionally
# preserved verbatim in PR-A. They will be neutralized in later PRs.

escape_server_cfg_value() {
  local value="$1"
  printf '%s' "$value" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

read_server_cvar() {
  local config_path="$1"
  local cvar="$2"
  local line=""

  [[ "$cvar" =~ ^[A-Za-z0-9_]+$ ]] || fail "Invalid server cvar key: ${cvar}"
  [[ -f "$config_path" ]] || return 0

  while IFS= read -r line; do
    if [[ "$line" =~ ^[[:space:]]*set[a-z]*[[:space:]]+$cvar[[:space:]]+\"([^\"]*)\" ]]; then
      printf '%s\n' "${BASH_REMATCH[1]}"
      return 0
    fi
    if [[ "$line" =~ ^[[:space:]]*set[a-z]*[[:space:]]+$cvar[[:space:]]+([^[:space:]]+) ]]; then
      printf '%s\n' "${BASH_REMATCH[1]}"
      return 0
    fi
  done < "$config_path"
}

upsert_server_cvar() {
  local config_path="$1"
  local cvar="$2"
  local value="$3"
  local escaped_value=""
  local replacement=""
  local temp_file=""

  [[ "$cvar" =~ ^[A-Za-z0-9_]+$ ]] || fail "Invalid server cvar key: ${cvar}"
  escaped_value="$(escape_server_cfg_value "$value")"
  replacement="seta ${cvar} \"${escaped_value}\""
  temp_file="$(mktemp)"

  awk -v target="$cvar" -v replacement="$replacement" '
    BEGIN {
      IGNORECASE = 1
      replaced = 0
    }
    {
      if ($0 ~ "^[[:space:]]*set[a-z]*[[:space:]]+" target "([[:space:]]+|$)") {
        if (!replaced) {
          print replacement
          replaced = 1
        }
        next
      }
      print
    }
    END {
      if (!replaced) {
        print replacement
      }
    }
  ' "$config_path" > "$temp_file"

  cat "$temp_file" > "$config_path"
  rm -f "$temp_file"
}

write_runtime_state() {
  local runtime_dir="/home/container/.runtime"
  local runtime_env_path="${runtime_dir}/taystjk-effective.env"
  local runtime_json_path="${runtime_dir}/taystjk-effective.json"
  local overrides_enabled_json="false"
  local live_output_enabled_json="false"

  mkdir -p "$runtime_dir"
  chmod 700 "$runtime_dir"

  if [[ "$SERVER_CFG_OVERRIDES_ENABLED" == "true" ]]; then
    overrides_enabled_json="true"
  fi

  if [[ "$TAYSTJK_LIVE_OUTPUT_ENABLED" == "true" ]]; then
    live_output_enabled_json="true"
  fi

  if [[ -n "$TAYSTJK_LIVE_OUTPUT_PATH" ]]; then
    mkdir -p "$(dirname "$TAYSTJK_LIVE_OUTPUT_PATH")"
  fi

  {
    printf 'TAYSTJK_ACTIVE_MOD_DIR=%q\n' "$TAYSTJK_ACTIVE_MOD_DIR"
    printf 'TAYSTJK_ACTIVE_SERVER_CONFIG=%q\n' "$TAYSTJK_ACTIVE_SERVER_CONFIG"
    printf 'TAYSTJK_ACTIVE_SERVER_CONFIG_PATH=%q\n' "$TAYSTJK_ACTIVE_SERVER_CONFIG_PATH"
    printf 'TAYSTJK_ACTIVE_SERVER_LOG_PATH=%q\n' "$TAYSTJK_ACTIVE_SERVER_LOG_PATH"
    printf 'TAYSTJK_SERVER_CFG_OVERRIDES_ENABLED=%q\n' "$TAYSTJK_SERVER_CFG_OVERRIDES_ENABLED"
    printf 'TAYSTJK_EFFECTIVE_SERVER_BINARY=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_BINARY"
    printf 'TAYSTJK_EFFECTIVE_SERVER_PORT=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_PORT"
    printf 'TAYSTJK_EFFECTIVE_SERVER_HOSTNAME=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_HOSTNAME"
    printf 'TAYSTJK_EFFECTIVE_SERVER_MOTD=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_MOTD"
    printf 'TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS"
    printf 'TAYSTJK_EFFECTIVE_SERVER_GAMETYPE=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_GAMETYPE"
    printf 'TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD"
    printf 'TAYSTJK_LIVE_OUTPUT_ENABLED=%q\n' "$TAYSTJK_LIVE_OUTPUT_ENABLED"
    printf 'TAYSTJK_LIVE_OUTPUT_MODE=%q\n' "$TAYSTJK_LIVE_OUTPUT_MODE"
    printf 'TAYSTJK_LIVE_OUTPUT_SOURCE=%q\n' "$TAYSTJK_LIVE_OUTPUT_SOURCE"
    printf 'TAYSTJK_LIVE_OUTPUT_FORMAT=%q\n' "$TAYSTJK_LIVE_OUTPUT_FORMAT"
    printf 'TAYSTJK_LIVE_OUTPUT_PATH=%q\n' "$TAYSTJK_LIVE_OUTPUT_PATH"
    printf 'TAYSTJK_LIVE_OUTPUT_MAX_BYTES=%q\n' "$TAYSTJK_LIVE_OUTPUT_MAX_BYTES"
  } > "$runtime_env_path"
  chmod 600 "$runtime_env_path"

  jq -n \
    --arg active_mod_dir "$TAYSTJK_ACTIVE_MOD_DIR" \
    --arg active_server_config "$TAYSTJK_ACTIVE_SERVER_CONFIG" \
    --arg active_server_config_path "$TAYSTJK_ACTIVE_SERVER_CONFIG_PATH" \
    --arg active_server_log_path "$TAYSTJK_ACTIVE_SERVER_LOG_PATH" \
    --arg effective_server_binary "$TAYSTJK_EFFECTIVE_SERVER_BINARY" \
    --arg effective_server_port "$TAYSTJK_EFFECTIVE_SERVER_PORT" \
    --arg effective_server_hostname "$TAYSTJK_EFFECTIVE_SERVER_HOSTNAME" \
    --arg effective_server_motd "$TAYSTJK_EFFECTIVE_SERVER_MOTD" \
    --arg effective_server_maxclients "$TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS" \
    --arg effective_server_gametype "$TAYSTJK_EFFECTIVE_SERVER_GAMETYPE" \
    --arg live_output_mode "$TAYSTJK_LIVE_OUTPUT_MODE" \
    --arg live_output_source "$TAYSTJK_LIVE_OUTPUT_SOURCE" \
    --arg live_output_format "$TAYSTJK_LIVE_OUTPUT_FORMAT" \
    --arg live_output_path "$TAYSTJK_LIVE_OUTPUT_PATH" \
    --arg live_output_max_bytes "$TAYSTJK_LIVE_OUTPUT_MAX_BYTES" \
    --argjson server_cfg_overrides_enabled "$overrides_enabled_json" \
    --argjson live_output_enabled "$live_output_enabled_json" \
    '{
      active_mod_dir: $active_mod_dir,
      active_server_config: $active_server_config,
      active_server_config_path: $active_server_config_path,
      active_server_log_path: $active_server_log_path,
      server_cfg_overrides_enabled: $server_cfg_overrides_enabled,
      effective_server_binary: $effective_server_binary,
      effective_server_port: $effective_server_port,
      effective_server_hostname: $effective_server_hostname,
      effective_server_motd: $effective_server_motd,
      effective_server_maxclients: $effective_server_maxclients,
      effective_server_gametype: $effective_server_gametype,
      live_output: {
        enabled: $live_output_enabled,
        mode: $live_output_mode,
        source: $live_output_source,
        format: $live_output_format,
        path: $live_output_path,
        max_bytes: $live_output_max_bytes
      }
    }' > "$runtime_json_path"
  chmod 600 "$runtime_json_path"
}

configure_server_settings() {
  # The manual-first egg model never writes panel-supplied cvars into
  # server.cfg. Hostname/MOTD/maxclients/gametype/rconpassword are
  # owned by the operator's server.cfg and are read from there at
  # resolve time. This function now only validates the port fallback
  # sourced from .server.port_fallback in jka-runtime.json.
  SERVER_CFG_OVERRIDES_ENABLED="false"
  SERVER_HOSTNAME=""
  SERVER_MOTD=""
  SERVER_MAXCLIENTS=""
  SERVER_GAMETYPE=""
  SERVER_RCON_PASSWORD=""

  if [[ ! "$SERVER_PORT" =~ ^[0-9]+$ || "$SERVER_PORT" -lt 1 || "$SERVER_PORT" -gt 65535 ]]; then
    warn "server.port_fallback=${SERVER_PORT} is invalid, falling back to 29070"
    SERVER_PORT="29070"
  fi
}

configure_server_logging() {
  : "${SERVER_LOG_FILENAME:=server.log}"
  require_safe_component "$SERVER_LOG_FILENAME" "SERVER_LOG_FILENAME"
}

active_server_log_path() {
  printf '/home/container/%s/%s\n' "$active_game_dir" "$SERVER_LOG_FILENAME"
}

# Live-output addon mirror configuration. The supervisor mirrors every
# stdout/stderr line it scans into a runtime-managed file under
# /home/container/.runtime/live so addons can subscribe to live server
# output without having to scrape Pterodactyl console output. Defaults
# can be overridden by the operator via TAYSTJK_LIVE_OUTPUT_* environment
# variables.
configure_live_output_settings() {
  : "${TAYSTJK_LIVE_OUTPUT_PATH:=/home/container/.runtime/live/server-output.log}"
  : "${TAYSTJK_LIVE_OUTPUT_MAX_BYTES:=10485760}"
  : "${TAYSTJK_LIVE_OUTPUT_FORMAT:=lines}"
  : "${TAYSTJK_LIVE_OUTPUT_SOURCE:=stdout-first}"

  if [[ ! "$TAYSTJK_LIVE_OUTPUT_MAX_BYTES" =~ ^[0-9]+$ ]]; then
    warn "TAYSTJK_LIVE_OUTPUT_MAX_BYTES=${TAYSTJK_LIVE_OUTPUT_MAX_BYTES} is invalid, falling back to 10485760"
    TAYSTJK_LIVE_OUTPUT_MAX_BYTES="10485760"
  fi

  require_safe_container_path "$TAYSTJK_LIVE_OUTPUT_PATH" "TAYSTJK_LIVE_OUTPUT_PATH"
}

resolve_effective_live_output_settings() {
  # Live-output mirroring is OFF by default in the new
  # process-output-only architecture: the supervisor reads the
  # dedicated server's stdout/stderr exactly once and dispatches
  # parsed events to in-process modules (anti-VPN, RCON guard, addon
  # event bus). Bundled addons no longer tail this file.
  #
  # The mirror remains available as an explicit debug/export feature
  # for operators who want a tailable live-output file. Enable it by
  # setting JKA_LIVE_OUTPUT_MIRROR_ENABLED=true in the Pterodactyl
  # startup environment. The legacy TAYSTJK_LIVE_OUTPUT_ENABLED name
  # is accepted as a deprecated alias when the canonical variable is
  # not set.
  local mirror_flag="${JKA_LIVE_OUTPUT_MIRROR_ENABLED:-${TAYSTJK_LIVE_OUTPUT_ENABLED:-false}}"
  mirror_flag="$(printf '%s' "$mirror_flag" | tr '[:upper:]' '[:lower:]')"

  if [[ "$ANTI_VPN_EFFECTIVE_MODE" == "off" || "$mirror_flag" != "true" ]]; then
    TAYSTJK_LIVE_OUTPUT_ENABLED="false"
    TAYSTJK_LIVE_OUTPUT_MODE="disabled"
  else
    TAYSTJK_LIVE_OUTPUT_ENABLED="true"
    TAYSTJK_LIVE_OUTPUT_MODE="supervisor-mirror"
  fi
  JKA_LIVE_OUTPUT_MIRROR_ENABLED="$TAYSTJK_LIVE_OUTPUT_ENABLED"
}

resolve_effective_server_settings() {
  local active_config_path="$1"
  local config_port=""
  local config_hostname=""
  local config_motd=""
  local config_maxclients=""
  local config_gametype=""
  local config_rcon_password=""

  # The manual-first egg model never writes managed cvars into
  # server.cfg from panel variables. Effective values are derived
  # from the active server.cfg only, with port_fallback used when
  # net_port is missing.

  config_port="$(read_server_cvar "$active_config_path" "net_port")"
  config_hostname="$(read_server_cvar "$active_config_path" "sv_hostname")"
  config_motd="$(read_server_cvar "$active_config_path" "g_motd")"
  config_maxclients="$(read_server_cvar "$active_config_path" "sv_maxclients")"
  config_gametype="$(read_server_cvar "$active_config_path" "g_gametype")"
  config_rcon_password="$(read_server_cvar "$active_config_path" "rconpassword")"

  TAYSTJK_ACTIVE_MOD_DIR="$active_game_dir"
  TAYSTJK_ACTIVE_SERVER_CONFIG="$SERVER_CONFIG"
  TAYSTJK_ACTIVE_SERVER_CONFIG_PATH="$active_config_path"
  TAYSTJK_ACTIVE_SERVER_LOG_PATH="$(active_server_log_path)"
  TAYSTJK_SERVER_CFG_OVERRIDES_ENABLED="false"
  TAYSTJK_EFFECTIVE_SERVER_BINARY="$server_binary_name"

  resolve_effective_live_output_settings

  TAYSTJK_EFFECTIVE_SERVER_PORT="${config_port:-$SERVER_PORT}"
  TAYSTJK_EFFECTIVE_SERVER_HOSTNAME="${config_hostname:-TaystJK Pterodactyl Server}"
  TAYSTJK_EFFECTIVE_SERVER_MOTD="${config_motd:-Powered by TaystJK on Pterodactyl}"
  TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS="${config_maxclients:-16}"
  TAYSTJK_EFFECTIVE_SERVER_GAMETYPE="${config_gametype:-0}"
  TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD="$config_rcon_password"

  export \
    TAYSTJK_ACTIVE_MOD_DIR \
    TAYSTJK_ACTIVE_SERVER_CONFIG \
    TAYSTJK_ACTIVE_SERVER_CONFIG_PATH \
    TAYSTJK_ACTIVE_SERVER_LOG_PATH \
    TAYSTJK_SERVER_CFG_OVERRIDES_ENABLED \
    TAYSTJK_EFFECTIVE_SERVER_BINARY \
    TAYSTJK_EFFECTIVE_SERVER_PORT \
    TAYSTJK_EFFECTIVE_SERVER_HOSTNAME \
    TAYSTJK_EFFECTIVE_SERVER_MOTD \
    TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS \
    TAYSTJK_EFFECTIVE_SERVER_GAMETYPE \
    TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD \
    TAYSTJK_LIVE_OUTPUT_ENABLED \
    TAYSTJK_LIVE_OUTPUT_MODE \
    TAYSTJK_LIVE_OUTPUT_SOURCE \
    TAYSTJK_LIVE_OUTPUT_FORMAT \
    TAYSTJK_LIVE_OUTPUT_PATH \
    TAYSTJK_LIVE_OUTPUT_MAX_BYTES

  write_runtime_state
}

ensure_managed_taystjk_server_config() {
  local config_path="/home/container/${active_game_dir}/${SERVER_CONFIG}"

  # The runtime only seeds a minimal default server.cfg when the
  # expected config is missing AND the active mod is the image-managed
  # TaystJK directory. An existing user-owned server.cfg is never
  # overwritten.
  if ! is_taystjk_managed_mod_dir "$active_game_dir"; then
    return 0
  fi

  if [[ -f "$config_path" ]]; then
    return 0
  fi

  cat > "$config_path" <<CFG
seta sv_hostname "TaystJK Pterodactyl Server"
seta g_motd "Powered by TaystJK on Pterodactyl"
seta sv_maxclients "16"
seta dedicated "2"
seta net_port "${SERVER_PORT}"
seta g_gametype "0"
set d1 "set g_gametype 0; map mp/ffa3; set nextmap vstr d1"
vstr d1
CFG
}
