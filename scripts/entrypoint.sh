#!/usr/bin/env bash
set -euo pipefail

export PATH="/home/container/bin:${PATH}"

COLOR_RESET=""
COLOR_BOLD=""
COLOR_INFO=""
COLOR_OK=""
COLOR_WARN=""
COLOR_ERROR=""
COLOR_ACTIVE=""
COLOR_SECTION=""
COLOR_DIM=""
LAST_FAILURE_MESSAGE=""

setup_colors() {
  if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
    COLOR_RESET=$'\033[0m'
    COLOR_BOLD=$'\033[1m'
    COLOR_INFO=$'\033[36m'
    COLOR_OK=$'\033[32m'
    COLOR_WARN=$'\033[33m'
    COLOR_ERROR=$'\033[31m'
    COLOR_ACTIVE=$'\033[35m'
    COLOR_SECTION=$'\033[97m'
    COLOR_DIM=$'\033[90m'
  fi
}

print_header() {
  printf '%b\n' "${COLOR_BOLD}============================================================${COLOR_RESET}"
  printf '%b\n' "${COLOR_BOLD} TaystJK Pterodactyl Runtime${COLOR_RESET}"
  printf '%b\n' "${COLOR_INFO} Created by akiondev${COLOR_RESET}"
  printf '%b\n\n' "${COLOR_BOLD}============================================================${COLOR_RESET}"
}

divider() {
  printf '%b\n' "${COLOR_DIM}------------------------------------------------------------${COLOR_RESET}"
}

section() {
  printf '\n'
  divider
  printf '%b\n' "${COLOR_SECTION}${COLOR_BOLD}[${1}]${COLOR_RESET}"
  divider
}

kv() {
  printf '%-14s : %s\n' "$1" "$2"
}

kv_highlight() {
  printf '%-14s : %b%s%b\n' "$1" "${COLOR_BOLD}" "$2" "${COLOR_RESET}"
}

kv_value() {
  printf '%-14s : %b%s%b\n' "$1" "$2" "$3" "${COLOR_RESET}"
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

ready() {
  printf '%b\n' "${COLOR_OK}${COLOR_BOLD}[READY]${COLOR_RESET} $*"
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

require_safe_container_path() {
  local value="$1"
  local variable_name="$2"

  if [[ -z "$value" || "$value" != /home/container* || "$value" == *$'\n'* || "$value" == *$'\r'* || "$value" == *'..'* || "$value" == *'//'*
        || "$value" == *'\\'* || ! "$value" =~ ^/home/container(/[A-Za-z0-9._-]+)*$ ]]; then
    fail "${variable_name} must stay under /home/container and may use only letters, numbers, dots, underscores, dashes and slashes"
  fi
}

require_no_newlines() {
  local value="$1"
  local variable_name="$2"

  if [[ "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
    fail "${variable_name} may not contain newline characters"
  fi
}

normalize_optional_boolean() {
  local value="$1"
  local variable_name="$2"
  local fallback="$3"
  local normalized

  normalized="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
  case "$normalized" in
    true|false)
      printf '%s\n' "$normalized"
      ;;
    *)
      warn "${variable_name}=${value} is invalid, falling back to ${fallback}"
      printf '%s\n' "$fallback"
      ;;
  esac
}

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

  mkdir -p "$runtime_dir"
  chmod 700 "$runtime_dir"

  if [[ "$SERVER_CFG_OVERRIDES_ENABLED" == "true" ]]; then
    overrides_enabled_json="true"
  fi

  {
    printf 'TAYSTJK_ACTIVE_MOD_DIR=%q\n' "$TAYSTJK_ACTIVE_MOD_DIR"
    printf 'TAYSTJK_ACTIVE_SERVER_CONFIG=%q\n' "$TAYSTJK_ACTIVE_SERVER_CONFIG"
    printf 'TAYSTJK_ACTIVE_SERVER_CONFIG_PATH=%q\n' "$TAYSTJK_ACTIVE_SERVER_CONFIG_PATH"
    printf 'TAYSTJK_SERVER_CFG_OVERRIDES_ENABLED=%q\n' "$TAYSTJK_SERVER_CFG_OVERRIDES_ENABLED"
    printf 'TAYSTJK_EFFECTIVE_SERVER_BINARY=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_BINARY"
    printf 'TAYSTJK_EFFECTIVE_SERVER_PORT=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_PORT"
    printf 'TAYSTJK_EFFECTIVE_SERVER_HOSTNAME=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_HOSTNAME"
    printf 'TAYSTJK_EFFECTIVE_SERVER_MOTD=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_MOTD"
    printf 'TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS"
    printf 'TAYSTJK_EFFECTIVE_SERVER_GAMETYPE=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_GAMETYPE"
    printf 'TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD=%q\n' "$TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD"
  } > "$runtime_env_path"
  chmod 600 "$runtime_env_path"

  jq -n \
    --arg active_mod_dir "$TAYSTJK_ACTIVE_MOD_DIR" \
    --arg active_server_config "$TAYSTJK_ACTIVE_SERVER_CONFIG" \
    --arg active_server_config_path "$TAYSTJK_ACTIVE_SERVER_CONFIG_PATH" \
    --arg effective_server_binary "$TAYSTJK_EFFECTIVE_SERVER_BINARY" \
    --arg effective_server_port "$TAYSTJK_EFFECTIVE_SERVER_PORT" \
    --arg effective_server_hostname "$TAYSTJK_EFFECTIVE_SERVER_HOSTNAME" \
    --arg effective_server_motd "$TAYSTJK_EFFECTIVE_SERVER_MOTD" \
    --arg effective_server_maxclients "$TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS" \
    --arg effective_server_gametype "$TAYSTJK_EFFECTIVE_SERVER_GAMETYPE" \
    --argjson server_cfg_overrides_enabled "$overrides_enabled_json" \
    '{
      active_mod_dir: $active_mod_dir,
      active_server_config: $active_server_config,
      active_server_config_path: $active_server_config_path,
      server_cfg_overrides_enabled: $server_cfg_overrides_enabled,
      effective_server_binary: $effective_server_binary,
      effective_server_port: $effective_server_port,
      effective_server_hostname: $effective_server_hostname,
      effective_server_motd: $effective_server_motd,
      effective_server_maxclients: $effective_server_maxclients,
      effective_server_gametype: $effective_server_gametype
    }' > "$runtime_json_path"
  chmod 600 "$runtime_json_path"
}

