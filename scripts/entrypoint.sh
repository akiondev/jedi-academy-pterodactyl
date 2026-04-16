#!/usr/bin/env bash
set -euo pipefail

COLOR_RESET=""
COLOR_BOLD=""
COLOR_INFO=""
COLOR_OK=""
COLOR_WARN=""
COLOR_ERROR=""
COLOR_SECTION=""
LAST_FAILURE_MESSAGE=""

setup_colors() {
  if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
    COLOR_RESET=$'\033[0m'
    COLOR_BOLD=$'\033[1m'
    COLOR_INFO=$'\033[36m'
    COLOR_OK=$'\033[32m'
    COLOR_WARN=$'\033[33m'
    COLOR_ERROR=$'\033[31m'
    COLOR_SECTION=$'\033[97m'
  fi
}

print_header() {
  printf '%b\n' "${COLOR_BOLD}============================================================${COLOR_RESET}"
  printf '%b\n' "${COLOR_BOLD} TaystJK Pterodactyl Runtime${COLOR_RESET}"
  printf '%b\n' "${COLOR_INFO} Created by akiondev${COLOR_RESET}"
  printf '%b\n\n' "${COLOR_BOLD}============================================================${COLOR_RESET}"
}

section() {
  printf '\n'
  printf '%b\n' "${COLOR_SECTION}${COLOR_BOLD}[SECTION]${COLOR_RESET} ${COLOR_BOLD}${1}${COLOR_RESET}"
}

kv() {
  printf '%-16s %s\n' "$1" "$2"
}

info() {
  printf '%b\n' "${COLOR_INFO}[INFO]${COLOR_RESET} $*"
}

ok() {
  printf '%b\n' "${COLOR_OK}[ OK ]${COLOR_RESET} $*"
}

warn() {
  printf '%b\n' "${COLOR_WARN}[WARN]${COLOR_RESET} $*"
}

log() {
  info "$*"
}

fail() {
  LAST_FAILURE_MESSAGE="$*"
  printf '%b\n' "${COLOR_ERROR}[ERROR]${COLOR_RESET} $*" >&2
  printf '\n' >&2
  section "What to Check Next" >&2
  printf '%s\n' "- Confirm /home/container/base/assets0.pk3 exists" >&2
  printf '%s\n' "- Confirm COPYRIGHT_ACKNOWLEDGED is set to true" >&2
  printf '%s\n' "- Confirm the selected mod directory contains the expected files" >&2
  printf '%s\n' "- Confirm the selected server config exists" >&2
  if [[ "$LAST_FAILURE_MESSAGE" == *"server binary"* ]]; then
    printf '%s\n' "- Confirm the runtime image was rebuilt and published successfully" >&2
  fi
  exit 1
}

print_path_status() {
  local label="$1"
  local path="$2"
  local kind="${3:-file}"

  if [[ "$kind" == "dir" ]]; then
    if [[ -d "$path" ]]; then
      ok "${label}: found at ${path}"
    else
      warn "${label}: missing at ${path}"
    fi
    return
  fi

  if [[ -f "$path" ]]; then
    ok "${label}: found at ${path}"
  else
    warn "${label}: missing at ${path}"
  fi
}

list_dir_files() {
  local path="$1"
  local pattern="${2:-*}"
  local files=()
  local file
  local result=""

  if [[ ! -d "$path" ]]; then
    printf 'directory missing\n'
    return
  fi

  while IFS= read -r file; do
    files+=("$file")
  done < <(
    find "$path" -maxdepth 1 -type f -name "$pattern" -printf '%f\n' 2>/dev/null | sort
  )

  for file in "${files[@]}"; do
    if [[ -n "$result" ]]; then
      result+=", "
    fi
    result+="$file"
  done

  if [[ -n "$result" ]]; then
    printf '%s\n' "$result"
  else
    printf 'none detected\n'
  fi
}

debug() {
  [[ "${DEBUG_STARTUP}" == "true" ]] || return 0
  printf '%b\n' "${COLOR_INFO}[DEBUG]${COLOR_RESET} $*"
}

