#!/usr/bin/env python3
"""
Bundled default addon: scheduled Python announcer.

This script is launched by the runtime layer when
``addons.announcer.enabled = true`` in
``/home/container/config/jka-addons.json``. The runtime passes the
``addons.announcer`` JSON section as the ``JKA_ADDON_CONFIG_JSON``
environment variable so the addon does not need its own
``*.config.json`` file. When the addon loader executes it during
startup, the script launches a detached background worker and exits
quickly so the normal server startup can continue.

**Message configuration.**
Announcer messages are read from the ``messages`` array inside the
``addons.announcer`` object in
``/home/container/config/jka-addons.json``::

    "announcer": {
        "enabled": true,
        "messages": [
            "jknexus.se - JK Web Based Client > Real Live Time & Search Master List Browser!"
        ]
    }

Edit that file to change what the announcer broadcasts.  There is no
separate ``announcer.messages.txt`` file.  If ``messages`` is absent,
empty, or contains only blank/non-string entries the worker falls back
to the hardcoded default list defined in this script.

The background worker reads:

* ``JKA_ADDON_CONFIG_JSON`` -- runtime configuration JSON (passed by
  the runtime layer). When absent the script falls back to the
  ``addons.announcer`` section in ``jka-addons.json`` directly so
  ``python3 announcer.py`` keeps working from a shell.

It then sends announcements over local RCON. The default RCON command is
``svsay`` (server announcement, no name prefix). You can switch to ``say``
(speaks as the dedicated server console) by setting ``announce_command`` in
the config; any other value is rejected with a warning and ``svsay`` is used.

Two scheduling modes are supported:

1. **Simple interval** (default, back-compat): set ``interval_seconds`` and
   the worker rotates through the ``messages`` list every N seconds.

2. **Explicit schedule**: provide a non-empty ``schedule`` list. Each entry
   is one of:

       {"time": "HH:MM"}                 # fires daily at this wall-clock time
       {"time": "HH:MM:SS"}              # second-precision daily
       {"every_seconds": 1800}           # periodic, independent cadence

   Any entry may also carry a ``message`` literal to send at that slot
   (otherwise the next message from the ``messages`` array is used). Schedule
   entries fire independently; the daily ``time`` form uses the configured
   ``timezone`` (``"local"`` by default; IANA zone names like
   ``"Europe/Stockholm"`` also work).

Optimisation notes vs. earlier revisions:

* PID liveness now uses an exclusive ``fcntl.flock`` on a dedicated lockfile
  instead of ``os.kill(pid, 0)`` -- immune to PID reuse after a crash.
* Sleeps go through a ``threading.Event`` so SIGTERM/SIGINT exits promptly.
* Messages are re-read from ``jka-addons.json`` on every loop iteration so
  edits are picked up without restarting the worker.
* Wall-clock scheduling uses ``time.monotonic()`` for the sleep budget but
  resolves daily ``HH:MM`` slots against the real clock so DST / clock skew
  cannot drift the schedule.
* A one-shot startup diagnostic line is written to the worker log so silent
  misconfiguration (missing RCON password, empty messages, etc.) is visible
  without enabling debug logging.
"""

from __future__ import annotations

import atexit
import datetime as _dt
import errno
import fcntl
import json
import os
import re
import shlex
import signal
import socket
import subprocess
import sys
import threading
import time
from pathlib import Path
from typing import Any

try:  # zoneinfo is stdlib on Python 3.9+
    from zoneinfo import ZoneInfo, ZoneInfoNotFoundError
except ImportError:  # pragma: no cover - very old runtimes only
    ZoneInfo = None  # type: ignore[assignment]
    ZoneInfoNotFoundError = Exception  # type: ignore[assignment,misc]

