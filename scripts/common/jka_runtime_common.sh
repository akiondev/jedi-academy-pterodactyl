# shellcheck shell=bash
#
# scripts/common/jka_runtime_common.sh
#
# PR-A skeleton: color globals and shared UI/logging/formatting helpers.
# Functions and globals are textually copied from scripts/entrypoint.sh
# and are NOT sourced by the runtime yet (see scripts/common/README.md).

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

count_dir_files() {
  local path="$1"
  local pattern="${2:-*}"

  if [[ ! -d "$path" ]]; then
    printf '0\n'
    return
  fi

  find "$path" -maxdepth 1 -type f -name "$pattern" | wc -l | tr -d ' '
}
