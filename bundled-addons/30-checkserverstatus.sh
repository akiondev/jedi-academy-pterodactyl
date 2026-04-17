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

COLOR_RESET=$'\033[0m'
COLOR_BOLD=$'\033[1m'
COLOR_DIM=$'\033[2m'
COLOR_BLUE=$'\033[38;5;39m'
COLOR_PURPLE=$'\033[38;5;141m'
COLOR_GREEN=$'\033[38;5;82m'
COLOR_YELLOW=$'\033[38;5;220m'
COLOR_RED=$'\033[38;5;203m'
COLOR_CYAN=$'\033[38;5;45m'
COLOR_GRAY=$'\033[38;5;245m'

log() {
  printf '%s %s\n' "${ADDON_LABEL}" "$*"
}

divider() {
  printf '%s------------------------------------------------------------%s\n' "${COLOR_GRAY}" "${COLOR_RESET}"
}

section() {
  divider
  printf '%s%s[%s]%s\n' "${COLOR_BOLD}" "${COLOR_PURPLE}" "$1" "${COLOR_RESET}"
  divider
}

kv() {
  printf '%s%-15s%s : %s\n' "${COLOR_DIM}" "$1" "${COLOR_RESET}" "$2"
}

status_value() {
  local value="$1"
  local color="$2"
  printf '%s%s%s%s' "${COLOR_BOLD}" "${color}" "$value" "${COLOR_RESET}"
}

render_state() {
  local value="$1"
  case "$value" in
    RUNNING|CONFIGURED|AVAILABLE)
      status_value "$value" "$COLOR_GREEN"
      ;;
    UNAVAILABLE|NOT\ SET)
      status_value "$value" "$COLOR_YELLOW"
      ;;
    NOT\ RUNNING|FAILED)
      status_value "$value" "$COLOR_RED"
      ;;
    *)
      status_value "$value" "$COLOR_CYAN"
      ;;
  esac
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
  local runtime_env="$RUNTIME_ENV_FILE"
  local process_state="NOT RUNNING"
  local rcon_state="NOT SET"

  process_line="$(pgrep -af 'taystjkded' | head -n 1 || true)"
  [[ -n "$process_line" ]] && process_state="RUNNING"
  [[ -n "$(effective_rcon_password)" ]] && rcon_state="CONFIGURED"

  printf '%s%sTaystJK Server Status%s\n' "${COLOR_BOLD}" "${COLOR_BLUE}" "${COLOR_RESET}"
  section "SERVER"
  kv "Time" "$(date '+%Y-%m-%d %H:%M:%S %Z')"
  kv "Mod" "$(active_mod_dir)"
  kv "Config" "$config_path"
  kv "Port" "$(status_value "$(active_server_port)" "$COLOR_CYAN")"
  kv "RCON" "$(render_state "$rcon_state")"
  kv "Runtime env" "$runtime_env"
  kv "Log file" "$log_path"
  kv "Process" "$(render_state "$process_state")"

  if [[ -n "$process_line" ]]; then
    kv "Process cmd" "$process_line"
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

  section "LIVE STATUS"
  if [[ -n "$current_map" ]]; then
    kv "Current map" "$(status_value "$current_map" "$COLOR_CYAN")"
  else
    kv "Current map" "$(render_state "UNAVAILABLE")"
  fi

  if [[ -n "$players_block" ]]; then
    player_count="$(printf '%s\n' "$players_block" | awk 'NR > 1 && NF { count++ } END { print count + 0 }')"
    kv "Online players" "$(status_value "$player_count" "$COLOR_GREEN")"
    printf '\n%s%sPlayers%s\n' "${COLOR_BOLD}" "${COLOR_PURPLE}" "${COLOR_RESET}"
    printf '%s\n' "$players_block" | awk -v header="${COLOR_BOLD}${COLOR_CYAN}" -v row="${COLOR_RESET}" -v reset="${COLOR_RESET}" '
      NR == 1 { printf "%s%s%s\n", header, $0, reset; next }
      { printf "%s%s%s\n", row, $0, reset }
    '
  else
    kv "Online players" "$(status_value "0" "$COLOR_YELLOW")"
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
    section "LIVE STATUS"
    kv "Current map" "$(render_state "UNAVAILABLE")"
    kv "Online players" "$(render_state "UNAVAILABLE")"
    printf '\n%sHint%s: Set %sSERVER_RCON_PASSWORD%s in the egg or %srconpassword%s in the active server config to enable live player lookups.\n' \
      "${COLOR_BOLD}" "${COLOR_RESET}" "${COLOR_CYAN}" "${COLOR_RESET}" "${COLOR_CYAN}" "${COLOR_RESET}"
    return 0
  fi

  live_status="$(query_live_status "$(active_server_port)" "$password" 2>/dev/null || true)"
  if [[ -z "$live_status" ]]; then
    section "LIVE STATUS"
    kv "Current map" "$(render_state "UNAVAILABLE")"
    kv "Online players" "$(render_state "UNAVAILABLE")"
    printf '\n%sWarning%s: The server did not return a live RCON status response. Confirm the server is running and %srconpassword%s is correct.\n' \
      "${COLOR_BOLD}${COLOR_YELLOW}" "${COLOR_RESET}" "${COLOR_CYAN}" "${COLOR_RESET}"
    return 0
  fi

  printf '\n'
  print_live_player_section "$live_status"
}

install_status_command() {
  local existing_target=""

  mkdir -p /home/container/bin

  if [[ -L "$INSTALL_TARGET" ]]; then
    existing_target="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$INSTALL_TARGET" 2>/dev/null || true)"
    if [[ "$existing_target" == "$SELF_PATH" ]]; then
      log "Command already available at ${INSTALL_TARGET}"
      return 0
    fi

    rm -f "$INSTALL_TARGET"
    ln -s "$SELF_PATH" "$INSTALL_TARGET"
    log "Updated command symlink at ${INSTALL_TARGET}"
    return 0
  fi

  if [[ -e "$INSTALL_TARGET" ]]; then
    log "Preserving existing command file at ${INSTALL_TARGET}"
    return 0
  fi

  ln -s "$SELF_PATH" "$INSTALL_TARGET"
  log "Installed ${COMMAND_NAME} into /home/container/bin"
  log "Run '${COMMAND_NAME}' from the Pterodactyl console or inside the container shell to view current server status"
}

if [[ "$(basename "$0")" == "${COMMAND_NAME}" ]]; then
  run_status_command
else
  install_status_command
fi
