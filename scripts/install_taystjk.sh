#!/usr/bin/env bash
set -euo pipefail

COLOR_RESET=""
COLOR_BOLD=""
COLOR_INFO=""
COLOR_OK=""
COLOR_WARN=""
COLOR_ERROR=""

setup_colors() {
  if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
    COLOR_RESET=$'\033[0m'
    COLOR_BOLD=$'\033[1m'
    COLOR_INFO=$'\033[36m'
    COLOR_OK=$'\033[32m'
    COLOR_WARN=$'\033[33m'
    COLOR_ERROR=$'\033[31m'
  fi
}

print_header() {
  printf '%s\n' "============================================================"
  printf '%s\n' " TaystJK Installer"
  printf '%s\n' " Created by akiondev"
  printf '%s\n\n' "============================================================"
}

section() {
  printf '%b\n' "${COLOR_BOLD}${1}${COLOR_RESET}"
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
  printf '%b\n' "${COLOR_ERROR}[ERROR]${COLOR_RESET} $*" >&2
  printf '\n' >&2
  section "What to Check Next" >&2
  printf '%s\n' "- Confirm the selected asset archive contains GameData/base/assets0.pk3 or base/assets0.pk3" >&2
  printf '%s\n' "- Confirm COPYRIGHT_ACKNOWLEDGED is set to true" >&2
  printf '%s\n' "- Confirm the selected mod directory and config names are valid" >&2
  exit 1
}

