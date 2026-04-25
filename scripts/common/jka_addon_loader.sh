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

sync_managed_addon_examples() {
  info "Syncing addon examples into ${ADDON_EXAMPLES_DIR}"
  sync_image_managed_addon_tree "${JKA_PATH_BUNDLED_ADDONS}/examples" "$ADDON_EXAMPLES_DIR" "addon examples"
}

sync_managed_addon_defaults() {
  info "Syncing managed addon helpers into ${ADDON_DEFAULTS_DIR}"
  sync_image_managed_addon_tree "${JKA_PATH_BUNDLED_ADDONS}/defaults" "$ADDON_DEFAULTS_DIR" "addon defaults"
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

install_managed_rcon_live_guard_helper() {
  local helper_path="${ADDON_DEFAULTS_DIR}/50-rcon-live-guard.py"
  local helper_exit=0

  if [[ ! -f "$helper_path" ]]; then
    warn "Managed RCON live guard helper was not found at ${helper_path}"
    return 0
  fi

  if [[ "$ADDON_RCON_LIVE_GUARD_ENABLED" != "true" ]]; then
    info "Managed RCON live guard is disabled (built-in supervisor RCON guard supersedes it)"
    return 0
  fi

  warn "Managed RCON live guard helper is deprecated; the built-in supervisor RCON guard (RCON_GUARD_ENABLED) handles bad-RCON detection from the process stdout/stderr stream and does not require log tailing"
  set +e
  python3 "$helper_path"
  helper_exit=$?
  set -e

  if [[ "$helper_exit" -ne 0 ]]; then
    warn "Managed RCON live guard helper failed to start with exit code ${helper_exit}"
  fi
}

configure_addons() {
  : "${ADDONS_ENABLED:=true}"
  : "${ADDONS_DIR:=/home/container/addons}"
  : "${ADDON_CHECKSERVERSTATUS_ENABLED:=false}"
  : "${ADDON_CHATLOGGER_ENABLED:=false}"
  : "${ADDON_RCON_LIVE_GUARD_ENABLED:=false}"
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
  ADDON_EXAMPLES_DIR="${ADDONS_DIR}/examples"
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
  kv "Examples dir" "$ADDON_EXAMPLES_DIR"
  kv "Defaults dir" "$ADDON_DEFAULTS_DIR"
  kv "Checkserverstatus" "$(printf '%s' "$(bool_state "$ADDON_CHECKSERVERSTATUS_ENABLED")" | tr '[:lower:]' '[:upper:]')"
  kv "Chatlogger" "$(printf '%s' "$(bool_state "$ADDON_CHATLOGGER_ENABLED")" | tr '[:lower:]' '[:upper:]')"
  kv "RCON live guard" "$(printf '%s' "$(bool_state "$ADDON_RCON_LIVE_GUARD_ENABLED")" | tr '[:lower:]' '[:upper:]')"
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
