# Addon README

This file is the single source of addon documentation for the
TaystJK modern64 Pterodactyl image. It is synced into the running
container at:

```text
/home/container/addons/docs/ADDON_README.md
```

It covers the quick guide, the central `jka-addons.json` reference,
the event-bus NDJSON protocol, the runtime state available to
addons, and the advanced authoring guide. There are no other addon
docs in the image.

> **Configuration sources.**
>
> * Runtime-wide knobs (`anti_vpn.*`, `rcon_guard.*`, `addons.*`,
>   `supervisor.*`, etc.) live in
>   `/home/container/config/jka-runtime.json`.
> * Per-addon enable/disable and addon-specific options live in
>   `/home/container/config/jka-addons.json`.
> * The Pterodactyl egg only exposes four panel variables:
>   `COPYRIGHT_ACKNOWLEDGED`, `EXTRA_STARTUP_ARGS`, `SERVER_BINARY`,
>   `TAYSTJK_AUTO_UPDATE_BINARY`. Everything else is JSON.
>
> The runtime never overwrites either JSON file once it exists on disk.

## 1. Folder layout

```text
/home/container/config/
  jka-runtime.json
  jka-addons.json

/home/container/addons/
  10-my-addon.sh           ← user addons (top level only)
  20-my-addon.py
  defaults/                ← image-managed bundled default addons
    announcer.py
    live-team-announcer.py
    chatlogger.py
  docs/
    ADDON_README.md
```

* Top-level `.sh` and `.py` files in `/home/container/addons` are
  user addons, executed once at startup in alphabetical order.
* `/home/container/addons/defaults/` is image-managed. The bundled
  default addons live there with no numeric prefix and no per-addon
  config files. Their lifecycle is driven entirely by
  `/home/container/config/jka-addons.json`.
* `/home/container/addons/docs/` is image-managed and contains only
  this file.
* There is no `/home/container/addons/events/` directory and no
  `events/` subdirectory under `defaults/`. Event addons launch
  directly from
  `/home/container/addons/defaults/`.

## 2. Quick guide: enabling default addons

The image ships three default addons, all disabled by default:

| Name                  | Type        | Script (under `defaults/`) | Purpose                                                |
| --------------------- | ----------- | -------------------------- | ------------------------------------------------------ |
| `announcer`           | `scheduled` | `announcer.py`             | Periodic `svsay` announcements driven by a messages array in `jka-addons.json` |
| `live_team_announcer` | `event`     | `live-team-announcer.py`   | Announces team changes from `team_change` NDJSON events |
| `chatlogger`          | `event`     | `chatlogger.py`            | Writes daily chat logs from `chat_message` NDJSON events |

To enable any of them, edit
`/home/container/config/jka-addons.json` and flip its `enabled` flag
to `true`. The runtime picks up the change on the next start.

```json
{
  "addons": {
    "announcer":           { "enabled": true },
    "live_team_announcer": { "enabled": true },
    "chatlogger":          { "enabled": true }
  }
}
```

(Real edits should keep the rest of the keys; see the reference
below.)

The runtime uses the file as follows:

* `scheduled` addons (e.g. `announcer`) are launched once during
  startup. The script is responsible for spawning its own background
  worker if it needs one.
* `event` addons (e.g. `live_team_announcer`, `chatlogger`) are
  launched by the supervisor, which pipes parsed NDJSON events to
  the addon's stdin. There is no `/addons/events/` symlink layer.

## 3. `jka-addons.json` reference

Default content, written by the runtime on first boot when the file
is missing:

```json
{
  "addons": {
    "announcer": {
      "enabled": false,
      "order": 20,
      "type": "scheduled",
      "script": "announcer.py",
      "announce_command": "svsay",
      "interval_seconds": 300,
      "messages": [
        "jknexus.se - JK Web Based Client > Real Live Time & Search Master List Browser!"
      ]
    },
    "live_team_announcer": {
      "enabled": false,
      "order": 30,
      "type": "event",
      "script": "live-team-announcer.py",
      "announce_command": "svsay",
      "min_seconds_between_announcements": 3
    },
    "chatlogger": {
      "enabled": false,
      "order": 40,
      "type": "event",
      "script": "chatlogger.py"
    }
  }
}
```

