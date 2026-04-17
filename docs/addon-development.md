# Addon Development Guide

## Purpose

This document explains how to write Bash and Python addons that work correctly inside this Jedi Academy / TaystJK Pterodactyl environment.

This guide is also synced automatically by the runtime image into:

```text
/home/container/addons/ADDON_DEVELOPMENT.md
```

That copy is there for server owners and addon authors working directly inside the running server container.

## Runtime model

Addons are executed inside the current server container before normal server startup.

That means your addon code runs in the same container environment as the game server.

Important implications:

- your addon can access `/home/container`
- your addon can read and write server files
- your addon can use environment variables provided by the egg and entrypoint
- your addon should assume it is running in a Linux container
- your addon should not assume interactive input is available

## Supported languages

Officially supported addon types:

- Bash scripts: `.sh`
- Python 3 scripts: `.py`

Common bundled support files that may live beside addons:

- `.md`
- `.json`
- `.txt`

Recommended rule:

- use Bash for simple file/setup tasks
- use Python for more advanced logic, API calls, parsing, and automation

## Working directory and paths

Your addon should assume the main working area is:

```text
/home/container
```

Recommended practice:

- build absolute paths explicitly
- do not rely on unclear relative paths
- do not assume a specific mod directory unless you read it from environment variables

Good example:

```bash
CONFIG_PATH="/home/container/${FS_GAME_MOD:-taystjk}/${SERVER_CONFIG:-server.cfg}"
```

Bad example:

```bash
CONFIG_PATH="taystjk/server.cfg"
```

## Environment variables

Your addon should prefer environment variables over hardcoded assumptions.

Useful variables may include:

- `FS_GAME_MOD`
- `SERVER_PORT`
- `SERVER_CONFIG`
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
- `TAYSTJK_EFFECTIVE_SERVER_PORT`
- `TAYSTJK_EFFECTIVE_SERVER_HOSTNAME`
- `TAYSTJK_EFFECTIVE_SERVER_MOTD`
- `TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS`
- `TAYSTJK_EFFECTIVE_SERVER_GAMETYPE`
- `TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD`

Those values are also written to:

- `/home/container/.runtime/taystjk-effective.env`
- `/home/container/.runtime/taystjk-effective.json`

The `.env` file includes the full effective runtime state, including the current effective RCON password when one exists. The `.json` file contains selected non-sensitive values only.

The `SERVER_CFG_OVERRIDES_ENABLED` toggle controls whether non-empty egg override fields are written into the active `server.cfg`. When an override field is blank, your addon should expect the runtime state to fall back to the current config value and then to the built-in default.

Use sensible defaults when reading them.

### Bash example

```bash
MOD_DIR="${FS_GAME_MOD:-taystjk}"
CONFIG_FILE="${SERVER_CONFIG:-server.cfg}"
```

### Python example

```python
import os

mod_dir = os.getenv("TAYSTJK_ACTIVE_MOD_DIR", os.getenv("FS_GAME_MOD", "taystjk"))
config_file = os.getenv("TAYSTJK_ACTIVE_SERVER_CONFIG", os.getenv("SERVER_CONFIG", "server.cfg"))
```

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
- exit with non-zero on real failure

### Good Bash example

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "[addon:bash] Starting"

MOD_DIR="${FS_GAME_MOD:-taystjk}"
CONFIG_FILE="${SERVER_CONFIG:-server.cfg}"
TARGET="/home/container/${MOD_DIR}/${CONFIG_FILE}"

if [[ ! -f "${TARGET}" ]]; then
    echo "[addon:bash] Missing config: ${TARGET}"
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
- avoid assuming `pip` packages are installed
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

mod_dir = os.getenv("FS_GAME_MOD", "taystjk")
config_file = os.getenv("SERVER_CONFIG", "server.cfg")
path = f"/home/container/{mod_dir}/{config_file}"

if not os.path.isfile(path):
    print(f"[addon:python] Missing config: {path}")
    sys.exit(1)