bool_state() {
  if [[ "${1,,}" == "true" || "${1}" == "1" || "${1,,}" == "yes" || "${1,,}" == "on" ]]; then
    printf 'enabled\n'
  else
    printf 'disabled\n'
  fi
}

join_csv() {
  local result=""
  local item

  for item in "$@"; do
    [[ -n "$item" ]] || continue
    if [[ -n "$result" ]]; then
      result+=", "
    fi
    result+="$item"
  done

  printf '%s\n' "$result"
}

print_command_preview() {
  local rendered=""
  local arg

  for arg in "${STARTUP_COMMAND[@]}"; do
    if [[ -n "$rendered" ]]; then
      rendered+=" "
    fi
    rendered+="$(printf '%q' "$arg")"
  done

  debug "Resolved startup command: ${rendered}"
}

require_safe_component() {
  local value="$1"
  local variable_name="$2"

  if [[ -z "$value" || "$value" == "." || "$value" == ".." || "$value" == *"/"* || "$value" == *"\\"* || ! "$value" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
    fail "${variable_name} must be a simple relative name using only letters, numbers, dots, underscores or dashes"
  fi
}

normalize_server_binary_name() {
  local requested="${SERVER_BINARY#./}"

  require_safe_component "$requested" "SERVER_BINARY"
  printf '%s\n' "$requested"
}

resolve_active_game_dir() {
  local requested="$1"
  local requested_lower

  requested_lower="$(printf '%s' "$requested" | tr '[:upper:]' '[:lower:]')"
  if [[ -z "$requested_lower" || "$requested_lower" == "base" ]]; then
    printf 'base\n'
    return
  fi

  require_safe_component "$requested" "FS_GAME_MOD"
  printf '%s\n' "$requested"
}

parse_extra_startup_args() {
  EXTRA_STARTUP_ARGV=()

  [[ -n "$EXTRA_STARTUP_ARGS" ]] || return 0

  if [[ "$EXTRA_STARTUP_ARGS" == *$'\n'* || "$EXTRA_STARTUP_ARGS" == *$'\r'* || "$EXTRA_STARTUP_ARGS" == *'`'* || "$EXTRA_STARTUP_ARGS" == *'$'* || "$EXTRA_STARTUP_ARGS" == *';'* || "$EXTRA_STARTUP_ARGS" == *'&'* || "$EXTRA_STARTUP_ARGS" == *'|'* || "$EXTRA_STARTUP_ARGS" == *'<'* || "$EXTRA_STARTUP_ARGS" == *'>'* || "$EXTRA_STARTUP_ARGS" == *'('* || "$EXTRA_STARTUP_ARGS" == *')'* ]]; then
    fail "EXTRA_STARTUP_ARGS may use quotes for grouping, but it cannot contain shell control characters"
  fi

  set -f
  if ! eval "set -- $EXTRA_STARTUP_ARGS"; then
    set +f
    fail "EXTRA_STARTUP_ARGS contains invalid shell-style quoting"
  fi
  set +f

  EXTRA_STARTUP_ARGV=("$@")
}

sync_runtime_files() {
  local runtime_binary
  local found_runtime_binary=0

  if compgen -G "/opt/taystjk-dist/taystjkded.*" >/dev/null; then
    log "Syncing image-managed runtime files into container volume"
    for runtime_binary in /opt/taystjk-dist/taystjkded.*; do
      [[ -f "$runtime_binary" ]] || continue
      install -m 0755 "$runtime_binary" "/home/container/${runtime_binary##*/}"
      found_runtime_binary=1
    done
  fi

  if [[ -d /opt/taystjk-dist/taystjk ]]; then
    mkdir -p /home/container/taystjk
    cp -af /opt/taystjk-dist/taystjk/. /home/container/taystjk/
  fi

  if [[ "$found_runtime_binary" -eq 0 ]]; then
    log "No image-provided dedicated binaries were found under /opt/taystjk-dist"
  fi
}

require_base_assets() {
  [[ -f /home/container/base/assets0.pk3 ]] || fail "Missing /home/container/base/assets0.pk3. Provide your legally owned Jedi Academy base assets before starting the server."
}

build_startup_command() {
  STARTUP_COMMAND=(
    "$server_binary_path"
    +set dedicated 2
    +set net_port "$SERVER_PORT"
    +set fs_cdpath /home/container
    +set fs_basepath /home/container
    +set fs_homepath /home/container
  )

  if [[ "$active_game_dir" != "base" ]]; then
    STARTUP_COMMAND+=(+set fs_game "$active_game_dir")
  fi

  STARTUP_COMMAND+=(+exec "$SERVER_CONFIG")

  if [[ "${#EXTRA_STARTUP_ARGV[@]}" -gt 0 ]]; then
    STARTUP_COMMAND+=("${EXTRA_STARTUP_ARGV[@]}")
  fi
}

configure_anti_vpn() {
  : "${ANTI_VPN_ENABLED:=false}"
  : "${ANTI_VPN_MODE:=log-only}"
  : "${ANTI_VPN_CACHE_TTL:=6h}"
  : "${ANTI_VPN_SCORE_THRESHOLD:=90}"
  : "${ANTI_VPN_ALLOWLIST:=}"
  : "${ANTI_VPN_PROXYCHECK_API_KEY:=}"
  : "${ANTI_VPN_IPAPIIS_API_KEY:=}"
  : "${ANTI_VPN_IPHUB_API_KEY:=}"
  : "${ANTI_VPN_VPNAPI_IO_API_KEY:=}"
  : "${ANTI_VPN_TIMEOUT_MS:=1500}"
  : "${ANTI_VPN_LOG_DECISIONS:=true}"
  : "${ANTI_VPN_CACHE_PATH:=/home/container/.cache/taystjk-antivpn/cache.json}"
  : "${ANTI_VPN_CACHE_FLUSH_INTERVAL:=2s}"
  : "${ANTI_VPN_AUDIT_LOG_PATH:=/home/container/logs/anti-vpn-audit.log}"
  : "${ANTI_VPN_BAN_COMMAND:=addip %IP%}"
  : "${ANTI_VPN_KICK_COMMAND:=clientkick %SLOT%}"
  : "${ANTI_VPN_LOG_PATH:=/home/container/${active_game_dir}/server.log}"

  ANTI_VPN_MODE_NORMALIZED="$(printf '%s' "$ANTI_VPN_MODE" | tr '[:upper:]' '[:lower:]')"
  case "$ANTI_VPN_MODE_NORMALIZED" in
    off|log-only|block) ;;
    *)
      warn "ANTI_VPN_MODE=${ANTI_VPN_MODE} is invalid, falling back to off"
      ANTI_VPN_MODE_NORMALIZED="off"
      ;;
  esac

  if [[ "${ANTI_VPN_ENABLED,,}" != "true" || "$ANTI_VPN_MODE_NORMALIZED" == "off" ]]; then
    ANTI_VPN_EFFECTIVE_MODE="off"
  else
    ANTI_VPN_EFFECTIVE_MODE="$ANTI_VPN_MODE_NORMALIZED"
  fi

  mkdir -p "$(dirname "$ANTI_VPN_CACHE_PATH")"
  mkdir -p "$(dirname "$ANTI_VPN_AUDIT_LOG_PATH")"
}

