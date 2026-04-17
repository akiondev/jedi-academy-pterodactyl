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
  20-python-announcer.py
  20-python-announcer.config.json
  20-python-announcer.messages.txt
  30-checkserverstatus.sh
  90-custom-webhook.py
```

The two Markdown files are image-provided documentation. The announcer and status command are bundled example addons. Your own `.sh` and `.py` files can live beside them.

## Supported file types

The addon loader officially supports:

- `.sh` -> executed with `bash`
- `.py` -> executed with `python3`

All other file types are ignored.

The loader also ignores:

- directories
- hidden files
- documentation files such as `.md`
- addon support files such as `.json` and `.txt`
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

## Bundled example addons

The image now ships two ready-made example addons by default:

- `20-python-announcer.py`
- `30-checkserverstatus.sh`

Their companion files are also included:

- `20-python-announcer.config.json`
- `20-python-announcer.messages.txt`

These bundled files are copied into `/home/container/addons` during the first managed startup of a server container.

Important behavior:

- bundled examples are meant to become **your live working copies**
- the runtime does **not** blindly overwrite those live copies on later startups
- if you edit them, your edits stay in place
- if you remove them after bootstrap, they stay removed

This keeps the default experience simple while still treating the live addon directory as user-owned runtime space.

## Bundled example 1: Python announcer

The bundled Python announcer is a real addon example for repeated background work.

Files:

```text
/home/container/addons/20-python-announcer.py
/home/container/addons/20-python-announcer.config.json
/home/container/addons/20-python-announcer.messages.txt
```

What it does:

- launches a small detached Python worker during addon loading
- waits for a configurable startup delay
- reads a message list from the bundled messages file
- sends repeated `svsay` announcements over local RCON

How to configure it:

1. Edit `20-python-announcer.config.json`
2. Edit `20-python-announcer.messages.txt`
3. Set `SERVER_RCON_PASSWORD` in the egg or keep `rconpassword` in the active server config

Important notes:

- the addon is enabled by default in its JSON config
- set `"enabled": false` if you want to disable it without deleting files
- the addon writes its own worker log to `/home/container/logs/bundled-python-announcer.log`
- it reads effective runtime settings from `/home/container/.runtime/taystjk-effective.env` first and then falls back to the JSON state file for non-sensitive values
- it falls back to the active mod/config from `FS_GAME_MOD` and `SERVER_CONFIG`

## Bundled example 2: Bash status command

The bundled Bash status example demonstrates a practical admin utility addon.

File:

```text
/home/container/addons/30-checkserverstatus.sh
```

What it does:

- runs as a normal startup addon
- makes a shell command named `checkserverstatus` available inside the container
- when that command is run, it prints:
  - basic current server information
  - current online players from a live RCON `status` query

How to run it:

```bash
checkserverstatus
```

Important notes:

- the command is installed into `/home/container/bin/checkserverstatus`
- `/home/container/bin` is added to `PATH` by the managed runtime startup
- live player output uses the effective runtime RCON password when available
- you can provide that password through `SERVER_RCON_PASSWORD` in the egg or `rconpassword` in the active server config
- if RCON is not configured, the command still shows basic server information

## Edit, disable, or remove bundled examples

The bundled examples are just normal runtime files after bootstrap.

That means you can:

- edit them directly
- rename them
- delete them
- replace them with your own versions

Recommended approaches:

- disable the announcer by setting `"enabled": false` in `20-python-announcer.config.json`
- remove the announcer by deleting its `.py`, `.json`, and `.txt` files
- remove the status example by deleting `30-checkserverstatus.sh`
- remove the helper command by deleting `/home/container/bin/checkserverstatus`

Your own custom addons and the bundled example addons all follow the same loader rules.

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
- `SERVER_CFG_OVERRIDES_ENABLED`
- `SERVER_RCON_PASSWORD`
- `EXTRA_STARTUP_ARGS`

The managed runtime also writes effective server values to:

- `/home/container/.runtime/taystjk-effective.env`
- `/home/container/.runtime/taystjk-effective.json`

The `.env` file includes the full effective runtime state, including the current effective RCON password when one exists. The `.json` file contains selected non-sensitive values for addons that prefer JSON parsing.

The optional `SERVER_CFG_OVERRIDES_ENABLED` toggle controls whether non-empty override variables from the egg are written back into the active `server.cfg`. When an override field is left blank, addons fall back to the current config value and then to the built-in runtime default.

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
- the file is not just documentation or support data such as `.md`, `.json`, or `.txt`

### My bundled announcer does not send messages

Check:

- `20-python-announcer.config.json` still has `"enabled": true`
- `20-python-announcer.messages.txt` contains at least one non-empty message
- `SERVER_RCON_PASSWORD` is set in the egg, or the active server config contains `rconpassword`
- `/home/container/logs/bundled-python-announcer.log` for announcer worker output

### The `checkserverstatus` command is missing

Check:

- addon support is enabled
- the server used the normal managed startup path
- `30-checkserverstatus.sh` still exists in `/home/container/addons`
- `/home/container/bin/checkserverstatus` exists inside the container

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
