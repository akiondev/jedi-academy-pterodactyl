# Addon Development Guide

## Purpose

This document explains how to write Bash and Python addons that work correctly inside this Jedi Academy / TaystJK Pterodactyl environment.

This guide is also synced automatically into:

```text
/home/container/addons/docs/ADDON_DEVELOPMENT.md
```

That copy is there for server owners and addon authors working directly inside the running server container.

## The mental model

Keep the addon model simple:

- a top-level `.sh` or `.py` file in `/home/container/addons` is a **startup hook**
- the loader runs those top-level scripts in alphabetical order
- the loader does not execute files from `docs/`, `examples/`, or `defaults/`

If a script lives under:

- `/home/container/addons/docs` -> documentation only
- `/home/container/addons/examples` -> example/template only
- `/home/container/addons/defaults` -> image-managed helper/default only

That separation keeps the system easier to understand and easier to debug.

## Runtime model

Addons run inside the current server container before normal managed startup.

That means:

- your addon can access `/home/container`
- your addon can read and write server files
- your addon can use environment variables provided by the egg and entrypoint
- your addon should assume Linux shell semantics
- your addon should not assume interactive input is available

## Supported languages

Officially supported addon types:

- Bash scripts: `.sh`
- Python 3 scripts: `.py`

Recommended rule:

- use Bash for simple setup, validation, and file edits
- use Python for heavier parsing, APIs, or structured logic
- do not hardcode `taystjk` unless your addon is intentionally TaystJK-specific

## Working directory and paths

Your addon should assume the main working area is:

```text
/home/container
```

Recommended practice:

- build absolute paths explicitly
- do not rely on unclear relative paths
- do not assume a specific mod directory unless you read it from runtime state

Good example:

```bash
CONFIG_PATH="${TAYSTJK_ACTIVE_SERVER_CONFIG_PATH:-/home/container/${TAYSTJK_ACTIVE_MOD_DIR:-${FS_GAME_MOD:-taystjk}}/${TAYSTJK_ACTIVE_SERVER_CONFIG:-${SERVER_CONFIG:-server.cfg}}}"
```

Bad example:

```bash
CONFIG_PATH="taystjk/server.cfg"
```

## Environment variables and runtime state

Useful environment variables may include:

- `FS_GAME_MOD`
- `SERVER_PORT`
- `SERVER_CONFIG`
- `SERVER_BINARY`
- `EXTRA_STARTUP_ARGS`
- `ADDONS_ENABLED`
- `ADDONS_DIR`
- `ADDONS_STRICT`
- `ADDONS_TIMEOUT_SECONDS`
- `ADDONS_LOG_OUTPUT`

For resolved runtime values, prefer the effective state published by the entrypoint:

- `TAYSTJK_ACTIVE_MOD_DIR`
- `TAYSTJK_ACTIVE_SERVER_CONFIG`
- `TAYSTJK_ACTIVE_SERVER_CONFIG_PATH`
- `TAYSTJK_EFFECTIVE_SERVER_BINARY`
- `TAYSTJK_EFFECTIVE_SERVER_PORT`
- `TAYSTJK_EFFECTIVE_SERVER_HOSTNAME`
- `TAYSTJK_EFFECTIVE_SERVER_MOTD`
- `TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS`
- `TAYSTJK_EFFECTIVE_SERVER_GAMETYPE`
- `TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD`

Those values are also written to:

- `/home/container/.runtime/taystjk-effective.env`
- `/home/container/.runtime/taystjk-effective.json`

The `.env` file includes the full effective runtime state, including the effective RCON password when one exists. The `.json` file contains selected non-sensitive values only.

The `SERVER_CFG_OVERRIDES_ENABLED` toggle controls whether non-empty egg override fields are written into the active `server.cfg`. When an override field is blank, your addon should expect the runtime state to fall back to the current config value and then to the built-in default.