anti_vpn_provider_summary() {
  local providers=()

  if [[ -n "$ANTI_VPN_PROXYCHECK_API_KEY" ]]; then
    providers+=("proxycheck.io")
  else
    providers+=("proxycheck.io (anonymous)")
  fi

  if [[ -n "$ANTI_VPN_IPAPIIS_API_KEY" ]]; then
    providers+=("ipapi.is")
  else
    providers+=("ipapi.is (anonymous)")
  fi

  if [[ -n "$ANTI_VPN_IPHUB_API_KEY" ]]; then
    providers+=("IPHub")
  fi

  if [[ -n "$ANTI_VPN_VPNAPI_IO_API_KEY" ]]; then
    providers+=("vpnapi.io")
  fi

  join_csv "${providers[@]}"
}

anti_vpn_allowlist_status() {
  if [[ -n "${ANTI_VPN_ALLOWLIST//[[:space:],]/}" ]]; then
    printf 'configured\n'
  else
    printf 'not set\n'
  fi
}

print_runtime_summary() {
  section "Runtime Summary"
  kv "Image creator  :" "akiondev"
  kv "Startup source :" "$startup_source"
  kv "Startup detail :" "$startup_detail"
  kv "Runtime mode   :" "dedicated server"
  kv "Mod directory  :" "$active_game_dir"
  kv "Server config  :" "${active_game_dir}/${SERVER_CONFIG}"
  kv "Server binary  :" "$server_binary_name"
  kv "Server port    :" "$SERVER_PORT"
  kv "Copyright ack  :" "$COPYRIGHT_ACKNOWLEDGED"
  if [[ "${#EXTRA_STARTUP_ARGV[@]}" -gt 0 ]]; then
    kv "Extra args     :" "set"
  else
    kv "Extra args     :" "not set"
  fi
  if [[ "${DEBUG_STARTUP}" == "true" ]]; then
    kv "Debug startup  :" "enabled"
  else
    kv "Debug startup  :" "disabled"
  fi
}