require_safe_component() {
  local value="$1"
  local variable_name="$2"

  if [[ -z "$value" || "$value" == "." || "$value" == ".." || "$value" == *"/"* || "$value" == *"\\"* || ! "$value" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
    fail "${variable_name} must be a simple relative name using only letters, numbers, dots, underscores or dashes"
  fi
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

is_taystjk_managed_mod_dir() {
  local mod_dir="$1"
  [[ "$mod_dir" == "taystjk" ]]
}

is_base_mode() {
  local mod_dir="$1"
  [[ "$mod_dir" == "base" ]]
}

describe_mod_ownership() {
  local mod_dir="$1"

  if is_taystjk_managed_mod_dir "$mod_dir"; then
    printf 'image-managed TaystJK\n'
  elif is_base_mode "$mod_dir"; then
    printf 'manual base assets\n'
  else
    printf 'manual user-supplied\n'
  fi
}

prepare_selected_mod_directory() {
  local mod_path="/mnt/server/${active_game_dir}"

  if is_taystjk_managed_mod_dir "$active_game_dir"; then
    mkdir -p "$mod_path"
    return 0
  fi

  if is_base_mode "$active_game_dir"; then
    return 0
  fi

  if [[ -d "$mod_path" ]]; then
    return 0
  fi

  warn "Configured manual mod directory ${active_game_dir} does not exist yet"
  warn "Only taystjk is prepared automatically. Upload manual alternative mod folders into ${mod_path} before startup"
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

print_install_summary() {
  section "Installation Summary"
  kv "Assets mode    :" "$GAME_ASSETS_MODE"
  kv "Mod directory  :" "$active_game_dir"
  kv "Mod mode       :" "$(describe_mod_ownership "$active_game_dir")"
  kv "Server config  :" "${active_game_dir}/${SERVER_CONFIG}"
  kv "Server port    :" "$SERVER_PORT"
  printf '\n'
}

print_install_checks() {
  section "Installation Checks"
  ok "Server directories created"
  if is_taystjk_managed_mod_dir "$active_game_dir"; then
    ok "Managed TaystJK mod directory prepared at /mnt/server/${active_game_dir}"
  elif is_base_mode "$active_game_dir"; then
    ok "Base assets directory ready at /mnt/server/base"
  elif [[ -d "/mnt/server/${active_game_dir}" ]]; then
    ok "Manual mod directory detected at /mnt/server/${active_game_dir}"
  else
    warn "Manual mod directory is not present yet at /mnt/server/${active_game_dir}"
  fi
  print_path_status "Base assets" "/mnt/server/base/assets0.pk3"
  printf '\n'
}

print_install_inventory() {
  section "File Inventory"
  kv "Base files      :" "$(list_dir_files /mnt/server/base 'assets*.pk3')"
  kv "Mod files       :" "$(list_dir_files "/mnt/server/${active_game_dir}")"
  printf '\n'
}

print_install_result() {
  section "Result"
  info "Installation complete"
  if [[ -f /mnt/server/base/assets0.pk3 ]]; then
    ok "Server can proceed to runtime startup checks"
  else
    warn "Upload your legally owned Jedi Academy base assets to /mnt/server/base"
  fi
}

cd /mnt/server
setup_colors
print_header

: "${GAME_ASSETS_MODE:=manual}"
: "${GAME_ASSETS_URL:=}"
: "${GAME_ASSETS_ARCHIVE_TYPE:=auto}"
: "${GAME_ASSETS_SHA256:=}"
: "${COPYRIGHT_ACKNOWLEDGED:=false}"
: "${SERVER_PORT:=29070}"
: "${SERVER_CONFIG:=server.cfg}"
: "${FS_GAME_MOD:=taystjk}"

[[ "${COPYRIGHT_ACKNOWLEDGED}" == "true" ]] || fail "COPYRIGHT_ACKNOWLEDGED must be true. This egg does not distribute Jedi Academy assets."

mkdir -p /mnt/server/base /mnt/server/logs /mnt/server/addons /mnt/server/addons/bundled-addons /tmp/taystjk-install

require_safe_component "$SERVER_CONFIG" "SERVER_CONFIG"
active_game_dir="$(resolve_active_game_dir "$FS_GAME_MOD")"
prepare_selected_mod_directory
print_install_summary

extract_assets_archive() {
  local archive="$1"
  local workdir="/tmp/taystjk-install/assets"
  rm -rf "$workdir"
  mkdir -p "$workdir"

  case "$GAME_ASSETS_ARCHIVE_TYPE" in
    auto)
      if tar -tzf "$archive" >/dev/null 2>&1; then
        tar -xzf "$archive" -C "$workdir"
      elif unzip -t "$archive" >/dev/null 2>&1; then
        unzip -oq "$archive" -d "$workdir"
      else
        fail "Could not detect asset archive type automatically."
      fi
      ;;
    tar.gz|tgz)
      tar -xzf "$archive" -C "$workdir"
      ;;
    zip)
      unzip -oq "$archive" -d "$workdir"
      ;;
    *)
      fail "Unsupported GAME_ASSETS_ARCHIVE_TYPE: $GAME_ASSETS_ARCHIVE_TYPE"
      ;;
  esac

  if [[ -d "$workdir/GameData/base" ]]; then
    cp -an "$workdir/GameData/base"/* /mnt/server/base/ || true
  elif [[ -d "$workdir/base" ]]; then
    cp -an "$workdir/base"/* /mnt/server/base/ || true
  else
    cp -an "$workdir"/* /mnt/server/base/ || true
  fi

  [[ -f /mnt/server/base/assets0.pk3 ]] || fail "Asset archive extracted, but base/assets0.pk3 was not found."
}

if is_taystjk_managed_mod_dir "$active_game_dir" && [[ ! -f "/mnt/server/${active_game_dir}/${SERVER_CONFIG}" ]]; then
  cat > "/mnt/server/${active_game_dir}/${SERVER_CONFIG}" <<CFG
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

case "$GAME_ASSETS_MODE" in
  manual)
    info "GAME_ASSETS_MODE is set to manual"
    warn "Upload your legally owned Jedi Academy files so that /mnt/server/base/assets0.pk3 exists"
    ;;
  url)
    [[ -n "$GAME_ASSETS_URL" ]] || fail "GAME_ASSETS_MODE=url requires GAME_ASSETS_URL"
    local_archive="/tmp/taystjk-install/game_assets"
    info "Downloading user-provided game assets archive"
    curl -L --fail --retry 3 "$GAME_ASSETS_URL" -o "$local_archive"

    if [[ -n "$GAME_ASSETS_SHA256" ]]; then
      info "Verifying SHA256"
      echo "${GAME_ASSETS_SHA256}  ${local_archive}" | sha256sum -c -
    fi

    extract_assets_archive "$local_archive"
    ;;
  none)
    info "GAME_ASSETS_MODE is set to none"
    warn "Assuming assets will be mounted or added later"
    ;;
  *)
    fail "Unsupported GAME_ASSETS_MODE: $GAME_ASSETS_MODE"
    ;;
esac

print_install_checks
print_install_inventory
info "Selected fs_game mod directory: ${active_game_dir}"
if is_taystjk_managed_mod_dir "$active_game_dir"; then
  ok "taystjk remains the only automatically prepared mod directory"
else
  warn "Selected mod directory ${active_game_dir} is treated as a manual user-owned path"
  warn "Provide ${active_game_dir}/${SERVER_CONFIG} and any required mod files yourself before startup"
fi

print_install_result