configure_server_settings() {
  : "${SERVER_CFG_OVERRIDES_ENABLED:=true}"
  : "${SERVER_HOSTNAME:=}"
  : "${SERVER_MOTD:=}"
  : "${SERVER_MAXCLIENTS:=}"
  : "${SERVER_GAMETYPE:=}"
  : "${SERVER_RCON_PASSWORD:=}"

  SERVER_CFG_OVERRIDES_ENABLED="$(normalize_optional_boolean "$SERVER_CFG_OVERRIDES_ENABLED" "SERVER_CFG_OVERRIDES_ENABLED" "true")"

  require_no_newlines "$SERVER_HOSTNAME" "SERVER_HOSTNAME"
  require_no_newlines "$SERVER_MOTD" "SERVER_MOTD"
  require_no_newlines "$SERVER_RCON_PASSWORD" "SERVER_RCON_PASSWORD"

  if [[ ! "$SERVER_PORT" =~ ^[0-9]+$ || "$SERVER_PORT" -lt 1 || "$SERVER_PORT" -gt 65535 ]]; then
    warn "SERVER_PORT=${SERVER_PORT} is invalid, falling back to 29070"
    SERVER_PORT="29070"
  fi

  if [[ -n "$SERVER_MAXCLIENTS" && ! "$SERVER_MAXCLIENTS" =~ ^[0-9]+$ ]]; then
    warn "SERVER_MAXCLIENTS=${SERVER_MAXCLIENTS} is invalid, ignoring the override"
    SERVER_MAXCLIENTS=""
  fi

  if [[ -n "$SERVER_GAMETYPE" && ! "$SERVER_GAMETYPE" =~ ^[0-9]+$ ]]; then
    warn "SERVER_GAMETYPE=${SERVER_GAMETYPE} is invalid, ignoring the override"
    SERVER_GAMETYPE=""
  fi
}