HOME_DIR = Path("/home/container")
SCRIPT_PATH = Path(__file__).resolve()
# The addon's runtime configuration is supplied by the runtime layer
# through the ``JKA_ADDON_CONFIG_JSON`` environment variable. The value
# is the JSON object copied from
# ``/home/container/config/jka-addons.json`` -> ``addons.<name>``. This
# replaces the legacy ``*.config.json`` file that used to live next to
# this script. ``ADDONS_CONFIG_PATH`` is the centralised file the
# runtime layer reads from; it is consulted only as a debug fallback
# when ``JKA_ADDON_CONFIG_JSON`` is not provided.
ADDON_CONFIG_ENV = "JKA_ADDON_CONFIG_JSON"
ADDON_NAME_ENV = "JKA_ADDON_NAME"
ADDONS_CONFIG_PATH = Path(
    os.environ.get(
        "JKA_ADDONS_CONFIG_PATH",
        "/home/container/config/jka-addons.json",
    )
)
DEFAULT_ADDON_NAME = "announcer"
LOGS_DIR = HOME_DIR / "logs"
DEFAULT_LOG_PATH = LOGS_DIR / "bundled-python-announcer.log"
PID_PATH = LOGS_DIR / "bundled-python-announcer.pid"
LOCK_PATH = LOGS_DIR / "bundled-python-announcer.lock"
RUNTIME_STATE_PATH = HOME_DIR / ".runtime" / "taystjk-effective.json"
RUNTIME_ENV_PATH = HOME_DIR / ".runtime" / "taystjk-effective.env"

ALLOWED_ANNOUNCE_COMMANDS = ("svsay", "say")
DEFAULT_ANNOUNCE_COMMAND = "svsay"

# Hardcoded fallback message list used when ``addons.announcer.messages``
# is absent, empty, or contains only blank/non-string entries.
# Must contain exactly one entry.
DEFAULT_FALLBACK_MESSAGES: list[str] = [
    "jknexus.se - JK Web Based Client > Real Live Time & Search Master List Browser!",
]

# Minimum cadence for ``every_seconds`` schedule entries. Anything smaller
# would risk RCON spam and is almost certainly a config typo.
MIN_INTERVAL_SECONDS = 30

# Maximum the scheduler ever sleeps between ticks, even when the next slot
# is far away. Keeps response time to config edits, DST transitions and
# clock-skew bounded without busy-looping.
MAX_SLEEP_INTERVAL_SECONDS = 60

DEFAULT_CONFIG: dict[str, Any] = {
    "enabled": True,
    "startup_delay_seconds": 60,
    "interval_seconds": 900,
    "rcon_host": "127.0.0.1",
    "rcon_timeout_seconds": 3,
    "announce_command": DEFAULT_ANNOUNCE_COMMAND,
    "messages": DEFAULT_FALLBACK_MESSAGES,
    "log_file": str(DEFAULT_LOG_PATH),
    # Empty = use simple interval mode. Populate to use exact-time scheduling.
    "schedule": [],
    # "local" or any IANA zone name (e.g. "Europe/Stockholm").
    "timezone": "local",
}

WORKER_FLAG = "--run-worker"

_shutdown_event = threading.Event()


# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------


def log(message: str) -> None:
    print(f"[addon:python-announcer] {message}", flush=True)


# ---------------------------------------------------------------------------
# Config / message parsing
# ---------------------------------------------------------------------------


def safe_int(value: Any, default: int, minimum: int) -> int:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        return default
    return max(minimum, parsed)


def safe_bool(value: Any, default: bool) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        normalized = value.strip().lower()
        if normalized in {"true", "1", "yes", "on"}:
            return True
        if normalized in {"false", "0", "no", "off"}:
            return False
    return default


def resolve_sibling_path(value: str | None, fallback: Path) -> Path:
    if not value:
        return fallback

    candidate = Path(value)
    if candidate.is_absolute():
        return candidate
    return SCRIPT_PATH.parent / candidate


def normalise_announce_command(value: Any) -> str:
    # Whitelist on purpose: ``announce_command`` is interpolated into an
    # RCON command string sent over UDP. Restricting it to a small allow-list
    # of well-known JKA chat commands prevents config typos (or hostile
    # config files) from turning the announcer into an arbitrary RCON
    # executor.
    raw = str(value or "").strip().lower()
    if raw in ALLOWED_ANNOUNCE_COMMANDS:
        return raw
    if raw:
        log(
            f"announce_command={raw!r} is not allowed; "
            f"falling back to {DEFAULT_ANNOUNCE_COMMAND!r} "
            f"(allowed: {', '.join(ALLOWED_ANNOUNCE_COMMANDS)})"
        )
    return DEFAULT_ANNOUNCE_COMMAND


