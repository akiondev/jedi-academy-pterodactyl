# Addon Support

## Overview

This project supports simple runtime addons for self-hosted Pterodactyl users.

Addons are user-owned scripts placed inside the current server container. They are executed automatically before the normal Jedi Academy / TaystJK server startup.

This is **not** a plugin API for the TaystJK engine.
This is **not** a secure sandbox.
This is a lightweight runtime loader for user scripts inside the current server container.

This guide is also synced automatically by the runtime image into:

```text
/home/container/addons/ADDONS.md
```

That copy is there to help server owners directly inside the container and may be refreshed by future image updates.

## What addons affect

Addons only affect the **current server container**.

They do **not**:

- modify the GitHub repository
- sync files back to the repository
- affect other users' servers
- affect other Pterodactyl containers

## Addon directory

Place addon files here:

```text
/home/container/addons
```

Example:

```text
addons/
  ADDONS.md
  ADDON_DEVELOPMENT.md
  10-backup-servercfg.sh
  20-discord-webhook.py
```

The two Markdown files are image-provided documentation. Your own `.sh` and `.py` files can live beside them.

## Supported file types

The addon loader officially supports:

- `.sh` -> executed with `bash`
- `.py` -> executed with `python3`

All other file types are ignored.

The loader also ignores:

- directories
- hidden files
- documentation files such as `.md`
- unsupported files

## Execution order

Addon files are executed in **alphabetical order**.

Recommended naming style:

```text
00-setup.sh
10-download-files.py
20-patch-config.sh
90-notify.py
```

This makes startup order explicit and easy to reason about.

## When addons run

Addons run **before normal server startup**.

Typical startup flow:

1. runtime preparation
2. image-managed addon docs are synced into the addon directory
3. addon loading
4. normal server startup

If addon support is disabled, addon execution is skipped. The image can still refresh the built-in addon documentation files in the addon directory.

If the container is started with a custom startup command instead of the normal managed startup path, addon execution is intentionally bypassed.

## Environment variables

The addon system uses these variables:

- `ADDONS_ENABLED`
- `ADDONS_DIR`
- `ADDONS_STRICT`
- `ADDONS_TIMEOUT_SECONDS`
- `ADDONS_LOG_OUTPUT`

Your scripts may also use normal runtime variables such as:

- `FS_GAME_MOD`
- `SERVER_PORT`
- `SERVER_CONFIG`
- `EXTRA_STARTUP_ARGS`

## Strict mode

### Best-effort mode

If an addon fails:

- the failure is logged
- startup continues

This is the default behavior.

### Strict mode

If an addon fails:

- the failure is logged
- startup stops

Use strict mode only when your addon is truly required for a correct startup.

## Timeout behavior

Each addon has its own configurable timeout.

If an addon exceeds the timeout:

- the timeout is logged
- the addon is treated as failed
- behavior then depends on strict mode

## Logging

Addon activity is logged clearly.

Typical events:

- addon directory found
- addon file detected
- addon skipped
- addon started
- addon completed successfully
- addon timed out
- addon failed with a non-zero exit code

If `ADDONS_LOG_OUTPUT=true`, addon stdout and stderr are also shown in the Pterodactyl console.

## Runtime tools available in the image

The runtime image includes a strong but still focused addon baseline:

- `bash`
- `python3`
- `python3-pip`
- `python3-venv`
- `sqlite3`
- `curl`
- `wget`
- `unzip`
- `tar`
- `jq`
- `ca-certificates`
- `git`
- `rsync`
- `procps`

This gives Bash and Python addons enough freedom for common automation, validation, maintenance, and API workflows.

## Common addon use cases

Addons can be used for many server-local tasks, for example:

- patch config files before startup
- create backups of important files
- validate required files exist
- download extra files from trusted sources
- send a Discord webhook when the server starts
- run maintenance logic
- clean up old logs
- generate runtime-specific config content
- store local helper state in SQLite
- call JSON APIs from Bash with `curl` and `jq`

## Example Bash addon

File:

```text
/home/container/addons/10-backup-servercfg.sh
```

Example:

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "[addon:bash] Starting config backup"

MOD_DIR="${FS_GAME_MOD:-taystjk}"
CONFIG_FILE="${SERVER_CONFIG:-server.cfg}"
CONFIG_PATH="/home/container/${MOD_DIR}/${CONFIG_FILE}"
BACKUP_DIR="/home/container/backups"

mkdir -p "${BACKUP_DIR}"

if [[ -f "${CONFIG_PATH}" ]]; then
    cp "${CONFIG_PATH}" "${BACKUP_DIR}/${CONFIG_FILE}.bak"
    echo "[addon:bash] Backup created"
else
    echo "[addon:bash] Config file not found, skipping"
fi
```

## Example Python addon

File:

```text
/home/container/addons/20-discord-webhook.py
```

Example:

```python
#!/usr/bin/env python3
import json
import os
import urllib.request

webhook_url = os.getenv("DISCORD_WEBHOOK_URL", "").strip()
if not webhook_url:
    print("[addon:python] No DISCORD_WEBHOOK_URL set, skipping")
    raise SystemExit(0)

payload = {
    "content": "Jedi Academy server is starting."
}

data = json.dumps(payload).encode("utf-8")
request = urllib.request.Request(
    webhook_url,
    data=data,
    headers={"Content-Type": "application/json"},
    method="POST",
)

with urllib.request.urlopen(request, timeout=10) as response:
    print(f"[addon:python] Webhook sent, status: {response.status}")
```

## Responsibility and safety notes

Addons are intentionally powerful within the current server container.

They may:

- read and write files
- modify configs
- call external APIs
- download files
- change the runtime environment of the current server

That means addon behavior is the **server owner’s responsibility**.

This project provides the addon loader, but does not guarantee the safety, correctness, or quality of user-provided addons.

## Troubleshooting

### My addon does not run

Check:

- the file is inside `/home/container/addons`
- the file extension is `.sh` or `.py`
- the file is not hidden
- addon support is enabled
- the file is not just documentation such as `.md`

### My Python addon fails

Check:

- `python3` is installed in the image
- the script uses only available dependencies
- the script is valid Python 3

### My server does not start after adding an addon

Check:

- whether strict mode is enabled
- the container logs for addon failure output
- whether the addon timed out
- whether the addon exited with a non-zero code

### My addon runs in the wrong order

Rename files so that alphabetical order matches the order you want.

Example:

```text
00-first.sh
10-second.py
20-third.sh
```