The image-managed runtime only auto-prepares the default `taystjk` path. If a server owner switches to a manual alternative binary or mod folder, treat those paths as user-owned and assume they must already exist.

## Bash authoring guidelines

Use this header:

```bash
#!/usr/bin/env bash
set -euo pipefail
```

Recommended practices:

- quote variable expansions
- check files exist before modifying them
- log clearly
- use explicit paths
- keep scripts non-interactive
- exit non-zero on real failure

### Good Bash example

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "[addon:bash] Starting"

TARGET="${TAYSTJK_ACTIVE_SERVER_CONFIG_PATH:-}"

if [[ -z "${TARGET}" || ! -f "${TARGET}" ]]; then
    echo "[addon:bash] Missing config: ${TARGET:-not set}"
    exit 1
fi

echo "[addon:bash] Found config: ${TARGET}"
```

## Python authoring guidelines

Use this header:

```python
#!/usr/bin/env python3
```

Recommended practices:

- prefer the Python standard library only
- avoid assuming extra `pip` packages are present
- read environment variables with defaults
- print clear log messages
- exit with explicit status codes when needed
- keep scripts deterministic and non-interactive

### Good Python example

```python
#!/usr/bin/env python3
import os
import sys

print("[addon:python] Starting")

path = os.getenv("TAYSTJK_ACTIVE_SERVER_CONFIG_PATH", "")
if not path or not os.path.isfile(path):
    print(f"[addon:python] Missing config: {path or 'not set'}")
    sys.exit(1)

print(f"[addon:python] Found config: {path}")
sys.exit(0)
```

## Exit codes, strict mode, and timeouts

Success:

- `0` means success

Failure:

- any non-zero exit code means failure

How the loader reacts:

- `ADDONS_STRICT=false` -> failure is logged and startup continues
- `ADDONS_STRICT=true` -> failure stops startup

Timeouts:

- each addon is subject to `ADDONS_TIMEOUT_SECONDS`
- a timed-out addon is treated as failed

## Logging guidance

Use clear prefixes so addon logs are easy to find.

Examples:

```bash
echo "[addon:bash] Doing something"
```

```python
print("[addon:python] Doing something")
```

Recommended logging style:

- say what you are checking
- say what you changed
- say why you are skipping something
- avoid noisy debug spam unless it adds real value

## Support files beside an addon

Support files are fine when they belong to a top-level addon.

Common examples:

- `20-my-addon.config.json`
- `20-my-addon.messages.txt`
- `20-my-addon.env`

Keep the relationship obvious:

- `20-my-addon.py`
- `20-my-addon.config.json`

or:

- `10-patch-config.sh`
- `10-patch-config.rules.txt`

The key rule is still simple:

- only the top-level `.sh` and `.py` files are executed

## Examples shipped by the image

This repository ships two kinds of image-managed addon-related material:

### Example template

`/home/container/addons/examples/20-python-announcer.py` demonstrates how a Python addon can:

- read sibling config and message files
- read runtime state
- start a small background worker
- perform repeated RCON actions

If you want to use it, copy the script and its support files into the top-level addon directory.

### Managed helper/default

`/home/container/addons/defaults/30-checkserverstatus.sh` is a project-managed helper, not a user addon template.

It demonstrates useful shell patterns, but it is primarily there to provide the built-in `checkserverstatus` command.

## Good use cases for addons

- patch config files before startup
- validate required files
- download extra files before startup
- send webhooks
- prepare JSON or SQLite-backed local state
- perform simple maintenance logic before the server launches

## Poor use cases for addons

- anything that requires interactive input
- anything that should really live in the game engine/mod itself
- large service orchestration that deserves its own supervised runtime
- unnecessary complexity just because Python can do it

## Practical checklist

Keep addons:

- small
- explicit
- non-interactive
- path-aware
- easy to delete
- easy to copy into `/home/container/addons`
- easy to debug from console output

If your addon fails silently, debugging becomes much harder.