print(f"[addon:python] Found config: {path}")
sys.exit(0)
```

## Exit codes and startup behavior

Exit codes matter.

### Success

- `0` means success

### Failure

- any non-zero code means failure

How failure is handled depends on addon mode:

### Best-effort mode

- failure is logged
- startup continues

### Strict mode

- failure is logged
- startup stops

## Logging conventions

Use clear prefixes so addon logs are easy to find.

### Bash

```bash
echo "[addon:bash] Doing something"
```

### Python

```python
print("[addon:python] Doing something")
```

Recommended logging style:

- what the script is doing
- what file/path it is using
- what decision it made
- why it failed, if it failed

## Timeout awareness

Your addon may be subject to a timeout.

That means your code should:

- avoid hanging forever
- avoid waiting for user input
- avoid long uncontrolled network calls
- use explicit timeouts for HTTP/API work

## Bundled example patterns in this project

This repository now includes two bundled example addons that demonstrate two useful patterns:

### Pattern 1: background worker addon

`20-python-announcer.py` shows how an addon can:

- launch a detached background worker
- exit cleanly so normal server startup can continue
- read a JSON config file
- read a separate message list
- use local RCON for repeated actions

This pattern is useful when you need periodic behavior without turning the core runtime into a framework feature.

### Pattern 2: command installer addon

`30-checkserverstatus.sh` shows how an addon can:

- run once during startup
- install a user-facing helper command
- integrate with the managed runtime console bridge
- reuse the same script as both the addon and the live command entry point

This pattern is useful for admin commands, validators, and maintenance helpers that should be easy to run on demand.

## Good use cases for addons

Addons work well for:

- generating or patching config files
- validating required files exist
- downloading trusted resources
- copying or backing up files
- sending Discord/webhooks
- calling APIs
- log parsing
- maintenance logic
- cleanup tasks

## Poor use cases for addons

Addons are not ideal for:

- replacing the main server process
- acting as a full plugin API for TaystJK
- implementing deep engine/game logic
- long-running uncontrolled daemon processes without clear intent

## Common pitfalls

### Hardcoding the mod directory

Do not assume `taystjk` is always the active mod.

Use:

- `FS_GAME_MOD`
- `SERVER_CONFIG`

### Assuming files always exist

Always check before reading, copying, or modifying.

### Assuming internet is available

Network calls can fail. Handle this cleanly.

### Using unsupported dependencies

Do not assume extra Python packages are installed unless you manage them explicitly yourself.

### Forgetting exit codes

If your addon fails silently, debugging becomes much harder.

### Writing destructive code without safeguards

Be careful with `rm`, overwrites, and large file operations.

## Example Bash addon

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "[addon:bash] Patching hostname"

MOD_DIR="${FS_GAME_MOD:-taystjk}"
CONFIG_FILE="${SERVER_CONFIG:-server.cfg}"
TARGET="/home/container/${MOD_DIR}/${CONFIG_FILE}"

if [[ -f "${TARGET}" ]]; then
    grep -q 'seta sv_hostname' "${TARGET}" || echo 'seta sv_hostname "Custom Server"' >> "${TARGET}"
fi
```

## Example Python addon

```python
#!/usr/bin/env python3
import json
import os
import sqlite3
from pathlib import Path

db_path = Path("/home/container/addons/addon-state.db")
db = sqlite3.connect(db_path)
db.execute("create table if not exists runs (id integer primary key, note text)")
db.execute("insert into runs(note) values (?)", ("startup hook executed",))
db.commit()

print(json.dumps({"addon": "startup-state", "status": "ok", "db": str(db_path)}))
```

## Final advice

Keep addons:

- explicit
- deterministic
- well logged
- small when possible
- safe around file writes

If a script grows too large or too critical, split it into smaller steps or document it clearly so future maintenance stays manageable.

Bundled examples should feel educational first:

- easy to read
- easy to disable
- easy to copy and rename
- easy to replace with your own version