_TIME_OF_DAY_RE = re.compile(r"^(?P<h>\d{1,2}):(?P<m>\d{2})(?::(?P<s>\d{2}))?$")


def _parse_time_of_day(raw: str) -> _dt.time | None:
    """Parse an "HH:MM" or "HH:MM:SS" string into a ``datetime.time``."""
    match = _TIME_OF_DAY_RE.match(raw.strip())
    if not match:
        return None
    hour = int(match.group("h"))
    minute = int(match.group("m"))
    second = int(match.group("s") or 0)
    if not (0 <= hour <= 23 and 0 <= minute <= 59 and 0 <= second <= 59):
        return None
    return _dt.time(hour, minute, second)


def normalise_schedule(raw: Any) -> list[dict[str, Any]]:
    """Validate and normalise a ``schedule`` list from the config.

    Returns a list of dicts each with EXACTLY one of the keys
    ``time_of_day`` (a ``datetime.time``) or ``every_seconds`` (int >= 30),
    plus optional ``message`` (str). Invalid entries are dropped with a
    warning so a single typo never disables the whole worker.
    """
    if not isinstance(raw, list):
        if raw not in (None, ""):
            log(f"schedule must be a JSON list; got {type(raw).__name__!r}, ignoring")
        return []

    normalised: list[dict[str, Any]] = []
    for index, entry in enumerate(raw):
        if not isinstance(entry, dict):
            log(f"schedule[{index}] is not an object; skipping")
            continue

        item: dict[str, Any] = {}
        time_raw = entry.get("time")
        every_raw = entry.get("every_seconds", entry.get("interval_seconds"))
        message_raw = entry.get("message")

        if time_raw is not None and every_raw is not None:
            log(
                f"schedule[{index}] sets both 'time' and 'every_seconds'; "
                f"using 'time' and ignoring 'every_seconds'"
            )
            every_raw = None

        if time_raw is not None:
            tod = _parse_time_of_day(str(time_raw))
            if tod is None:
                log(
                    f"schedule[{index}] time={time_raw!r} is not 'HH:MM' or "
                    f"'HH:MM:SS'; skipping entry"
                )
                continue
            item["time_of_day"] = tod
        elif every_raw is not None:
            seconds = safe_int(every_raw, -1, 1)
            if seconds < MIN_INTERVAL_SECONDS:
                log(
                    f"schedule[{index}] every_seconds={every_raw!r} is below "
                    f"the {MIN_INTERVAL_SECONDS}s minimum; skipping entry"
                )
                continue
            item["every_seconds"] = seconds
        else:
            log(
                f"schedule[{index}] must define either 'time' or "
                f"'every_seconds'; skipping entry"
            )
            continue

        if message_raw is not None:
            text = str(message_raw).strip()
            if text:
                item["message"] = text

        normalised.append(item)

    return normalised


def _resolve_timezone(raw: Any) -> _dt.tzinfo | None:
    """Return a tzinfo for daily scheduling, or ``None`` for naive local time."""
    name = str(raw or "").strip()
    if not name or name.lower() == "local":
        # Use the system's local timezone (DST-aware via /etc/localtime).
        return _dt.datetime.now().astimezone().tzinfo
    if ZoneInfo is None:
        log(f"timezone={name!r} requested but zoneinfo is unavailable; using local")
        return _dt.datetime.now().astimezone().tzinfo
    try:
        return ZoneInfo(name)
    except ZoneInfoNotFoundError:
        log(f"timezone={name!r} could not be resolved; falling back to local")
        return _dt.datetime.now().astimezone().tzinfo


def _load_section_from_addons_file(name: str) -> dict[str, Any] | None:
    """Best-effort fallback when ``JKA_ADDON_CONFIG_JSON`` is missing.

    Reads the centralised ``jka-addons.json`` file and returns the
    ``addons.<name>`` object. Used only by direct invocation; the
    runtime layer normally passes the section as an env var.
    """
    try:
        raw = ADDONS_CONFIG_PATH.read_text(encoding="utf-8")
    except OSError:
        return None
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError:
        return None
    if not isinstance(parsed, dict):
        return None
    addons = parsed.get("addons")
    if not isinstance(addons, dict):
        return None
    section = addons.get(name)
    if isinstance(section, dict):
        return section
    return None


