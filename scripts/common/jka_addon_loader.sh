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
  sync_image_managed_addon_tree "${JKA_PATH_DOCS}/addons" "$ADDON_DOCS_DIR" "addon docs"
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
  # Image-managed addon source files (.py / .sh) are refreshed
  # byte-for-byte from the image. Per-addon *.config.json files are
  # user-owned (the operator toggles `enabled` and provides addon
  # settings) and are NOT overwritten by the sync.
  mkdir -p "$ADDON_DEFAULTS_DIR"
  if [[ ! -d "${JKA_PATH_BUNDLED_ADDONS}/defaults" ]]; then
    debug "No image-managed addon defaults found under ${JKA_PATH_BUNDLED_ADDONS}/defaults"
    return 0
  fi

  # Mirror all source/script files but preserve any *.config.json the
  # operator may have edited.
  rsync -a \
    --include='*/' \
    --include='*.py' --include='*.sh' --include='*.txt' --include='*.md' \
    --exclude='*' \
    "${JKA_PATH_BUNDLED_ADDONS}/defaults/" "${ADDON_DEFAULTS_DIR}/"
  find "$ADDON_DEFAULTS_DIR" -type f \( -name '*.sh' -o -name '*.py' \) -exec chmod 0755 {} +
  debug "Refreshed image-managed addon defaults in ${ADDON_DEFAULTS_DIR}"
}

# sync_managed_addon_default_configs copies any *.config.json file
# shipped under bundled-addons/defaults/ into the operator's
# /home/container/addons/defaults/ tree ONLY when the destination file
# does not already exist. This preserves operator edits across image
# upgrades while still seeding new addons (or new keys) on first boot.
sync_managed_addon_default_configs() {
  local source_root="${JKA_PATH_BUNDLED_ADDONS}/defaults"
  local target_root="${ADDON_DEFAULTS_DIR}"
  local relative_path=""
  local source_path=""
  local target_path=""

  if [[ ! -d "$source_root" ]]; then
    return 0
  fi

  while IFS= read -r -d '' source_path; do
    relative_path="${source_path#"${source_root}"/}"
    target_path="${target_root}/${relative_path}"
    if [[ -f "$target_path" ]]; then
      debug "Preserving user-owned addon config: ${target_path}"
      continue
    fi
    mkdir -p "$(dirname "$target_path")"
    cp "$source_path" "$target_path"
    info "Seeded default addon config (disabled): ${target_path}"
  done < <(find "$source_root" -type f -name '*.config.json' -print0)
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

install_managed_chatlogger_helper() {
  install_managed_event_addon "events/40-chatlogger.py"
}

install_managed_python_announcer_helper() {
  # Phase 3 default-addon model: the Python announcer is shipped as a
  # managed default addon, disabled via its own config file. When the
  # operator flips `enabled=true` in
  # /home/container/addons/defaults/20-python-announcer.config.json
  # this helper launches the script so it can spawn its scheduled
  # background worker. When disabled, any leftover lockfile/PID is
  # ignored: the addon decides on next run whether to take the lock.
  local helper_path="${ADDON_DEFAULTS_DIR}/20-python-announcer.py"
  local cfg_path="${ADDON_DEFAULTS_DIR}/20-python-announcer.config.json"
  local enabled
  enabled="$(addon_config_enabled "$cfg_path")"
  if [[ "$enabled" != "true" ]]; then
    debug "Default Python announcer is disabled (edit ${cfg_path} to enable)"
    return 0
  fi
  if [[ ! -f "$helper_path" ]]; then
    warn "Default Python announcer helper missing at ${helper_path}"
    return 0
  fi
  info "Starting default Python announcer (enabled in ${cfg_path})"
  set +e
  python3 "$helper_path"
  set -e
}

install_managed_live_team_announcer_helper() {
  install_managed_event_addon "events/30-live-team-announcer.py"
}

# install_managed_event_addon is the shared lifecycle helper for
# event-driven default addons. It reads the addon's own
# *.config.json `enabled` flag and either symlinks the helper into
# /home/container/addons/events (so the supervisor's addon runner
# picks it up) or removes the symlink if disabled. User config files
# are never overwritten by this helper.
install_managed_event_addon() {
  local relative="$1"
  local helper_path="${ADDON_DEFAULTS_DIR}/${relative}"
  local cfg_path="${helper_path%.py}.config.json"
  local addon_basename
  addon_basename="$(basename "$relative")"
  local event_addons_dir="${ADDON_EVENT_ADDONS_DIR:-/home/container/addons/events}"
  local link_path="${event_addons_dir}/${addon_basename}"
  local enabled
  enabled="$(addon_config_enabled "$cfg_path")"

  if [[ "$enabled" != "true" ]]; then
    if [[ -L "$link_path" || -f "$link_path" ]]; then
      rm -f "$link_path"
      info "Removed disabled event addon link: ${link_path}"
    fi
    debug "Event addon ${addon_basename} disabled (edit ${cfg_path} to enable)"
    return 0
  fi

  if [[ ! -f "$helper_path" ]]; then
    warn "Event addon helper missing at ${helper_path}"
    return 0
  fi

  mkdir -p "$event_addons_dir"
  ln -sfn "$helper_path" "$link_path"
  chmod 0755 "$helper_path" 2>/dev/null || true
  info "Installed event addon ${addon_basename} -> ${helper_path}"
}

# addon_config_enabled returns "true" when the per-addon config file
# at the given path exists and contains `"enabled": true`. Missing
# files or invalid JSON are treated as disabled so a broken config
# never silently runs an addon.
addon_config_enabled() {
  local path="$1"
  if [[ ! -f "$path" ]]; then
    printf 'false\n'
    return
  fi
  if ! command -v jq >/dev/null 2>&1; then
    printf 'false\n'
    return
  fi
  local enabled
  enabled="$(jq -r '.enabled // false' "$path" 2>/dev/null || true)"
  if [[ "$enabled" == "true" ]]; then
    printf 'true\n'
  else
    printf 'false\n'
  fi
}

install_managed_rcon_live_guard_helper() {
  # The legacy 50-rcon-live-guard.py helper was removed from the
  # bundled defaults set in Phase 2 because the supervisor now ships
  # a built-in RCON guard module that consumes ``Bad rcon`` events
  # directly from the process stdout/stderr stream. This function is
  # retained as a no-op so existing entrypoint orderings keep working
  # after upgrade; it logs once if an operator left the legacy env
  # variable set.
  local legacy_path="${ADDON_DEFAULTS_DIR}/50-rcon-live-guard.py"

  if [[ "$ADDON_RCON_LIVE_GUARD_ENABLED" == "true" ]]; then
    warn "ADDON_RCON_LIVE_GUARD_ENABLED=true is deprecated; the built-in supervisor RCON guard (RCON_GUARD_ENABLED) supersedes the Python addon. The bundled 50-rcon-live-guard.py was moved to bundled-addons/examples/deprecated/ and is no longer launched by default."
  fi

  if [[ -f "$legacy_path" ]]; then
    debug "Removing legacy 50-rcon-live-guard.py from ${ADDON_DEFAULTS_DIR}; the built-in supervisor RCON guard supersedes it"
    rm -f "$legacy_path"
  fi
}

configure_addons() {
  : "${ADDONS_ENABLED:=true}"
  : "${ADDONS_DIR:=/home/container/addons}"
  : "${ADDON_CHATLOGGER_ENABLED:=false}"
  : "${ADDON_RCON_LIVE_GUARD_ENABLED:=false}"
  : "${ADDON_EVENT_ADDONS_DIR:=${ADDONS_DIR}/events}"
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

  ADDON_CHATLOGGER_ENABLED_NORMALIZED="$(printf '%s' "$ADDON_CHATLOGGER_ENABLED" | tr '[:upper:]' '[:lower:]')"
  case "$ADDON_CHATLOGGER_ENABLED_NORMALIZED" in
    true|false) ;;
    *)
      warn "ADDON_CHATLOGGER_ENABLED=${ADDON_CHATLOGGER_ENABLED} is invalid, falling back to true"
      ADDON_CHATLOGGER_ENABLED_NORMALIZED="true"
      ;;
  esac
  ADDON_CHATLOGGER_ENABLED="$ADDON_CHATLOGGER_ENABLED_NORMALIZED"

  ADDON_RCON_LIVE_GUARD_ENABLED_NORMALIZED="$(printf '%s' "$ADDON_RCON_LIVE_GUARD_ENABLED" | tr '[:upper:]' '[:lower:]')"
  case "$ADDON_RCON_LIVE_GUARD_ENABLED_NORMALIZED" in
    true|false) ;;
    *)
      warn "ADDON_RCON_LIVE_GUARD_ENABLED=${ADDON_RCON_LIVE_GUARD_ENABLED} is invalid, falling back to false"
      ADDON_RCON_LIVE_GUARD_ENABLED_NORMALIZED="false"
      ;;
  esac
  ADDON_RCON_LIVE_GUARD_ENABLED="$ADDON_RCON_LIVE_GUARD_ENABLED_NORMALIZED"

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

