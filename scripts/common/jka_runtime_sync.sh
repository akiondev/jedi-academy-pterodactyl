# shellcheck shell=bash
#
# scripts/common/jka_runtime_sync.sh
#
# Manifest-driven sync of image-managed runtime files into the
# Pterodactyl container volume at /home/container/.
#
# This file owns the engine + payload sync exclusively. It depends only
# on the JKA_PATH_* variables exported by load_runtime_manifest in
# jka_runtime_manifest.sh, and on the `log` helper from
# jka_runtime_common.sh.
#
# Layout assumed:
#   ${JKA_PATH_ENGINE_DIST}/${JKA_PATH_ENGINE_BINARY_GLOB}
#       Top-level engine binaries (e.g. taystjkded.x86_64,
#       openjkded.x86_64) and upstream stamp files.
#   ${JKA_PATH_ENGINE_PAYLOAD_ROOT}/<subdir>/
#       One or more syncable payload subdirectories that map 1:1 to
#       /home/container/<subdir>/ (e.g. taystjk/ for the managed
#       TaystJK mod tree, base/ for OpenJK's jampgamex86_64.so).
#
# Behavior is intentionally conservative:
#   - missing engine binaries are reported but not fatal here;
#     downstream validation (validate_server_binary_selection) decides
#     whether the configured SERVER_BINARY is acceptable.
#   - a missing or empty payload root is a no-op; payload-less runtimes
#     remain valid.
#   - existing files under /home/container are overwritten by the
#     image-managed copy, matching the pre-refactor behavior of the
#     hardcoded TaystJK sync.
#
# Destination root is resolved from JKA_CONTAINER_ROOT and defaults to
# /home/container, matching the production Pterodactyl volume mount.
# The override exists strictly to make the helper unit-testable without
# requiring privileges to bind-mount /home/container.

sync_runtime_files() {
  local runtime_binary
  local found_runtime_binary=0
  local payload_entry
  local payload_name
  local container_root="${JKA_CONTAINER_ROOT:-/home/container}"
  local auto_update_binary="${TAYSTJK_AUTO_UPDATE_BINARY:-false}"
  local sync_payload="${JKA_SYNC_MANAGED_TAYSTJK_PAYLOAD:-true}"

  if [[ -z "${JKA_PATH_ENGINE_DIST:-}" || -z "${JKA_PATH_ENGINE_BINARY_GLOB:-}" || -z "${JKA_PATH_ENGINE_PAYLOAD_ROOT:-}" ]]; then
    fail "sync_runtime_files requires JKA_PATH_ENGINE_DIST, JKA_PATH_ENGINE_BINARY_GLOB and JKA_PATH_ENGINE_PAYLOAD_ROOT to be set by the runtime manifest loader"
  fi

  auto_update_binary="$(printf '%s' "$auto_update_binary" | tr '[:upper:]' '[:lower:]')"
  sync_payload="$(printf '%s' "$sync_payload" | tr '[:upper:]' '[:lower:]')"

  if [[ "$auto_update_binary" == "true" ]]; then
    if compgen -G "${JKA_PATH_ENGINE_DIST}/${JKA_PATH_ENGINE_BINARY_GLOB}" >/dev/null; then
      log "Image-managed binary auto-update enabled: syncing engine binaries from image into container volume"
      for runtime_binary in "${JKA_PATH_ENGINE_DIST}"/${JKA_PATH_ENGINE_BINARY_GLOB}; do
        [[ -f "$runtime_binary" ]] || continue
        install -m 0755 "$runtime_binary" "${container_root}/${runtime_binary##*/}"
        found_runtime_binary=1
      done
    fi
  else
    log "TAYSTJK_AUTO_UPDATE_BINARY=false: leaving operator-managed binaries under ${container_root} untouched"
  fi

  if [[ "$sync_payload" == "true" ]]; then
    if [[ -d "${JKA_PATH_ENGINE_PAYLOAD_ROOT}" ]]; then
      while IFS= read -r -d '' payload_entry; do
        payload_name="${payload_entry##*/}"
        mkdir -p "${container_root}/${payload_name}"
        cp -af "${payload_entry}/." "${container_root}/${payload_name}/"
      done < <(find "${JKA_PATH_ENGINE_PAYLOAD_ROOT}" -mindepth 1 -maxdepth 1 -type d -print0)
    fi
  else
    log "server.sync_managed_taystjk_payload=false: leaving operator-managed mod directories under ${container_root} untouched"
  fi

  if [[ "$auto_update_binary" == "true" && "$found_runtime_binary" -eq 0 ]]; then
    log "No image-provided dedicated binaries were found under ${JKA_PATH_ENGINE_DIST} matching ${JKA_PATH_ENGINE_BINARY_GLOB}"
  fi

  JKA_SYNC_FOUND_RUNTIME_BINARY="$found_runtime_binary"
  export JKA_SYNC_FOUND_RUNTIME_BINARY
}
