#!/usr/bin/env python3
"""
Bundled example addon template: live-event team change announcer.

This example demonstrates the **preferred** model for event-driven addons:
consume the runtime-managed live server output mirror written by the
anti-VPN supervisor instead of tailing ``server.log`` yourself.

When the addon loader executes this script during startup it launches a
detached background worker and exits quickly so the normal server startup
can continue. The background worker reads
``$TAYSTJK_LIVE_OUTPUT_PATH`` (typically
``/home/container/.runtime/live/server-output.log``) line by line and
reacts to ``ChangeTeam`` events shaped like:

    ChangeTeam: 0 [203.0.113.1] (GUID) "Padawan" SPECTATOR -> RED

It then emits announcements over local RCON shaped like:

    Padawan joined RED TEAM
    Padawan joined BLUE TEAM
    Padawan changed SPECTATORS

The announcement command is configurable (``svsay`` or ``say``) via
``20-live-team-announcer.config.json`` next to this file.

To activate, copy these files into ``/home/container/addons``:

    /home/container/addons/20-live-team-announcer.py
    /home/container/addons/20-live-team-announcer.config.json
"""

from __future__ import annotations

import atexit
import fcntl
import json
import os
import re
import shlex
import signal
import socket
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

HOME_DIR = Path("/home/container")
SCRIPT_PATH = Path(__file__).resolve()
CONFIG_PATH = SCRIPT_PATH.with_suffix(".config.json")
LOGS_DIR = HOME_DIR / "logs"
DEFAULT_LOG_PATH = LOGS_DIR / "bundled-live-team-announcer.log"
PID_PATH = LOGS_DIR / "bundled-live-team-announcer.pid"
LOCK_PATH = LOGS_DIR / "bundled-live-team-announcer.lock"
RUNTIME_ENV_PATH = HOME_DIR / ".runtime" / "taystjk-effective.env"
RUNTIME_STATE_PATH = HOME_DIR / ".runtime" / "taystjk-effective.json"

# Verified line shape, including timestamp/ANSI variants seen in the wild:
#   ChangeTeam: 0 [203.0.113.1] (GUID) "Padawan" SPECTATOR -> RED
# We are intentionally permissive about leading timestamps and color
# escape sequences because the live mirror preserves whatever the engine
# printed to stdout/stderr.
ANSI_RE = re.compile(r"\x1b\[[0-9;]*m")
CHANGE_TEAM_RE = re.compile(
    r"ChangeTeam:\s*\d+\s*\[[^\]]*\]\s*\([^)]*\)\s*"
    r'"(?P<player>[^"]+)"\s+'
    r"(?P<old_team>[A-Za-z]+)\s*->\s*(?P<new_team>[A-Za-z]+)"
)
QUAKE_COLOR_RE = re.compile(r"\^(?:[0-9A-Za-z])")

DEFAULT_CONFIG: dict[str, Any] = {
    "enabled": True,
    "rcon_host": "127.0.0.1",
    "rcon_timeout_seconds": 3,
    "announce_command": "svsay",
    "log_file": str(DEFAULT_LOG_PATH),
    # If the live mirror file is not available (supervisor disabled) we
    # silently fall back to tailing the active server.log.
    "fallback_to_server_log": True,
    # Coalesce repeated ChangeTeam events for the same player+team within
    # this many seconds so quick model/skin re-applies do not spam.
    "min_seconds_between_announcements": 3,
}


def log(message: str) -> None:
    print(f"[addon:live-team-announcer] {message}", flush=True)


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


def load_config() -> dict[str, Any]:
    config = dict(DEFAULT_CONFIG)
    if CONFIG_PATH.is_file():
        try:
            loaded = json.loads(CONFIG_PATH.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError) as exc:
            log(f"Failed to read config {CONFIG_PATH}: {exc}. Using defaults.")
        else:
            if isinstance(loaded, dict):
                config.update(loaded)
            else:
                log(f"Config file {CONFIG_PATH} does not contain a JSON object. Using defaults.")
    config["enabled"] = safe_bool(config.get("enabled", True), True)
    config["rcon_timeout_seconds"] = safe_int(config.get("rcon_timeout_seconds"), 3, 1)
    config["min_seconds_between_announcements"] = safe_int(
        config.get("min_seconds_between_announcements"), 3, 0
    )
    command = str(config.get("announce_command", "svsay")).strip().lower()
    if command not in {"say", "svsay"}:
        log(f"Unsupported announce_command {command!r}; falling back to svsay")
        command = "svsay"
    config["announce_command"] = command
    config["rcon_host"] = str(config.get("rcon_host", "127.0.0.1")).strip() or "127.0.0.1"
    config["fallback_to_server_log"] = safe_bool(config.get("fallback_to_server_log", True), True)
    return config


