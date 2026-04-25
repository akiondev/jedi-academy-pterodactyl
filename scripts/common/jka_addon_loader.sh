# shellcheck shell=bash
#
# scripts/common/jka_addon_loader.sh
#
# PR-A skeleton: addon directory sync, configuration, and execution.
# Functions are textually copied from scripts/entrypoint.sh and are NOT
# sourced by the runtime yet (see scripts/common/README.md).
#
# Requires: jka_runtime_common.sh sourced first (for `info`, `ok`,
# `warn`, `debug`, `kv`, `kv_highlight`, `bool_state`), and
# jka_security.sh for `require_safe_container_path`.
#
# Note: the hardcoded image-side paths previously referenced here are
# now sourced from the runtime manifest via ${JKA_PATH_BUNDLED_ADDONS}
# and ${JKA_PATH_DOCS}.

sync_addon_docs() {
  # The image ships exactly one addon doc file (ADDON_README.md) under
  # ${JKA_PATH_DOCS}/addons. Sync mirrors only that file into
  # /home/container/addons/docs and prunes any older synced docs left
  # over from earlier image revisions.
  local source_dir="${JKA_PATH_DOCS}/addons"
  local target_dir="$ADDON_DOCS_DIR"

  mkdir -p "$target_dir"

  if [[ ! -d "$source_dir" ]]; then
    debug "No image-managed addon docs found under ${source_dir}"
    return 0
  fi

  # --delete + restrictive --include keeps only ADDON_README.md.
  rsync -a --delete \
    --include='ADDON_README.md' \
    --exclude='*' \
    "${source_dir}/" "${target_dir}/"
  debug "Refreshed image-managed addon docs in ${target_dir}"
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

sync_managed_addon_defaults() {
  info "Syncing managed addon helpers into ${ADDON_DEFAULTS_DIR}"
  # Image-managed addon source files (.py / .sh / .txt / .md) are
  # refreshed byte-for-byte from the image. Per-addon configuration
  # lives centrally in /home/container/config/jka-addons.json and is
  # never overwritten by this sync.
  mkdir -p "$ADDON_DEFAULTS_DIR"
  if [[ ! -d "${JKA_PATH_BUNDLED_ADDONS}/defaults" ]]; then
    debug "No image-managed addon defaults found under ${JKA_PATH_BUNDLED_ADDONS}/defaults"
    return 0
  fi

  rsync -a \
    --include='*/' \
    --include='*.py' --include='*.sh' --include='*.md' \
    --exclude='*' \
    "${JKA_PATH_BUNDLED_ADDONS}/defaults/" "${ADDON_DEFAULTS_DIR}/"
  find "$ADDON_DEFAULTS_DIR" -type f \( -name '*.sh' -o -name '*.py' \) -exec chmod 0755 {} +
  debug "Refreshed image-managed addon defaults in ${ADDON_DEFAULTS_DIR}"
}

# cleanup_legacy_addon_paths removes image-managed paths from earlier
# revisions of this runtime that should no longer exist on disk:
#
#   * /home/container/addons/events           (former event-addon
#     symlink directory; event addons now launch directly from
#     /home/container/addons/defaults driven by jka-addons.json)
#   * /home/container/addons/defaults/events  (former numeric-prefixed
#     default subdirectory)
#   * /home/container/addons/defaults/<numeric-prefixed default files>
#
# This function is conservative: it only removes well-known legacy
# paths, never arbitrary user files. User-owned scripts in the
# top-level /home/container/addons directory are not touched.
cleanup_legacy_addon_paths() {
  local removed=0
  local target=""

  if [[ -d "${ADDONS_DIR}/events" ]]; then
    rm -rf "${ADDONS_DIR}/events"
    info "Removed legacy event-addons directory: ${ADDONS_DIR}/events"
    removed=$((removed + 1))
  fi

  if [[ -d "${ADDON_DEFAULTS_DIR}/events" ]]; then
    rm -rf "${ADDON_DEFAULTS_DIR}/events"
    info "Removed legacy defaults/events directory: ${ADDON_DEFAULTS_DIR}/events"
    removed=$((removed + 1))
  fi

  for target in \
    "${ADDON_DEFAULTS_DIR}/announcer.messages.txt" \
    "${ADDON_DEFAULTS_DIR}/20-python-announcer.py" \
    "${ADDON_DEFAULTS_DIR}/20-python-announcer.messages.txt" \
    "${ADDON_DEFAULTS_DIR}/20-python-announcer.config.json" \
    "${ADDON_DEFAULTS_DIR}/30-live-team-announcer.py" \
    "${ADDON_DEFAULTS_DIR}/30-live-team-announcer.config.json" \
    "${ADDON_DEFAULTS_DIR}/40-chatlogger.py" \
    "${ADDON_DEFAULTS_DIR}/40-chatlogger.config.json"; do
    if [[ -f "$target" || -L "$target" ]]; then
      rm -f "$target"
      info "Removed legacy default addon file: ${target}"
      removed=$((removed + 1))
    fi
  done

  if [[ "$removed" -gt 0 ]]; then
    debug "cleanup_legacy_addon_paths removed ${removed} legacy entr$( [[ "$removed" -eq 1 ]] && printf 'y' || printf 'ies' )"
  fi
}

# cleanup_stale_live_output_files removes /home/container/.runtime/live/
# server-output.log* artefacts when the live-output mirror is disabled.
# The mirror is OFF by default in the new event-bus architecture; stale
# files left over from earlier sessions used to mislead operators into
# thinking an addon was still tailing them. The cleanup is scoped to
# the supervisor's own runtime directory so unrelated user files are
# never touched.
cleanup_stale_live_output_files() {
  local mirror_flag="${JKA_LIVE_OUTPUT_MIRROR_ENABLED:-${TAYSTJK_LIVE_OUTPUT_ENABLED:-false}}"
  mirror_flag="$(printf '%s' "$mirror_flag" | tr '[:upper:]' '[:lower:]')"
  if [[ "$mirror_flag" == "true" ]]; then
    return 0
  fi

  local live_dir="/home/container/.runtime/live"
  [[ -d "$live_dir" ]] || return 0

  local removed=0
  local file=""
  while IFS= read -r -d '' file; do
    rm -f "$file"
    removed=$((removed + 1))
  done < <(find "$live_dir" -maxdepth 1 -type f -name 'server-output.log*' -print0)

  if [[ "$removed" -gt 0 ]]; then
    info "Removed ${removed} stale live-output file(s) from ${live_dir} (live_output_mirror_enabled=false)"
  fi
}

# default_addons_config_template emits the canonical jka-addons.json
# that the runtime materialises on first boot. Keep this in sync with
# docs/addons/ADDON_README.md.
default_addons_config_template() {
  cat <<'JSON'
{
  "addons": {
    "announcer": {
      "enabled": false,
      "order": 20,
      "type": "scheduled",
      "script": "announcer.py",
      "announce_command": "svsay",
      "interval_seconds": 300,
      "messages": [
        "jknexus.se - JK Web Based Client > Real Live Time & Search Master List Browser!"
      ]
    },
    "live_team_announcer": {
      "enabled": false,
      "order": 30,
      "type": "event",
      "script": "live-team-announcer.py",
      "announce_command": "svsay",
      "min_seconds_between_announcements": 3
    },
    "chatlogger": {
      "enabled": false,
      "order": 40,
      "type": "event",
      "script": "chatlogger.py"
    }
  }
}
JSON
}

# load_addons_json_config materialises /home/container/config/jka-addons.json
# from the default template when missing, validates it as JSON, and
# exports per-addon environment variables consumed by the rest of the
# runtime (including the Go supervisor's addon runner via
# JKA_ADDONS_CONFIG_PATH).
#
# Exports:
#   JKA_ADDONS_CONFIG_PATH                  — absolute path to the file
#   ADDON_ANNOUNCER_ENABLED                 — true|false
#   ADDON_ANNOUNCER_SCRIPT                  — relative script name
#   ADDON_ANNOUNCER_CONFIG_JSON             — addons.announcer JSON object
#   ADDON_LIVE_TEAM_ANNOUNCER_ENABLED       — true|false
#   ADDON_LIVE_TEAM_ANNOUNCER_SCRIPT
#   ADDON_LIVE_TEAM_ANNOUNCER_CONFIG_JSON
#   ADDON_CHATLOGGER_ENABLED                — true|false
#   ADDON_CHATLOGGER_SCRIPT
#   ADDON_CHATLOGGER_CONFIG_JSON
#
# The user-owned file is never overwritten.
JKA_ADDONS_CONFIG_DIR="${JKA_ADDONS_CONFIG_DIR:-/home/container/config}"
JKA_ADDONS_CONFIG_PATH="${JKA_ADDONS_CONFIG_PATH:-${JKA_ADDONS_CONFIG_DIR}/jka-addons.json}"

load_addons_json_config() {
  if ! command -v jq >/dev/null 2>&1; then
    fail "Addons JSON config loader requires jq, but jq is not available"
  fi

  mkdir -p "$JKA_ADDONS_CONFIG_DIR"

  if [[ ! -f "$JKA_ADDONS_CONFIG_PATH" ]]; then
    info "Creating default addons config at ${JKA_ADDONS_CONFIG_PATH}"
    default_addons_config_template > "$JKA_ADDONS_CONFIG_PATH"
  fi

  if ! jq -e . "$JKA_ADDONS_CONFIG_PATH" >/dev/null 2>&1; then
    fail "Addons config at ${JKA_ADDONS_CONFIG_PATH} is not valid JSON"
  fi

  export JKA_ADDONS_CONFIG_PATH

  local name=""
  local upper=""
  local enabled=""
  # shellcheck disable=SC2034  # consumed via eval below
  local script=""
  # shellcheck disable=SC2034  # consumed via eval below
  local section=""
  for name in announcer live_team_announcer chatlogger; do
    upper="$(printf '%s' "$name" | tr '[:lower:]' '[:upper:]')"
    enabled="$(jq -r --arg n "$name" '(.addons[$n].enabled // false) | tostring' "$JKA_ADDONS_CONFIG_PATH" 2>/dev/null || echo false)"
    # shellcheck disable=SC2034  # consumed via eval below
    script="$(jq -r --arg n "$name" '.addons[$n].script // ""' "$JKA_ADDONS_CONFIG_PATH" 2>/dev/null || echo "")"
    # shellcheck disable=SC2034  # consumed via eval below
    section="$(jq -c --arg n "$name" '.addons[$n] // {}' "$JKA_ADDONS_CONFIG_PATH" 2>/dev/null || echo '{}')"
    case "$enabled" in
      true|false) ;;
      *) enabled="false" ;;
    esac
    eval "ADDON_${upper}_ENABLED=\"\$enabled\""
    eval "ADDON_${upper}_SCRIPT=\"\$script\""
    eval "ADDON_${upper}_CONFIG_JSON=\"\$section\""
    eval "export ADDON_${upper}_ENABLED ADDON_${upper}_SCRIPT ADDON_${upper}_CONFIG_JSON"
  done
}