resolve_effective_server_settings() {
  local active_config_path="$1"
  local config_port=""
  local config_hostname=""
  local config_motd=""
  local config_maxclients=""
  local config_gametype=""
  local config_rcon_password=""

  if [[ "$SERVER_CFG_OVERRIDES_ENABLED" == "true" ]]; then
    upsert_server_cvar "$active_config_path" "net_port" "$SERVER_PORT"
    [[ -n "$SERVER_HOSTNAME" ]] && upsert_server_cvar "$active_config_path" "sv_hostname" "$SERVER_HOSTNAME"
    [[ -n "$SERVER_MOTD" ]] && upsert_server_cvar "$active_config_path" "g_motd" "$SERVER_MOTD"
    [[ -n "$SERVER_MAXCLIENTS" ]] && upsert_server_cvar "$active_config_path" "sv_maxclients" "$SERVER_MAXCLIENTS"
    [[ -n "$SERVER_GAMETYPE" ]] && upsert_server_cvar "$active_config_path" "g_gametype" "$SERVER_GAMETYPE"

    if [[ -n "$SERVER_RCON_PASSWORD" ]]; then
      upsert_server_cvar "$active_config_path" "rconpassword" "$SERVER_RCON_PASSWORD"
    fi
  fi

  config_port="$(read_server_cvar "$active_config_path" "net_port")"
  config_hostname="$(read_server_cvar "$active_config_path" "sv_hostname")"
  config_motd="$(read_server_cvar "$active_config_path" "g_motd")"
  config_maxclients="$(read_server_cvar "$active_config_path" "sv_maxclients")"
  config_gametype="$(read_server_cvar "$active_config_path" "g_gametype")"
  config_rcon_password="$(read_server_cvar "$active_config_path" "rconpassword")"

  TAYSTJK_ACTIVE_MOD_DIR="$active_game_dir"
  TAYSTJK_ACTIVE_SERVER_CONFIG="$SERVER_CONFIG"
  TAYSTJK_ACTIVE_SERVER_CONFIG_PATH="$active_config_path"
  TAYSTJK_SERVER_CFG_OVERRIDES_ENABLED="$SERVER_CFG_OVERRIDES_ENABLED"
  TAYSTJK_EFFECTIVE_SERVER_BINARY="$server_binary_name"

  if [[ "$SERVER_CFG_OVERRIDES_ENABLED" == "true" ]]; then
    TAYSTJK_EFFECTIVE_SERVER_PORT="$SERVER_PORT"
    TAYSTJK_EFFECTIVE_SERVER_HOSTNAME="${SERVER_HOSTNAME:-${config_hostname:-TaystJK Pterodactyl Server}}"
    TAYSTJK_EFFECTIVE_SERVER_MOTD="${SERVER_MOTD:-${config_motd:-Powered by TaystJK on Pterodactyl}}"
    TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS="${SERVER_MAXCLIENTS:-${config_maxclients:-16}}"
    TAYSTJK_EFFECTIVE_SERVER_GAMETYPE="${SERVER_GAMETYPE:-${config_gametype:-0}}"
    TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD="${SERVER_RCON_PASSWORD:-$config_rcon_password}"
  else
    TAYSTJK_EFFECTIVE_SERVER_PORT="${config_port:-$SERVER_PORT}"
    TAYSTJK_EFFECTIVE_SERVER_HOSTNAME="${config_hostname:-TaystJK Pterodactyl Server}"
    TAYSTJK_EFFECTIVE_SERVER_MOTD="${config_motd:-Powered by TaystJK on Pterodactyl}"
    TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS="${config_maxclients:-16}"
    TAYSTJK_EFFECTIVE_SERVER_GAMETYPE="${config_gametype:-0}"
    TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD="$config_rcon_password"
  fi

  export \
    TAYSTJK_ACTIVE_MOD_DIR \
    TAYSTJK_ACTIVE_SERVER_CONFIG \
    TAYSTJK_ACTIVE_SERVER_CONFIG_PATH \
    TAYSTJK_SERVER_CFG_OVERRIDES_ENABLED \
    TAYSTJK_EFFECTIVE_SERVER_BINARY \
    TAYSTJK_EFFECTIVE_SERVER_PORT \
    TAYSTJK_EFFECTIVE_SERVER_HOSTNAME \
    TAYSTJK_EFFECTIVE_SERVER_MOTD \
    TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS \
    TAYSTJK_EFFECTIVE_SERVER_GAMETYPE \
    TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD

  write_runtime_state
}

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
  [[ -f "/opt/taystjk-dist/${binary_name}" ]]
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
    log "Syncing image-managed TaystJK runtime files into container volume"
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

