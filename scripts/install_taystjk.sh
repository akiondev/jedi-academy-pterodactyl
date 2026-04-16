#!/usr/bin/env bash
set -euo pipefail

log() {
  echo "[install] $*"
}
fail() {
  echo "[install][error] $*" >&2
  exit 1
}

cd /mnt/server

: "${GAME_ASSETS_MODE:=manual}"
: "${GAME_ASSETS_URL:=}"
: "${GAME_ASSETS_ARCHIVE_TYPE:=auto}"
: "${GAME_ASSETS_SHA256:=}"
: "${COPYRIGHT_ACKNOWLEDGED:=false}"
: "${SERVER_PORT:=29070}"
: "${SERVER_CONFIG:=server.cfg}"
: "${FS_GAME_MOD:=taystjk}"

[[ "${COPYRIGHT_ACKNOWLEDGED}" == "true" ]] || fail "COPYRIGHT_ACKNOWLEDGED must be true. This egg does not distribute Jedi Academy assets."

mkdir -p /mnt/server/base /mnt/server/logs /tmp/taystjk-install

mod_dir="${FS_GAME_MOD}"
mod_dir_lower="$(printf '%s' "$mod_dir" | tr '[:upper:]' '[:lower:]')"
if [[ -z "$mod_dir_lower" || "$mod_dir_lower" == "base" ]]; then
  active_game_dir="base"
else
  active_game_dir="$mod_dir"
fi
mkdir -p "/mnt/server/${active_game_dir}"

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

if [[ ! -f "/mnt/server/${active_game_dir}/${SERVER_CONFIG}" ]]; then
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
    log "GAME_ASSETS_MODE=manual"
    log "Upload your legally owned Jedi Academy files so that /mnt/server/base/assets0.pk3 exists"
    ;;
  url)
    [[ -n "$GAME_ASSETS_URL" ]] || fail "GAME_ASSETS_MODE=url requires GAME_ASSETS_URL"
    local_archive="/tmp/taystjk-install/game_assets"
    log "Downloading user-provided game assets archive"
    curl -L --fail --retry 3 "$GAME_ASSETS_URL" -o "$local_archive"

    if [[ -n "$GAME_ASSETS_SHA256" ]]; then
      log "Verifying SHA256"
      echo "${GAME_ASSETS_SHA256}  ${local_archive}" | sha256sum -c -
    fi

    extract_assets_archive "$local_archive"
    ;;
  none)
    log "GAME_ASSETS_MODE=none"
    log "Assuming assets will be mounted or added later"
    ;;
  *)
    fail "Unsupported GAME_ASSETS_MODE: $GAME_ASSETS_MODE"
    ;;
esac

log "Selected fs_game mod directory: ${active_game_dir}"
if [[ "$active_game_dir" != "base" ]]; then
  log "If you switch to japlus, japro, mbii or another mod, install that mod manually into /mnt/server/${active_game_dir}"
fi

if [[ -f /mnt/server/base/assets0.pk3 ]]; then
  log "Detected base/assets0.pk3"
else
  log "No base/assets0.pk3 present yet"
fi

log "Installation complete"
