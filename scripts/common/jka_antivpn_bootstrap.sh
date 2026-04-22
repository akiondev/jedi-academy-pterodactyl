# shellcheck shell=bash
#
# scripts/common/jka_antivpn_bootstrap.sh
#
# Anti-VPN configuration and reporting helpers.
# Sourced by scripts/entrypoint.sh as part of the shared runtime layer.
#
# Requires: jka_runtime_common.sh sourced first (for `info`, `ok`,
# `warn`, `debug`, `kv`, `kv_highlight`, `kv_value`, `bool_state`,
# `section`).
#
# Note: hardcoded references to the TaystJK-namespaced cache path are
# intentionally preserved verbatim — the cache lives under the user
# volume (/home/container/) and is out of scope for the in-image path
# migration. The supervisor binary path is sourced from the runtime
# manifest via ${JKA_PATH_ANTIVPN_BINARY}.

configure_anti_vpn() {
  : "${ANTI_VPN_ENABLED:=false}"
  : "${ANTI_VPN_MODE:=block}"
  : "${ANTI_VPN_CACHE_TTL:=6h}"
  : "${ANTI_VPN_SCORE_THRESHOLD:=90}"
  : "${ANTI_VPN_ALLOWLIST:=}"
  : "${ANTI_VPN_PROXYCHECK_API_KEY:=}"
  : "${ANTI_VPN_IPAPIIS_API_KEY:=}"
  : "${ANTI_VPN_IPHUB_API_KEY:=}"
  : "${ANTI_VPN_VPNAPI_IO_API_KEY:=}"
  : "${ANTI_VPN_IPQUALITYSCORE_API_KEY:=}"
  : "${ANTI_VPN_IPLOCATE_API_KEY:=}"
  : "${ANTI_VPN_TIMEOUT_MS:=1500}"
  : "${ANTI_VPN_LOG_DECISIONS:=true}"
  : "${ANTI_VPN_CACHE_PATH:=/home/container/.cache/taystjk-antivpn/cache.json}"
  : "${ANTI_VPN_CACHE_FLUSH_INTERVAL:=2s}"
  : "${ANTI_VPN_AUDIT_LOG_PATH:=/home/container/logs/anti-vpn-audit.log}"
  : "${ANTI_VPN_ENFORCEMENT_MODE:=kick-only}"
  : "${ANTI_VPN_BROADCAST_MODE:=pass-and-block}"
  : "${ANTI_VPN_BROADCAST_COOLDOWN:=90s}"
  : "${ANTI_VPN_BROADCAST_PASS_TEMPLATE:=say [Anti-VPN] VPN PASS: %PLAYER% cleared checks (%SCORE%/%THRESHOLD%). %SUMMARY%}"
  : "${ANTI_VPN_BROADCAST_BLOCK_TEMPLATE:=say [Anti-VPN] VPN BLOCKED: %PLAYER% triggered anti-VPN (%SCORE%/%THRESHOLD%). %SUMMARY%}"
  : "${ANTI_VPN_BAN_COMMAND:=}"
  : "${ANTI_VPN_KICK_COMMAND:=clientkick %SLOT%}"
  : "${ANTI_VPN_EVENT_DEDUPE_INTERVAL:=90s}"
  : "${ANTI_VPN_LOG_PATH:=$(active_server_log_path)}"

  ANTI_VPN_MODE_NORMALIZED="$(printf '%s' "$ANTI_VPN_MODE" | tr '[:upper:]' '[:lower:]')"
  case "$ANTI_VPN_MODE_NORMALIZED" in
    off|log-only|block) ;;
    *)
      warn "ANTI_VPN_MODE=${ANTI_VPN_MODE} is invalid, falling back to off"
      ANTI_VPN_MODE_NORMALIZED="off"
      ;;
  esac

  ANTI_VPN_BROADCAST_MODE_NORMALIZED="$(printf '%s' "$ANTI_VPN_BROADCAST_MODE" | tr '[:upper:]' '[:lower:]')"
  case "$ANTI_VPN_BROADCAST_MODE_NORMALIZED" in
    off|block-only|pass-and-block) ;;
    *)
      warn "ANTI_VPN_BROADCAST_MODE=${ANTI_VPN_BROADCAST_MODE} is invalid, falling back to pass-and-block"
      ANTI_VPN_BROADCAST_MODE_NORMALIZED="pass-and-block"
      ;;
  esac
  ANTI_VPN_BROADCAST_MODE="$ANTI_VPN_BROADCAST_MODE_NORMALIZED"

  ANTI_VPN_ENFORCEMENT_MODE_NORMALIZED="$(printf '%s' "$ANTI_VPN_ENFORCEMENT_MODE" | tr '[:upper:]' '[:lower:]')"
  case "$ANTI_VPN_ENFORCEMENT_MODE_NORMALIZED" in
    kick-only|ban-and-kick|ban-only|custom) ;;
    *)
      warn "ANTI_VPN_ENFORCEMENT_MODE=${ANTI_VPN_ENFORCEMENT_MODE} is invalid, falling back to kick-only"
      ANTI_VPN_ENFORCEMENT_MODE_NORMALIZED="kick-only"
      ;;
  esac
  ANTI_VPN_ENFORCEMENT_MODE="$ANTI_VPN_ENFORCEMENT_MODE_NORMALIZED"

  if [[ "${ANTI_VPN_ENABLED,,}" != "true" || "$ANTI_VPN_MODE_NORMALIZED" == "off" ]]; then
    ANTI_VPN_EFFECTIVE_MODE="off"
  else
    ANTI_VPN_EFFECTIVE_MODE="$ANTI_VPN_MODE_NORMALIZED"
  fi

  mkdir -p "$(dirname "$ANTI_VPN_CACHE_PATH")"
  mkdir -p "$(dirname "$ANTI_VPN_AUDIT_LOG_PATH")"
}

