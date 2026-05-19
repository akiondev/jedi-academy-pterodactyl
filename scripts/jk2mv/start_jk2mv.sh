#!/usr/bin/env bash
# JK2MV (Jedi Outcast) dedicated server start command.
#
# This script is intended to be invoked as the container CMD, after
# scripts/jk2mv/entrypoint.sh has prepared /home/container.
set -euo pipefail

CONTAINER_HOME="${HOME:-/home/container}"
CONTAINER_BASE_DIR="${CONTAINER_HOME}/base"
CONTAINER_BINARY="${CONTAINER_HOME}/jk2mvded"

SERVER_PORT="${SERVER_PORT:-28070}"
SERVER_CONFIG="${SERVER_CONFIG:-server.cfg}"
JK2MV_SERVER_VERSION="${JK2MV_SERVER_VERSION:-1.04}"
EXTRA_STARTUP_ARGS="${EXTRA_STARTUP_ARGS:-}"

cd "${CONTAINER_HOME}"

if [[ ! -f "${CONTAINER_BINARY}" ]]; then
    printf '[start_jk2mv] ERROR: %s not found. The runtime entrypoint should have installed it.\n' "${CONTAINER_BINARY}" >&2
    exit 1
fi
if [[ ! -x "${CONTAINER_BINARY}" ]]; then
    printf '[start_jk2mv] ERROR: %s is not executable.\n' "${CONTAINER_BINARY}" >&2
    exit 1
fi

# Required Raven Jedi Outcast assets. These are NOT distributed by this
# runtime - the operator must upload their own legally owned copies.
required_assets=(assets0.pk3 assets1.pk3)
warn_assets=()

case "${JK2MV_SERVER_VERSION}" in
    1.04)
        # 1.04 needs the full asset set.
        required_assets=(assets0.pk3 assets1.pk3 assets2.pk3 assets5.pk3)
        ;;
    1.02|1.03)
        warn_assets=(assets2.pk3 assets5.pk3)
        ;;
    auto|*)
        warn_assets=(assets2.pk3 assets5.pk3)
        ;;
esac

missing_required=()
for asset in "${required_assets[@]}"; do
    if [[ ! -f "${CONTAINER_BASE_DIR}/${asset}" ]]; then
        missing_required+=("${asset}")
    fi
done
if (( ${#missing_required[@]} > 0 )); then
    printf '[start_jk2mv] ERROR: missing required Jedi Outcast base assets in %s: %s\n' \
        "${CONTAINER_BASE_DIR}" "${missing_required[*]}" >&2
    printf '[start_jk2mv] Upload your legally owned Jedi Outcast assets and restart the server.\n' >&2
    exit 1
fi

for asset in "${warn_assets[@]}"; do
    if [[ ! -f "${CONTAINER_BASE_DIR}/${asset}" ]]; then
        printf '[start_jk2mv] WARNING: optional asset %s missing in %s (required for JK2 1.04 clients).\n' \
            "${asset}" "${CONTAINER_BASE_DIR}" >&2
    fi
done

# Build the argv. SERVER_CONFIG is passed bare to +exec because the JK2/Quake3
# engine resolves config files relative to the active fs_game (here: base/),
# i.e. base/${SERVER_CONFIG}.
declare -a argv
argv=(
    "./jk2mvded"
    "+set" "dedicated" "2"
    "+set" "net_port" "${SERVER_PORT}"
    "+set" "mv_serverversion" "${JK2MV_SERVER_VERSION}"
    "+exec" "${SERVER_CONFIG}"
)

# Append EXTRA_STARTUP_ARGS via word splitting so operators can pass
# additional `+set` style arguments. Quoting whole tokens is intentional.
if [[ -n "${EXTRA_STARTUP_ARGS}" ]]; then
    # shellcheck disable=SC2206
    extra=( ${EXTRA_STARTUP_ARGS} )
    argv+=( "${extra[@]}" )
fi

printf '[start_jk2mv] launching: %s\n' "${argv[*]}"
exec "${argv[@]}"
