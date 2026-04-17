# ADDON README ADVANCED

This document is the advanced reference for the addon system used by this TaystJK-first Pterodactyl project.

It is synced automatically into:

```text
/home/container/addons/docs/ADDON_README_ADVANCED.md
```

Use this file when you need the full execution model, ownership model, runtime state model, authoring rules, and practical guidance for building correct Bash or Python addons.

## Purpose

This addon system is intentionally simple.

It is:

- a script-based startup hook system
- designed for self-hosted Pterodactyl operators
- centered around the managed TaystJK runtime
- practical rather than generic

It is not:

- a TaystJK engine plugin API
- a sandbox
- a multi-phase plugin framework
- a dependency-managed extension platform

The runtime stays TaystJK-first:

- TaystJK is the default runtime
- TaystJK is the only automatically managed runtime/distribution
- manual alternative binaries and mod folders are allowed, but remain user-owned

## Core model

The addon system has four separate areas under `/home/container/addons`:

```text
/home/container/addons
  10-my-addon.sh
  20-my-addon.py
  docs/
    ADDON_README.md
    ADDON_README_ADVANCED.md
  examples/
    20-python-announcer.py
    20-python-announcer.config.json
    20-python-announcer.messages.txt
  defaults/
    30-checkserverstatus.sh
```

Meaning:

- top-level `/home/container/addons/*.sh` and `*.py` are **live user addons**
- `/home/container/addons/docs` is **image-managed documentation**
- `/home/container/addons/examples` is **image-managed example material**
- `/home/container/addons/defaults` is **image-managed helper/default material**

Only the **top-level** `.sh` and `.py` files are executed by the addon loader.

If a top-level addon filename ends with `.disable`, it is treated as intentionally disabled and is not executed.

Files inside `docs/`, `examples/`, and `defaults/` are not executed by the loader.

## Ownership model

### User-owned

These are user-owned:

- top-level addon scripts in `/home/container/addons`
- manual alternative binaries under `/home/container`
- manual alternative mod folders under `/home/container`
- copied example files after the user moves them into the top-level addon directory

### Image-managed

These are image-managed:

- `/home/container/addons/docs/*`
- `/home/container/addons/examples/*`
- `/home/container/addons/defaults/*`
- the built-in `checkserverstatus` helper installation path logic

Image-managed addon material is refreshed from the current image during normal managed startup.

Implications:

- if image-managed files are deleted, they return on the next managed startup
- if image-managed files are edited in place, those edits can be replaced on the next managed startup
- if the server owner wants an editable example, the example should be copied into the top-level addon directory first

## Execution model

### What executes

The loader executes:

- top-level `.sh` files with `bash`
- top-level `.py` files with `python3`

### What does not execute

The loader does not execute:

- files in subdirectories
- directories
- hidden files
- top-level files ending with `.disable`
- top-level support files such as `.md`, `.json`, and `.txt`
- image-managed docs/examples/defaults

### Order

Top-level addon scripts execute in alphabetical order.

Recommended naming convention:

```text
00-setup.sh
10-download-assets.py
20-patch-config.sh
90-webhook.py
```

This gives explicit, deterministic order without introducing a plugin framework.

### Startup placement

Normal managed startup flow is:

1. runtime preparation
2. image-managed addon docs refresh
3. image-managed addon examples refresh
4. image-managed helper/default refresh
5. managed `checkserverstatus` helper installation/update
6. top-level user addon execution
7. normal server startup

If `ADDONS_ENABLED=false`, step 6 is skipped, but the image-managed material still refreshes.

If the server is started with a fully custom startup command instead of the managed startup path, addon execution is bypassed.

## Failure model

Relevant variables:

- `ADDONS_ENABLED`
- `ADDON_CHECKSERVERSTATUS_ENABLED`
- `ADDONS_STRICT`
- `ADDONS_TIMEOUT_SECONDS`
- `ADDONS_LOG_OUTPUT`

### Success

- exit code `0` means success

### Failure

- any non-zero exit code means failure

### Strict vs best-effort

- `ADDONS_STRICT=false` -> failure is logged and startup continues
- `ADDONS_STRICT=true` -> failure stops startup