**Announcer messages** are edited directly in
`/home/container/config/jka-addons.json` under the key
`addons.announcer.messages`. The default message is:

```
jknexus.se - JK Web Based Client > Real Live Time & Search Master List Browser!
```

To change what the announcer broadcasts, edit the `messages` array in
`jka-addons.json`. There is no separate `announcer.messages.txt` file.
Do **not** edit `/home/container/addons/defaults/*` directly — that
directory is image-managed and is refreshed on every start.

Common keys for every addon entry:

| Key       | Type              | Notes                                                                    |
| --------- | ----------------- | ------------------------------------------------------------------------ |
| `enabled` | bool              | Required. Disabled by default.                                            |
| `order`   | int               | Lower = earlier. Used to sort startup/launch order between addons.        |
| `type`    | `scheduled`/`event` | Decides whether the shell launcher or the supervisor owns the addon.    |
| `script`  | string            | Filename relative to `/home/container/addons/defaults/`. No `..`/abs paths. |

Addon-specific keys are documented in the comments inside each
script and in the table above.

The full JSON object for a given addon is delivered to the addon
process at launch time as the `JKA_ADDON_CONFIG_JSON` environment
variable. This means addons no longer need a sidecar `*.config.json`
file on disk.

Rules:

* The runtime creates `jka-addons.json` only when missing. Existing
  files are never overwritten.
* A malformed `jka-addons.json` aborts startup with a clear error.
* `script` must be a simple relative name. Paths containing `..`
  or absolute paths are rejected.

## 4. Event-bus NDJSON protocol

