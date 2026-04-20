#!/usr/bin/env bash
set -euo pipefail

export PATH="/home/container/bin:${PATH}"

JKA_COMMON_DIR="${JKA_COMMON_DIR:-/opt/jka/runtime/common}"
if [[ ! -d "${JKA_COMMON_DIR}" ]]; then
  printf 'entrypoint: shared layer not found at %s\n' "${JKA_COMMON_DIR}" >&2
  exit 1
fi
source "${JKA_COMMON_DIR}/jka_runtime_common.sh"
source "${JKA_COMMON_DIR}/jka_runtime_manifest.sh"
source "${JKA_COMMON_DIR}/jka_runtime_sync.sh"
source "${JKA_COMMON_DIR}/jka_security.sh"
source "${JKA_COMMON_DIR}/jka_server_cfg.sh"
source "${JKA_COMMON_DIR}/jka_addon_loader.sh"
source "${JKA_COMMON_DIR}/jka_antivpn_bootstrap.sh"

load_runtime_manifest

normalize_server_binary_name() {
  local requested="${SERVER_BINARY#./}"

  require_safe_component "$requested" "SERVER_BINARY"
  printf '%s\n' "$requested"
}

is_taystjk_managed_mod_dir() {
  local mod_dir="$1"
  [[ "$mod_dir" == "taystjk" ]]
}

is_base_mode() {
  local mod_dir="$1"
  [[ "$mod_dir" == "base" ]]
}

