#!/usr/bin/env bash
#
# scripts/test/sync_runtime_files_test.sh
#
# Regression test for the manifest-driven sync_runtime_files() helper.
#
# Builds a mock TaystJK image tree, runs the helper with
# JKA_CONTAINER_ROOT pointed at a sandbox, and asserts:
#
#   - With TAYSTJK_AUTO_UPDATE_BINARY=true the image-managed binary is
#     installed mode 0755 and the taystjk/ payload is mirrored under
#     /home/container/taystjk/ when sync_managed_taystjk_payload=true.
#   - With TAYSTJK_AUTO_UPDATE_BINARY=false the binary is left
#     untouched in the container volume (manual user-supplied path).
#   - With sync_managed_taystjk_payload=false the taystjk/ payload is
#     left untouched in the container volume (manual user-owned mod
#     directory).
#   - Upstream stamp files are never leaked into the container volume.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORK_DIR="$(mktemp -d -t jka-sync-test.XXXXXX)"
trap 'rm -rf "${WORK_DIR}"' EXIT

PASSED=0
FAILED=0

pass() {
  PASSED=$((PASSED + 1))
  printf '[ OK ] %s\n' "$1"
}

fail_assert() {
  FAILED=$((FAILED + 1))
  printf '[FAIL] %s\n' "$1" >&2
}

assert_file_equal() {
  local label="$1"
  local expected="$2"
  local actual="$3"

  if [[ ! -f "$actual" ]]; then
    fail_assert "${label}: missing ${actual}"
    return
  fi
  if cmp -s "$expected" "$actual"; then
    pass "${label}: ${actual} matches source byte-for-byte"
  else
    fail_assert "${label}: ${actual} differs from ${expected}"
  fi
}

assert_mode() {
  local label="$1"
  local path="$2"
  local expected_mode="$3"
  local actual_mode

  actual_mode="$(stat -c '%a' "$path" 2>/dev/null || echo "")"
  if [[ "$actual_mode" == "$expected_mode" ]]; then
    pass "${label}: ${path} has mode ${expected_mode}"
  else
    fail_assert "${label}: ${path} has mode '${actual_mode}', expected '${expected_mode}'"
  fi
}

assert_absent() {
  local label="$1"
  local path="$2"

  if [[ -e "$path" ]]; then
    fail_assert "${label}: unexpected entry ${path}"
  else
    pass "${label}: ${path} correctly absent"
  fi
}

assert_file_contents() {
  local label="$1"
  local path="$2"
  local expected_content="$3"

  if [[ ! -f "$path" ]]; then
    fail_assert "${label}: missing ${path}"
    return
  fi
  local actual_content
  actual_content="$(cat "$path")"
  if [[ "$actual_content" == "$expected_content" ]]; then
    pass "${label}: ${path} matches expected operator-managed content"
  else
    fail_assert "${label}: ${path} contains '${actual_content}' but operator content '${expected_content}' was expected"
  fi
}

# Stub `log` and `fail` so the helper is sourceable without the rest
# of the runtime common layer.
log() { printf '[log] %s\n' "$*"; }
fail() { printf '[fail] %s\n' "$*" >&2; exit 1; }

# shellcheck source=../common/jka_runtime_sync.sh
source "${REPO_ROOT}/scripts/common/jka_runtime_sync.sh"

setup_case() {
  local case_root="$1"
  local engine_dir="${case_root}/opt/jka/engine"
  local payload_root="${case_root}/opt/jka/engine-payload"
  local container_dir="${case_root}/home/container"

  mkdir -p "${engine_dir}" "${payload_root}/taystjk" "${container_dir}"

  printf 'mock-binary:taystjk\n' > "${engine_dir}/taystjkded.x86_64"
  chmod 0644 "${engine_dir}/taystjkded.x86_64"
  printf 'taystjk-commit\n' > "${engine_dir}/.upstream-commit"
  printf 'taystjk-ref\n' > "${engine_dir}/.upstream-ref"

  printf 'payload-file:taystjk\n' > "${payload_root}/taystjk/cgame.qvm"
  mkdir -p "${payload_root}/taystjk/nested"
  printf 'nested:taystjk\n' > "${payload_root}/taystjk/nested/inner.txt"
}