ensure_managed_taystjk_server_config() {
  local config_path="/home/container/${active_game_dir}/${SERVER_CONFIG}"

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

sync_addon_docs() {
  sync_image_managed_addon_tree "/opt/taystjk-docs/addons" "$ADDON_DOCS_DIR" "addon docs"
}

sync_image_managed_addon_tree() {
  local source_dir="$1"
  local target_dir="$2"
  local label="$3"

  mkdir -p "$target_dir"

  if [[ ! -d "$source_dir" ]]; then
    debug "No image-managed ${label} found under ${source_dir}"
    return 0
  fi

  rsync -a --delete "${source_dir}/" "${target_dir}/"
  find "$target_dir" -maxdepth 1 -type f \( -name '*.sh' -o -name '*.py' \) -exec chmod 0755 {} +
  debug "Refreshed image-managed ${label} in ${target_dir}"
}

sync_managed_addon_examples() {
  info "Syncing addon examples into ${ADDON_EXAMPLES_DIR}"
  sync_image_managed_addon_tree "/opt/taystjk-bundled-addons/examples" "$ADDON_EXAMPLES_DIR" "addon examples"
}

sync_managed_addon_defaults() {
  info "Syncing managed addon helpers into ${ADDON_DEFAULTS_DIR}"
  sync_image_managed_addon_tree "/opt/taystjk-bundled-addons/defaults" "$ADDON_DEFAULTS_DIR" "addon defaults"
}

install_managed_status_helper() {
  local helper_path="${ADDON_DEFAULTS_DIR}/30-checkserverstatus.sh"
  local install_target="/home/container/bin/checkserverstatus"
  local existing_target=""
  local helper_exit=0

  mkdir -p /home/container/bin

  if [[ "$ADDON_CHECKSERVERSTATUS_ENABLED" != "true" ]]; then
    if [[ -L "$install_target" ]]; then
      existing_target="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$install_target" 2>/dev/null || true)"
      if [[ "$existing_target" == "$helper_path" ]]; then
        rm -f "$install_target"
        info "Managed checkserverstatus helper is disabled"
        return 0
      fi
      warn "ADDON_CHECKSERVERSTATUS_ENABLED=false, but preserving non-managed checkserverstatus symlink at ${install_target}"
      return 0
    fi

    if [[ -e "$install_target" ]]; then
      warn "ADDON_CHECKSERVERSTATUS_ENABLED=false, but preserving existing command file at ${install_target}"
      return 0
    fi

    info "Managed checkserverstatus helper is disabled"
    return 0
  fi

  if [[ ! -f "$helper_path" ]]; then
    warn "Managed checkserverstatus helper was not found at ${helper_path}"
    return 0
  fi

  set +e
  bash "$helper_path"
  helper_exit=$?
  set -e

  if [[ "$helper_exit" -ne 0 ]]; then
    warn "Managed checkserverstatus helper failed to refresh with exit code ${helper_exit}"
  fi
}