def load_config() -> dict[str, Any]:
    config = dict(DEFAULT_CONFIG)

    raw_env = os.environ.get(ADDON_CONFIG_ENV, "")
    loaded: dict[str, Any] | None = None
    if raw_env.strip():
        try:
            parsed = json.loads(raw_env)
        except json.JSONDecodeError as exc:
            log(f"Failed to parse {ADDON_CONFIG_ENV}: {exc}. Using defaults.")
        else:
            if isinstance(parsed, dict):
                loaded = parsed
            else:
                log(f"{ADDON_CONFIG_ENV} is not a JSON object. Using defaults.")

    if loaded is None:
        addon_name = os.environ.get(ADDON_NAME_ENV, DEFAULT_ADDON_NAME)
        loaded = _load_section_from_addons_file(addon_name)
        if loaded is None:
            log(
                "No addon config supplied via "
                f"{ADDON_CONFIG_ENV} or {ADDONS_CONFIG_PATH}; using built-in defaults."
            )

    if isinstance(loaded, dict):
        config.update(loaded)

    config["enabled"] = safe_bool(config.get("enabled", True), True)
    config["startup_delay_seconds"] = safe_int(config.get("startup_delay_seconds"), 60, 0)
    config["interval_seconds"] = safe_int(config.get("interval_seconds"), 900, 30)
    config["rcon_timeout_seconds"] = safe_int(config.get("rcon_timeout_seconds"), 3, 1)
    config["announce_command"] = normalise_announce_command(config.get("announce_command"))
    config["log_path"] = resolve_sibling_path(
        str(config.get("log_file", DEFAULT_LOG_PATH)),
        DEFAULT_LOG_PATH,
    )
    config["rcon_host"] = str(config.get("rcon_host", "127.0.0.1")).strip() or "127.0.0.1"
    config["schedule"] = normalise_schedule(config.get("schedule", []))
    config["tzinfo"] = _resolve_timezone(config.get("timezone", "local"))

    # Validate the ``messages`` array: keep only non-empty strings.
    raw_messages = config.get("messages")
    if isinstance(raw_messages, list):
        valid = [m for m in raw_messages if isinstance(m, str) and m.strip()]
    else:
        valid = []
    config["messages"] = valid if valid else list(DEFAULT_FALLBACK_MESSAGES)

    return config


# ---------------------------------------------------------------------------
# Runtime state (RCON port + password discovery)
# ---------------------------------------------------------------------------


def load_runtime_state() -> dict[str, Any]:
    state: dict[str, Any] = {}
    env_key_map = {
        "TAYSTJK_ACTIVE_SERVER_CONFIG_PATH": "active_server_config_path",
        "TAYSTJK_EFFECTIVE_SERVER_PORT": "effective_server_port",
        "TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD": "effective_server_rcon_password",
    }

    for key, mapped_key in env_key_map.items():
        value = os.getenv(key)
        if not value:
            continue
        state[mapped_key] = value.strip()

    if RUNTIME_ENV_PATH.is_file():
        try:
            for raw_line in RUNTIME_ENV_PATH.read_text(encoding="utf-8").splitlines():
                line = raw_line.strip()
                if not line or "=" not in line:
                    continue
                key, raw_value = line.split("=", 1)
                key = key.strip()
                if key not in env_key_map:
                    continue
                parsed_tokens = shlex.split(raw_value, posix=True)
                parsed_value = parsed_tokens[0] if parsed_tokens else ""
                state.setdefault(env_key_map[key], parsed_value)
        except OSError as exc:
            log(f"Failed to read runtime env {RUNTIME_ENV_PATH}: {exc}")

    if not RUNTIME_STATE_PATH.is_file():
        return state

    try:
        loaded = json.loads(RUNTIME_STATE_PATH.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError) as exc:
        log(f"Failed to read runtime state {RUNTIME_STATE_PATH}: {exc}")
        return state

    if isinstance(loaded, dict):
        for key in ("active_server_config_path", "effective_server_port"):
            if key not in state and key in loaded:
                state[key] = loaded[key]

    return state


