# shellcheck shell=bash
#
# scripts/common/jka_runtime_manifest.sh
#
# Loads the in-image runtime manifest (default: /opt/jka/runtime.json)
# and exports neutral path variables for downstream sourcing.
#
# This file owns runtime manifest loading exclusively. Installer-track
# helpers stay in jka_install_layout.sh and are out of scope here.
#
# Requires: jka_runtime_common.sh sourced first (for `fail`).
# Requires: `jq` available on PATH.
#
# Manifest schema (v1):
#   {
#     "schema_version": 1,
#     "paths": {
#       "engine_dist_dir":      "/opt/jka/engine",
#       "runtime_common_dir":   "/opt/jka/runtime/common",
#       "docs_dir":             "/opt/jka/docs",
#       "bundled_addons_dir":   "/opt/jka/bundled-addons",
#       "antivpn_binary":       "/usr/local/bin/jka-antivpn",
#       "upstream_commit_file": "/opt/jka/engine/.upstream-commit",
#       "upstream_ref_file":    "/opt/jka/engine/.upstream-ref"
#     }
#   }
#
# Exports (on success):
#   JKA_PATH_ENGINE_DIST
#   JKA_PATH_RUNTIME_COMMON
#   JKA_PATH_DOCS
#   JKA_PATH_BUNDLED_ADDONS
#   JKA_PATH_ANTIVPN_BINARY
#   JKA_PATH_UPSTREAM_COMMIT
#   JKA_PATH_UPSTREAM_REF

JKA_RUNTIME_MANIFEST_SCHEMA_VERSION=1

load_runtime_manifest() {
  local manifest_path="${JKA_RUNTIME_MANIFEST:-/opt/jka/runtime.json}"

  if [[ ! -f "$manifest_path" ]]; then
    fail "Runtime manifest not found at ${manifest_path}"
  fi

  if ! command -v jq >/dev/null 2>&1; then
    fail "Runtime manifest loader requires jq, but jq is not available"
  fi

  if ! jq -e . "$manifest_path" >/dev/null 2>&1; then
    fail "Runtime manifest at ${manifest_path} is not valid JSON"
  fi

  local schema_version
  schema_version="$(jq -r '.schema_version // empty' "$manifest_path")"
  if [[ "$schema_version" != "$JKA_RUNTIME_MANIFEST_SCHEMA_VERSION" ]]; then
    fail "Runtime manifest at ${manifest_path} has unsupported schema_version '${schema_version}' (expected ${JKA_RUNTIME_MANIFEST_SCHEMA_VERSION})"
  fi

  local key var value
  local pairs=(
    "engine_dist_dir|JKA_PATH_ENGINE_DIST"
    "runtime_common_dir|JKA_PATH_RUNTIME_COMMON"
    "docs_dir|JKA_PATH_DOCS"
    "bundled_addons_dir|JKA_PATH_BUNDLED_ADDONS"
    "antivpn_binary|JKA_PATH_ANTIVPN_BINARY"
    "upstream_commit_file|JKA_PATH_UPSTREAM_COMMIT"
    "upstream_ref_file|JKA_PATH_UPSTREAM_REF"
  )

  for pair in "${pairs[@]}"; do
    key="${pair%%|*}"
    var="${pair##*|}"
    value="$(jq -r --arg k "$key" '.paths[$k] // empty' "$manifest_path")"
    if [[ -z "$value" ]]; then
      fail "Runtime manifest at ${manifest_path} is missing required path '${key}'"
    fi
    printf -v "$var" '%s' "$value"
    export "${var?}"
  done
}