install_managed_chatlogger_helper() {
  local helper_path="${ADDON_DEFAULTS_DIR}/40-chatlogger.py"
  local helper_exit=0

  if [[ ! -f "$helper_path" ]]; then
    warn "Managed chat logger helper was not found at ${helper_path}"
    return 0
  fi

  if [[ "$ADDON_CHATLOGGER_ENABLED" != "true" ]]; then
    set +e
    python3 "$helper_path" --stop
    helper_exit=$?
    set -e

    if [[ "$helper_exit" -ne 0 ]]; then
      warn "Managed chat logger helper failed to stop cleanly with exit code ${helper_exit}"
    fi
    return 0
  fi

  set +e
  python3 "$helper_path"
  helper_exit=$?
  set -e

  if [[ "$helper_exit" -ne 0 ]]; then
    warn "Managed chat logger helper failed to refresh with exit code ${helper_exit}"
  fi
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

configure_addons() {
  : "${ADDONS_ENABLED:=true}"
  : "${ADDONS_DIR:=/home/container/addons}"
  : "${ADDON_CHECKSERVERSTATUS_ENABLED:=false}"
  : "${ADDON_CHATLOGGER_ENABLED:=false}"
  : "${ADDONS_STRICT:=false}"
  : "${ADDONS_TIMEOUT_SECONDS:=30}"
  : "${ADDONS_LOG_OUTPUT:=true}"

  if [[ "$ADDONS_DIR" != "/home/container" ]]; then
    ADDONS_DIR="${ADDONS_DIR%/}"
  fi

  ADDONS_ENABLED_NORMALIZED="$(printf '%s' "$ADDONS_ENABLED" | tr '[:upper:]' '[:lower:]')"
  case "$ADDONS_ENABLED_NORMALIZED" in
    true|false) ;;
    *)
      warn "ADDONS_ENABLED=${ADDONS_ENABLED} is invalid, falling back to true"
      ADDONS_ENABLED_NORMALIZED="true"
      ;;
  esac
  ADDONS_ENABLED="$ADDONS_ENABLED_NORMALIZED"

  ADDON_CHECKSERVERSTATUS_ENABLED_NORMALIZED="$(printf '%s' "$ADDON_CHECKSERVERSTATUS_ENABLED" | tr '[:upper:]' '[:lower:]')"
  case "$ADDON_CHECKSERVERSTATUS_ENABLED_NORMALIZED" in
    true|false) ;;
    *)
      warn "ADDON_CHECKSERVERSTATUS_ENABLED=${ADDON_CHECKSERVERSTATUS_ENABLED} is invalid, falling back to true"
      ADDON_CHECKSERVERSTATUS_ENABLED_NORMALIZED="true"
      ;;
  esac
  ADDON_CHECKSERVERSTATUS_ENABLED="$ADDON_CHECKSERVERSTATUS_ENABLED_NORMALIZED"

  ADDON_CHATLOGGER_ENABLED_NORMALIZED="$(printf '%s' "$ADDON_CHATLOGGER_ENABLED" | tr '[:upper:]' '[:lower:]')"
  case "$ADDON_CHATLOGGER_ENABLED_NORMALIZED" in
    true|false) ;;
    *)
      warn "ADDON_CHATLOGGER_ENABLED=${ADDON_CHATLOGGER_ENABLED} is invalid, falling back to true"
      ADDON_CHATLOGGER_ENABLED_NORMALIZED="true"
      ;;
  esac
  ADDON_CHATLOGGER_ENABLED="$ADDON_CHATLOGGER_ENABLED_NORMALIZED"

  ADDONS_STRICT_NORMALIZED="$(printf '%s' "$ADDONS_STRICT" | tr '[:upper:]' '[:lower:]')"
  case "$ADDONS_STRICT_NORMALIZED" in
    true|false) ;;
    *)
      warn "ADDONS_STRICT=${ADDONS_STRICT} is invalid, falling back to false"
      ADDONS_STRICT_NORMALIZED="false"
      ;;
  esac
  ADDONS_STRICT="$ADDONS_STRICT_NORMALIZED"

  ADDONS_LOG_OUTPUT_NORMALIZED="$(printf '%s' "$ADDONS_LOG_OUTPUT" | tr '[:upper:]' '[:lower:]')"
  case "$ADDONS_LOG_OUTPUT_NORMALIZED" in
    true|false) ;;
    *)
      warn "ADDONS_LOG_OUTPUT=${ADDONS_LOG_OUTPUT} is invalid, falling back to true"
      ADDONS_LOG_OUTPUT_NORMALIZED="true"
      ;;
  esac
  ADDONS_LOG_OUTPUT="$ADDONS_LOG_OUTPUT_NORMALIZED"

  if [[ ! "$ADDONS_TIMEOUT_SECONDS" =~ ^[0-9]+$ || "$ADDONS_TIMEOUT_SECONDS" -lt 1 || "$ADDONS_TIMEOUT_SECONDS" -gt 3600 ]]; then
    warn "ADDONS_TIMEOUT_SECONDS=${ADDONS_TIMEOUT_SECONDS} is invalid, falling back to 30"
    ADDONS_TIMEOUT_SECONDS="30"
  fi

  require_safe_container_path "$ADDONS_DIR" "ADDONS_DIR"
  ADDON_DOCS_DIR="${ADDONS_DIR}/docs"
  ADDON_EXAMPLES_DIR="${ADDONS_DIR}/examples"
  ADDON_DEFAULTS_DIR="${ADDONS_DIR}/defaults"

  ADDON_EXECUTED_COUNT=0
  ADDON_SKIPPED_COUNT=0
  ADDON_FAILED_COUNT=0
  ADDON_TIMED_OUT_COUNT=0
}