def active_server_config_path() -> Path:
    runtime_state = load_runtime_state()
    runtime_path = str(runtime_state.get("active_server_config_path", "")).strip()
    if runtime_path:
        return Path(runtime_path)

    mod_dir = os.getenv("FS_GAME_MOD", "taystjk").strip() or "taystjk"
    server_config = os.getenv("SERVER_CONFIG", "server.cfg").strip() or "server.cfg"
    return HOME_DIR / mod_dir / server_config


def extract_rcon_password(config_path: Path) -> str | None:
    if not config_path.is_file():
        return None

    pattern = re.compile(
        r"^\s*set[a-z]*\s+rconpassword\s+(?:\"([^\"]+)\"|(\S+))",
        re.IGNORECASE,
    )

    try:
        for line in config_path.read_text(encoding="utf-8", errors="replace").splitlines():
            match = pattern.search(line)
            if not match:
                continue
            return (match.group(1) or match.group(2) or "").strip() or None
    except OSError as exc:
        log(f"Failed to read active server config {config_path}: {exc}")

    return None


def current_server_port() -> int:
    runtime_state = load_runtime_state()
    if runtime_state.get("effective_server_port"):
        return safe_int(runtime_state.get("effective_server_port"), 29070, 1)
    return safe_int(os.getenv("SERVER_PORT", "29070"), 29070, 1)


def effective_rcon_password(config_path: Path) -> str | None:
    runtime_state = load_runtime_state()
    runtime_password = str(runtime_state.get("effective_server_rcon_password", "")).strip()
    if runtime_password:
        return runtime_password
    return extract_rcon_password(config_path)


# ---------------------------------------------------------------------------
# RCON
# ---------------------------------------------------------------------------


def send_rcon_command(host: str, port: int, password: str, timeout_seconds: int, command: str) -> str:
    payload = b"\xff\xff\xff\xffrcon " + password.encode("utf-8") + b" " + command.encode("utf-8")
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(timeout_seconds)
    try:
        sock.sendto(payload, (host, port))
        try:
            response, _ = sock.recvfrom(65535)
        except socket.timeout:
            return ""
    finally:
        sock.close()

    decoded = response.decode("utf-8", errors="replace")
    decoded = decoded.lstrip("\xff")
    if decoded.startswith("print\n"):
        decoded = decoded[6:]
    return decoded.strip()


def escape_command_argument(value: str) -> str:
    return value.replace("\\", "\\\\").replace("\"", "\\\"")


def dispatch_announcement(config: dict[str, Any], message: str) -> bool:
    """Send a single announcement. Returns True on apparent success."""
    config_path = active_server_config_path()
    password = effective_rcon_password(config_path)
    if not password:
        log(f"No effective RCON password found for {config_path}; cannot send announcement yet")
        return False

    command = f'{config["announce_command"]} "{escape_command_argument(message)}"'
    try:
        response = send_rcon_command(
            config["rcon_host"],
            current_server_port(),
            password,
            int(config["rcon_timeout_seconds"]),
            command,
        )
    except OSError as exc:
        log(f"Announcement failed: {exc}")
        return False

    if response and "bad rconpassword" in response.lower():
        log(f"Announcement rejected by server: {response}")
        return False

    if response:
        log(f"Sent announcement: {message}")
    else:
        log(f"Dispatched announcement without an RCON reply: {message}")
    return True


# ---------------------------------------------------------------------------
# PID + lockfile handling (PID-reuse-safe via fcntl.flock)
# ---------------------------------------------------------------------------


def acquire_worker_lock() -> Any | None:
    """Take an exclusive lock so only one worker runs at a time."""
    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    handle = open(LOCK_PATH, "a+", encoding="utf-8")  # noqa: SIM115 - lifetime managed
    try:
        fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        handle.close()
        return None
    handle.seek(0)
    handle.truncate()
    handle.write(f"{os.getpid()}\n")
    handle.flush()
    return handle


def previous_worker_alive() -> bool:
    """Best-effort check using the lockfile (race-free across PID reuse)."""
    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    try:
        handle = open(LOCK_PATH, "a+", encoding="utf-8")  # noqa: SIM115
    except OSError:
        return False
    try:
        try:
            fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            return True
        fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
        return False
    finally:
        handle.close()