### Timeout

Each top-level addon is wrapped in a timeout.

If an addon exceeds `ADDONS_TIMEOUT_SECONDS`:

- it is treated as timed out
- it is counted as failed
- startup either continues or stops depending on strict mode

## Logging model

The loader logs:

- addon detection
- addon execution
- addon success
- addon timeout
- addon failure

If `ADDONS_LOG_OUTPUT=true`, stdout and stderr from the addon script are mirrored to the console.

Recommended log style for scripts:

- Bash: `[addon:bash] ...`
- Python: `[addon:python] ...`

Good logging should state:

- what the script is checking
- what it changed
- why it skipped something
- why it failed

## Runtime state model

The runtime publishes resolved values to:

- `/home/container/.runtime/taystjk-effective.env`
- `/home/container/.runtime/taystjk-effective.json`

### Environment file

The `.env` file contains the full effective runtime state, including sensitive values such as the effective RCON password when one exists.

### JSON file

The `.json` file contains selected non-sensitive values only.

### Common effective values

Important values include:

- `TAYSTJK_ACTIVE_MOD_DIR`
- `TAYSTJK_ACTIVE_SERVER_CONFIG`
- `TAYSTJK_ACTIVE_SERVER_CONFIG_PATH`
- `TAYSTJK_SERVER_CFG_OVERRIDES_ENABLED`
- `TAYSTJK_EFFECTIVE_SERVER_BINARY`
- `TAYSTJK_EFFECTIVE_SERVER_PORT`
- `TAYSTJK_EFFECTIVE_SERVER_HOSTNAME`
- `TAYSTJK_EFFECTIVE_SERVER_MOTD`
- `TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS`
- `TAYSTJK_EFFECTIVE_SERVER_GAMETYPE`
- `TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD`

### Recommended read order for addons

When an addon needs runtime values, prefer this order:

1. effective `TAYSTJK_*` environment variables already present in the current process
2. `/home/container/.runtime/taystjk-effective.env`
3. `/home/container/.runtime/taystjk-effective.json` for non-sensitive values
4. fallback to direct config parsing only when truly needed

This keeps scripts aligned with the actual managed runtime state instead of hardcoding assumptions.

## TaystJK-first implications for addons

This project is TaystJK-first, but addons must still respect manual alternatives.

That means:

- do not assume the active mod is always `taystjk`
- do not assume the active binary is always `taystjkded.*`
- do not assume the active config path is always `/home/container/taystjk/server.cfg`

Instead:

- prefer `TAYSTJK_ACTIVE_SERVER_CONFIG_PATH`
- prefer `TAYSTJK_ACTIVE_MOD_DIR`
- treat manual alternative binaries/mod folders as user-owned paths that must already exist

## Built-in managed helper

The project ships a managed helper:

```text
/home/container/addons/defaults/30-checkserverstatus.sh
```

This helper is not a user addon.

It exists to provide the built-in command:

```text
checkserverstatus
```

Behavior:

- it is refreshed from the image every managed startup
- it installs/updates `/home/container/bin/checkserverstatus`
- it is available from the Pterodactyl console through the runtime bridge
- it can also be run from a shell inside the container
- it is controlled by the egg variable `ADDON_CHECKSERVERSTATUS_ENABLED`

What it does:

- prints current server information
- reads runtime state
- performs a live RCON `status` lookup when RCON is configured

## Bundled example template

The project ships a Python announcer example:

```text
/home/container/addons/examples/20-python-announcer.py
/home/container/addons/examples/20-python-announcer.config.json
/home/container/addons/examples/20-python-announcer.messages.txt
```

This is an example template, not a live addon by default.

To activate it, copy those files into the top-level addon directory:

```text
/home/container/addons/20-python-announcer.py
/home/container/addons/20-python-announcer.config.json
/home/container/addons/20-python-announcer.messages.txt
```

Once copied there, the loader treats `20-python-announcer.py` as a live addon script.

To keep the copied example without running it, rename the copied executable file to end with `.disable`.

## Authoring rules

### General

