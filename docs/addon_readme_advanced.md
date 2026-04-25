# ADDON README ADVANCED

> **Architecture notice (process-output-only runtime).** The supervisor is now the single owner/reader of the dedicated server's stdout/stderr. Bundled addons no longer tail `server.log` or the runtime live-output mirror as the supported event source — the live mirror is OFF by default and the legacy `server.log` fallback for the anti-VPN supervisor is OFF by default.
>
> The long-term direction is for addons to receive parsed events (`client_connect`, `client_disconnect`, `client_userinfo_changed`, `bad_rcon`, `chat_message`, `init_game`, `shutdown_game`, etc.) from the supervisor's event bus rather than scraping log files. Bundled addons that still tail logs (`40-chatlogger.py`) are marked deprecated for that event source. The bundled `50-rcon-live-guard.py` addon is superseded entirely by the built-in supervisor RCON guard module (`RCON_GUARD_ENABLED`) and is disabled by default.
>
> When writing new addons, do not rely on `server.log` or `/home/container/.runtime/live/server-output.log` being present or being authoritative. Those are debug/export artifacts only.

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
    40-chatlogger.py
```

Meaning:

- top-level `/home/container/addons/*.sh` and `*.py` are **live user addons**
- `/home/container/addons/docs` is **image-managed documentation**
- `/home/container/addons/examples` is **image-managed example material**
- `/home/container/addons/defaults` is **image-managed helper/default material**

Only the **top-level** `.sh` and `.py` files are executed by the addon loader.

If a top-level addon filename ends with `.disable`, it is treated as intentionally disabled and is not executed.

Files inside `docs/`, `examples/`, and `defaults/` are not executed by the loader.

The current default addon root is:

```text
/home/container/addons
```

The addon root can be changed with `ADDONS_DIR`, but it must remain under `/home/container`.

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

The loader invokes the interpreter directly:

- Bash addons are run as `bash /path/to/addon.sh`
- Python addons are run as `python3 /path/to/addon.py`

That means:

- a correct shebang is still strongly recommended
- the normal managed loader does not depend on the script having the executable bit set

### What does not execute

The loader does not execute:

- files in subdirectories
- directories
- hidden files
- top-level files ending with `.disable`
- top-level support files such as `.md`, `.json`, and `.txt`
- image-managed docs/examples/defaults

Important practical detail:

- top-level `.md`, `.json`, and `.txt` files are quietly treated as support files
- other unexpected top-level filenames are logged as unsupported addon files

So if an addon needs extra support material beyond `.md`, `.json`, or `.txt`, the cleanest choices are:

- place those support files in a subdirectory beside the addon
- or keep the support filename inside one of the silently ignored extensions when that makes sense

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

The managed helpers in `defaults/` are separate from user addon execution:

- `checkserverstatus` and `chatlogger` are refreshed and handled by dedicated runtime logic
- they are controlled by `ADDON_CHECKSERVERSTATUS_ENABLED` and `ADDON_CHATLOGGER_ENABLED`
- they are not normal top-level user addons
- they are not wrapped by the user addon timeout

## Failure model

Relevant variables:

- `ADDONS_ENABLED`
- `ADDON_CHECKSERVERSTATUS_ENABLED`
- `ADDON_CHATLOGGER_ENABLED`
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

Verified current runtime behavior:

- the current default timeout is `30` seconds
- accepted values are validated to the range `1` to `3600`
- the loader uses `timeout --foreground`

This matters for watcher-style addons:

- a long-running addon that stays in the foreground will eventually time out
- a watcher should not block the managed startup path unless that is explicitly intended

The practical pattern for long-running behavior is:

1. a short startup launcher runs as the addon
2. the launcher validates inputs, starts a detached worker, writes any needed PID/state files, and exits quickly
3. the detached worker continues in the background after startup

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

For detached workers, use a dedicated worker log file under `/home/container/logs` instead of assuming the original startup console output will remain available.

Verified local examples use:

- `/home/container/logs/bundled-python-announcer.log`
- `/home/container/logs/chatlogger-helper.log`
- `/home/container/logs/chatlogger.pid`
- `/home/container/logs/bundled-python-announcer.pid`

These are conventions, not mandatory fixed paths, but they are good patterns to follow.

## Runtime image tool baseline

In the current official Docker image for this project, the runtime intentionally installs a stronger addon baseline.

Verified from the local `docker/taystjk-modern64/Dockerfile`, the runtime image includes:

- `bash`
- `python3`
- `pip`
- `venv`
- `sqlite3`
- `curl`
- `wget`
- `jq`
- `git`
- `rsync`
- `procps`
- `tar`
- `unzip`

It also includes standard runtime pieces that matter operationally here:

- `coreutils`, which provides the `timeout` command used by the addon loader

Conservative contract:

- addon authors in this project can reasonably rely on the above tools in the official shipped image
- addon authors should still prefer the Python standard library unless extra tooling is genuinely needed
- third-party Python packages are not preinstalled just because `pip` and `venv` exist
- if someone replaces the entire runtime image with a custom image, this baseline is no longer guaranteed by this project

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
- `TAYSTJK_ACTIVE_SERVER_LOG_PATH`
- `TAYSTJK_SERVER_CFG_OVERRIDES_ENABLED`
- `TAYSTJK_EFFECTIVE_SERVER_BINARY`
- `TAYSTJK_EFFECTIVE_SERVER_PORT`
- `TAYSTJK_EFFECTIVE_SERVER_HOSTNAME`
- `TAYSTJK_EFFECTIVE_SERVER_MOTD`
- `TAYSTJK_EFFECTIVE_SERVER_MAXCLIENTS`
- `TAYSTJK_EFFECTIVE_SERVER_GAMETYPE`
- `TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD`

### Live-output runtime state

The runtime also publishes the live-output addon interface state. Every variable below is exported into the addon process environment **and** persisted into both the `.env` and `.json` runtime state files:

- `TAYSTJK_LIVE_OUTPUT_ENABLED` — `true` when the supervisor is running and is mirroring live server output, `false` otherwise. When `false`, addons must fall back to tailing `TAYSTJK_ACTIVE_SERVER_LOG_PATH`.
- `TAYSTJK_LIVE_OUTPUT_MODE` — `supervisor-mirror` when enabled, `disabled` otherwise. Reserved for future modes; addons should treat anything other than `disabled` as "live output is available".
- `TAYSTJK_LIVE_OUTPUT_SOURCE` — describes where the supervisor reads from. Default: `stdout-first` (matches the existing anti-VPN capture model: stdout/stderr first, log file fallback if appropriate).
- `TAYSTJK_LIVE_OUTPUT_FORMAT` — line delivery format. Default: `lines` (one server-printed line per file line, no JSON envelope, no timestamping by the runtime).
- `TAYSTJK_LIVE_OUTPUT_PATH` — absolute path to the live mirror file. Default: `/home/container/.runtime/live/server-output.log`. Multiple addons may `tail -F` this file concurrently.
- `TAYSTJK_LIVE_OUTPUT_MAX_BYTES` — soft size cap before the supervisor rotates the file. When the file grows past this size the supervisor renames it to `<path>.1` (replacing any previous archive) and reopens a fresh file. Default: `10485760` (10 MiB).

## Live server output (preferred event source)

Event-driven addons should consume **live server output** instead of tailing `server.log` themselves.

### Why this exists

A regular user addon launched from `entrypoint.sh` does not own the dedicated server's stdout/stderr pipes. The anti-VPN supervisor does, because it directly supervises the engine. Historically that meant:

- backend-managed features (anti-VPN) reacted in near real time
- addon-based features depended on tailing `server.log`, which the engine flushes lazily and rotates between maps, leading to delayed or fragile reactions

To close that gap, the supervisor now mirrors every line it scans on stdout/stderr into a runtime-managed **live output file**. Addons consume that file with any line-oriented reader, and benefit from the same near-real-time stream the supervisor uses internally.

### Architecture

- The anti-VPN supervisor owns the engine's stdout/stderr pipes (verified in `internal/antivpn/supervisor.go`, the `scanOutput` path).
- For every line the supervisor scans, it appends that line (newline-terminated) to `$TAYSTJK_LIVE_OUTPUT_PATH`.
- The file is a regular append-only file, **not** a FIFO/socket. This is intentional: a regular file allows multiple consumers (`tail -F`) to read concurrently, never blocks the supervisor's hot path on a missing reader, never loses lines on consumer disconnect, and is trivially inspectable with `cat` / `less`.
- The supervisor performs size-based rotation. When the file grows past `TAYSTJK_LIVE_OUTPUT_MAX_BYTES` it renames the current file to `<path>.1` (replacing any previous archive) and reopens a fresh file. Consumers using `tail -F` reattach automatically.
- When the supervisor is disabled (`ANTI_VPN_ENABLED=false` or `ANTI_VPN_MODE=off`), `TAYSTJK_LIVE_OUTPUT_ENABLED=false` and the live file is not produced. Addons must fall back to `TAYSTJK_ACTIVE_SERVER_LOG_PATH`.

### When to use live output vs server.log fallback

- **Use live output** for any event-driven addon: chat watchers, team-change watchers, kill-feed watchers, RCON-triggered automations, webhook bridges, map-change announcers, etc.
- **Use the `server.log` fallback** only when:
  - the addon explicitly needs to scan history written before it started, or
  - the supervisor is disabled and you still want the addon to function in a degraded mode.

The bundled `chatlogger` helper demonstrates the recommended pattern: prefer `TAYSTJK_LIVE_OUTPUT_PATH` when it is enabled, otherwise transparently fall back to `TAYSTJK_ACTIVE_SERVER_LOG_PATH`.

### Lifecycle and reliability guarantees

- The live mirror file exists for the entire lifetime of an active supervisor run. The file is opened on supervisor startup and closed on supervisor shutdown.
- Across server restarts (engine respawn), the supervisor process keeps running and keeps appending. There is **no** mid-stream truncation. Lines from `ShutdownGame` to `InitGame` continue uninterrupted.
- Across worker (supervisor) restarts the file is reopened in append mode, so consumers using `tail -F` resume cleanly.
- Rotation is best-effort and never fatal. If rotation fails (e.g. disk full), the supervisor disables further mirror writes for the rest of the run rather than blocking the server. A warning is logged once.
- Mirror writes are **never** allowed to block the engine. A failing write disables the mirror for the rest of the run; the engine continues normally.

### Performance caveats

- Every line the engine prints is written to disk via the supervisor. This is the same I/O profile the engine's own `server.log` already produces, so it is not a new bottleneck in practice. Operators with very chatty mods can lower `TAYSTJK_LIVE_OUTPUT_MAX_BYTES` to keep the active file small.
- Multiple `tail -F` consumers are cheap, but each one runs its own `tail` process. Prefer one consumer per addon.
- There is no per-line JSON envelope. If an addon needs structured events, parse them from the line text itself, the same way anti-VPN parses `ClientConnect:` / `ClientUserinfoChanged:` / `ChangeTeam:` lines.

### Bundled / default consumers

- `defaults/40-chatlogger.py` — primary input is the live mirror file when available, with automatic fallback to `server.log`. The status command (`--status`) prints which source is currently being tailed.
- `examples/20-live-team-announcer.py` — bundled example that consumes the live mirror file, parses `ChangeTeam:` events, and announces team changes via `say` or `svsay` over RCON. Activate it by copying the script and its `.config.json` into the top-level addon directory.

### Concrete examples

#### Bash: react to ChangeTeam events from live output

```bash
#!/usr/bin/env bash
set -euo pipefail

LIVE="${TAYSTJK_LIVE_OUTPUT_PATH:-/home/container/.runtime/live/server-output.log}"

if [[ ! -e "${LIVE}" ]]; then
  echo "[addon:bash-live] live output not available; supervisor disabled?"
  exit 1
fi

# tail -F follows rotation, truncation, and unlink+recreate.
tail -n 0 -F -- "${LIVE}" | while IFS= read -r line; do
  if [[ "${line}" == *"ChangeTeam:"* ]]; then
    echo "[addon:bash-live] ${line}"
  fi
done
```

#### Python: react to ChangeTeam events and announce via RCON

```python
#!/usr/bin/env python3
import os, re, socket, subprocess

LIVE = os.getenv("TAYSTJK_LIVE_OUTPUT_PATH", "/home/container/.runtime/live/server-output.log")
PORT = int(os.getenv("TAYSTJK_EFFECTIVE_SERVER_PORT", "29070"))
PASSWORD = os.getenv("TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD", "")

CHANGE_TEAM = re.compile(
    r'ChangeTeam:\s*\d+\s*\[[^\]]*\]\s*\([^)]*\)\s*"(?P<player>[^"]+)"\s+\w+\s*->\s*(?P<team>\w+)'
)

def announce(message: str, command: str = "svsay") -> None:
    if not PASSWORD:
        return
    payload = b"\xff\xff\xff\xffrcon " + PASSWORD.encode() + f' {command} "{message}"'.encode()
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(3)
    try:
        sock.sendto(payload, ("127.0.0.1", PORT))
    finally:
        sock.close()

proc = subprocess.Popen(["tail", "-n", "0", "-F", "--", LIVE],
                        stdout=subprocess.PIPE, text=True, bufsize=1)
for line in proc.stdout:
    match = CHANGE_TEAM.search(line)
    if not match:
        continue
    team = match.group("team").upper()
    player = match.group("player")
    if team == "RED":
        announce(f"{player} joined RED TEAM")
    elif team == "BLUE":
        announce(f"{player} joined BLUE TEAM")
    elif team in {"SPECTATOR", "FREE"}:
        announce(f"{player} changed SPECTATORS")
```

To use `say` instead of `svsay`, change the `command` argument in `announce()`. Both go through the same UDP RCON path used by the bundled examples.

#### Migration: switching an existing tail addon to live output

Before:

```bash
tail -n 0 -F -- "${TAYSTJK_ACTIVE_SERVER_LOG_PATH}"
```

After:

```bash
TAIL_PATH="${TAYSTJK_LIVE_OUTPUT_PATH:-${TAYSTJK_ACTIVE_SERVER_LOG_PATH}}"
if [[ "${TAYSTJK_LIVE_OUTPUT_ENABLED:-false}" != "true" ]]; then
  TAIL_PATH="${TAYSTJK_ACTIVE_SERVER_LOG_PATH}"
fi
tail -n 0 -F -- "${TAIL_PATH}"
```

### Recommended read order for addons

When an addon needs runtime values, prefer this order:

1. effective `TAYSTJK_*` environment variables already present in the current process
2. `/home/container/.runtime/taystjk-effective.env`
3. `/home/container/.runtime/taystjk-effective.json` for non-sensitive values
4. fallback to direct config parsing only when truly needed

This keeps scripts aligned with the actual managed runtime state instead of hardcoding assumptions.

Practical storage guidance:

- use `/home/container/.runtime` for runtime values produced by the managed startup path
- use `/home/container/logs` for addon logs and PID files
- use a dedicated user-owned directory under `/home/container` if the addon needs its own durable state or cache

Do not write custom addon state into image-managed addon trees such as:

- `/home/container/addons/docs`
- `/home/container/addons/examples`
- `/home/container/addons/defaults`

## TaystJK-first implications for addons

This project is TaystJK-first, but addons must still respect manual alternatives.

That means:

- do not assume the active mod is always `taystjk`
- do not assume the active binary is always `taystjkded.*`
- do not assume the active config path is always `/home/container/taystjk/server.cfg`
- do not assume the active log path is always `/home/container/taystjk/server.log`

Instead:

- prefer `TAYSTJK_ACTIVE_SERVER_CONFIG_PATH`
- prefer `TAYSTJK_ACTIVE_SERVER_LOG_PATH`
- prefer `TAYSTJK_ACTIVE_MOD_DIR`
- treat manual alternative binaries/mod folders as user-owned paths that must already exist

## Built-in managed helpers

The project ships managed helpers:

```text
/home/container/addons/defaults/30-checkserverstatus.sh
/home/container/addons/defaults/40-chatlogger.py
```

These helpers are not user addons.

### checkserverstatus

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

Practical note:

- this is a managed helper command bridged by the runtime supervisor
- it is not a TaystJK engine command implemented inside the game itself
- typing `checkserverstatus` or `rcon checkserverstatus` in the Pterodactyl console is handled by the runtime bridge, not by in-game remote admin clients

What it does:

- prints current server information
- reads runtime state
- performs a live RCON `status` lookup when RCON is configured

### chatlogger

The managed chat logger:

- is refreshed from the image every managed startup
- is controlled by the egg variable `ADDON_CHATLOGGER_ENABLED`
- prefers the runtime-managed **live output mirror** (`TAYSTJK_LIVE_OUTPUT_PATH`,
  default `/home/container/.runtime/live/server-output.log`) as its
  primary input, because that file is written directly by the anti-VPN
  supervisor that owns the engine's stdout/stderr pipes
- falls back to tailing `TAYSTJK_ACTIVE_SERVER_LOG_PATH` with
  `tail -n 0 -F` when the live mirror is unavailable (e.g. supervisor
  disabled), preserving backwards compatibility. In both cases `tail -F`
  transparently reattaches when the underlying file rotates, truncates,
  is unlinked, or is recreated
- writes clean daily chat logs into `/home/container/chatlogs`
- maintains `/home/container/chatlogs/latest.log` as a symlink to the current day
- keeps recent logs as plain `.log` files
- compresses older logs to `.gz`
- deletes very old logs automatically
- keeps worker state in `/home/container/logs/chatlogger.pid` (JSON)
- writes worker output, restart traces, and a periodic heartbeat to
  `/home/container/logs/chatlogger-helper.log`
- records progress, the active tail source, and the most recent captured
  chat in `/home/container/logs/chatlogger-state.json` for `--status`

Recognized chat verbs include `say`, `chat`, `sayteam`, `teamsay`,
`team`, `tell`, `whisper`, `pm`, `amsay`, `amtell`, `smsay`, `smtell`,
`tsay`, `csay`, `vsay`, `vchat`, plus a generic fallback for any other
`<verb>: <name>: <message>` shaped mod prefix.

Current chat log format:

```text
[2026-04-17 15:42:08 CEST] [PUBLIC] Akion: hello everyone
[2026-04-17 15:42:15 CEST] [TEAM] Akion: rally at duel room
[2026-04-17 15:42:21 CEST] [WHISPER] Akion -> Robin: meet me at duel room
[2026-04-17 15:42:30 CEST] [ADMIN] Admin: server restart in 5
[2026-04-17 15:42:35 CEST] [ADMIN_WHISPER] Admin -> Robin: stop telekilling
```

The helper strips Quake color codes such as `^1` from names and messages before writing them.

Optional environment overrides (managed defaults are safe for most
installs):

- `CHATLOGGER_KEEP_PLAIN_DAYS` — days to keep logs as plain `.log`
  before compressing to `.log.gz` (default `7`)
- `CHATLOGGER_KEEP_TOTAL_DAYS` — days to keep logs in any form before
  deletion (default `60`)
- `CHATLOGGER_HEARTBEAT_SECONDS` — heartbeat cadence written to the
  helper log and state file (default `300`, minimum `10`)
- `CHATLOGGER_TAIL_RESTART_BACKOFF_MAX` — maximum seconds the worker
  waits before re-spawning `tail` after it exits (default `30`)

Helper command-line modes (run as `python3
/home/container/addons/defaults/40-chatlogger.py <flag>`):

- no flag — refresh the managed worker (start it if not running)
- `--stop` — stop the managed worker
- `--status` — print PID, last heartbeat and last captured chat
- `--selftest` — exercise the parser against synthetic lines

Operational pattern it demonstrates:

- a short launcher runs during startup
- the launcher checks for an existing PID
- stale PID files are removed if the process no longer exists
- a detached worker is started in a new session
- the worker tails the runtime live-output mirror (or falls back to the resolved active server log path) and keeps running after startup

## Bundled example template

The project ships a Python announcer example:

```text
/home/container/addons/examples/20-python-announcer.py
/home/container/addons/examples/20-python-announcer.config.json
/home/container/addons/examples/20-python-announcer.messages.txt
```

This is an example template, not a live addon by default. It uses a periodic timer (no live event input).

A second bundled example demonstrates the **preferred** event-driven model — consuming the live server output mirror written by the supervisor:

```text
/home/container/addons/examples/20-live-team-announcer.py
/home/container/addons/examples/20-live-team-announcer.config.json
```

The live team announcer parses `ChangeTeam:` lines from `TAYSTJK_LIVE_OUTPUT_PATH` and emits human-friendly messages such as `Padawan joined RED TEAM`, `Padawan joined BLUE TEAM`, and `Padawan changed SPECTATORS` over `say` or `svsay`. Use it as a reference when writing new event-driven addons.

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
- use absolute paths under `/home/container`
- use `curl`, `wget`, `jq`, `sqlite3`, `git`, `rsync`, `tar`, and `unzip` only when they materially help the addon

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
- use `venv` and `pip` only when an addon truly needs packaged dependencies and you intentionally manage that lifecycle
- if launching a background worker, use explicit PID/log handling and exit the launcher quickly

## Support files

Support files are allowed beside a top-level addon script when needed.

Examples:

- `20-my-addon.py`
- `20-my-addon.config.json`
- `20-my-addon.messages.txt`

The loader still only executes the `.sh` or `.py` file.

Support files beside addons are fine when they are clearly tied to a single addon.

Practical guidance:

- keep support files beside the executable addon when the relationship is one-to-one
- use a subdirectory if the addon needs many auxiliary files or non-standard extensions
- prefer predictable sibling filenames so humans and AI can infer the relationship quickly

## Recommended addon patterns

Good addon patterns:

- config validator
- small config patcher
- downloader
- webhook sender
- environment preparation script
- small Python worker launcher when truly necessary
- launcher + background worker with PID/log handling when a watcher is truly needed

Patterns to avoid:

- interactive scripts
- overcomplicated orchestration
- large long-running systems that deserve their own service model
- pretending to be a plugin framework

## Long-running watcher addons

Long-running addons are possible, but the current loader model is still a startup hook system, not a service manager.

Because each top-level user addon is wrapped in `timeout`, the safe pattern is:

- the top-level addon acts as a launcher
- it starts a detached worker
- it exits quickly so startup can continue

Verified local examples:

- `20-python-announcer.py` uses a launcher + detached worker pattern with a PID file and a dedicated log file
- `40-chatlogger.py` uses a managed helper launcher + detached worker pattern with stale-PID cleanup

Recommended lifecycle rules for watcher addons:

- maintain a PID file if duplicate workers would be harmful
- check whether an existing PID is still alive before starting another worker
- remove stale PID files after crashes or hard restarts
- write worker logs to a durable file under `/home/container/logs`
- design the worker so a normal managed server restart can safely relaunch it

What to avoid:

- keeping the watcher in the foreground of the top-level addon entrypoint
- starting duplicate workers on every restart
- storing worker state only in process memory
- assuming the startup console is a durable log sink

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
10. Treat `.md`, `.json`, and `.txt` as safe support-file extensions beside live addons; other top-level extensions can create unsupported-file warnings.
11. Prefer simple startup hooks over detached workers unless the use case truly requires background behavior.
12. If a detached worker is needed, use a launcher + background worker pattern with PID/log handling.
13. Treat TaystJK as the default managed runtime, but do not hardcode it unless the addon is intentionally TaystJK-specific.
14. Prefer `TAYSTJK_*` runtime state over direct config parsing.
15. Do not store addon-owned state in image-managed addon trees.

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
- if you rely on external packages, make sure you intentionally installed and managed them yourself

### My watcher addon keeps timing out

Check:

- the top-level addon is not staying in the foreground
- the launcher exits before `ADDONS_TIMEOUT_SECONDS`
- the background worker is started detached from the startup path
- PID files are not forcing duplicate launches or blocking relaunch after a stale crash

### checkserverstatus is missing

Check:

- `ADDON_CHECKSERVERSTATUS_ENABLED=true`
- the server used the normal managed startup path
- `/home/container/addons/defaults/30-checkserverstatus.sh` exists
- `/home/container/bin/checkserverstatus` exists

On older existing servers, also confirm that the panel saved an explicit value for `ADDON_CHECKSERVERSTATUS_ENABLED`, because the egg default only applies automatically to new installs.

### chatlogger is missing or not writing logs

Check:

- `ADDON_CHATLOGGER_ENABLED=true`
- the server used the normal managed startup path
- `/home/container/addons/defaults/40-chatlogger.py` exists
- `/home/container/logs/chatlogger-helper.log` exists and shows
  recent `alive lines=… chats=… last_chat=…` heartbeat lines (the
  worker emits one every `CHATLOGGER_HEARTBEAT_SECONDS`, default 5
  minutes)
- `/home/container/logs/chatlogger.pid` is not stale — the worker
  validates the recorded PID against `/proc/<pid>/cmdline` on every
  managed startup, so a recycled PID will trigger a clean restart
  automatically
- the active server log exists at the path published in `TAYSTJK_ACTIVE_SERVER_LOG_PATH`, **or** the live mirror file at `TAYSTJK_LIVE_OUTPUT_PATH` is being written by the supervisor (check the chatlogger `--status` output for the active `tail source`)

Useful commands:

- `python3 /home/container/addons/defaults/40-chatlogger.py --status`
  prints PID, last heartbeat, lines seen, chats logged and the most
  recent captured chat line
- `python3 /home/container/addons/defaults/40-chatlogger.py --selftest`
  exercises the parser against synthetic lines without touching the
  live server log

The worker re-attaches automatically when the engine rotates,
truncates, unlinks or recreates `server.log` between maps or
restarts, so a chat going silent after `ShutdownGame` / `InitGame`
should no longer happen. If it does, inspect
`/home/container/logs/chatlogger-helper.log` for `tail exited` or
exception traces.

### Legacy bundled-addons directory exists

If you still have an old `/home/container/addons/bundled-addons` directory from previous image versions, treat it as legacy. The loader no longer executes files from that path.