def read_existing_pid() -> int | None:
    if not PID_PATH.is_file():
        return None
    try:
        return int(PID_PATH.read_text(encoding="utf-8").strip())
    except (OSError, ValueError):
        return None


def remove_pid_if_owned() -> None:
    existing_pid = read_existing_pid()
    if existing_pid == os.getpid():
        try:
            PID_PATH.unlink(missing_ok=True)
        except OSError:
            pass


# ---------------------------------------------------------------------------
# Scheduling
# ---------------------------------------------------------------------------


def _today_target(now: _dt.datetime, target: _dt.time) -> _dt.datetime:
    return now.replace(
        hour=target.hour,
        minute=target.minute,
        second=target.second,
        microsecond=0,
    )


def _seconds_until_slot(
    target: _dt.time,
    tz: _dt.tzinfo | None,
    fired_today: bool,
) -> float:
    """Return seconds to wait before this daily slot is due to fire.

    If today's slot has not been fired yet and the wall clock is already
    at-or-past it (e.g. the worker just started), returns ``0.0`` so the
    firing pass executes it immediately. Otherwise returns the strictly
    positive distance to the next not-yet-fired occurrence.
    """
    now = _dt.datetime.now(tz=tz)
    today_target = _today_target(now, target)
    if not fired_today and now >= today_target:
        return 0.0
    next_target = today_target if now < today_target else today_target + _dt.timedelta(days=1)
    return max(0.0, (next_target - now).total_seconds())


def _slot_due_now(
    target: _dt.time,
    tz: _dt.tzinfo | None,
    fired_today: bool,
) -> tuple[bool, _dt.date]:
    """Return ``(due_now, today_date)`` for a daily slot.

    ``due_now`` is True iff the wall clock is at-or-past today's slot AND we
    have not already fired today's occurrence. ``today_date`` is the local
    (or zone-aware) date corresponding to the current wall clock so the
    caller can record it as the new ``last_fired`` marker.
    """
    now = _dt.datetime.now(tz=tz)
    today_target = _today_target(now, target)
    return (not fired_today and now >= today_target, now.date())


def _interruptible_sleep(seconds: float) -> bool:
    """Sleep for ``seconds`` or until shutdown is signalled. Returns True if shutdown."""
    if seconds <= 0:
        return _shutdown_event.is_set()
    return _shutdown_event.wait(timeout=seconds)


def _next_message(messages: list[str], cursor: list[int]) -> str | None:
    """Return the next message in rotation, advancing the cursor list in-place."""
    if not messages:
        return None
    index = cursor[0] % len(messages)
    cursor[0] = (cursor[0] + 1) % max(1, len(messages))
    return messages[index]


def _format_schedule_for_log(schedule: list[dict[str, Any]]) -> str:
    parts: list[str] = []
    for entry in schedule:
        if "time_of_day" in entry:
            tod: _dt.time = entry["time_of_day"]
            label = f"@{tod.strftime('%H:%M:%S')}"
        else:
            label = f"every {entry['every_seconds']}s"
        if entry.get("message"):
            label += " (literal)"
        parts.append(label)
    return ", ".join(parts) if parts else "<empty>"


def _emit_startup_diagnostics(
    config: dict[str, Any],
    messages: list[str],
) -> None:
    config_path = active_server_config_path()
    password = effective_rcon_password(config_path)
    port = current_server_port()
    mode = "schedule" if config["schedule"] else f"interval={config['interval_seconds']}s"
    tz_label = str(config.get("timezone") or "local")
    log("=== announcer startup ===")
    log(f"  mode             : {mode}")
    if config["schedule"]:
        log(f"  schedule entries : {_format_schedule_for_log(config['schedule'])}")
        log(f"  timezone         : {tz_label}")
    log(f"  announce_command : {config['announce_command']}")
    log(f"  rcon_host:port   : {config['rcon_host']}:{port}")
    log(f"  rcon_password    : {'present' if password else 'MISSING'}")
    log(f"  active_server_cfg: {config_path}")
    log(f"  messages         : {len(messages)} entries (from jka-addons.json)")
    log(f"  log_file         : {config['log_path']}")
    log("=========================")


