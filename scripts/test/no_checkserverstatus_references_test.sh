#!/usr/bin/env bash
#
# scripts/test/no_checkserverstatus_references_test.sh
#
# Regression guard: the `checkserverstatus` addon was removed during
# the Phase 3 migration. There must be no active references in
# scripts, Go source, runtime configs, the egg, or live docs. Strictly
# historical mentions are tolerated only inside CHANGELOG.md and
# release notes under releases/.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

PASSED=0
FAILED=0

pass() { PASSED=$((PASSED + 1)); printf '[ OK ] %s\n' "$1"; }
fail() { FAILED=$((FAILED + 1)); printf '[FAIL] %s\n' "$1" >&2; }

declare -a hits=()
while IFS= read -r match; do
  hits+=("$match")
done < <(
  cd "$REPO_ROOT" && \
  grep -RIn -E 'checkserverstatus|CHECKSERVERSTATUS' \
    --include='*.go' \
    --include='*.sh' \
    --include='*.py' \
    --include='*.json' \
    --include='*.md' \
    --include='*.yml' \
    --include='*.yaml' \
    . 2>/dev/null \
  | grep -v -E '^(\./)?(CHANGELOG\.md|releases/)' \
  | grep -v -E '^(\./)?scripts/test/no_(checkserverstatus_references|default_log_tailing)_test\.sh' \
  || true
)

if [[ ${#hits[@]} -eq 0 ]]; then
  pass "no active checkserverstatus references in source/docs/configs"
else
  printf '[FAIL] residual checkserverstatus references:\n' >&2
  for hit in "${hits[@]}"; do
    printf '   %s\n' "$hit" >&2
  done
  FAILED=$((FAILED + 1))
fi

echo
printf 'Passed: %d   Failed: %d\n' "$PASSED" "$FAILED"
if [[ $FAILED -gt 0 ]]; then
  exit 1
fi
