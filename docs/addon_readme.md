# Addon README

This guide is synced into:

```text
/home/container/addons/docs/ADDON_README.md
```

> **Configuration source**
> All addon-related toggles below (`addons.enabled`, `addons.strict`,
> `addons.chatlogger_enabled`, `addons.event_bus.*`, etc.) are
> configured in `/home/container/config/jka-runtime.json`. Per-addon
> behaviour for the managed default addons is controlled by each
> addon's own `*.config.json` file under
> `/home/container/addons/defaults/`. The Pterodactyl egg only
> exposes four panel variables; everything else lives in JSON.

## 1. What you need to know

- Addons are simple startup scripts.
- Only top-level `.sh` and `.py` files in `/home/container/addons` are executed by the user-addon loader.
- Rename a top-level addon file to end with `.disable` if you want to keep it without running it.
- Files inside `docs/` and `defaults/` are **not** executed by the user-addon loader; the managed default addons under `defaults/` follow their own lifecycle (see below).
- **Managed default addons** are shipped with the image under `bundled-addons/defaults/` and synced into `/home/container/addons/defaults/`. They are **disabled by default** through their own `*.config.json` file. Enable an addon by editing its config file and setting `"enabled": true`. The runtime never overwrites edited config files.
  - Python announcer: `defaults/20-python-announcer.py` + `defaults/20-python-announcer.config.json`
  - Live team announcer (event-driven): `defaults/events/30-live-team-announcer.py` + `defaults/events/30-live-team-announcer.config.json`
  - Chatlogger (event-driven): `defaults/events/40-chatlogger.py` + `defaults/events/40-chatlogger.config.json`
- The legacy `rcon-live-guard` Python addon has been **removed from bundled defaults** and superseded by the built-in supervisor RCON guard module (`RCON_GUARD_ENABLED`, default `true`), which receives parsed `bad_rcon` events directly from the dedicated server's stdout/stderr and never tails any file. The reference copy is preserved under `bundled-addons/examples/deprecated/` for historical use only. See `docs/anti-vpn.md` for the full RCON guard configuration.
- The runtime live-output mirror file (`/home/container/.runtime/live/server-output.log`) is **disabled by default** and is no longer a supported addon event source. When the mirror is disabled the runtime also removes any stale `server-output.log*` files it left behind from earlier runs.
- Scripts run in alphabetical order before normal managed startup.
- If `ADDONS_STRICT=false`, failed addons are logged and startup continues.
- If `ADDONS_STRICT=true`, a failed addon stops startup.
- Useful runtime state is available in:
  - `/home/container/.runtime/taystjk-effective.env`
  - `/home/container/.runtime/taystjk-effective.json`
- **Event-driven addons** subscribe to a stable NDJSON event stream produced by the supervisor. The supervisor is the only reader of the dedicated server's stdout/stderr; addons no longer need to tail `server.log` or any mirror file. Drop a `.sh` or `.py` script into `/home/container/addons/events/` (path configurable via `ADDON_EVENT_ADDONS_DIR`) and the supervisor will spawn it and pipe events to its stdin. Tailing `server.log` or `server-output.log` from an addon is no longer supported as a runtime input.

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

To enable the bundled Python announcer, edit its managed config file in place:

```text
/home/container/addons/defaults/20-python-announcer.config.json
```

Set `"enabled": true` and adjust `interval_seconds`, `messages_file`, and the optional `schedule` array as needed. The image refreshes the addon source files (`20-python-announcer.py`, `20-python-announcer.messages.txt`) on every start, but it never overwrites your edited config file.

To enable the event-driven live team announcer, edit:

```text
/home/container/addons/defaults/events/30-live-team-announcer.config.json
```

Set `"enabled": true`. The supervisor will spawn `30-live-team-announcer.py` and pipe `team_change` NDJSON events to its stdin; the addon turns each one into a short `svsay` announcement using the runtime-managed RCON port and password.

To enable the event-driven chatlogger, edit:

```text
/home/container/addons/defaults/events/40-chatlogger.config.json
```

Set `"enabled": true`. The chatlogger consumes `chat_message` NDJSON events and writes daily logs into `/home/container/chatlogs`.

You should not copy any addon out of `defaults/` into the top-level `/home/container/addons/` directory; the managed lifecycle expects the addon files to live where the image puts them. If you want to disable an addon again, just set `"enabled": false` in its config file.

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

To send a message back to the server while reacting to a live line, use the same RCON path as the bundled `defaults/20-python-announcer.py` (`say` or `svsay` over UDP using `TAYSTJK_EFFECTIVE_SERVER_PORT` and `TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD`). See `defaults/events/30-live-team-announcer.py` for a complete worked event-driven example.