def run_simple_interval_loop(
    initial_config: dict[str, Any],
) -> int:
    cursor = [0]
    while not _shutdown_event.is_set():
        config = load_config()
        if not config["enabled"]:
            log("Announcer was disabled in config; worker will exit")
            return 0
        if config["schedule"]:
            log("Schedule was added to config at runtime; switching to scheduled mode")
            return run_scheduled_loop(config)

        messages = config["messages"]
        if not messages:
            log("No announcer messages are configured; retrying later")
        else:
            message = _next_message(messages, cursor) or ""
            if message:
                dispatch_announcement(config, message)

        if _interruptible_sleep(int(config["interval_seconds"])):
            return 0
    return 0


def run_scheduled_loop(
    initial_config: dict[str, Any],
) -> int:
    """Dispatch announcements based on the ``schedule`` list."""
    cursor = [0]
    # Per-entry monotonic deadlines for ``every_seconds`` entries.
    # Keyed by list index; if the user reorders entries mid-run a deadline
    # may carry over to a sibling entry, but for an example helper that's
    # an acceptable trade-off vs. fingerprinting.
    monotonic_deadlines: dict[int, float] = {}
    # Per-entry "date of last successful fire" for ``time_of_day`` entries
    # so each daily slot fires exactly once per day even if the scheduler
    # wakes slightly after the wall-clock target.
    last_fired_date: dict[int, _dt.date] = {}

    base = time.monotonic()
    for index, entry in enumerate(initial_config["schedule"]):
        if "every_seconds" in entry:
            monotonic_deadlines[index] = base + float(entry["every_seconds"])

    while not _shutdown_event.is_set():
        config = load_config()
        if not config["enabled"]:
            log("Announcer was disabled in config; worker will exit")
            return 0
        if not config["schedule"]:
            log("Schedule was removed from config at runtime; switching to interval mode")
            return run_simple_interval_loop(config)

        # Reconcile per-entry deadlines if the schedule list shrunk/grew.
        now_monotonic = time.monotonic()
        for index, entry in enumerate(config["schedule"]):
            if "every_seconds" in entry and index not in monotonic_deadlines:
                monotonic_deadlines[index] = now_monotonic + float(entry["every_seconds"])
        valid_every_indices = {
            i for i, e in enumerate(config["schedule"]) if "every_seconds" in e
        }
        for stale in list(monotonic_deadlines.keys()):
            if stale not in valid_every_indices:
                monotonic_deadlines.pop(stale, None)
        valid_time_indices = {
            i for i, e in enumerate(config["schedule"]) if "time_of_day" in e
        }
        for stale in list(last_fired_date.keys()):
            if stale not in valid_time_indices:
                last_fired_date.pop(stale, None)

        # Compute sleep horizon = min over all entries of their next-due time.
        tz = config.get("tzinfo")
        today = _dt.datetime.now(tz=tz).date()
        wakeups: list[float] = []
        now_monotonic = time.monotonic()
        for index, entry in enumerate(config["schedule"]):
            if "time_of_day" in entry:
                fired_today = last_fired_date.get(index) == today
                wakeups.append(_seconds_until_slot(entry["time_of_day"], tz, fired_today))
            else:
                deadline = monotonic_deadlines.get(index, now_monotonic)
                wakeups.append(max(0.0, deadline - now_monotonic))

        # Sleep until the soonest entry. Cap at MAX_SLEEP_INTERVAL_SECONDS
        # so config edits and clock changes (DST) are picked up promptly
        # without spinning. A 0.0 wakeup falls through immediately and the
        # firing pass handles the slot.
        sleep_for = min(wakeups) if wakeups else float(MAX_SLEEP_INTERVAL_SECONDS)
        sleep_for = min(float(MAX_SLEEP_INTERVAL_SECONDS), max(0.0, sleep_for))
        if sleep_for > 0 and _interruptible_sleep(sleep_for):
            return 0
        if _shutdown_event.is_set():
            return 0

        # Fire any entries whose deadline has now passed.
        now_monotonic = time.monotonic()
        messages = config["messages"]
        for index, entry in enumerate(config["schedule"]):
            if "time_of_day" in entry:
                fired_today = last_fired_date.get(index) == today
                due, today_for_entry = _slot_due_now(entry["time_of_day"], tz, fired_today)
                if not due:
                    continue
            else:
                deadline = monotonic_deadlines.get(index)
                if deadline is None or now_monotonic < deadline:
                    continue
                monotonic_deadlines[index] = now_monotonic + float(entry["every_seconds"])

            literal = entry.get("message")
            message = literal if literal else _next_message(messages, cursor)
            if not message:
                if "time_of_day" in entry:
                    log("Schedule slot due but no literal message and messages list is empty")
                continue
            ok = dispatch_announcement(config, message)

            if "time_of_day" in entry and ok:
                # Mark today's occurrence as fired so we don't repeat until
                # the next calendar day in this entry's timezone.
                last_fired_date[index] = today_for_entry

    return 0