- use explicit absolute paths
- keep scripts non-interactive
- keep behavior deterministic
- fail explicitly when required inputs are missing
- log clearly
- avoid unnecessary magic

### Bash rules

Recommended header:

```bash
#!/usr/bin/env bash
set -euo pipefail
```

Recommended practices:

- quote all variable expansions
- check file existence before editing
- prefer `jq` for JSON parsing
- prefer clear `echo` logs with `[addon:bash]`

### Python rules

Recommended header:

```python
#!/usr/bin/env python3
```

Recommended practices:

- prefer the Python standard library
- avoid assuming extra packages are installed
- use explicit `sys.exit(...)`
- prefer clear `print(...)` logs with `[addon:python]`

## Support files

Support files are allowed beside a top-level addon script when needed.

Examples:

- `20-my-addon.py`
- `20-my-addon.config.json`
- `20-my-addon.messages.txt`

The loader still only executes the `.sh` or `.py` file.

## Recommended addon patterns

Good addon patterns:

- config validator
- small config patcher
- downloader
- webhook sender
- environment preparation script
- small Python worker launcher when truly necessary

Patterns to avoid:

- interactive scripts
- overcomplicated orchestration
- large long-running systems that deserve their own service model
- pretending to be a plugin framework

## AI / generator guidance

If an AI or code generator is producing an addon for this project, it should follow these rules:

1. Place executable scripts only in top-level `/home/container/addons`.
2. If a script should stay present but not run, rename it to end with `.disable`.
3. Place support files beside the top-level script only when necessary.
4. Never place live executable addons in `docs/`, `examples/`, or `defaults/`.
5. Prefer runtime state over hardcoded assumptions.
6. Use explicit absolute paths under `/home/container`.
7. Keep scripts non-interactive.
8. Use clear log prefixes.
9. Fail clearly when required files or values are missing.
10. Prefer simple startup hooks over detached workers unless the use case truly requires background behavior.
11. Treat TaystJK as the default managed runtime, but do not hardcode it unless the addon is intentionally TaystJK-specific.

## Minimal examples

### Bash example

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "[addon:bash] Starting"

TARGET="${TAYSTJK_ACTIVE_SERVER_CONFIG_PATH:-}"
if [[ -z "${TARGET}" || ! -f "${TARGET}" ]]; then
  echo "[addon:bash] Missing config"
  exit 1
fi

echo "[addon:bash] Found config: ${TARGET}"
```

### Python example

```python
#!/usr/bin/env python3
import os
import sys

print("[addon:python] Starting")

path = os.getenv("TAYSTJK_ACTIVE_SERVER_CONFIG_PATH", "")
if not path or not os.path.isfile(path):
    print("[addon:python] Missing config")
    sys.exit(1)

print(f"[addon:python] Found config: {path}")
```

## Troubleshooting

### My addon does not run

Check:

- the file is directly inside `/home/container/addons`
- the filename ends with `.sh` or `.py`
- the filename does not end with `.disable`
- the startup path is the normal managed startup path
- `ADDONS_ENABLED=true`

### My addon runs in the wrong order

Use numeric prefixes:

- `00-...`
- `10-...`
- `20-...`

### My addon cannot find the active config or mod

Use:

- `TAYSTJK_ACTIVE_SERVER_CONFIG_PATH`
- `TAYSTJK_ACTIVE_MOD_DIR`
- `.runtime/taystjk-effective.env`

Do not hardcode `taystjk/server.cfg` unless the addon is intentionally TaystJK-specific.

### My Python addon fails

Check:

- the header is `#!/usr/bin/env python3`
- the script only uses available modules
- paths and environment variables are spelled correctly

### checkserverstatus is missing

Check:

- `ADDON_CHECKSERVERSTATUS_ENABLED=true`
- the server used the normal managed startup path
- `/home/container/addons/defaults/30-checkserverstatus.sh` exists
- `/home/container/bin/checkserverstatus` exists

### Legacy bundled-addons directory exists

If you still have an old `/home/container/addons/bundled-addons` directory from previous image versions, treat it as legacy. The loader no longer executes files from that path.