print_addon_summary() {
  section "ADDONS"
  kv_highlight "Status" "$(printf '%s' "$(bool_state "$ADDONS_ENABLED")" | tr '[:lower:]' '[:upper:]')"
  kv "User dir" "$ADDONS_DIR"
  kv "Docs dir" "$ADDON_DOCS_DIR"
  kv "Defaults dir" "$ADDON_DEFAULTS_DIR"
  kv "Event addons dir" "$ADDON_EVENT_ADDONS_DIR"
  kv "Announcer" "$(addon_default_state '20-python-announcer.config.json')"
  kv "Live team announcer" "$(addon_default_state 'events/30-live-team-announcer.config.json')"
  kv "Chatlogger" "$(addon_default_state 'events/40-chatlogger.config.json')"
  kv "Strict" "$(printf '%s' "$(bool_state "$ADDONS_STRICT")" | tr '[:lower:]' '[:upper:]')"
  kv "Timeout" "${ADDONS_TIMEOUT_SECONDS}s"
  kv "Log output" "$(printf '%s' "$(bool_state "$ADDONS_LOG_OUTPUT")" | tr '[:lower:]' '[:upper:]')"

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

# addon_default_state inspects the per-addon config file (if present)
# and prints ENABLED/DISABLED based on its `enabled` flag. Used by the
# ADDONS console section so operators see the effective state without
# having to dig through individual config files.
addon_default_state() {
  local rel="$1"
  local cfg="${ADDON_DEFAULTS_DIR}/${rel}"
  if [[ ! -f "$cfg" ]]; then
    printf 'NOT INSTALLED\n'
    return
  fi
  local enabled
  enabled="$(jq -r '.enabled // false' "$cfg" 2>/dev/null || true)"
  if [[ "$enabled" == "true" ]]; then
    printf 'ENABLED\n'
  else
    printf 'DISABLED\n'
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