configure_anti_vpn() {
  : "${ANTI_VPN_ENABLED:=false}"
  : "${ANTI_VPN_MODE:=block}"
  : "${ANTI_VPN_CACHE_TTL:=6h}"
  : "${ANTI_VPN_SCORE_THRESHOLD:=90}"
  : "${ANTI_VPN_ALLOWLIST:=}"
  : "${ANTI_VPN_PROXYCHECK_API_KEY:=}"
  : "${ANTI_VPN_IPAPIIS_API_KEY:=}"
  : "${ANTI_VPN_IPHUB_API_KEY:=}"
  : "${ANTI_VPN_VPNAPI_IO_API_KEY:=}"
  : "${ANTI_VPN_IPQUALITYSCORE_API_KEY:=}"
  : "${ANTI_VPN_IPLOCATE_API_KEY:=}"
  : "${ANTI_VPN_TIMEOUT_MS:=1500}"
  : "${ANTI_VPN_LOG_DECISIONS:=true}"
  : "${ANTI_VPN_CACHE_PATH:=/home/container/.cache/taystjk-antivpn/cache.json}"
  : "${ANTI_VPN_CACHE_FLUSH_INTERVAL:=2s}"
  : "${ANTI_VPN_AUDIT_LOG_PATH:=/home/container/logs/anti-vpn-audit.log}"
  : "${ANTI_VPN_ENFORCEMENT_MODE:=kick-only}"
  : "${ANTI_VPN_BROADCAST_MODE:=pass-and-block}"
  : "${ANTI_VPN_BROADCAST_COOLDOWN:=90s}"
  : "${ANTI_VPN_BROADCAST_PASS_TEMPLATE:=say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%}"
  : "${ANTI_VPN_BROADCAST_BLOCK_TEMPLATE:=say [Anti-VPN] VPN BLOCKED: %PLAYER% triggered anti-VPN (%SCORE%/%THRESHOLD%). %SUMMARY%}"
  : "${ANTI_VPN_BAN_COMMAND:=}"
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

  ANTI_VPN_BROADCAST_MODE_NORMALIZED="$(printf '%s' "$ANTI_VPN_BROADCAST_MODE" | tr '[:upper:]' '[:lower:]')"
  case "$ANTI_VPN_BROADCAST_MODE_NORMALIZED" in
    off|block-only|pass-and-block) ;;
    *)
      warn "ANTI_VPN_BROADCAST_MODE=${ANTI_VPN_BROADCAST_MODE} is invalid, falling back to pass-and-block"
      ANTI_VPN_BROADCAST_MODE_NORMALIZED="pass-and-block"
      ;;
  esac
  ANTI_VPN_BROADCAST_MODE="$ANTI_VPN_BROADCAST_MODE_NORMALIZED"

  ANTI_VPN_ENFORCEMENT_MODE_NORMALIZED="$(printf '%s' "$ANTI_VPN_ENFORCEMENT_MODE" | tr '[:upper:]' '[:lower:]')"
  case "$ANTI_VPN_ENFORCEMENT_MODE_NORMALIZED" in
    kick-only|ban-and-kick|ban-only|custom) ;;
    *)
      warn "ANTI_VPN_ENFORCEMENT_MODE=${ANTI_VPN_ENFORCEMENT_MODE} is invalid, falling back to kick-only"
      ANTI_VPN_ENFORCEMENT_MODE_NORMALIZED="kick-only"
      ;;
  esac
  ANTI_VPN_ENFORCEMENT_MODE="$ANTI_VPN_ENFORCEMENT_MODE_NORMALIZED"

  if [[ "${ANTI_VPN_ENABLED,,}" != "true" || "$ANTI_VPN_MODE_NORMALIZED" == "off" ]]; then
    ANTI_VPN_EFFECTIVE_MODE="off"
  else
    ANTI_VPN_EFFECTIVE_MODE="$ANTI_VPN_MODE_NORMALIZED"
  fi

  mkdir -p "$(dirname "$ANTI_VPN_CACHE_PATH")"
  mkdir -p "$(dirname "$ANTI_VPN_AUDIT_LOG_PATH")"
}

anti_vpn_provider_row() {
  local label="$1"
  local state="$2"
  local note="${3:-}"

  if [[ "$state" == "ENABLED" ]]; then
    if [[ -n "$note" ]]; then
      kv_value "$label" "$COLOR_ACTIVE" "${state} ${note}"
    else
      kv_value "$label" "$COLOR_ACTIVE" "$state"
    fi
  else
    kv_value "$label" "$COLOR_ERROR" "DISABLED"
  fi
}

print_anti_vpn_providers() {
  anti_vpn_provider_row "proxycheck.io" "ENABLED" "$( [[ -n "$ANTI_VPN_PROXYCHECK_API_KEY" ]] && printf '' || printf '(ANONYMOUS)' )"
  anti_vpn_provider_row "ipapi.is" "ENABLED" "$( [[ -n "$ANTI_VPN_IPAPIIS_API_KEY" ]] && printf '' || printf '(ANONYMOUS)' )"

  if [[ -n "$ANTI_VPN_IPHUB_API_KEY" ]]; then
    anti_vpn_provider_row "IPHub" "ENABLED"
  else
    anti_vpn_provider_row "IPHub" "DISABLED"
  fi

  if [[ -n "$ANTI_VPN_VPNAPI_IO_API_KEY" ]]; then
    anti_vpn_provider_row "vpnapi.io" "ENABLED"
  else
    anti_vpn_provider_row "vpnapi.io" "DISABLED"
  fi

  if [[ -n "$ANTI_VPN_IPQUALITYSCORE_API_KEY" ]]; then
    anti_vpn_provider_row "IPQualityScore" "ENABLED"
  else
    anti_vpn_provider_row "IPQualityScore" "DISABLED"
  fi

  if [[ -n "$ANTI_VPN_IPLOCATE_API_KEY" ]]; then
    anti_vpn_provider_row "IPLocate" "ENABLED"
  else
    anti_vpn_provider_row "IPLocate" "DISABLED"
  fi
}

anti_vpn_allowlist_status() {
  if [[ -n "${ANTI_VPN_ALLOWLIST//[[:space:],]/}" ]]; then
    printf 'configured\n'
  else
    printf 'not set\n'
  fi
}

