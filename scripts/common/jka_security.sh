# shellcheck shell=bash
#
# scripts/common/jka_security.sh
#
# PR-A skeleton: input validation and normalization helpers.
# Functions are textually copied from scripts/entrypoint.sh and are NOT
# sourced by the runtime yet (see scripts/common/README.md).
#
# Requires: jka_runtime_common.sh sourced first (for `fail`, `warn`).

require_safe_component() {
  local value="$1"
  local variable_name="$2"

  if [[ -z "$value" || "$value" == "." || "$value" == ".." || "$value" == *"/"* || "$value" == *"\\"* || ! "$value" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
    fail "${variable_name} must be a simple relative name using only letters, numbers, dots, underscores or dashes"
  fi
}

require_safe_container_path() {
  local value="$1"
  local variable_name="$2"

  if [[ -z "$value" || "$value" != /home/container* || "$value" == *$'\n'* || "$value" == *$'\r'* || "$value" == *'..'* || "$value" == *'//'*
        || "$value" == *'\\'* || ! "$value" =~ ^/home/container(/[A-Za-z0-9._-]+)*$ ]]; then
    fail "${variable_name} must stay under /home/container and may use only letters, numbers, dots, underscores, dashes and slashes"
  fi
}

require_no_newlines() {
  local value="$1"
  local variable_name="$2"

  if [[ "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
    fail "${variable_name} may not contain newline characters"
  fi
}

normalize_optional_boolean() {
  local value="$1"
  local variable_name="$2"
  local fallback="$3"
  local normalized

  normalized="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
  case "$normalized" in
    true|false)
      printf '%s\n' "$normalized"
      ;;
    *)
      warn "${variable_name}=${value} is invalid, falling back to ${fallback}"
      printf '%s\n' "$fallback"
      ;;
  esac
}
