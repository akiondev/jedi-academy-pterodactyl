#!/usr/bin/env bash
#
# scripts/test/sync_runtime_files_test.sh
#
# Regression test for the manifest-driven sync_runtime_files() helper.
#
# Builds two mock image trees (TaystJK and OpenJK modern64), runs the
# helper with JKA_CONTAINER_ROOT pointed at a sandbox, and asserts that
# the resulting layout matches both:
#   - the pre-PR-D TaystJK behavior (engine binaries copied with mode
#     0755, taystjk/ payload mirrored under /home/container/taystjk/,
#     upstream stamp files NOT leaked into the container), and
#   - the OpenJK modern64 split layout (openjkded.x86_64 at the top
#     level, jampgamex86_64.so under base/).
#
# This test exists because PR-D refactors the previously hardcoded
# TaystJK sync into a manifest-driven helper. It must prove TaystJK
# is functionally unchanged, not merely claim it.
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

# Stub `log` and `fail` so the helper is sourceable without the rest
# of the runtime common layer.
log() { printf '[log] %s\n' "$*"; }
fail() { printf '[fail] %s\n' "$*" >&2; exit 1; }

# shellcheck source=../common/jka_runtime_sync.sh
source "${REPO_ROOT}/scripts/common/jka_runtime_sync.sh"

run_case() {
  local name="$1"
  local engine_glob="$2"
  local binary_filename="$3"
  local payload_subdir="$4"
  local payload_filename="$5"

  local case_root="${WORK_DIR}/${name}"
  local engine_dir="${case_root}/opt/jka/engine"
  local payload_root="${case_root}/opt/jka/engine-payload"
  local container_dir="${case_root}/home/container"

  mkdir -p "${engine_dir}" "${payload_root}/${payload_subdir}" "${container_dir}"

  printf 'mock-binary:%s\n' "${name}" > "${engine_dir}/${binary_filename}"
  chmod 0644 "${engine_dir}/${binary_filename}"
  printf '%s-commit\n' "${name}" > "${engine_dir}/.upstream-commit"
  printf '%s-ref\n' "${name}" > "${engine_dir}/.upstream-ref"

  printf 'payload-file:%s\n' "${name}" > "${payload_root}/${payload_subdir}/${payload_filename}"
  mkdir -p "${payload_root}/${payload_subdir}/nested"
  printf 'nested:%s\n' "${name}" > "${payload_root}/${payload_subdir}/nested/inner.txt"

  (
    JKA_PATH_ENGINE_DIST="${engine_dir}"
    JKA_PATH_ENGINE_BINARY_GLOB="${engine_glob}"
    JKA_PATH_ENGINE_PAYLOAD_ROOT="${payload_root}"
    JKA_CONTAINER_ROOT="${container_dir}"
    export JKA_PATH_ENGINE_DIST JKA_PATH_ENGINE_BINARY_GLOB JKA_PATH_ENGINE_PAYLOAD_ROOT JKA_CONTAINER_ROOT
    sync_runtime_files
  )

  local label_prefix="[${name}]"

  assert_file_equal "${label_prefix} engine binary" \
    "${engine_dir}/${binary_filename}" \
    "${container_dir}/${binary_filename}"
  assert_mode "${label_prefix} engine binary mode" \
    "${container_dir}/${binary_filename}" 755

  assert_file_equal "${label_prefix} payload top file" \
    "${payload_root}/${payload_subdir}/${payload_filename}" \
    "${container_dir}/${payload_subdir}/${payload_filename}"
  assert_file_equal "${label_prefix} payload nested file" \
    "${payload_root}/${payload_subdir}/nested/inner.txt" \
    "${container_dir}/${payload_subdir}/nested/inner.txt"

  # Stamp files must NOT be synced — they live in engine_dist_dir only.
  assert_absent "${label_prefix} commit stamp leak" \
    "${container_dir}/.upstream-commit"
  assert_absent "${label_prefix} ref stamp leak" \
    "${container_dir}/.upstream-ref"
}

# Case 1: TaystJK regression — proves PR-D's manifest-driven sync
# produces the same /home/container layout the hardcoded sync did
# (taystjkded.* binary copied to top level, taystjk/ payload mirrored).
run_case "taystjk" "taystjkded.*" "taystjkded.x86_64" "taystjk" "cgame.qvm"

# Case 2: OpenJK modern64 — proves the same helper handles a different
# engine binary glob and a different payload subdir with no code
# changes (openjkded.x86_64 + base/jampgamex86_64.so).
run_case "openjk-modern64" "openjkded.*" "openjkded.x86_64" "base" "jampgamex86_64.so"

printf '\n----- sync_runtime_files regression: %d passed, %d failed -----\n' \
  "${PASSED}" "${FAILED}"

if [[ "${FAILED}" -gt 0 ]]; then
  exit 1
fi