Event addons are spawned by the supervisor (the same process that
owns the dedicated server's stdout/stderr). The supervisor parses
the engine's output into typed events and writes one JSON object
per line to each addon's stdin. The addon writes nothing back on
stdout that the runtime cares about; its stdout/stderr are
forwarded to the console with a `[addon:<name>:stdout|stderr]`
prefix.

Each line looks like:

```json
{"type":"chat_message","ts":"2026-04-25T17:42:09Z","payload":{...}}
```

Supported event types currently include:

| `type`                    | Payload highlights                          |
| ------------------------- | ------------------------------------------- |
| `client_connect`          | `slot`, `name`, `ip`                        |
| `client_disconnect`       | `slot`, `name`, `ip`                        |
| `client_userinfo_changed` | `slot`, `name`, `ip` (when present)         |
| `chat_message`            | `slot`, `name`, `text`, optional `team`     |
| `team_change`             | `slot`, `name`, `team`                      |
| `bad_rcon`                | `host`, `command` (built-in RCON guard)     |
| `init_game` / `shutdown_game` | game lifecycle                          |

Authoring rules:

* Read line by line. Do **not** parse the engine's `server.log` or
  the optional `/home/container/.runtime/live/server-output.log`
  mirror. The mirror is OFF by default and is not a supported addon
  input.
* Treat every other line as forward-compatible. Unknown event types
  may appear; ignore what you do not handle.
* Honor SIGTERM and SIGINT. The supervisor closes your stdin and
  cancels your context on shutdown.
* The addon receives its config as JSON in `JKA_ADDON_CONFIG_JSON`
  and its name in `JKA_ADDON_NAME`.

## 5. RCON guard and anti-VPN runtime state

* The supervisor's built-in RCON guard
  (`rcon_guard.enabled = true` by default) consumes parsed
  `bad_rcon` events directly. There is no Python addon for this
  any more; the bundled `50-rcon-live-guard.py` was removed.
* The anti-VPN module is OFF by default. When enabled it writes
  PASS/BLOCKED chat lines on every connect (see
  `anti_vpn.broadcast.*` in `jka-runtime.json`) and persists allow
  rows to `/home/container/logs/anti-vpn-audit.log` unless
  `anti_vpn.audit_allow=false`.
* Useful runtime state files for addons:
  * `/home/container/.runtime/taystjk-effective.env`
  * `/home/container/.runtime/taystjk-effective.json`
  Both expose the resolved port, RCON password, fs_game, and
  active server config path so addons can talk to RCON without
  scraping `server.cfg`.

## 6. User addons

Drop a `.sh` or `.py` file directly in `/home/container/addons` to
have it executed once at startup:

```text
00-setup.sh
10-download-assets.py
20-patch-config.sh
90-webhook.py
```

Rules:

* Top-level only. Files in subdirectories are not executed.
* Recognized extensions: `.sh` (run with `bash`), `.py` (run with
  `python3`).
* Add the suffix `.disable` to keep a file but skip its execution
  (e.g. `90-webhook.py.disable`).
* Top-level `.md`, `.json`, `.txt` files are silently treated as
  support files.
* When `addons.strict=true` in `jka-runtime.json`, a failed user
  addon stops startup. The default is `false` — failures are logged
  and startup continues.
* Each addon is run with a wall-clock timeout
  (`addons.timeout_seconds`, default 30). Long-running work belongs
  in a detached background process.

## 7. Authoring guide

### Bash skeleton

```bash
#!/usr/bin/env bash
set -euo pipefail

LIVE_CONFIG="${TAYSTJK_ACTIVE_SERVER_CONFIG_PATH:-}"
if [[ -z "$LIVE_CONFIG" || ! -f "$LIVE_CONFIG" ]]; then
  echo "[addon:bash] missing config" >&2
  exit 1
fi
echo "[addon:bash] config=${LIVE_CONFIG}"
```

### Python skeleton

```python
#!/usr/bin/env python3
import os, sys

cfg = os.getenv("TAYSTJK_ACTIVE_SERVER_CONFIG_PATH", "")
if not cfg or not os.path.isfile(cfg):
    print("[addon:python] missing config", file=sys.stderr)
    sys.exit(1)
print(f"[addon:python] config={cfg}")
```

### Event addon skeleton (NDJSON on stdin)

```python
#!/usr/bin/env python3
import json, os, sys

config = json.loads(os.environ.get("JKA_ADDON_CONFIG_JSON", "{}"))
if not config.get("enabled"):
    sys.exit(0)

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        ev = json.loads(line)
    except json.JSONDecodeError:
        continue
    if ev.get("type") == "chat_message":
        print(f"chat: {ev['payload'].get('name')!r}: {ev['payload'].get('text')!r}")
```

To send something back to the server (e.g. `svsay`), use UDP RCON
on `127.0.0.1` with the port and password resolved by the
supervisor and exposed in the runtime state files. See
`bundled-addons/defaults/announcer.py` and
`bundled-addons/defaults/live-team-announcer.py` for complete,
production-quality examples.

### Ownership and lifecycle rules

* Image-managed (refreshed on every start, do not edit in place):
  * `/home/container/addons/docs/ADDON_README.md`
  * `/home/container/addons/defaults/*.py`
* User-owned (never overwritten by the runtime):
  * `/home/container/config/jka-runtime.json`
  * `/home/container/config/jka-addons.json`
  * Top-level `/home/container/addons/*.sh|*.py|*.disable`
* On every start the runtime safely removes legacy paths from
  earlier image revisions: `/home/container/addons/events`,
  `/home/container/addons/defaults/events`,
  `/home/container/addons/defaults/announcer.messages.txt`, and the old
  numeric default files (`20-python-announcer.*`, `30-live-team-announcer.*`,
  `40-chatlogger.*`). User-owned files outside those known legacy
  paths are not touched.

## 8. Diagnosing addon problems

* The startup banner prints an `[ADDONS]` block with the effective
  state of every default addon and the path to `jka-addons.json`.
* The supervisor logs `event addon started` / `event addon exited`
  events with addon name and PID.
* `addons.log_output=true` (default) forwards each addon's
  stdout/stderr to the runtime console, prefixed with the addon
  name and stream label.
* If an addon is enabled in `jka-addons.json` but the script does
  not exist under `defaults/`, the runtime logs a warning and
  continues. It never deletes user files trying to recover from
  this.