run_sync() {
  local case_root="$1"
  local auto_update="$2"
  local sync_payload="$3"

  (
    JKA_PATH_ENGINE_DIST="${case_root}/opt/jka/engine"
    JKA_PATH_ENGINE_BINARY_GLOB="taystjkded.*"
    JKA_PATH_ENGINE_PAYLOAD_ROOT="${case_root}/opt/jka/engine-payload"
    JKA_CONTAINER_ROOT="${case_root}/home/container"
    TAYSTJK_AUTO_UPDATE_BINARY="$auto_update"
    JKA_SYNC_MANAGED_TAYSTJK_PAYLOAD="$sync_payload"
    export JKA_PATH_ENGINE_DIST JKA_PATH_ENGINE_BINARY_GLOB JKA_PATH_ENGINE_PAYLOAD_ROOT \
      JKA_CONTAINER_ROOT TAYSTJK_AUTO_UPDATE_BINARY JKA_SYNC_MANAGED_TAYSTJK_PAYLOAD
    sync_runtime_files
  )
}

# Case 1: auto-update on, payload sync on — both binary and payload
# are installed from the image (image-managed runtime mode).
case1="${WORK_DIR}/auto-update-on"
setup_case "$case1"
run_sync "$case1" "true" "true"
assert_file_equal "[auto-update-on] engine binary" \
  "${case1}/opt/jka/engine/taystjkded.x86_64" \
  "${case1}/home/container/taystjkded.x86_64"
assert_mode "[auto-update-on] engine binary mode" \
  "${case1}/home/container/taystjkded.x86_64" 755
assert_file_equal "[auto-update-on] payload top file" \
  "${case1}/opt/jka/engine-payload/taystjk/cgame.qvm" \
  "${case1}/home/container/taystjk/cgame.qvm"
assert_file_equal "[auto-update-on] payload nested file" \
  "${case1}/opt/jka/engine-payload/taystjk/nested/inner.txt" \
  "${case1}/home/container/taystjk/nested/inner.txt"
assert_absent "[auto-update-on] commit stamp leak" \
  "${case1}/home/container/.upstream-commit"
assert_absent "[auto-update-on] ref stamp leak" \
  "${case1}/home/container/.upstream-ref"

# Case 2: auto-update off, payload sync on — operator-supplied
# binary in the container volume is preserved; payload is mirrored.
case2="${WORK_DIR}/auto-update-off"
setup_case "$case2"
mkdir -p "${case2}/home/container"
printf 'operator-managed:taystjk\n' > "${case2}/home/container/taystjkded.x86_64"
chmod 0700 "${case2}/home/container/taystjkded.x86_64"
run_sync "$case2" "false" "true"
assert_file_contents "[auto-update-off] operator binary preserved" \
  "${case2}/home/container/taystjkded.x86_64" \
  "operator-managed:taystjk"
assert_mode "[auto-update-off] operator binary mode preserved" \
  "${case2}/home/container/taystjkded.x86_64" 700
assert_file_equal "[auto-update-off] payload top file still synced" \
  "${case2}/opt/jka/engine-payload/taystjk/cgame.qvm" \
  "${case2}/home/container/taystjk/cgame.qvm"

# Case 3: payload sync off — taystjk/ payload is left as the
# operator-owned content even when files exist in the image payload.
case3="${WORK_DIR}/payload-sync-off"
setup_case "$case3"
mkdir -p "${case3}/home/container/taystjk"
printf 'operator-mod-file\n' > "${case3}/home/container/taystjk/cgame.qvm"
run_sync "$case3" "true" "false"
assert_file_equal "[payload-sync-off] engine binary still synced" \
  "${case3}/opt/jka/engine/taystjkded.x86_64" \
  "${case3}/home/container/taystjkded.x86_64"
assert_file_contents "[payload-sync-off] operator mod file preserved" \
  "${case3}/home/container/taystjk/cgame.qvm" \
  "operator-mod-file"

printf '\n----- sync_runtime_files regression: %d passed, %d failed -----\n' \
  "${PASSED}" "${FAILED}"

if [[ "${FAILED}" -gt 0 ]]; then
  exit 1
fi