print_anti_vpn_summary() {
  section "Anti-VPN"
  kv "Enabled         :" "$(bool_state "$ANTI_VPN_ENABLED")"
  kv "Configured mode :" "$ANTI_VPN_MODE_NORMALIZED"
  kv "Effective mode  :" "$ANTI_VPN_EFFECTIVE_MODE"
  kv "Capture mode    :" "stdout-first with server.log fallback"
  kv "Cache TTL       :" "$ANTI_VPN_CACHE_TTL"
  kv "Cache path      :" "$ANTI_VPN_CACHE_PATH"
  kv "Cache flush     :" "$ANTI_VPN_CACHE_FLUSH_INTERVAL"
  kv "Threshold       :" "$ANTI_VPN_SCORE_THRESHOLD"
  kv "Timeout         :" "$ANTI_VPN_TIMEOUT_MS"
  kv "Decision logs   :" "$(bool_state "$ANTI_VPN_LOG_DECISIONS")"
  kv "Allowlist       :" "$(anti_vpn_allowlist_status)"
  kv "Log path        :" "$ANTI_VPN_LOG_PATH"
  kv "Audit log       :" "$ANTI_VPN_AUDIT_LOG_PATH"
  kv "Providers       :" "$(anti_vpn_provider_summary)"

  if [[ "$ANTI_VPN_EFFECTIVE_MODE" == "off" ]]; then
    warn "Anti-VPN supervision is disabled"
    return
  fi

  if [[ -x /usr/local/bin/taystjk-antivpn ]]; then
    ok "Anti-VPN supervisor binary found at /usr/local/bin/taystjk-antivpn"
  else
    warn "Anti-VPN supervisor binary is missing; startup will continue without anti-VPN enforcement"
  fi
}

print_preflight_checks() {
  section "Preflight Checks"
  ok "Runtime files synced from image"
  ok "Server binary found at ${server_binary_path}"
  ok "Container home prepared at /home/container"
  if [[ "${#EXTRA_STARTUP_ARGV[@]}" -gt 0 ]]; then
    ok "Extra startup args set: ${EXTRA_STARTUP_ARGS}"
  else
    warn "Extra startup args not set"
  fi
}

print_asset_detection() {
  section "Asset Detection"
  print_path_status "Base assets" "/home/container/base/assets0.pk3"
  print_path_status "Bundled TaystJK files" "/home/container/taystjk" "dir"
}

print_mod_detection() {
  local mod_path="/home/container/${active_game_dir}"
  local config_path="${mod_path}/${SERVER_CONFIG}"

  section "Mod Detection"
  kv "Selected mod path :" "$mod_path"
  print_path_status "Active mod directory" "$mod_path" "dir"
  print_path_status "Server config" "$config_path"

  if [[ "$active_game_dir" == "base" ]]; then
    info "Running in base mode without an fs_game override"
  elif find "$mod_path" -maxdepth 1 -type f | read -r _; then
    ok "Active mod directory contains files"
  else
    warn "Active mod directory exists but appears empty"
  fi
}