print_addon_summary() {
  section "ADDONS"
  kv_highlight "Status" "$(printf '%s' "$(bool_state "$ADDONS_ENABLED")" | tr '[:lower:]' '[:upper:]')"
  kv "User dir" "$ADDONS_DIR"
  kv "Docs dir" "$ADDON_DOCS_DIR"
  kv "Examples dir" "$ADDON_EXAMPLES_DIR"
  kv "Defaults dir" "$ADDON_DEFAULTS_DIR"
  kv "Checkserverstatus" "$(printf '%s' "$(bool_state "$ADDON_CHECKSERVERSTATUS_ENABLED")" | tr '[:lower:]' '[:upper:]')"
  kv "Chatlogger" "$(printf '%s' "$(bool_state "$ADDON_CHATLOGGER_ENABLED")" | tr '[:lower:]' '[:upper:]')"
  kv "Strict" "$(printf '%s' "$(bool_state "$ADDONS_STRICT")" | tr '[:lower:]' '[:upper:]')"
  kv "Timeout" "${ADDONS_TIMEOUT_SECONDS}s"
  kv "Log output" "$(printf '%s' "$(bool_state "$ADDONS_LOG_OUTPUT")" | tr '[:lower:]' '[:upper:]')"

  if [[ "$ADDONS_ENABLED" != "true" ]]; then
    warn "User addon execution is disabled; managed docs, examples, and helpers still refresh"
  elif [[ -d "$ADDONS_DIR" ]]; then
    ok "User addon directory ready"
  else
    info "No user addon directory found; continuing without addon execution"
  fi

  if [[ -d "${ADDONS_DIR}/bundled-addons" ]]; then
    warn "Legacy bundled-addons directory detected; it is no longer executed by the addon loader"
  fi
}

run_addons() {
  local entry_name=""
  local entry_path=""
  local addon_kind=""
  local addon_exit=0
  local addon_entries=()
  local addon_count=0
  local addon_candidate_count=0

  if [[ "$ADDONS_ENABLED" != "true" ]]; then
    return 0
  fi

  if [[ ! -d "$ADDONS_DIR" ]]; then
    return 0
  fi

  while IFS= read -r entry_name; do
    addon_entries+=("$entry_name")
  done < <(find "$ADDONS_DIR" -mindepth 1 -maxdepth 1 -type f -printf '%f\n' | LC_ALL=C sort)

  addon_count="${#addon_entries[@]}"

  if [[ "$addon_count" -eq 0 ]]; then
    info "No top-level addon files found in ${ADDONS_DIR}; continuing without addon execution"
    return 0
  fi

  for entry_name in "${addon_entries[@]}"; do
    [[ "$entry_name" == .* ]] && continue
    case "$entry_name" in
      *.md|*.json|*.txt|*.disable)
        continue
        ;;
    esac
    addon_candidate_count=$((addon_candidate_count + 1))
  done

  if [[ "$addon_candidate_count" -eq 0 ]]; then
    info "No executable top-level addon scripts were found in ${ADDONS_DIR}; continuing without addon execution"
    return 0
  fi

  info "Scanning ${addon_candidate_count} user addon candidate$( [[ "$addon_candidate_count" -eq 1 ]] && printf '' || printf 's' ) in ${ADDONS_DIR}"

  for entry_name in "${addon_entries[@]}"; do
    entry_path="${ADDONS_DIR}/${entry_name}"

    if [[ "$entry_name" == .* ]]; then
      warn "Skipping hidden addon entry: ${entry_name}"
      ADDON_SKIPPED_COUNT=$((ADDON_SKIPPED_COUNT + 1))
      continue
    fi

    if [[ ! -f "$entry_path" ]]; then
      warn "Skipping non-file addon entry: ${entry_name}"
      ADDON_SKIPPED_COUNT=$((ADDON_SKIPPED_COUNT + 1))
      continue
    fi

    case "$entry_name" in
      *.md|*.json|*.txt)
        debug "Ignoring addon support file: ${entry_name}"
        continue
        ;;
      *.disable)
        info "Addon disabled by filename suffix: ${entry_name}"
        ADDON_SKIPPED_COUNT=$((ADDON_SKIPPED_COUNT + 1))
        continue
        ;;
    esac

    info "Addon detected: ${entry_name}"

    case "$entry_name" in
      *.sh)
        addon_kind="bash"
        ;;
      *.py)
        addon_kind="python3"
        if ! command -v python3 >/dev/null 2>&1; then
          warn "Addon failed: python3 is not available for ${entry_name}"
          ADDON_FAILED_COUNT=$((ADDON_FAILED_COUNT + 1))
          [[ "$ADDONS_STRICT" == "true" ]] && fail "Addon ${entry_name} requires python3, but python3 is not available in the runtime image"
          continue
        fi
        ;;
      *)
        warn "Skipping unsupported addon file: ${entry_name}"
        ADDON_SKIPPED_COUNT=$((ADDON_SKIPPED_COUNT + 1))
        continue
        ;;
    esac

    info "Executing ${addon_kind} addon: ${entry_name}"
    set +e
    if [[ "$ADDONS_LOG_OUTPUT" == "true" ]]; then
      timeout --foreground "${ADDONS_TIMEOUT_SECONDS}" "$addon_kind" "$entry_path"
    else
      timeout --foreground "${ADDONS_TIMEOUT_SECONDS}" "$addon_kind" "$entry_path" >/dev/null 2>&1
    fi
    addon_exit=$?
    set -e

    case "$addon_exit" in
      0)
        ok "Addon completed successfully: ${entry_name}"
        ADDON_EXECUTED_COUNT=$((ADDON_EXECUTED_COUNT + 1))
        ;;
      124|137)
        warn "Addon timed out after ${ADDONS_TIMEOUT_SECONDS}s: ${entry_name}"
        ADDON_TIMED_OUT_COUNT=$((ADDON_TIMED_OUT_COUNT + 1))
        [[ "$ADDONS_STRICT" == "true" ]] && fail "Addon ${entry_name} timed out after ${ADDONS_TIMEOUT_SECONDS}s"
        ;;
      *)
        warn "Addon failed with exit code ${addon_exit}: ${entry_name}"
        ADDON_FAILED_COUNT=$((ADDON_FAILED_COUNT + 1))
        [[ "$ADDONS_STRICT" == "true" ]] && fail "Addon ${entry_name} failed with exit code ${addon_exit}"
        ;;
    esac
  done

  info "Addon execution summary: executed=${ADDON_EXECUTED_COUNT}, skipped=${ADDON_SKIPPED_COUNT}, failed=${ADDON_FAILED_COUNT}, timed_out=${ADDON_TIMED_OUT_COUNT}"
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

