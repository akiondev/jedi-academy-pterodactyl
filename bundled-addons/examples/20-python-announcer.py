#!/usr/bin/env python3
"""
Bundled example addon template: periodic Python announcer.

This script is meant to be copied into /home/container/addons and then read
and modified by server owners. When the addon loader executes it during
startup, the script launches a detached background worker and exits quickly so
the normal TaystJK startup can continue.

The background worker reads:
  - 20-python-announcer.config.json
  - 20-python-announcer.messages.txt

It then sends `svsay` announcements over local RCON at a configurable interval.
"""

from __future__ import annotations

import atexit
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
DEFAULT_MESSAGES_PATH = SCRIPT_PATH.with_suffix(".messages.txt")
LOGS_DIR = HOME_DIR / "logs"
DEFAULT_LOG_PATH = LOGS_DIR / "bundled-python-announcer.log"
PID_PATH = LOGS_DIR / "bundled-python-announcer.pid"
RUNTIME_STATE_PATH = HOME_DIR / ".runtime" / "taystjk-effective.json"
RUNTIME_ENV_PATH = HOME_DIR / ".runtime" / "taystjk-effective.env"

DEFAULT_CONFIG: dict[str, Any] = {
    "enabled": True,
    "startup_delay_seconds": 60,
    "interval_seconds": 900,
    "rcon_host": "127.0.0.1",
    "rcon_timeout_seconds": 3,
    "announce_command": "svsay",
    "messages_file": DEFAULT_MESSAGES_PATH.name,
    "log_file": str(DEFAULT_LOG_PATH),
}


def log(message: str) -> None:
    print(f"[addon:python-announcer] {message}", flush=True)


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
    else:
        log(f"Config file not found at {CONFIG_PATH}. Using defaults.")

    config["enabled"] = safe_bool(config.get("enabled", True), True)
    config["startup_delay_seconds"] = safe_int(config.get("startup_delay_seconds"), 60, 0)
    config["interval_seconds"] = safe_int(config.get("interval_seconds"), 900, 30)
    config["rcon_timeout_seconds"] = safe_int(config.get("rcon_timeout_seconds"), 3, 1)
    config["announce_command"] = str(config.get("announce_command", "svsay")).strip() or "svsay"
    config["messages_path"] = resolve_sibling_path(
        str(config.get("messages_file", DEFAULT_MESSAGES_PATH.name)),
        DEFAULT_MESSAGES_PATH,
    )
    config["log_path"] = resolve_sibling_path(
        str(config.get("log_file", DEFAULT_LOG_PATH)),
        DEFAULT_LOG_PATH,
    )
    config["rcon_host"] = str(config.get("rcon_host", "127.0.0.1")).strip() or "127.0.0.1"

    return config


def load_messages(messages_path: Path) -> list[str]:
    if not messages_path.is_file():
        log(f"Messages file not found at {messages_path}")
        return []

    messages: list[str] = []
    for raw_line in messages_path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        messages.append(line)
    return messages


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


def read_existing_pid() -> int | None:
    if not PID_PATH.is_file():
        return None
    try:
        return int(PID_PATH.read_text(encoding="utf-8").strip())
    except (OSError, ValueError):
        return None


def process_is_running(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    return True


def remove_pid_if_owned() -> None:
    existing_pid = read_existing_pid()
    if existing_pid == os.getpid():
        try:
            PID_PATH.unlink(missing_ok=True)
        except OSError:
            pass


def run_worker() -> int:
    config = load_config()
    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    PID_PATH.write_text(f"{os.getpid()}\n", encoding="utf-8")
    atexit.register(remove_pid_if_owned)

    def stop_worker(signum: int, _frame: Any) -> None:
        log(f"Received signal {signum}; stopping announcer loop")
        raise SystemExit(0)

    signal.signal(signal.SIGTERM, stop_worker)
    signal.signal(signal.SIGINT, stop_worker)

    if not config["enabled"]:
        log("Announcer is disabled in config; worker will exit")
        return 0

    startup_delay = int(config["startup_delay_seconds"])
    if startup_delay > 0:
        log(f"Initial startup delay: {startup_delay}s")
        time.sleep(startup_delay)

    message_index = 0

    while True:
        config = load_config()
        if not config["enabled"]:
            log("Announcer was disabled in config; worker will exit")
            return 0

        messages = load_messages(config["messages_path"])
        if not messages:
            log("No announcer messages are configured; retrying later")
        else:
            config_path = active_server_config_path()
            password = effective_rcon_password(config_path)
            if not password:
                log(f"No effective RCON password found for {config_path}; cannot send announcements yet")
            else:
                message = messages[message_index % len(messages)]
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
                else:
                    if response and "bad rconpassword" in response.lower():
                        log(f"Announcement rejected by server: {response}")
                    else:
                        if response:
                            log(f"Sent announcement: {message}")
                        else:
                            log(f"Dispatched announcement without an RCON reply: {message}")
                        message_index += 1

        time.sleep(int(config["interval_seconds"]))


def launch_background_worker() -> int:
    config = load_config()
    if not config["enabled"]:
        log("Announcer is disabled in config; skipping background launch")
        return 0

    messages = load_messages(config["messages_path"])
    if not messages:
        log("No announcer messages found; skipping background launch")
        return 0

    existing_pid = read_existing_pid()
    if existing_pid and process_is_running(existing_pid):
        log(f"Announcer is already running with PID {existing_pid}")
        return 0

    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    log_path = Path(config["log_path"])
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
    log(f"Bundled announcer started in the background with PID {process.pid}")
    log(f"Bundled announcer log file: {log_path}")
    return 0


def main() -> int:
    if "--run-worker" in sys.argv[1:]:
        return run_worker()
    return launch_background_worker()


if __name__ == "__main__":
    raise SystemExit(main())