def load_runtime_env() -> dict[str, str]:
    """Read TAYSTJK_* values from the current process and the runtime env file."""
    state: dict[str, str] = {}
    tracked = {
        "TAYSTJK_ACTIVE_SERVER_CONFIG_PATH",
        "TAYSTJK_ACTIVE_SERVER_LOG_PATH",
        "TAYSTJK_EFFECTIVE_SERVER_PORT",
        "TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD",
        "TAYSTJK_LIVE_OUTPUT_ENABLED",
        "TAYSTJK_LIVE_OUTPUT_PATH",
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
            log(f"Failed to read runtime env {RUNTIME_ENV_PATH}: {exc}")
    return state


def preferred_tail_source(config: dict[str, Any]) -> tuple[Path, str] | None:
    """Pick the live-output mirror when available, with optional fallback."""
    runtime = load_runtime_env()
    enabled = runtime.get("TAYSTJK_LIVE_OUTPUT_ENABLED", "").strip().lower() == "true"
    live_path = runtime.get("TAYSTJK_LIVE_OUTPUT_PATH", "").strip()
    if enabled and live_path:
        return Path(live_path), "live-output"
    if not config["fallback_to_server_log"]:
        return None
    server_log = runtime.get("TAYSTJK_ACTIVE_SERVER_LOG_PATH", "").strip()
    if not server_log:
        return None
    return Path(server_log), "server-log"


def active_server_config_path() -> Path:
    runtime = load_runtime_env()
    candidate = runtime.get("TAYSTJK_ACTIVE_SERVER_CONFIG_PATH", "").strip()
    if candidate:
        return Path(candidate)
    mod_dir = os.getenv("FS_GAME_MOD", "taystjk").strip() or "taystjk"
    server_config = os.getenv("SERVER_CONFIG", "server.cfg").strip() or "server.cfg"
    return HOME_DIR / mod_dir / server_config


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
        log(f"Failed to read active server config {config_path}: {exc}")
    return None


def effective_rcon_password() -> str | None:
    runtime = load_runtime_env()
    direct = runtime.get("TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD", "").strip()
    if direct:
        return direct
    return extract_rcon_password_from_config(active_server_config_path())


def current_server_port() -> int:
    runtime = load_runtime_env()
    if runtime.get("TAYSTJK_EFFECTIVE_SERVER_PORT"):
        return safe_int(runtime["TAYSTJK_EFFECTIVE_SERVER_PORT"], 29070, 1)
    return safe_int(os.getenv("SERVER_PORT", "29070"), 29070, 1)


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
    decoded = response.decode("utf-8", errors="replace").lstrip("\xff")
    if decoded.startswith("print\n"):
        decoded = decoded[6:]
    return decoded.strip()


def escape_command_argument(value: str) -> str:
    return value.replace("\\", "\\\\").replace("\"", "\\\"")


def normalize_player_name(value: str) -> str:
    return QUAKE_COLOR_RE.sub("", value).strip()


def announcement_for(player: str, new_team: str) -> str | None:
    """Map a parsed ChangeTeam event into a public-friendly announcement."""
    team = new_team.strip().upper()
    name = normalize_player_name(player)
    if not name:
        return None
    if team == "RED":
        return f"{name} joined RED TEAM"
    if team == "BLUE":
        return f"{name} joined BLUE TEAM"
    if team in {"SPECTATOR", "SPECTATORS", "FREE"}:
        return f"{name} changed SPECTATORS"
    # Unknown team value: still emit something useful.
    return f"{name} joined {team}"


def parse_change_team(line: str) -> tuple[str, str, str] | None:
    cleaned = ANSI_RE.sub("", line)
    match = CHANGE_TEAM_RE.search(cleaned)
    if not match:
        return None
    return match.group("player"), match.group("old_team"), match.group("new_team")


# ---------------------------------------------------------------------------
# PID + lockfile handling
# ---------------------------------------------------------------------------
#
# Earlier revisions relied on `os.kill(pid, 0)` to detect whether the previous
# worker was still alive. That approach is racy: after a server restart the OS
# may have recycled the previous PID for an unrelated process, in which case
# the PID check returns "alive" forever and the announcer never starts.
#
# We instead hold an exclusive `fcntl.flock` on a dedicated lockfile for the
# lifetime of the worker process. The lock is released automatically when the
# worker exits (graceful or crash), so a fresh launch can always tell whether
# a real worker is currently running.


def acquire_worker_lock() -> Any | None:
    """Take an exclusive lock so only one worker runs at a time.

    Returns the open file handle on success (kept alive for the worker's
    lifetime) or None if another worker is already running.
    """
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


def previous_worker_alive() -> bool:
    """Best-effort check using the lockfile.

    Tries to acquire the lock briefly; if it fails another process owns it
    (i.e. a previous worker is still running). This is race-free across PID
    reuse because the lock is bound to the open file description, not the
    PID.
    """
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
        # We acquired it momentarily; release immediately so the actual
        # worker can take it.
        fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
        return False
    finally:
        handle.close()


# ---------------------------------------------------------------------------
# Worker
# ---------------------------------------------------------------------------


def worker_log(handle, message: str) -> None:
    handle.write(f"[{time.strftime('%Y-%m-%d %H:%M:%S')}] {message}\n")
    handle.flush()


def run_worker() -> int:
    config = load_config()
    LOGS_DIR.mkdir(parents=True, exist_ok=True)

    lock_handle = acquire_worker_lock()
    if lock_handle is None:
        log("Another worker already holds the lockfile; exiting")
        return 0

    PID_PATH.write_text(f"{os.getpid()}\n", encoding="utf-8")
    atexit.register(remove_pid_if_owned)

    def stop_worker(signum: int, _frame: Any) -> None:
        raise SystemExit(0)

    signal.signal(signal.SIGTERM, stop_worker)
    signal.signal(signal.SIGINT, stop_worker)

    log_path = Path(str(config.get("log_file", DEFAULT_LOG_PATH)))
    log_path.parent.mkdir(parents=True, exist_ok=True)
    handle = log_path.open("a", encoding="utf-8")
    try:
        if not config["enabled"]:
            worker_log(handle, "announcer is disabled in config; worker will exit")
            return 0

        # One-time startup diagnostic so operators can tell at a glance how
        # the worker resolved its inputs (helps when an earlier release
        # silently failed to announce because RCON was unset, the live
        # mirror was disabled, etc).
        runtime = load_runtime_env()
        password = effective_rcon_password()
        worker_log(
            handle,
            "startup diagnostic: "
            f"announce_command={config['announce_command']} "
            f"rcon_host={config['rcon_host']} "
            f"rcon_port={current_server_port()} "
            f"rcon_password_resolved={'yes' if password else 'no'} "
            f"live_output_enabled={runtime.get('TAYSTJK_LIVE_OUTPUT_ENABLED', '<unset>')} "
            f"live_output_path={runtime.get('TAYSTJK_LIVE_OUTPUT_PATH', '<unset>')} "
            f"server_log_path={runtime.get('TAYSTJK_ACTIVE_SERVER_LOG_PATH', '<unset>')} "
            f"fallback_to_server_log={config['fallback_to_server_log']} "
            f"min_seconds_between_announcements={config['min_seconds_between_announcements']}",
        )

        last_announced: dict[tuple[str, str], float] = {}
        backoff = 1
        while True:
            tail_target = preferred_tail_source(config)
            if tail_target is None:
                worker_log(handle, "no tail source available yet (live mirror disabled and no fallback); retrying in 5s")
                time.sleep(5)
                continue

            tail_path, source_label = tail_target
            if not tail_path.exists():
                worker_log(handle, f"tail source {tail_path} ({source_label}) not present yet, waiting")
                time.sleep(2)
                continue

            worker_log(handle, f"tailing {tail_path} (source={source_label})")
            try:
                proc = subprocess.Popen(
                    ["tail", "-n", "0", "-F", "--", str(tail_path)],
                    stdout=subprocess.PIPE,
                    stderr=subprocess.DEVNULL,
                    text=True,
                    encoding="utf-8",
                    errors="replace",
                    bufsize=1,
                    close_fds=True,
                )
            except OSError as exc:
                worker_log(handle, f"failed to start tail: {exc}; retrying in {backoff}s")
                time.sleep(backoff)
                backoff = min(backoff * 2, 30)
                continue

            backoff = 1
            try:
                assert proc.stdout is not None
                for raw_line in proc.stdout:
                    parsed = parse_change_team(raw_line)
                    if parsed is None:
                        continue
                    player, _old_team, new_team = parsed
                    message = announcement_for(player, new_team)
                    if message is None:
                        continue

                    key = (normalize_player_name(player), new_team.strip().upper())
                    now = time.monotonic()
                    last = last_announced.get(key, 0.0)
                    if now - last < int(config["min_seconds_between_announcements"]):
                        continue
                    last_announced[key] = now

                    password = effective_rcon_password()
                    if not password:
                        worker_log(handle, f"no effective RCON password resolved; skipping announcement for {message!r}")
                        continue
                    command = f'{config["announce_command"]} "{escape_command_argument(message)}"'
                    try:
                        send_rcon_command(
                            config["rcon_host"],
                            current_server_port(),
                            password,
                            int(config["rcon_timeout_seconds"]),
                            command,
                        )
                    except OSError as exc:
                        worker_log(handle, f"announcement failed: {exc}")
                    else:
                        worker_log(handle, f"announced: {message}")
            finally:
                if proc.poll() is None:
                    try:
                        proc.terminate()
                        proc.wait(timeout=2)
                    except (OSError, subprocess.TimeoutExpired):
                        try:
                            proc.kill()
                        except OSError:
                            pass

            worker_log(handle, "tail exited; restarting")
            time.sleep(1)
    finally:
        try:
            handle.close()
        except OSError:
            pass
        # Keep lock_handle alive for the worker's lifetime; the OS releases
        # the flock automatically when the process exits, even on crash.
        try:
            lock_handle.close()
        except OSError:
            pass


def launch_background_worker() -> int:
    config = load_config()
    if not config["enabled"]:
        log("Announcer is disabled in config; skipping background launch")
        return 0

    if previous_worker_alive():
        existing_pid = read_existing_pid()
        if existing_pid:
            log(f"Announcer is already running (lockfile held, PID {existing_pid}); skipping launch")
        else:
            log("Announcer is already running (lockfile held); skipping launch")
        return 0

    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    log_path = Path(str(config.get("log_file", DEFAULT_LOG_PATH)))
    log_path.parent.mkdir(parents=True, exist_ok=True)

    with log_path.open("a", encoding="utf-8") as log_handle:
        process = subprocess.Popen(
            [sys.executable, str(SCRIPT_PATH), "--run-worker"],
            cwd=str(HOME_DIR),
            stdin=subprocess.DEVNULL,
            stdout=log_handle,
            stderr=subprocess.STDOUT,
            close_fds=True,
            start_new_session=True,
        )
    PID_PATH.write_text(f"{process.pid}\n", encoding="utf-8")
    log(f"Bundled live team announcer started in the background with PID {process.pid}")
    log(f"Bundled live team announcer log file: {log_path}")
    return 0


def main() -> int:
    if "--run-worker" in sys.argv[1:]:
        return run_worker()
    if "--selftest" in sys.argv[1:]:
        return run_selftest()
    return launch_background_worker()


SELFTEST_LINES: tuple[tuple[str, str | None], ...] = (
    (
        'ChangeTeam: 0 [203.0.113.1] (GUID) "Padawan" SPECTATOR -> RED',
        "Padawan joined RED TEAM",
    ),
    (
        'ChangeTeam: 1 [203.0.113.2:29070] (GUID) "Akion^7" RED -> BLUE',
        "Akion joined BLUE TEAM",
    ),
    (
        'ChangeTeam: 2 [203.0.113.3] (GUID) "Robin" BLUE -> SPECTATOR',
        "Robin changed SPECTATORS",
    ),
    (
        'ClientConnect: 0 [203.0.113.1] (GUID) "Padawan"',
        None,
    ),
    (
        'say: Akion: hello',
        None,
    ),
)


def run_selftest() -> int:
    failures = 0
    for raw, expected in SELFTEST_LINES:
        parsed = parse_change_team(raw)
        if parsed is None:
            actual = None
        else:
            player, _old_team, new_team = parsed
            actual = announcement_for(player, new_team)
        print(f"  in : {raw!r}")
        print(f"  out: {actual!r}")
        if actual != expected:
            print(f"  FAIL expected {expected!r}")
            failures += 1
    if failures:
        print(f"[addon:live-team-announcer] self-test failed with {failures} error(s)")
        return 1
    print("[addon:live-team-announcer] self-test ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
