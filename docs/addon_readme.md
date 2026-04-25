# Addon README

This guide is synced into:

```text
/home/container/addons/docs/ADDON_README.md
```

## 1. What you need to know

- Addons are simple startup scripts.
- Only top-level `.sh` and `.py` files in `/home/container/addons` are executed.
- Rename a top-level addon file to end with `.disable` if you want to keep it without running it.
- Files inside `docs/`, `examples/`, and `defaults/` are **not** executed.
- The built-in `checkserverstatus` helper is controlled by `ADDON_CHECKSERVERSTATUS_ENABLED`.
- The built-in `chatlogger` helper is controlled by `ADDON_CHATLOGGER_ENABLED`. It writes daily player chat logs into `/home/container/chatlogs`. It currently still tails the dedicated server's log file; that event source is **deprecated** in favour of the supervisor's process-output event bus (see `docs/addon_readme_advanced.md`) but continues to work.
- The legacy `rcon-live-guard` Python addon (`ADDON_RCON_LIVE_GUARD_ENABLED`, default **false**) is **deprecated** and superseded by the built-in supervisor RCON guard module (`RCON_GUARD_ENABLED`, default `true`), which receives parsed `bad_rcon` events directly from the dedicated server's stdout/stderr and never tails any file. See `docs/anti-vpn.md` for the full RCON guard configuration.
- The runtime live-output mirror file (`/home/container/.runtime/live/server-output.log`) is **disabled by default** and is no longer the supported addon event source. Operators that explicitly want a tailable file for debugging or external tooling can enable it with `JKA_LIVE_OUTPUT_MIRROR_ENABLED=true` (legacy alias `TAYSTJK_LIVE_OUTPUT_ENABLED=true`).
- Scripts run in alphabetical order before normal managed startup.
- If `ADDONS_STRICT=false`, failed addons are logged and startup continues.
- If `ADDONS_STRICT=true`, a failed addon stops startup.
- Useful runtime state is available in:
  - `/home/container/.runtime/taystjk-effective.env`
  - `/home/container/.runtime/taystjk-effective.json`
- **Live server output** is the preferred event source for event-driven addons. The anti-VPN supervisor mirrors every line the server prints to its stdout/stderr into `$TAYSTJK_LIVE_OUTPUT_PATH` (default `/home/container/.runtime/live/server-output.log`). Multiple addons can `tail -F` it concurrently. Tailing `server.log` is now considered fallback / legacy.

## 2. How to use addons

1. Put your script directly in:

```text
/home/container/addons
```

2. Use one of these file types:

- `something.sh`
- `something.py`

3. Restart the server with the normal managed startup path.

4. Watch the console for addon logs.

To disable a top-level addon without deleting it, rename it like this:

```text
20-webhook.py.disable
```

Recommended naming:

```text
10-setup.sh
20-webhook.py
90-finish.sh
```

If you want to use the bundled announcer example, copy these files from `examples/` into the top-level addon folder:

```text
/home/container/addons/examples/20-python-announcer.py
/home/container/addons/examples/20-python-announcer.config.json
/home/container/addons/examples/20-python-announcer.messages.txt
```

If you want to use the bundled live-event team announcer example (recommended pattern for new event-driven addons), copy these instead:

```text
/home/container/addons/examples/20-live-team-announcer.py
/home/container/addons/examples/20-live-team-announcer.config.json
```

If you later want to keep that copied announcer without running it, rename the copied script to:

```text
/home/container/addons/20-python-announcer.py.disable
```

## 3. Short examples

### Bash example

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "[addon:bash] Checking config"

TARGET="${TAYSTJK_ACTIVE_SERVER_CONFIG_PATH:-}"
if [[ -z "${TARGET}" || ! -f "${TARGET}" ]]; then
  echo "[addon:bash] Missing config"
  exit 1
fi

echo "[addon:bash] Config found: ${TARGET}"
```

### Python example

```python
#!/usr/bin/env python3
import os
import sys

print("[addon:python] Starting")

config_path = os.getenv("TAYSTJK_ACTIVE_SERVER_CONFIG_PATH", "")
if not config_path or not os.path.isfile(config_path):
    print("[addon:python] Missing config")
    sys.exit(1)

print(f"[addon:python] Config found: {config_path}")
```

### Live-output example (preferred for event-driven addons)

The supervisor mirrors every server stdout/stderr line into a runtime-managed file. Addons can subscribe to it with `tail -F`.

Bash:

```bash
#!/usr/bin/env bash
set -euo pipefail

LIVE="${TAYSTJK_LIVE_OUTPUT_PATH:-/home/container/.runtime/live/server-output.log}"

# tail -F handles rotation, truncation, and unlink+recreate transparently.
tail -n 0 -F -- "${LIVE}" | while IFS= read -r line; do
  if [[ "${line}" == *"ChangeTeam:"* ]]; then
    echo "[addon:bash-live] ${line}"
  fi
done
```

Python:

```python
#!/usr/bin/env python3
import os, subprocess

live = os.getenv("TAYSTJK_LIVE_OUTPUT_PATH", "/home/container/.runtime/live/server-output.log")
proc = subprocess.Popen(["tail", "-n", "0", "-F", "--", live],
                        stdout=subprocess.PIPE, text=True, bufsize=1)
for line in proc.stdout:
    if "ChangeTeam:" in line:
        print(f"[addon:python-live] {line.rstrip()}")
```

To send a message back to the server while reacting to a live line, use the same RCON path as the bundled `examples/20-python-announcer.py` (`say` or `svsay` over UDP using `TAYSTJK_EFFECTIVE_SERVER_PORT` and `TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD`). See `examples/20-live-team-announcer.py` for a complete worked example.
