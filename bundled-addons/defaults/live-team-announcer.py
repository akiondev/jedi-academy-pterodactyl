#!/usr/bin/env python3
"""
Event-driven default helper: live team-change announcer.

This is the event-bus replacement for the legacy
``20-live-team-announcer.py`` daemon that used to tail the runtime
live-output mirror or the engine's ``server.log``. The supervisor now
reads the dedicated server's stdout/stderr exactly once and publishes
parsed events through a central dispatcher; this addon receives those
events as newline-delimited JSON on stdin.

Configuration is supplied by the runtime layer via the
``JKA_ADDON_CONFIG_JSON`` environment variable (the
``addons.live_team_announcer`` section of
``/home/container/config/jka-addons.json``); the addon no longer reads
its own ``*.config.json`` file.

Protocol:
    One JSON object per line on stdin. The supervisor emits a
    ``team_change`` event for every parsed engine line shaped like:

        2026-04-25 15:12:32 ChangeTeam: 0 [90.144.88.223] (GUID) "akiondev" BLUE -> RED

    The event carries ``slot``, ``ip``, ``name``, ``old_team`` and
    ``new_team`` fields. Other event types are ignored.

Behaviour:
    For every team_change event the addon sends a short RCON say/svsay
    announcement to the dedicated server using the runtime-managed
    state files for port + rcon password. Repeated transitions for
    the same player to the same team within
    ``min_seconds_between_announcements`` are coalesced.

Constraints (enforced by design):
    * never tails ``server.log``
    * never tails the live-output mirror
    * never daemonises; lifetime is owned by the supervisor's addon runner
    * exits cleanly when stdin closes (EOF from supervisor)
    * writes its own log file under /home/container/logs
"""

from __future__ import annotations

import datetime as _dt
import json
import os
import re
import shlex
import signal
import socket
import sys
import time
from pathlib import Path
from typing import Any

ADDON_LABEL = "[helper:live-team-announcer]"
HOME_DIR = Path(os.environ.get("JKA_LIVE_TEAM_ANNOUNCER_HOME", "/home/container"))
SCRIPT_PATH = Path(__file__).resolve()
# Runtime configuration is passed in via the ``JKA_ADDON_CONFIG_JSON``
# environment variable (the ``addons.live_team_announcer`` section of
# ``/home/container/config/jka-addons.json``). When absent the addon
# falls back to reading the centralised file directly so direct
# invocations (`python3 live-team-announcer.py`) still work.
ADDON_CONFIG_ENV = "JKA_ADDON_CONFIG_JSON"
ADDON_NAME_ENV = "JKA_ADDON_NAME"
ADDONS_CONFIG_PATH = Path(
    os.environ.get(
        "JKA_ADDONS_CONFIG_PATH",
        str(HOME_DIR / "config" / "jka-addons.json"),
    )
)
DEFAULT_ADDON_NAME = "live_team_announcer"
LOGS_DIR = HOME_DIR / "logs"
DEFAULT_LOG_PATH = LOGS_DIR / "bundled-live-team-announcer.log"
RUNTIME_ENV_PATH = HOME_DIR / ".runtime" / "taystjk-effective.env"

QUAKE_COLOR_RE = re.compile(r"\^(?:[0-9A-Za-z])")

DEFAULT_CONFIG: dict[str, Any] = {
    "enabled": False,
    "rcon_host": "127.0.0.1",
    "rcon_timeout_seconds": 3,
    "announce_command": "svsay",
    "log_file": str(DEFAULT_LOG_PATH),
    "min_seconds_between_announcements": 3,
}

ALLOWED_ANNOUNCE_COMMANDS = ("svsay", "say")


def _now_iso() -> str:
    return _dt.datetime.now().astimezone().isoformat(timespec="seconds")


_log_handle = None


def log(message: str) -> None:
    line = f"{_now_iso()} {ADDON_LABEL} {message}\n"
    if _log_handle is not None:
        try:
            _log_handle.write(line)
            _log_handle.flush()
        except OSError:
            pass
    sys.stderr.write(line)
    sys.stderr.flush()


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
        v = value.strip().lower()
        if v in {"true", "1", "yes", "on"}:
            return True
        if v in {"false", "0", "no", "off"}:
            return False
    return default


def _load_section_from_addons_file(name: str) -> dict[str, Any] | None:
    """Best-effort fallback when ``JKA_ADDON_CONFIG_JSON`` is missing."""
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
            log(f"failed to parse {ADDON_CONFIG_ENV}: {exc}; using defaults")
        else:
            if isinstance(parsed, dict):
                loaded = parsed
            else:
                log(f"{ADDON_CONFIG_ENV} is not a JSON object; using defaults")
    if loaded is None:
        addon_name = os.environ.get(ADDON_NAME_ENV, DEFAULT_ADDON_NAME)
        loaded = _load_section_from_addons_file(addon_name)
    if isinstance(loaded, dict):
        config.update(loaded)
    config["enabled"] = safe_bool(config.get("enabled", False), False)
    config["rcon_timeout_seconds"] = safe_int(config.get("rcon_timeout_seconds"), 3, 1)
    config["min_seconds_between_announcements"] = safe_int(
        config.get("min_seconds_between_announcements"), 3, 0
    )
    raw_command = str(config.get("announce_command", "svsay")).strip().lower()
    if raw_command not in ALLOWED_ANNOUNCE_COMMANDS:
        log(f"announce_command={raw_command!r} not allowed; using svsay")
        raw_command = "svsay"
    config["announce_command"] = raw_command
    config["rcon_host"] = str(config.get("rcon_host", "127.0.0.1")).strip() or "127.0.0.1"
    return config


