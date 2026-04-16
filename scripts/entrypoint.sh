#!/usr/bin/env bash
set -euo pipefail

log() {
  echo "[entrypoint] $*"
}
fail() {
  echo "[entrypoint][error] $*" >&2
  exit 1
}

cd /home/container

: "${SERVER_BINARY:=./taystjkded.x86_64}"
: "${SERVER_PORT:=29070}"
: "${SERVER_CONFIG:=server.cfg}"
: "${EXTRA_STARTUP_ARGS:=}"
: "${FS_GAME_MOD:=taystjk}"
: "${COPYRIGHT_ACKNOWLEDGED:=false}"

[[ "${COPYRIGHT_ACKNOWLEDGED}" == "true" ]] || fail "COPYRIGHT_ACKNOWLEDGED must be true. This image does not ship Jedi Academy base assets."

mkdir -p /home/container/base /home/container/logs

if [[ ! -f /home/container/taystjkded.x86_64 && -f /opt/taystjk-dist/taystjkded.x86_64 ]]; then
  log "Copying TaystJK dedicated server files into container volume"
  cp -an /opt/taystjk-dist/taystjkded.x86_64 /home/container/
  cp -a /opt/taystjk-dist/taystjk /home/container/ 2>/dev/null || true
fi

[[ -f /home/container/taystjkded.x86_64 ]] || fail "taystjkded.x86_64 not found in container volume"
chmod +x /home/container/taystjkded.x86_64

mod_dir="${FS_GAME_MOD}"
mod_dir_lower="$(printf '%s' "$mod_dir" | tr '[:upper:]' '[:lower:]')"
if [[ -z "$mod_dir_lower" || "$mod_dir_lower" == "base" ]]; then
  active_game_dir="base"
  default_fs_game_args=()
else
  active_game_dir="$mod_dir"
  default_fs_game_args=(+set fs_game "$mod_dir")
fi

mkdir -p "/home/container/${active_game_dir}"

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

if [[ ! -f /home/container/base/assets0.pk3 ]]; then
  log "Missing /home/container/base/assets0.pk3"
  log "Provide your legally owned Jedi Academy base assets before the server can fully start"
fi

export HOME=/home/container

if [[ "$#" -gt 0 ]]; then
  log "Starting server using panel startup command"
  exec "$@"
fi

log "No startup command arguments were provided, using image fallback startup"
exec ${SERVER_BINARY} \
  +set dedicated 2 \
  +set net_port "${SERVER_PORT}" \
  +set fs_cdpath /home/container \
  +set fs_basepath /home/container \
  +set fs_homepath /home/container \
  "${default_fs_game_args[@]}" \
  +exec "${SERVER_CONFIG}" \
  ${EXTRA_STARTUP_ARGS}