is_image_managed_server_binary() {
  local binary_name="$1"
  [[ -f "${JKA_PATH_ENGINE_DIST}/${binary_name}" ]]
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

determine_runtime_ownership() {
  if is_image_managed_server_binary "$server_binary_name"; then
    SERVER_BINARY_OWNERSHIP="image-managed TaystJK"
  else
    SERVER_BINARY_OWNERSHIP="manual user-supplied"
  fi

  if is_taystjk_managed_mod_dir "$active_game_dir"; then
    ACTIVE_MOD_OWNERSHIP="image-managed TaystJK"
  elif is_base_mode "$active_game_dir"; then
    ACTIVE_MOD_OWNERSHIP="manual base assets"
  else
    ACTIVE_MOD_OWNERSHIP="manual user-supplied"
  fi
}

validate_server_binary_selection() {
  if [[ -f "$server_binary_path" ]]; then
    return 0
  fi

  if is_image_managed_server_binary "$server_binary_name"; then
    fail "Configured TaystJK server binary ${server_binary_name} was not found in the image-managed runtime"
  fi

  fail "Configured manual server binary ${server_binary_name} was not found under /home/container. Only TaystJK binaries are synced automatically; manual alternatives must be uploaded by the server owner"
}

validate_selected_runtime_paths() {
  local mod_path="/home/container/${active_game_dir}"
  local config_path="${mod_path}/${SERVER_CONFIG}"

  if is_taystjk_managed_mod_dir "$active_game_dir"; then
    [[ -d "$mod_path" ]] || fail "Managed TaystJK mod directory ${active_game_dir} was not found in the image-managed runtime"
  elif is_base_mode "$active_game_dir"; then
    [[ -d "$mod_path" ]] || fail "Configured base assets directory was not found under /home/container/base"
  else
    [[ -d "$mod_path" ]] || fail "Configured manual mod directory ${active_game_dir} was not found under /home/container. Only TaystJK is prepared automatically; manual mod folders must be uploaded by the server owner"

    if ! find "$mod_path" -maxdepth 1 -type f | read -r _; then
      fail "Configured manual mod directory ${active_game_dir} exists but appears empty. Manual mod folders must already contain their own files before startup"
    fi
  fi

  if [[ -f "$config_path" ]]; then
    return 0
  fi

  if is_taystjk_managed_mod_dir "$active_game_dir"; then
    fail "Managed TaystJK server config ${active_game_dir}/${SERVER_CONFIG} is missing after runtime preparation"
  fi

  if is_base_mode "$active_game_dir"; then
    fail "Configured server config base/${SERVER_CONFIG} was not found. Base mode is allowed, but its config file must be provided manually"
  fi

  fail "Configured server config ${active_game_dir}/${SERVER_CONFIG} was not found. Manual mod directories must provide their own config file"
}

require_base_assets() {
  [[ -f /home/container/base/assets0.pk3 ]] || fail "Missing /home/container/base/assets0.pk3. Provide your legally owned Jedi Academy base assets before starting the server."
}

build_startup_command() {
  STARTUP_COMMAND=(
    "$server_binary_path"
    +set dedicated 2
    +set net_port "$TAYSTJK_EFFECTIVE_SERVER_PORT"
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

print_runtime_summary() {
  section "SERVER"
  kv_highlight "Mode" "Dedicated server"
  kv_highlight "Mod" "$active_game_dir"
  kv "Mod mode" "$ACTIVE_MOD_OWNERSHIP"
  kv_highlight "Config" "${active_game_dir}/${SERVER_CONFIG}"
  kv_highlight "Port" "$TAYSTJK_EFFECTIVE_SERVER_PORT"
  kv_highlight "Binary" "$server_binary_name"
  kv "Binary mode" "$SERVER_BINARY_OWNERSHIP"
  if [[ "$SERVER_CFG_OVERRIDES_ENABLED" == "true" ]]; then
    kv "Cfg mode" "managed"
  else
    kv "Cfg mode" "user-owned"
  fi
  if [[ -n "$TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD" ]]; then
    kv "RCON" "set"
  else
    kv "RCON" "not set"
  fi
  kv "Copyright" "$COPYRIGHT_ACKNOWLEDGED"
  if [[ "${#EXTRA_STARTUP_ARGV[@]}" -gt 0 ]]; then
    kv "Extra args" "set"
  else
    kv "Extra args" "not set"
  fi
  kv "Debug" "$(bool_state "$DEBUG_STARTUP")"
  debug "Image creator: akiondev"
  debug "Startup source: ${startup_source}"
  debug "Startup detail: ${startup_detail}"
}

print_preflight_checks() {
  section "CHECKS"
  ok "Runtime files synced from image"
  ok "Server binary found"
  ok "Container home prepared"
  if [[ "${#EXTRA_STARTUP_ARGV[@]}" -gt 0 ]]; then
    ok "Extra startup args set"
    debug "Extra startup args: ${EXTRA_STARTUP_ARGS}"
  else
    warn "Extra startup args not set"
  fi
}

print_asset_detection() {
  if [[ -f /home/container/base/assets0.pk3 ]]; then
    ok "Base assets found"
  else
    warn "Base assets missing"
  fi

  if [[ -d /home/container/taystjk ]]; then
    ok "Bundled TaystJK files found"
  else
    warn "Bundled TaystJK files missing"
  fi
}

print_mod_detection() {
  local mod_path="/home/container/${active_game_dir}"
  local config_path="${mod_path}/${SERVER_CONFIG}"

  if [[ -d "$mod_path" ]]; then
    ok "Active mod directory found"
  else
    warn "Active mod directory missing"
  fi

  if [[ -f "$config_path" ]]; then
    ok "Server config found"
  else
    warn "Server config missing"
  fi

  if is_taystjk_managed_mod_dir "$active_game_dir"; then
    ok "Using image-managed TaystJK mod directory"
  elif [[ "$active_game_dir" == "base" ]]; then
    info "Running in base mode without an fs_game override"
  else
    ok "Using manually supplied mod directory"
  fi
}

print_paths() {
  section "PATHS"
  kv "Binary path" "$server_binary_path"
  kv "Mod path" "/home/container/${active_game_dir}"
  kv "Addons dir" "$ADDONS_DIR"
  kv "Addon docs" "$ADDON_DOCS_DIR"
  kv "Addon examples" "$ADDON_EXAMPLES_DIR"
  kv "Addon defaults" "$ADDON_DEFAULTS_DIR"
  kv "Runtime env" "/home/container/.runtime/taystjk-effective.env"
  kv "Runtime json" "/home/container/.runtime/taystjk-effective.json"
  kv "Server log" "$TAYSTJK_ACTIVE_SERVER_LOG_PATH"
  kv "Anti-VPN log" "$ANTI_VPN_LOG_PATH"
  kv "Chatlogs dir" "/home/container/chatlogs"
  kv "Audit log" "$ANTI_VPN_AUDIT_LOG_PATH"
  kv "Cache path" "$ANTI_VPN_CACHE_PATH"
}

print_inventory_summary() {
  section "INVENTORY"
  kv "Base files" "$(count_dir_files /home/container/base 'assets*.pk3') found"
  kv "Mod files" "$(count_dir_files "/home/container/${active_game_dir}") found"
  if [[ -d /home/container/logs ]]; then
    kv "Logs dir" "present"
  else
    kv "Logs dir" "missing"
  fi
}

print_debug_inventory() {
  [[ "${DEBUG_STARTUP}" == "true" ]] || return 0
  section "DEBUG INVENTORY"
  kv "Base files" "$(list_dir_files /home/container/base 'assets*.pk3')"
  kv "Mod files" "$(list_dir_files "/home/container/${active_game_dir}")"
  kv "Addon files" "$(list_dir_files "$ADDONS_DIR")"
  kv "Addon docs" "$(list_dir_files "$ADDON_DOCS_DIR")"
  kv "Addon examples" "$(list_dir_files "$ADDON_EXAMPLES_DIR")"
  kv "Addon defaults" "$(list_dir_files "$ADDON_DEFAULTS_DIR")"
}

print_launch_decision() {
  section "LAUNCH"
  ready "Startup checks passed"
  print_command_preview
  if [[ "$ANTI_VPN_EFFECTIVE_MODE" == "off" ]]; then
    info "Launching configured dedicated server now..."
  else
    info "Launching configured dedicated server under anti-VPN supervision..."
  fi
}

launch_server() {
  if [[ "$ANTI_VPN_EFFECTIVE_MODE" != "off" && -x "${JKA_PATH_ANTIVPN_BINARY}" ]]; then
    exec "${JKA_PATH_ANTIVPN_BINARY}" supervise -- "${STARTUP_COMMAND[@]}"
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
: "${SERVER_LOG_FILENAME:=server.log}"
: "${EXTRA_STARTUP_ARGS:=}"
: "${FS_GAME_MOD:=taystjk}"
: "${COPYRIGHT_ACKNOWLEDGED:=false}"
: "${DEBUG_STARTUP:=false}"

[[ "${COPYRIGHT_ACKNOWLEDGED}" == "true" ]] || fail "COPYRIGHT_ACKNOWLEDGED must be true. This image does not ship Jedi Academy base assets."

require_safe_component "$SERVER_CONFIG" "SERVER_CONFIG"
server_binary_name="$(normalize_server_binary_name)"
active_game_dir="$(resolve_active_game_dir "$FS_GAME_MOD")"
server_binary_path="/home/container/${server_binary_name}"
configure_addons
configure_server_settings
configure_server_logging
configure_anti_vpn

mkdir -p /home/container/base /home/container/logs
if is_taystjk_managed_mod_dir "$active_game_dir"; then
  mkdir -p "/home/container/${active_game_dir}"
fi
sync_runtime_files
sync_addon_docs
sync_managed_addon_examples
sync_managed_addon_defaults
install_managed_status_helper
determine_runtime_ownership

validate_server_binary_selection
chmod +x "$server_binary_path"

export HOME=/home/container

if [[ "$#" -gt 0 && "$1" != "--panel-startup" ]]; then
  section "LAUNCH"
  info "Custom startup command detected"
  if [[ "$ADDONS_ENABLED" == "true" ]]; then
    warn "Addon loading is bypassed for custom startup commands"
  fi
  if [[ "$ANTI_VPN_EFFECTIVE_MODE" != "off" ]]; then
    warn "Anti-VPN supervision is bypassed for custom startup commands"
  fi
  warn "Managed server.cfg overrides are bypassed for custom startup commands"
  info "Executing: $*"
  exec "$@"
fi

ensure_managed_taystjk_server_config
validate_selected_runtime_paths

resolve_effective_server_settings "/home/container/${active_game_dir}/${SERVER_CONFIG}"
install_managed_chatlogger_helper

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
print_addon_summary
run_addons
print_preflight_checks
print_asset_detection
print_mod_detection
print_anti_vpn_summary
print_inventory_summary
print_paths
print_debug_inventory
require_base_assets
print_launch_decision
launch_server