print_file_inventory() {
  section "File Inventory"
  kv "Base files      :" "$(list_dir_files /home/container/base 'assets*.pk3')"
  kv "Mod files       :" "$(list_dir_files "/home/container/${active_game_dir}")"
  if [[ -d /home/container/logs ]]; then
    kv "Logs directory  :" "present"
  else
    kv "Logs directory  :" "missing"
  fi
}

print_launch_decision() {
  section "Launch Decision"
  ok "Preflight complete"
  print_command_preview
  if [[ "$ANTI_VPN_EFFECTIVE_MODE" == "off" ]]; then
    info "Launching TaystJK dedicated server now..."
  else
    info "Launching TaystJK dedicated server under anti-VPN supervision..."
  fi
}

launch_server() {
  if [[ "$ANTI_VPN_EFFECTIVE_MODE" != "off" && -x /usr/local/bin/taystjk-antivpn ]]; then
    exec /usr/local/bin/taystjk-antivpn supervise -- "${STARTUP_COMMAND[@]}"
  fi

  if [[ "$ANTI_VPN_EFFECTIVE_MODE" != "off" ]]; then
    warn "Continuing without anti-VPN supervision because the helper binary is unavailable"
  fi

  exec "${STARTUP_COMMAND[@]}"
}

cd /home/container
setup_colors
print_header

: "${SERVER_BINARY:=taystjkded.x86_64}"
: "${SERVER_PORT:=29070}"
: "${SERVER_CONFIG:=server.cfg}"
: "${EXTRA_STARTUP_ARGS:=}"
: "${FS_GAME_MOD:=taystjk}"
: "${COPYRIGHT_ACKNOWLEDGED:=false}"
: "${DEBUG_STARTUP:=false}"

[[ "${COPYRIGHT_ACKNOWLEDGED}" == "true" ]] || fail "COPYRIGHT_ACKNOWLEDGED must be true. This image does not ship Jedi Academy base assets."

require_safe_component "$SERVER_CONFIG" "SERVER_CONFIG"
server_binary_name="$(normalize_server_binary_name)"
active_game_dir="$(resolve_active_game_dir "$FS_GAME_MOD")"
server_binary_path="/home/container/${server_binary_name}"
configure_anti_vpn

mkdir -p /home/container/base /home/container/logs "/home/container/${active_game_dir}"
sync_runtime_files

[[ -f "$server_binary_path" ]] || fail "Configured server binary ${server_binary_name} was not found in the container volume or image runtime"
chmod +x "$server_binary_path"

if [[ ! -f "/home/container/${active_game_dir}/${SERVER_CONFIG}" ]]; then
  cat > "/home/container/${active_game_dir}/${SERVER_CONFIG}" <<CFG
seta sv_hostname "TaystJK Pterodactyl Server"
seta g_motd "Powered by TaystJK on Pterodactyl"
seta sv_maxclients "16"
seta dedicated "2"
seta net_port "${SERVER_PORT}"
seta g_gametype "0"
set d1 "set g_gametype 0; map mp/ffa3; set nextmap vstr d1"
vstr d1
CFG
fi

export HOME=/home/container

if [[ "$#" -gt 0 && "$1" != "--panel-startup" ]]; then
  section "Launch Decision"
  info "Custom startup command detected"
  if [[ "$ANTI_VPN_EFFECTIVE_MODE" != "off" ]]; then
    warn "Anti-VPN supervision is bypassed for custom startup commands"
  fi
  info "Executing: $*"
  exec "$@"
fi

if [[ "$#" -gt 0 ]]; then
  startup_source="Pterodactyl panel sentinel"
  startup_detail="Wings passed --panel-startup and the image built the final server command"
else
  startup_source="automatic image startup"
  startup_detail="No startup arguments were passed, so the image built the dedicated server command from environment variables"
fi

parse_extra_startup_args
build_startup_command

print_runtime_summary
print_preflight_checks
print_asset_detection
print_mod_detection
print_file_inventory
print_anti_vpn_summary
require_base_assets
print_launch_decision
launch_server