install_managed_python_announcer_helper() {
  # Manual-first launcher for the scheduled announcer addon. The
  # addon's own ``enabled`` flag in jka-addons.json gates this; the
  # script reads its full configuration from the JKA_ADDON_CONFIG_JSON
  # env var so no per-addon config file is ever written next to the
  # script.
  local script_name="${ADDON_ANNOUNCER_SCRIPT:-announcer.py}"
  local helper_path="${ADDON_DEFAULTS_DIR}/${script_name}"

  if [[ "${ADDON_ANNOUNCER_ENABLED:-false}" != "true" ]]; then
    debug "Default announcer is disabled (edit ${JKA_ADDONS_CONFIG_PATH} to enable)"
    return 0
  fi
  if [[ ! -f "$helper_path" ]]; then
    warn "Default announcer helper missing at ${helper_path}"
    return 0
  fi
  info "Starting default announcer (enabled in ${JKA_ADDONS_CONFIG_PATH})"
  # Use a local variable for the config JSON to avoid the bash brace-counting
  # trap: ${VAR:-{}} appends a stray } because bash terminates ${...} at the
  # first } inside the default, leaving the second } as literal text.
  local _announcer_config_json="${ADDON_ANNOUNCER_CONFIG_JSON}"
  [[ -z "$_announcer_config_json" ]] && _announcer_config_json='{}'
  set +e
  JKA_ADDON_NAME="announcer" \
    JKA_ADDON_CONFIG_JSON="$_announcer_config_json" \
    JKA_ADDONS_CONFIG_PATH="$JKA_ADDONS_CONFIG_PATH" \
    python3 "$helper_path"
  set -e
}