anti_vpn_provider_row() {
  local label="$1"
  local state="$2"
  local note="${3:-}"

  if [[ "$state" == "ENABLED" ]]; then
    if [[ -n "$note" ]]; then
      kv_value "$label" "$COLOR_ACTIVE" "${state} ${note}"
    else
      kv_value "$label" "$COLOR_ACTIVE" "$state"
    fi
  else
    kv_value "$label" "$COLOR_ERROR" "DISABLED"
  fi
}

print_anti_vpn_providers() {
  anti_vpn_provider_row "proxycheck.io" "ENABLED" "$( [[ -n "$ANTI_VPN_PROXYCHECK_API_KEY" ]] && printf '' || printf '(ANONYMOUS)' )"
  anti_vpn_provider_row "ipapi.is" "ENABLED" "$( [[ -n "$ANTI_VPN_IPAPIIS_API_KEY" ]] && printf '' || printf '(ANONYMOUS)' )"

  if [[ -n "$ANTI_VPN_IPHUB_API_KEY" ]]; then
    anti_vpn_provider_row "IPHub" "ENABLED"
  else
    anti_vpn_provider_row "IPHub" "DISABLED"
  fi

  if [[ -n "$ANTI_VPN_VPNAPI_IO_API_KEY" ]]; then
    anti_vpn_provider_row "vpnapi.io" "ENABLED"
  else
    anti_vpn_provider_row "vpnapi.io" "DISABLED"
  fi

  if [[ -n "$ANTI_VPN_IPQUALITYSCORE_API_KEY" ]]; then
    anti_vpn_provider_row "IPQualityScore" "ENABLED"
  else
    anti_vpn_provider_row "IPQualityScore" "DISABLED"
  fi

  if [[ -n "$ANTI_VPN_IPLOCATE_API_KEY" ]]; then
    anti_vpn_provider_row "IPLocate" "ENABLED"
  else
    anti_vpn_provider_row "IPLocate" "DISABLED"
  fi
}

anti_vpn_allowlist_status() {
  if [[ -n "${ANTI_VPN_ALLOWLIST//[[:space:],]/}" ]]; then
    printf 'configured\n'
  else
    printf 'not set\n'
  fi
}

print_anti_vpn_summary() {
  section "ANTI-VPN"
  kv_highlight "Status" "$(printf '%s' "$(bool_state "$ANTI_VPN_ENABLED")" | tr '[:lower:]' '[:upper:]')"
  kv_highlight "Mode" "$(printf '%s' "$ANTI_VPN_EFFECTIVE_MODE" | tr '[:lower:]' '[:upper:]')"
  kv_highlight "Enforce" "$(printf '%s' "$ANTI_VPN_ENFORCEMENT_MODE_NORMALIZED" | tr '[:lower:]' '[:upper:]')"
  kv_highlight "Broadcast" "$(printf '%s' "$ANTI_VPN_BROADCAST_MODE_NORMALIZED" | tr '[:lower:]' '[:upper:]')"
  kv_highlight "Threshold" "$ANTI_VPN_SCORE_THRESHOLD"
  kv "Allowlist" "$(anti_vpn_allowlist_status)"
  print_anti_vpn_providers
  debug "Capture mode: stdout-first with active log fallback"
  debug "Server log path: ${ANTI_VPN_LOG_PATH}"
  debug "Decision logs: $(printf '%s' "$(bool_state "$ANTI_VPN_LOG_DECISIONS")" | tr '[:lower:]' '[:upper:]')"
  debug "Cache TTL: ${ANTI_VPN_CACHE_TTL}"
  debug "Cache flush: ${ANTI_VPN_CACHE_FLUSH_INTERVAL}"
  debug "Timeout: ${ANTI_VPN_TIMEOUT_MS}"
  debug "Broadcast cooldown: ${ANTI_VPN_BROADCAST_COOLDOWN}"
  debug "Event dedupe interval: ${ANTI_VPN_EVENT_DEDUPE_INTERVAL}"

  if [[ "$ANTI_VPN_EFFECTIVE_MODE" == "off" ]]; then
    warn "Anti-VPN supervision is disabled"
    return
  fi

  if [[ -x "${JKA_PATH_ANTIVPN_BINARY}" ]]; then
    ok "Anti-VPN supervisor ready"
    debug "Supervisor binary: ${JKA_PATH_ANTIVPN_BINARY}"
  else
    warn "Anti-VPN supervisor binary is missing; startup will continue without anti-VPN enforcement"
  fi
}