def load_runtime_env() -> dict[str, str]:
    state: dict[str, str] = {}
    tracked = {
        "TAYSTJK_ACTIVE_SERVER_CONFIG_PATH",
        "TAYSTJK_EFFECTIVE_SERVER_PORT",
        "TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD",
    }
    for key in tracked:
        value = os.getenv(key)
        if value:
            state[key] = value.strip()
    if RUNTIME_ENV_PATH.is_file():
        try:
            for raw_line in RUNTIME_ENV_PATH.read_text(encoding="utf-8").splitlines():
                line = raw_line.strip()
                if not line or "=" not in line:
                    continue
                key, raw_value = line.split("=", 1)
                key = key.strip()
                if key not in tracked:
                    continue
                tokens = shlex.split(raw_value, posix=True)
                if not tokens:
                    continue
                state.setdefault(key, tokens[0])
        except OSError as exc:
            log(f"failed to read runtime env {RUNTIME_ENV_PATH}: {exc}")
    return state


def extract_rcon_password_from_config(config_path: Path) -> str | None:
    if not config_path.is_file():
        return None
    pattern = re.compile(
        r"^\s*set[a-z]*\s+rconpassword\s+(?:\"([^\"]+)\"|(\S+))",
        re.IGNORECASE,
    )
    try:
        for line in config_path.read_text(encoding="utf-8", errors="replace").splitlines():
            match = pattern.search(line)
            if match:
                return (match.group(1) or match.group(2) or "").strip() or None
    except OSError as exc:
        log(f"failed to read active server config {config_path}: {exc}")
    return None


def effective_rcon_password() -> str | None:
    runtime = load_runtime_env()
    direct = runtime.get("TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD", "").strip()
    if direct:
        return direct
    config_path = runtime.get("TAYSTJK_ACTIVE_SERVER_CONFIG_PATH", "").strip()
    if config_path:
        return extract_rcon_password_from_config(Path(config_path))
    return None


def current_server_port() -> int:
    runtime = load_runtime_env()
    if runtime.get("TAYSTJK_EFFECTIVE_SERVER_PORT"):
        return safe_int(runtime["TAYSTJK_EFFECTIVE_SERVER_PORT"], 29070, 1)
    return safe_int(os.getenv("SERVER_PORT", "29070"), 29070, 1)


def send_rcon_command(host: str, port: int, password: str, timeout_seconds: int, command: str) -> None:
    payload = b"\xff\xff\xff\xffrcon " + password.encode("utf-8") + b" " + command.encode("utf-8")
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(timeout_seconds)
    try:
        sock.sendto(payload, (host, port))
        try:
            sock.recvfrom(65535)
        except socket.timeout:
            pass
    finally:
        sock.close()


def announcement_for(name: str, new_team: str) -> str | None:
    cleaned = QUAKE_COLOR_RE.sub("", name).strip()
    if not cleaned:
        return None
    team = (new_team or "").strip().upper()
    if team == "RED":
        return f"{cleaned} joined RED TEAM"
    if team == "BLUE":
        return f"{cleaned} joined BLUE TEAM"
    if team in {"SPECTATOR", "SPECTATORS", "FREE"}:
        return f"{cleaned} changed SPECTATORS"
    return f"{cleaned} joined {team}"


def main(argv: list[str] | None = None) -> int:
    del argv  # unused
    global _log_handle  # noqa: PLW0603 - intentional module-level handle

    config = load_config()
    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    try:
        _log_handle = open(config["log_file"], "a", encoding="utf-8")  # noqa: SIM115
    except OSError as exc:
        sys.stderr.write(f"{ADDON_LABEL} failed to open log file {config['log_file']}: {exc}\n")
        _log_handle = None

    if not config["enabled"]:
        log("disabled in jka-addons.json; exiting")
        return 0

    log("starting event-driven live team announcer; reading NDJSON events from stdin")

    last_announced: dict[str, tuple[str, float]] = {}
    cooldown = config["min_seconds_between_announcements"]

    def _shutdown(signum, frame) -> None:  # pylint: disable=unused-argument
        del signum, frame
        log("shutting down on signal")
        sys.exit(0)

    signal.signal(signal.SIGINT, _shutdown)
    signal.signal(signal.SIGTERM, _shutdown)

    for raw in sys.stdin:
        line = raw.strip()
        if not line:
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError as exc:
            log(f"warning: skipped malformed event: {exc}")
            continue
        if not isinstance(event, dict):
            continue
        if event.get("type") != "team_change":
            continue
        name = str(event.get("name") or "").strip()
        new_team = str(event.get("new_team") or "").strip().upper()
        slot = str(event.get("slot") or "")
        if not name or not new_team:
            continue

        message = announcement_for(name, new_team)
        if not message:
            continue

        key = f"{slot}|{name.lower()}"
        now = time.monotonic()
        previous = last_announced.get(key)
        if previous is not None and previous[0] == new_team and (now - previous[1]) < cooldown:
            continue
        last_announced[key] = (new_team, now)

        password = effective_rcon_password()
        if not password:
            log("rcon password unavailable; skipping announcement")
            continue
        port = current_server_port()
        rcon_command = f"{config['announce_command']} {message}"
        try:
            send_rcon_command(
                config["rcon_host"],
                port,
                password,
                config["rcon_timeout_seconds"],
                rcon_command,
            )
            log(f"announced: {message}")
        except OSError as exc:
            log(f"rcon send failed: {exc}")

    log("stdin closed; exiting")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
