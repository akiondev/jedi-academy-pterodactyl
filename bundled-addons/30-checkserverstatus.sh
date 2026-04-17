#!/usr/bin/env bash
set -euo pipefail

# Bundled example addon: install a practical admin command called
# `checkserverstatus`. The same file acts as:
# 1. the startup addon that ensures the command is available on PATH
# 2. the live command implementation when invoked as `checkserverstatus`

COMMAND_NAME="checkserverstatus"
INSTALL_TARGET="/home/container/bin/${COMMAND_NAME}"
ADDON_LABEL="[addon:bash-status]"
SELF_PATH="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "${BASH_SOURCE[0]}")"
RUNTIME_ENV_FILE="/home/container/.runtime/taystjk-effective.env"

log() {
  printf '%s %s\n' "${ADDON_LABEL}" "$*"
}

load_runtime_state() {
  if [[ -f "$RUNTIME_ENV_FILE" ]]; then
    # shellcheck disable=SC1090
    source "$RUNTIME_ENV_FILE"
  fi
}

active_mod_dir() {
  if [[ -n "${TAYSTJK_ACTIVE_MOD_DIR:-}" ]]; then
    printf '%s\n' "$TAYSTJK_ACTIVE_MOD_DIR"
    return 0
  fi
  printf '%s\n' "${FS_GAME_MOD:-taystjk}"
}

active_server_config() {
  if [[ -n "${TAYSTJK_ACTIVE_SERVER_CONFIG:-}" ]]; then
    printf '%s\n' "$TAYSTJK_ACTIVE_SERVER_CONFIG"
    return 0
  fi
  printf '%s\n' "${SERVER_CONFIG:-server.cfg}"
}

active_server_port() {
  if [[ -n "${TAYSTJK_EFFECTIVE_SERVER_PORT:-}" ]]; then
    printf '%s\n' "$TAYSTJK_EFFECTIVE_SERVER_PORT"
    return 0
  fi
  printf '%s\n' "${SERVER_PORT:-29070}"
}

active_server_config_path() {
  if [[ -n "${TAYSTJK_ACTIVE_SERVER_CONFIG_PATH:-}" ]]; then
    printf '%s\n' "$TAYSTJK_ACTIVE_SERVER_CONFIG_PATH"
    return 0
  fi
  printf '/home/container/%s/%s\n' "$(active_mod_dir)" "$(active_server_config)"
}

extract_rcon_password() {
  local config_path="$1"

  [[ -f "$config_path" ]] || return 1

  awk '
    BEGIN { IGNORECASE = 1 }
    match($0, /^[[:space:]]*set[a-z]*[[:space:]]+rconpassword[[:space:]]+"([^"]+)"/, found) {
      print found[1]
      exit
    }
    match($0, /^[[:space:]]*set[a-z]*[[:space:]]+rconpassword[[:space:]]+([^[:space:]]+)/, found) {
      print found[1]
      exit
    }
  ' "$config_path"
}

query_live_status() {
  local port="$1"
  local password="$2"

  python3 - "$port" "$password" <<'PY'
import socket
import sys

port = int(sys.argv[1])
password = sys.argv[2]
packet = b"\xff\xff\xff\xffrcon " + password.encode("utf-8") + b" status"

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.settimeout(3)

try:
    sock.sendto(packet, ("127.0.0.1", port))
    response, _ = sock.recvfrom(65535)
except OSError:
    sys.exit(1)
finally:
    sock.close()

text = response.decode("utf-8", errors="replace").lstrip("\xff")
if text.startswith("print\n"):
    text = text[6:]

print(text.strip())
PY
}

effective_rcon_password() {
  if [[ -n "${TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD:-}" ]]; then
    printf '%s\n' "$TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD"
    return 0
  fi

  extract_rcon_password "$(active_server_config_path)" || true
}

print_basic_server_info() {
  local process_line=""
  local config_path="$1"
  local log_path="/home/container/$(active_mod_dir)/server.log"

  process_line="$(pgrep -af 'taystjkded' | head -n 1 || true)"

  printf '=== TaystJK Server Status ===\n'
  printf 'Time           : %s\n' "$(date '+%Y-%m-%d %H:%M:%S %Z')"
  printf 'Mod directory  : %s\n' "$(active_mod_dir)"
  printf 'Server config  : %s\n' "$config_path"
  printf 'Server port    : %s\n' "$(active_server_port)"
  printf 'Log file       : %s\n' "$log_path"

  if [[ -n "$process_line" ]]; then
    printf 'Server process : RUNNING (%s)\n' "$process_line"
  else
    printf 'Server process : NOT RUNNING\n'
  fi
}

print_live_player_section() {
  local status_output="$1"
  local current_map=""
  local players_block=""
  local player_count="0"

  current_map="$(printf '%s\n' "$status_output" | sed -n 's/^map: //p' | head -n 1)"
  players_block="$(printf '%s\n' "$status_output" | awk '
    /^num[[:space:]]+score[[:space:]]+ping[[:space:]]+name/ {
      capture = 1
    }
    capture {
      print
    }
  ')"

  if [[ -n "$current_map" ]]; then
    printf 'Current map    : %s\n' "$current_map"
  else
    printf 'Current map    : unavailable\n'
  fi

  if [[ -n "$players_block" ]]; then
    player_count="$(printf '%s\n' "$players_block" | awk 'NR > 1 && NF { count++ } END { print count + 0 }')"
    printf 'Online players : %s\n\n' "$player_count"
    printf '%s\n' "$players_block"
  else
    printf 'Online players : 0\n'
  fi
}

run_status_command() {
  local config_path=""
  local password=""
  local live_status=""

  load_runtime_state
  config_path="$(active_server_config_path)"
  print_basic_server_info "$config_path"

  password="$(effective_rcon_password)"
  if [[ -z "$password" ]]; then
    printf 'Current map    : unavailable\n'
    printf 'Online players : unavailable\n\n'
    printf 'Set SERVER_RCON_PASSWORD in the egg or rconpassword in the active server config to enable live player lookups.\n'
    return 0
  fi

  live_status="$(query_live_status "$(active_server_port)" "$password" 2>/dev/null || true)"
  if [[ -z "$live_status" ]]; then
    printf 'Current map    : unavailable\n'
    printf 'Online players : unavailable\n\n'
    printf 'The server did not return a live RCON status response. Confirm the server is running and rconpassword is correct.\n'
    return 0
  fi

  printf '\n'
  print_live_player_section "$live_status"
}

install_status_command() {
  local existing_target=""

  mkdir -p /home/container/bin

  if [[ -L "$INSTALL_TARGET" ]]; then
    existing_target="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$INSTALL_TARGET")"
    if [[ "$existing_target" == "$SELF_PATH" ]]; then
      log "Command already available at ${INSTALL_TARGET}"
      return 0
    fi
    log "Preserving existing command symlink at ${INSTALL_TARGET}"
    return 0
  fi

  if [[ -e "$INSTALL_TARGET" ]]; then
    log "Preserving existing command file at ${INSTALL_TARGET}"
    return 0
  fi

  ln -s "$SELF_PATH" "$INSTALL_TARGET"
  log "Installed ${COMMAND_NAME} into /home/container/bin"
  log "Run '${COMMAND_NAME}' inside the container shell to view current server status"
}

if [[ "$(basename "$0")" == "${COMMAND_NAME}" ]]; then
  run_status_command
else
  install_status_command
fi
