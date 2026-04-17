# Addon Support

## Overview

This project supports lightweight runtime addons for self-hosted Pterodactyl users.

Addons are simple Bash or Python scripts that run inside the current server container before normal startup.

This is **not** a TaystJK plugin API.
This is **not** a secure sandbox.
This is **not** a general application framework.

It is a practical startup hook system for server owners who want to patch files, validate state, download extra content, call APIs, or run small preparation logic before the dedicated server starts.

This guide is also synced automatically into:

```text
/home/container/addons/docs/ADDONS.md
```

## What addons affect

Addons only affect the **current server container**.

They do **not**:

- modify the GitHub repository
- sync files back to the repository
- affect other servers
- affect other containers

The runtime stays TaystJK-first, but addons should not assume the active binary or mod directory is always TaystJK. The image only auto-manages the default TaystJK runtime path. Manual alternative binaries and mod folders remain user-owned.

## Directory layout

The addon area now has a simple split between **user-owned executable scripts** and **image-managed reference material**:

```text
/home/container/addons
  10-my-script.sh
  20-my-script.py
  docs/
    ADDONS.md
    ADDON_DEVELOPMENT.md
  examples/
    20-python-announcer.py
    20-python-announcer.config.json
    20-python-announcer.messages.txt
  defaults/
    30-checkserverstatus.sh
```

Meaning:

- top-level `/home/container/addons/*.sh` and `*.py` are **your live addons**
- `/home/container/addons/docs` is **image-managed documentation**
- `/home/container/addons/examples` is **image-managed example material**
- `/home/container/addons/defaults` is **image-managed helper/default material**

Only the **top-level** `.sh` and `.py` files are executed by the addon loader.

If you still have an older `/home/container/addons/bundled-addons` directory from previous image versions, treat it as legacy. The loader no longer executes files from that path.

## Supported file types

The loader officially supports:

- `.sh` -> executed with `bash`
- `.py` -> executed with `python3`

Top-level support files such as `.md`, `.json`, and `.txt` are ignored, but the recommended pattern is to keep image-managed docs/examples in their dedicated subdirectories and to keep your own executable scripts directly at the top level.

## Execution order

Top-level addon scripts are executed in **alphabetical order**.

Recommended naming style:

```text
00-setup.sh
10-download-files.py
20-patch-config.sh
90-notify.py
```

That keeps startup order explicit and easy to debug.

## When addons run

Normal managed startup flow:

1. runtime preparation
2. image-managed addon docs are refreshed into `/home/container/addons/docs`
3. image-managed addon examples are refreshed into `/home/container/addons/examples`
4. image-managed helper defaults are refreshed into `/home/container/addons/defaults`
5. top-level user addon scripts are executed
6. normal server startup begins

If addon support is disabled, step 5 is skipped, but the image-managed docs/examples/defaults still refresh.

If the container is started with a fully custom startup command instead of the normal managed startup path, addon execution is intentionally bypassed.

## Built-in helper: checkserverstatus

The runtime ships a built-in helper command:

```text
checkserverstatus
```

It is backed by the managed helper file:

```text
/home/container/addons/defaults/30-checkserverstatus.sh
```

Important behavior:

- it is always refreshed from the current image during managed startup
- it is installed automatically into `/home/container/bin/checkserverstatus`
- it can be run from the Pterodactyl console
- it can also be run from a container shell if shell access is available

What it does:

- prints basic current server information
- reads effective runtime state from `/home/container/.runtime`
- performs a live RCON `status` lookup when RCON is configured
- shows current map and current online players when live RCON data is available

This helper is **not** part of the user addon loader. It is a project-managed admin helper that ships with the image.

## Bundled example: Python announcer

The runtime also ships an example addon template:

```text
/home/container/addons/examples/20-python-announcer.py
/home/container/addons/examples/20-python-announcer.config.json
/home/container/addons/examples/20-python-announcer.messages.txt
```

What it demonstrates:

- a Python addon that starts a detached worker
- repeated `svsay` announcements over local RCON
- simple JSON configuration
- sibling support files beside the script

Important behavior:

- it is always refreshed from the image
- it is **not** executed automatically from `examples/`
- if you want to use it, copy it into the top-level addon directory

Example enable flow:

```text
/home/container/addons/20-python-announcer.py
/home/container/addons/20-python-announcer.config.json
/home/container/addons/20-python-announcer.messages.txt
```

After that, the loader will execute the copied `20-python-announcer.py` because it is now a top-level addon script.

## How to enable or disable addons

### Enable your own addon

Place a `.sh` or `.py` file directly in:

```text
/home/container/addons
```

That is enough to make it eligible for execution during the next managed startup.

### Disable a top-level addon

Use any of these approaches:

- remove the file from `/home/container/addons`
- rename it so it no longer ends with `.sh` or `.py`
- move it into a non-executed subdirectory

### Disable all top-level addon execution

Set:

```text
ADDONS_ENABLED=false
```

That disables execution of user-owned top-level addon scripts, but the image-managed docs, examples, and defaults still refresh.