# ---------------------------------------------------------------------------
# Worker entry point
# ---------------------------------------------------------------------------


def _install_signal_handlers() -> None:
    def _stop(signum: int, _frame: Any) -> None:
        log(f"Received signal {signum}; stopping announcer loop")
        _shutdown_event.set()

    signal.signal(signal.SIGTERM, _stop)
    signal.signal(signal.SIGINT, _stop)


def run_worker() -> int:
    lock_handle = acquire_worker_lock()
    if lock_handle is None:
        log("Another announcer worker already holds the lock; exiting")
        return 0

    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    PID_PATH.write_text(f"{os.getpid()}\n", encoding="utf-8")
    atexit.register(remove_pid_if_owned)

    _install_signal_handlers()

    config = load_config()
    messages = config["messages"]

    _emit_startup_diagnostics(config, messages)

    if not config["enabled"]:
        log("Announcer is disabled in config; worker will exit")
        return 0

    startup_delay = int(config["startup_delay_seconds"])
    if startup_delay > 0:
        log(f"Initial startup delay: {startup_delay}s")
        if _interruptible_sleep(startup_delay):
            return 0

    try:
        if config["schedule"]:
            return run_scheduled_loop(config)
        return run_simple_interval_loop(config)
    finally:
        # Lock is released automatically on process exit, but be explicit for
        # clarity in logs and tests.
        try:
            fcntl.flock(lock_handle.fileno(), fcntl.LOCK_UN)
        except OSError:
            pass
        try:
            lock_handle.close()
        except OSError:
            pass


def launch_background_worker() -> int:
    config = load_config()
    if not config["enabled"]:
        log("Announcer is disabled in config; skipping background launch")
        return 0

    messages = config["messages"]
    if not messages and not any(entry.get("message") for entry in config["schedule"]):
        log("No announcer messages found and no literal schedule entries; skipping background launch")
        return 0

    if previous_worker_alive():
        log("Announcer is already running (lockfile held); skipping background launch")
        return 0

    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    log_path = Path(config["log_path"])
    log_path.parent.mkdir(parents=True, exist_ok=True)

    try:
        log_handle = open(log_path, "a", encoding="utf-8")  # noqa: SIM115
    except OSError as exc:
        log(f"Failed to open announcer log file {log_path}: {exc}")
        return 1

    try:
        process = subprocess.Popen(
            [sys.executable, str(SCRIPT_PATH), WORKER_FLAG],
            cwd=str(HOME_DIR),
            stdin=subprocess.DEVNULL,
            stdout=log_handle,
            stderr=subprocess.STDOUT,
            close_fds=True,
            start_new_session=True,
        )
    except OSError as exc:
        log_handle.close()
        log(f"Failed to launch background announcer worker: {exc}")
        return 1
    finally:
        # Parent doesn't need the FD; child inherited it via fork+exec.
        try:
            log_handle.close()
        except OSError:
            pass

    try:
        PID_PATH.write_text(f"{process.pid}\n", encoding="utf-8")
    except OSError as exc:
        log(f"Failed to record announcer PID file {PID_PATH}: {exc}")

    log(f"Bundled announcer started in the background with PID {process.pid}")
    log(f"Bundled announcer log file: {log_path}")
    return 0


def main() -> int:
    if WORKER_FLAG in sys.argv[1:]:
        return run_worker()
    return launch_background_worker()


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:  # pragma: no cover - signal handled above
        sys.exit(0)
    except OSError as exc:
        if exc.errno == errno.EPIPE:  # pragma: no cover - log pipe closed
            sys.exit(0)
        raise