install_managed_rcon_live_guard_helper() {
  # The legacy 50-rcon-live-guard.py helper was removed because the
  # supervisor now ships a built-in RCON guard module that consumes
  # ``Bad rcon`` events directly from the process stdout/stderr
  # stream. This function is retained as a no-op so existing
  # entrypoint orderings keep working after upgrade.
  local legacy_path="${ADDON_DEFAULTS_DIR}/50-rcon-live-guard.py"
  if [[ -f "$legacy_path" ]]; then
    debug "Removing legacy 50-rcon-live-guard.py from ${ADDON_DEFAULTS_DIR}"
    rm -f "$legacy_path"
  fi
}

configure_addons() {
  : "${ADDONS_ENABLED:=true}"
  : "${ADDONS_DIR:=/home/container/addons}"
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
  ADDON_DEFAULTS_DIR="${ADDONS_DIR}/defaults"

  ADDON_EXECUTED_COUNT=0
  ADDON_SKIPPED_COUNT=0
  ADDON_FAILED_COUNT=0
  ADDON_TIMED_OUT_COUNT=0
}

# addon_state_label prints ENABLED or DISABLED for the given env var.
addon_state_label() {
  if [[ "${1:-false}" == "true" ]]; then
    printf 'ENABLED\n'
  else
    printf 'DISABLED\n'
  fi
}