### Disable the announcer example

The example announcer is not active until you copy it into the top-level addon directory.

If you copied it into the live addon directory, disable it by:

- removing the copied files
- renaming the copied `.py` file so it is no longer executable by the loader
- or setting `"enabled": false` in the copied JSON config

## Variables

The addon system uses these runtime variables:

- `ADDONS_ENABLED`
- `ADDONS_DIR`
- `ADDONS_STRICT`
- `ADDONS_TIMEOUT_SECONDS`
- `ADDONS_LOG_OUTPUT`

Useful runtime state for addons is published to:

- `/home/container/.runtime/taystjk-effective.env`
- `/home/container/.runtime/taystjk-effective.json`

The `.env` file includes the full effective runtime state, including the effective RCON password when one exists. The `.json` file contains selected non-sensitive values only.

The optional `SERVER_CFG_OVERRIDES_ENABLED` toggle controls whether non-empty override variables from the egg are written back into the active `server.cfg`. When an override field is blank, addons should expect the runtime state to fall back to the current config value and then to the built-in runtime default.

## Failure handling

If an addon exits with a non-zero status or times out:

- `ADDONS_STRICT=false` -> log the failure and continue startup
- `ADDONS_STRICT=true` -> stop startup

Use strict mode only when your addon is truly required for a correct startup.

## Timeout behavior

Each top-level addon has its own timeout:

```text
ADDONS_TIMEOUT_SECONDS
```

If a script exceeds that limit:

- the loader marks it as timed out
- startup either continues or stops depending on strict mode

## Logging

The loader logs:

- addon detection
- addon execution
- addon success
- addon timeout
- addon failure

If `ADDONS_LOG_OUTPUT=true`, addon stdout and stderr are also mirrored into the console.

Recommended practice:

- print clear prefixes like `[addon:bash]` or `[addon:python]`
- keep messages short and useful
- log what you are changing and why

## Runtime tooling available in the image

The runtime image includes a focused addon baseline:

- `bash`
- `python3`
- `python3-pip`
- `python3-venv`
- `sqlite3`
- `curl`
- `wget`
- `tar`
- `unzip`
- `jq`
- `git`
- `rsync`
- `procps`
- `ca-certificates`

## Common addon use cases

- patch or validate config files before startup
- download extra files before startup
- send a webhook when the server starts
- validate required files and fail fast if something is missing
- run Bash or Python maintenance logic inside the current container
- parse JSON APIs from Bash with `jq`
- keep small local state in SQLite

## Example Bash addon

```text
/home/container/addons/10-backup-servercfg.sh
```

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "[addon:bash] Starting config backup"

TARGET="${TAYSTJK_ACTIVE_SERVER_CONFIG_PATH:-}"
BACKUP_DIR="/home/container/backups"

mkdir -p "${BACKUP_DIR}"

if [[ -n "${TARGET}" && -f "${TARGET}" ]]; then
    cp "${TARGET}" "${BACKUP_DIR}/server.cfg.bak"
    echo "[addon:bash] Backup created"
else
    echo "[addon:bash] Config file not found, skipping"
fi
```

## Example Python addon

```text
/home/container/addons/20-discord-webhook.py
```

```python
#!/usr/bin/env python3
import json
import os
import urllib.request

webhook = os.getenv("DISCORD_WEBHOOK_URL", "").strip()
if not webhook:
    print("[addon:python] No DISCORD_WEBHOOK_URL set, skipping")
    raise SystemExit(0)

payload = {
    "content": f"Server startup hook ran for {os.getenv('TAYSTJK_ACTIVE_MOD_DIR', 'unknown')}"
}

request = urllib.request.Request(
    webhook,
    data=json.dumps(payload).encode("utf-8"),
    headers={"Content-Type": "application/json"},
    method="POST",
)

with urllib.request.urlopen(request, timeout=10) as response:
    print(f"[addon:python] Webhook sent, status: {response.status}")
```

## Responsibility

Addons are intentionally powerful inside the current server container.

That means addon behavior is the **server owner’s responsibility**.

This project provides the loader, the managed helper/defaults, and the example material. It does not guarantee the safety, correctness, or quality of user-provided scripts.

## Troubleshooting

### My addon does not run

Check:

- the file is directly inside `/home/container/addons`
- the file ends with `.sh` or `.py`
- addon support is enabled
- the startup path is the normal managed startup path

### My addon runs in the wrong order

Rename the files with numeric prefixes such as:

- `00-...`
- `10-...`
- `20-...`

### My Python addon fails

Check:

- the file starts with `#!/usr/bin/env python3`
- the script uses only standard-library modules unless you explicitly install more
- the runtime variables and paths are spelled correctly

### checkserverstatus is missing

Check:

- the server used the normal managed startup path
- `/home/container/addons/defaults/30-checkserverstatus.sh` exists
- `/home/container/bin/checkserverstatus` exists

### My server does not start after adding an addon

Check:

- the console output for addon failure messages
- whether the addon timed out
- whether the addon exited with a non-zero status
- whether `ADDONS_STRICT=true`