print_anti_vpn_summary() {
  section "ANTI-VPN"
  kv_highlight "Status" "$(printf '%s' "$(bool_state "$ANTI_VPN_ENABLED")" | tr '[:lower:]' '[:upper:]')"
  kv_highlight "Mode" "$(printf '%s' "$ANTI_VPN_EFFECTIVE_MODE" | tr '[:lower:]' '[:upper:]')"
  kv_highlight "Enforce" "$(printf '%s' "$ANTI_VPN_ENFORCEMENT_MODE_NORMALIZED" | tr '[:lower:]' '[:upper:]')"
  kv_highlight "Broadcast" "$(printf '%s' "$ANTI_VPN_BROADCAST_MODE_NORMALIZED" | tr '[:lower:]' '[:upper:]')"
  kv_highlight "Threshold" "$ANTI_VPN_SCORE_THRESHOLD"
  kv "Allowlist" "$(anti_vpn_allowlist_status)"
  print_anti_vpn_providers
  debug "Capture mode: stdout-first with server.log fallback"
  debug "Decision logs: $(printf '%s' "$(bool_state "$ANTI_VPN_LOG_DECISIONS")" | tr '[:lower:]' '[:upper:]')"
  debug "Cache TTL: ${ANTI_VPN_CACHE_TTL}"
  debug "Cache flush: ${ANTI_VPN_CACHE_FLUSH_INTERVAL}"
  debug "Timeout: ${ANTI_VPN_TIMEOUT_MS}"
  debug "Broadcast cooldown: ${ANTI_VPN_BROADCAST_COOLDOWN}"

  if [[ "$ANTI_VPN_EFFECTIVE_MODE" == "off" ]]; then
    warn "Anti-VPN supervision is disabled"
    return
  fi

  if [[ -x /usr/local/bin/taystjk-antivpn ]]; then
    ok "Anti-VPN supervisor ready"
    debug "Supervisor binary: /usr/local/bin/taystjk-antivpn"
  else
    warn "Anti-VPN supervisor binary is missing; startup will continue without anti-VPN enforcement"
  fi
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

count_dir_files() {
  local path="$1"
  local pattern="${2:-*}"

  if [[ ! -d "$path" ]]; then
    printf '0\n'
    return
  fi

  find "$path" -maxdepth 1 -type f -name "$pattern" | wc -l | tr -d ' '
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
  kv "Log path" "$ANTI_VPN_LOG_PATH"
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
configure_addons
configure_server_settings
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
install_managed_chatlogger_helper
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