print_addon_summary() {
  section "ADDONS"
  kv_highlight "Status" "$(printf '%s' "$(bool_state "$ADDONS_ENABLED")" | tr '[:lower:]' '[:upper:]')"
  kv "Defaults dir" "$ADDON_DEFAULTS_DIR"
  kv "Config" "$JKA_ADDONS_CONFIG_PATH"
  kv "Announcer" "$(addon_state_label "${ADDON_ANNOUNCER_ENABLED:-false}")"
  kv "Live team announcer" "$(addon_state_label "${ADDON_LIVE_TEAM_ANNOUNCER_ENABLED:-false}")"
  kv "Chatlogger" "$(addon_state_label "${ADDON_CHATLOGGER_ENABLED:-false}")"
  kv "Strict" "$(printf '%s' "$(bool_state "$ADDONS_STRICT")" | tr '[:lower:]' '[:upper:]')"
  kv "Timeout" "${ADDONS_TIMEOUT_SECONDS}s"
  kv "Log output" "$(printf '%s' "$(bool_state "$ADDONS_LOG_OUTPUT")" | tr '[:lower:]' '[:upper:]')"
  kv "Docs" "${ADDON_DOCS_DIR}/ADDON_README.md"

  if [[ "$ADDONS_ENABLED" != "true" ]]; then
    warn "User addon execution is disabled; managed defaults still refresh from the image"
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
