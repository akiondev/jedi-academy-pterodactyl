#!/usr/bin/env python3
"""
Managed default helper: RCON live guard.

This helper is refreshed from the image into:
  /home/container/addons/defaults/50-rcon-live-guard.py

It monitors the live server output stream for RCON access attempts —
both authorized (correct password) and unauthorized (bad password) —
originating from non-local IP addresses.

When an attempt is detected the guard:
  1. Resolves the player's slot number via a local RCON status query.
  2. Issues the configured kick command (default: clientkick {slot}).
  3. Broadcasts a warning message to all connected players.
  4. Writes a timestamped incident entry to the guard log file.

The guard is active by default. To disable it set
``ADDON_RCON_LIVE_GUARD_ENABLED=false`` in the Pterodactyl startup
environment, or create a JSON config file at
``/home/container/addons/50-rcon-live-guard.config.json``
with ``{"enabled": false}``.

Configuration file (optional, user-editable):
  /home/container/addons/50-rcon-live-guard.config.json

All JSON keys are optional; missing keys fall back to built-in defaults.
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

ADDON_LABEL = "[helper:rcon-live-guard]"
HOME_DIR = Path("/home/container")
LOGS_DIR = HOME_DIR / "logs"
SCRIPT_PATH = Path(__file__).resolve()

# User-editable config lives outside the managed-defaults directory so it
# survives the rsync --delete that refreshes managed addon defaults on each
# server restart.
USER_CONFIG_PATH = HOME_DIR / "addons" / "50-rcon-live-guard.config.json"

DEFAULT_LOG_PATH = LOGS_DIR / "live-rcon-guard.log"
PID_PATH = LOGS_DIR / "live-rcon-guard.pid"
LOCK_PATH = LOGS_DIR / "live-rcon-guard.lock"
RUNTIME_ENV_PATH = HOME_DIR / ".runtime" / "taystjk-effective.env"

ANSI_RE = re.compile(r"\x1b\[[0-9;]*m")
QUAKE_COLOR_RE = re.compile(r"\^(?:[0-9A-Za-z])")

# Default RCON detection patterns.
# Each pattern must contain a named group ``ip`` that captures the source
# address (with or without port suffix).  An optional ``command`` group
# captures the attempted command text for log context.
DEFAULT_RCON_PATTERNS: list[str] = [
    r"Bad rcon from (?P<ip>[^\s:]+(?::\d+)?):\s*(?P<command>.*)",
    r"bad rconpassword from (?P<ip>[^\s:]+(?::\d+)?)",
    r"Rcon from (?P<ip>[^\s:]+(?::\d+)?):\s*(?P<command>.*)",
    r"rcon from (?P<ip>[^\s:]+(?::\d+)?):\s*(?P<command>.*)",
    r"remote command from (?P<ip>[^\s:]+(?::\d+)?)",
]

DEFAULT_IGNORE_IP_HOSTS: list[str] = ["127.0.0.1", "::1", "localhost"]

DEFAULT_CONFIG: dict[str, Any] = {
    "enabled": True,
    "rcon_host": "127.0.0.1",
    "rcon_timeout_seconds": 3,
    "broadcast_command": "svsay",
    "kick_command_template": "clientkick {slot}",
    "broadcast_template": "^1{player} was kicked for attempting to access server RCON.",
    "log_file": str(DEFAULT_LOG_PATH),
    "fallback_to_server_log": True,
    "ignore_ip_hosts": DEFAULT_IGNORE_IP_HOSTS,
    "rcon_attempt_patterns": DEFAULT_RCON_PATTERNS,
    # Minimum seconds between repeated enforcement for the same source IP.
    # Prevents a flood of kicks/broadcasts if the engine emits multiple
    # lines for a single attempt burst.
    "min_seconds_between_actions": 10,
}

# Regex for parsing player lines from RCON status output.
# JKA/TaystJK status format (fixed-width):
#   num score ping name(15) lastmsg address(21) qport rate
# Player names may contain spaces, so we anchor on the IPv4 address field.
_STATUS_PLAYER_RE = re.compile(
    r"^\s*(?P<slot>\d+)\s+[-\d]+\s+\d+\s+"  # slot, score, ping
    r"(?P<name>.+?)\s+"  # player name (non-greedy)
    r"\d+\s+"  # lastmsg
    r"(?P<address>\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(?::\d+)?)"  # IPv4 address[:port]
    r"\s+\d+\s+\d+"  # qport, rate
)

_STATUS_HEADER_RE = re.compile(r"^\s*num\s+score\s+ping\s+name", re.IGNORECASE)
_STATUS_SEPARATOR_RE = re.compile(r"^[-\s]+$")


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def log(message: str) -> None:
    print(f"{ADDON_LABEL} {message}", flush=True)


def safe_int(value: Any, default: int, minimum: int = 1) -> int:
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


def strip_ansi(value: str) -> str:
    return ANSI_RE.sub("", value)


def strip_quake_colors(value: str) -> str:
    return QUAKE_COLOR_RE.sub("", value)


def normalize_player_name(value: str) -> str:
    return strip_quake_colors(strip_ansi(value)).strip()


def bare_ip(address: str) -> str:
    """Return just the IP portion of an ``ip`` or ``ip:port`` string."""
    # IPv6 addresses may appear as [::1] or ::1; strip brackets and port.
    addr = address.strip()
    if addr.startswith("["):
        # [IPv6]:port → IPv6
        end = addr.find("]")
        if end != -1:
            return addr[1:end]
        return addr[1:]
    if ":" in addr:
        # IPv4:port → IPv4  (IPv6 without brackets left as-is)
        parts = addr.rsplit(":", 1)
        if len(parts) == 2 and parts[1].isdigit():
            return parts[0]
    return addr


# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------


def compile_patterns(raw_patterns: list[Any]) -> list[re.Pattern[str]]:
    """Compile pattern strings; skip entries that are invalid or lack ``ip`` group."""
    compiled: list[re.Pattern[str]] = []
    for raw in raw_patterns:
        if not isinstance(raw, str):
            continue
        try:
            pattern = re.compile(raw, re.IGNORECASE)
        except re.error:
            log(f"Config: invalid regex pattern (skipped): {raw!r}")
            continue
        if "ip" not in pattern.groupindex:
            log(f"Config: pattern missing named group 'ip' (skipped): {raw!r}")
            continue
        compiled.append(pattern)
    return compiled


def load_config() -> dict[str, Any]:
    config: dict[str, Any] = dict(DEFAULT_CONFIG)
    if USER_CONFIG_PATH.is_file():
        try:
            loaded = json.loads(USER_CONFIG_PATH.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError) as exc:
            log(f"Failed to read config {USER_CONFIG_PATH}: {exc}. Using defaults.")
        else:
            if isinstance(loaded, dict):
                config.update(loaded)
            else:
                log(f"Config {USER_CONFIG_PATH} is not a JSON object. Using defaults.")

    config["enabled"] = safe_bool(config.get("enabled", True), True)
    config["rcon_host"] = str(config.get("rcon_host", "127.0.0.1")).strip() or "127.0.0.1"
    config["rcon_timeout_seconds"] = safe_int(config.get("rcon_timeout_seconds"), 3, 1)
    config["fallback_to_server_log"] = safe_bool(config.get("fallback_to_server_log", True), True)
    config["log_file"] = str(config.get("log_file", str(DEFAULT_LOG_PATH))).strip() or str(
        DEFAULT_LOG_PATH
    )
    config["min_seconds_between_actions"] = safe_int(
        config.get("min_seconds_between_actions"), 10, 0
    )

    broadcast_command = str(config.get("broadcast_command", "svsay")).strip().lower()
    if broadcast_command not in {"say", "svsay"}:
        log(f"Config: unsupported broadcast_command {broadcast_command!r}; falling back to svsay")
        broadcast_command = "svsay"
    config["broadcast_command"] = broadcast_command

    kick_template = str(config.get("kick_command_template", "clientkick {slot}")).strip()
    if not kick_template:
        kick_template = "clientkick {slot}"
    config["kick_command_template"] = kick_template

    broadcast_template = str(
        config.get(
            "broadcast_template",
            "^1{player} was kicked for attempting to access server RCON.",
        )
    ).strip()
    config["broadcast_template"] = broadcast_template

    raw_ignore = config.get("ignore_ip_hosts", DEFAULT_IGNORE_IP_HOSTS)
    if not isinstance(raw_ignore, list):
        raw_ignore = DEFAULT_IGNORE_IP_HOSTS
    config["ignore_ip_hosts"] = [str(h).strip().lower() for h in raw_ignore if h]

    raw_patterns = config.get("rcon_attempt_patterns", DEFAULT_RCON_PATTERNS)
    if not isinstance(raw_patterns, list) or not raw_patterns:
        raw_patterns = DEFAULT_RCON_PATTERNS
    config["compiled_patterns"] = compile_patterns(raw_patterns)

    return config


# ---------------------------------------------------------------------------
# Runtime env
# ---------------------------------------------------------------------------


def load_runtime_env() -> dict[str, str]:
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
                try:
                    tokens = shlex.split(raw_value, posix=True)
                except ValueError:
                    tokens = [raw_value.strip().strip("\"'")]
                if tokens:
                    state.setdefault(key, tokens[0])
        except OSError as exc:
            log(f"Failed to read runtime env {RUNTIME_ENV_PATH}: {exc}")
    return state


def preferred_tail_source(config: dict[str, Any]) -> tuple[Path, str] | None:
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


def effective_rcon_password() -> str | None:
    runtime = load_runtime_env()
    direct = runtime.get("TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD", "").strip()
    if direct:
        return direct
    config_path_str = runtime.get("TAYSTJK_ACTIVE_SERVER_CONFIG_PATH", "").strip()
    if not config_path_str:
        mod_dir = os.getenv("FS_GAME_MOD", "taystjk").strip() or "taystjk"
        server_config = os.getenv("SERVER_CONFIG", "server.cfg").strip() or "server.cfg"
        config_path = HOME_DIR / mod_dir / server_config
    else:
        config_path = Path(config_path_str)
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
                value = (match.group(1) or match.group(2) or "").strip()
                return value or None
    except OSError:
        pass
    return None


def current_server_port() -> int:
    runtime = load_runtime_env()
    raw = runtime.get("TAYSTJK_EFFECTIVE_SERVER_PORT", "").strip()
    if raw:
        return safe_int(raw, 29070, 1)
    return safe_int(os.getenv("SERVER_PORT", "29070"), 29070, 1)


# ---------------------------------------------------------------------------
# RCON helpers
# ---------------------------------------------------------------------------


def send_rcon_command(host: str, port: int, password: str, timeout_seconds: int, command: str) -> str:
    """Send an RCON UDP command and return the response text (stripped)."""
    payload = b"\xff\xff\xff\xffrcon " + password.encode("utf-8") + b" " + command.encode("utf-8")
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(timeout_seconds)
    try:
        sock.sendto(payload, (host, port))
        try:
            response, _ = sock.recvfrom(65535)
        except socket.timeout:
            return ""
    except OSError:
        return ""
    finally:
        sock.close()
    decoded = response.decode("utf-8", errors="replace").lstrip("\xff")
    if decoded.startswith("print\n"):
        decoded = decoded[6:]
    return decoded.strip()


def parse_status_players(status_text: str) -> list[tuple[int, str, str]]:
    """Parse ``status`` RCON output, returning (slot, name, address) tuples."""
    players: list[tuple[int, str, str]] = []
    in_players = False
    for line in status_text.splitlines():
        if _STATUS_HEADER_RE.match(line):
            in_players = True
            continue
        if not in_players:
            continue
        if _STATUS_SEPARATOR_RE.match(line):
            continue
        match = _STATUS_PLAYER_RE.match(line)
        if match:
            slot = int(match.group("slot"))
            name = normalize_player_name(match.group("name"))
            address = match.group("address").strip()
            players.append((slot, name, address))
    return players


def find_player_slot_by_ip(
    players: list[tuple[int, str, str]], source_address: str
) -> tuple[int, str] | None:
    """Return (slot, name) for the first player whose IP matches ``source_address``."""
    src_bare = bare_ip(source_address)
    for slot, name, address in players:
        if bare_ip(address) == src_bare:
            return slot, name
    return None


def escape_command_argument(value: str) -> str:
    """Escape a string for inclusion inside a double-quoted RCON command argument."""
    return value.replace("\\", "\\\\").replace('"', '\\"')


def safe_display(value: str, max_length: int = 200) -> str:
    """Return a log-safe, length-capped version of a string."""
    cleaned = strip_ansi(value).replace("\n", " ").replace("\r", "")
    if len(cleaned) > max_length:
        cleaned = cleaned[:max_length] + "…"
    return cleaned


# ---------------------------------------------------------------------------
# Detection and enforcement
# ---------------------------------------------------------------------------


def try_detect_rcon_attempt(
    line: str, compiled_patterns: list[re.Pattern[str]]
) -> tuple[str, str | None] | None:
    """Check ``line`` against all RCON attempt patterns.

    Returns ``(source_address, command_or_None)`` on the first match, or
    ``None`` if the line does not match any pattern.
    """
    cleaned = strip_ansi(line)
    for pattern in compiled_patterns:
        match = pattern.search(cleaned)
        if match:
            ip_str = match.group("ip").strip()
            command: str | None = None
            if "command" in pattern.groupindex:
                try:
                    command = match.group("command").strip() or None
                except IndexError:
                    pass
            return ip_str, command
    return None


def is_ignored_ip(source_address: str, ignore_list: list[str]) -> bool:
    """Return True when the source IP is in the ignore list."""
    src = bare_ip(source_address).lower()
    return src in ignore_list


def enforce_rcon_attempt(
    source_address: str,
    attempted_command: str | None,
    config: dict[str, Any],
    handle: Any,
) -> None:
    """Kick the player and broadcast a warning when an RCON attempt is detected."""
    password = effective_rcon_password()
    if not password:
        worker_log(
            handle,
            f"RCON attempt from {source_address}: no effective RCON password resolved; "
            "logging only (cannot kick or broadcast)",
        )
        return

    port = current_server_port()
    host: str = config["rcon_host"]
    timeout: int = int(config["rcon_timeout_seconds"])

    # Resolve connected player slot for the source IP.
    status_text = send_rcon_command(host, port, password, timeout, "status")
    players = parse_status_players(status_text) if status_text else []
    found = find_player_slot_by_ip(players, source_address)

    slot: int | None = None
    player_display: str = bare_ip(source_address)

    if found is not None:
        slot, player_name = found
        if player_name:
            player_display = player_name

    cmd_log = f"attempted_command={safe_display(attempted_command)!r}" if attempted_command else ""
    worker_log(
        handle,
        f"RCON attempt detected: source={source_address} "
        f"slot={slot if slot is not None else 'not_connected'} "
        f"player={player_display!r} "
        + cmd_log,
    )

    # Kick if we found a connected slot.
    if slot is not None:
        kick_cmd = config["kick_command_template"].format(slot=slot)
        try:
            send_rcon_command(host, port, password, timeout, kick_cmd)
        except OSError as exc:
            worker_log(handle, f"Kick failed for slot {slot}: {exc}")
        else:
            worker_log(handle, f"Kicked slot {slot} ({player_display!r})")

    # Broadcast warning to the server.
    broadcast_msg = config["broadcast_template"].format(
        player=escape_command_argument(player_display),
        slot=slot if slot is not None else "",
        ip=bare_ip(source_address),
    )
    broadcast_command = (
        f'{config["broadcast_command"]} "{escape_command_argument(broadcast_msg)}"'
    )
    try:
        send_rcon_command(host, port, password, timeout, broadcast_command)
    except OSError as exc:
        worker_log(handle, f"Broadcast failed: {exc}")
    else:
        worker_log(handle, f"Broadcast sent: {broadcast_msg!r}")


# ---------------------------------------------------------------------------
# PID / lockfile handling
# ---------------------------------------------------------------------------


def acquire_worker_lock() -> Any | None:
    """Acquire an exclusive lockfile.  Returns the open handle on success or
    ``None`` if another worker already holds the lock."""
    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    # Context manager intentionally avoided: the handle must remain open for
    # the entire lifetime of the worker process so the OS flock is held.
    handle = open(LOCK_PATH, "a+", encoding="utf-8")  # noqa: SIM115
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
    """Return True if a previous worker is still holding the lockfile."""
    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    # Context manager intentionally avoided: we test the lock and release it
    # immediately; closing the handle releases the flock automatically.
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
    existing = read_existing_pid()
    if existing == os.getpid():
        try:
            PID_PATH.unlink(missing_ok=True)
        except OSError:
            pass


# ---------------------------------------------------------------------------
# Worker
# ---------------------------------------------------------------------------


def worker_log(handle: Any, message: str) -> None:
    stamp = time.strftime("%Y-%m-%d %H:%M:%S")
    handle.write(f"[{stamp}] {message}\n")
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

    # Per-source-IP cooldown: tracks the last time enforcement was applied.
    last_action_for_ip: dict[str, float] = {}

    try:
        if not config["enabled"]:
            worker_log(handle, "guard is disabled in config; worker will exit")
            return 0

        runtime = load_runtime_env()
        password = effective_rcon_password()
        worker_log(
            handle,
            "startup diagnostic: "
            f"rcon_host={config['rcon_host']} "
            f"rcon_port={current_server_port()} "
            f"rcon_password_resolved={'yes' if password else 'no'} "
            f"live_output_enabled={runtime.get('TAYSTJK_LIVE_OUTPUT_ENABLED', '<unset>')} "
            f"live_output_path={runtime.get('TAYSTJK_LIVE_OUTPUT_PATH', '<unset>')} "
            f"server_log_path={runtime.get('TAYSTJK_ACTIVE_SERVER_LOG_PATH', '<unset>')} "
            f"fallback_to_server_log={config['fallback_to_server_log']} "
            f"patterns={len(config['compiled_patterns'])} "
            f"ignore_ip_hosts={config['ignore_ip_hosts']}",
        )

        backoff = 1
        while True:
            tail_target = preferred_tail_source(config)
            if tail_target is None:
                worker_log(
                    handle,
                    "no tail source available (live mirror disabled and no fallback); retrying in 5s",
                )
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
                    detected = try_detect_rcon_attempt(raw_line, config["compiled_patterns"])
                    if detected is None:
                        continue

                    source_address, attempted_command = detected

                    if is_ignored_ip(source_address, config["ignore_ip_hosts"]):
                        continue

                    # Apply per-source-IP cooldown.
                    now = time.monotonic()
                    last = last_action_for_ip.get(bare_ip(source_address), 0.0)
                    min_interval = int(config["min_seconds_between_actions"])
                    if min_interval > 0 and (now - last) < min_interval:
                        continue
                    last_action_for_ip[bare_ip(source_address)] = now

                    try:
                        enforce_rcon_attempt(source_address, attempted_command, config, handle)
                    except Exception as exc:  # noqa: BLE001
                        worker_log(
                            handle,
                            f"enforcement error [{type(exc).__name__}] for {source_address}: {exc}",
                        )

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
        try:
            lock_handle.close()
        except OSError:
            pass

    return 0


# ---------------------------------------------------------------------------
# Launcher (invoked at startup by the managed addon loader)
# ---------------------------------------------------------------------------


def launch_background_worker() -> int:
    config = load_config()
    if not config["enabled"]:
        log("Guard is disabled in config; skipping background launch")
        return 0

    if previous_worker_alive():
        existing_pid = read_existing_pid()
        if existing_pid:
            log(f"Guard is already running (lockfile held, PID {existing_pid}); skipping launch")
        else:
            log("Guard is already running (lockfile held); skipping launch")
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
    log(f"RCON live guard started in the background with PID {process.pid}")
    log(f"RCON live guard log file: {log_path}")
    return 0


# ---------------------------------------------------------------------------
# Self-test
# ---------------------------------------------------------------------------

_SELFTEST_CASES: tuple[tuple[str, str | None], ...] = (
    # Unauthorized access (wrong password)
    ("Bad rcon from 203.0.113.1:29072: status", "203.0.113.1:29072"),
    ("bad rconpassword from 203.0.113.5", "203.0.113.5"),
    # Authorized access (correct password) — still detected
    ("Rcon from 203.0.113.2:50000: map mp/ffa3", "203.0.113.2:50000"),
    ("rcon from 203.0.113.3:12345: kick 0", "203.0.113.3:12345"),
    ("remote command from 203.0.113.4", "203.0.113.4"),
    # Lines that must NOT match
    ("say: Akion: hello", None),
    ("ClientConnect: 0 [203.0.113.1] (GUID) \"Akion\"", None),
    ("", None),
    # Local IPs must be ignored (not detected by pattern, but suppressed by ignore list)
    ("Bad rcon from 127.0.0.1:29072: status", "127.0.0.1:29072"),
)

_STATUS_PARSE_CASES: tuple[tuple[str, list[tuple[int, str, str]]], ...] = (
    (
        "map: mp/ffa3\n"
        "num score ping name            lastmsg address               qport rate\n"
        "--- ----- ---- --------------- ------- --------------------- ----- -----\n"
        "  0     0  999 ^7Akion               0 203.0.113.1:29071        32000 25000\n"
        "  1    10   50 Robin               0 203.0.113.5:45678        32000 25000\n",
        [(0, "Akion", "203.0.113.1:29071"), (1, "Robin", "203.0.113.5:45678")],
    ),
    (
        # No player section
        "map: mp/duel1\n",
        [],
    ),
)


def run_selftest() -> int:
    failures = 0
    compiled = compile_patterns(DEFAULT_RCON_PATTERNS)
    ignore_list = [h.lower() for h in DEFAULT_IGNORE_IP_HOSTS]

    print(f"{ADDON_LABEL} self-test: pattern detection")
    for raw_line, expected_ip in _SELFTEST_CASES:
        detected = try_detect_rcon_attempt(raw_line, compiled)
        actual_ip = detected[0] if detected is not None else None
        ignored = actual_ip is not None and is_ignored_ip(actual_ip, ignore_list)
        print(f"  in  : {raw_line!r}")
        print(f"  out : ip={actual_ip!r} ignored={ignored}")
        if expected_ip is None and actual_ip is not None and not ignored:
            print(f"  FAIL: expected no match, got {actual_ip!r}")
            failures += 1
        elif expected_ip is not None and actual_ip != expected_ip:
            print(f"  FAIL: expected ip={expected_ip!r}, got {actual_ip!r}")
            failures += 1

    print(f"\n{ADDON_LABEL} self-test: status parsing")
    for status_text, expected_players in _STATUS_PARSE_CASES:
        parsed = parse_status_players(status_text)
        print(f"  in  : {status_text.splitlines()[0]!r}...")
        print(f"  out : {parsed}")
        if parsed != expected_players:
            print(f"  FAIL: expected {expected_players!r}")
            failures += 1

    if failures:
        print(f"\n{ADDON_LABEL} self-test FAILED with {failures} error(s)")
        return 1
    print(f"\n{ADDON_LABEL} self-test OK")
    return 0


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def main() -> int:
    if "--run-worker" in sys.argv[1:]:
        return run_worker()
    if "--selftest" in sys.argv[1:]:
        return run_selftest()
    return launch_background_worker()


if __name__ == "__main__":
    raise SystemExit(main())
