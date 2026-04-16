#!/usr/bin/env bash
set -euo pipefail

log() {
  echo "[entrypoint] $*"
}

fail() {
  echo "[entrypoint][error] $*" >&2
  exit 1
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

cd /home/container

: "${SERVER_BINARY:=taystjkded.x86_64}"
: "${SERVER_PORT:=29070}"
: "${SERVER_CONFIG:=server.cfg}"
: "${EXTRA_STARTUP_ARGS:=}"
: "${FS_GAME_MOD:=taystjk}"
: "${COPYRIGHT_ACKNOWLEDGED:=false}"

[[ "${COPYRIGHT_ACKNOWLEDGED}" == "true" ]] || fail "COPYRIGHT_ACKNOWLEDGED must be true. This image does not ship Jedi Academy base assets."

require_safe_component "$SERVER_CONFIG" "SERVER_CONFIG"
server_binary_name="$(normalize_server_binary_name)"
active_game_dir="$(resolve_active_game_dir "$FS_GAME_MOD")"
server_binary_path="/home/container/${server_binary_name}"

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
  log "Starting custom command"
  exec "$@"
fi

require_base_assets
parse_extra_startup_args
build_startup_command

if [[ "$#" -gt 0 ]]; then
  log "Starting server using panel startup variables"
else
  log "No startup command arguments were provided, using image fallback startup"
fi

exec "${STARTUP_COMMAND[@]}"
