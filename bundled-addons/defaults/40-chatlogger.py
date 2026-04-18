#!/usr/bin/env python3
"""
Managed default helper: persistent player chat logging.

This helper is refreshed from the image into:
  /home/container/addons/defaults/40-chatlogger.py

During managed startup it launches a detached background worker that:
  - tails the active server.log
  - extracts common public/team/whisper chat lines
  - writes clean daily chat logs into /home/container/chatlogs
"""

from __future__ import annotations

import gzip
import os
import re
import signal
import subprocess
import sys
import time
from datetime import datetime, timedelta
from pathlib import Path

ADDON_LABEL = "[helper:chatlogger]"
HOME_DIR = Path("/home/container")
LOGS_DIR = HOME_DIR / "logs"
CHATLOGS_DIR = HOME_DIR / "chatlogs"
SCRIPT_PATH = Path(__file__).resolve()
PID_PATH = LOGS_DIR / "chatlogger.pid"
WORKER_LOG_PATH = LOGS_DIR / "chatlogger-helper.log"
RUNTIME_ENV_PATH = HOME_DIR / ".runtime" / "taystjk-effective.env"

KEEP_PLAIN_DAYS = 7
KEEP_TOTAL_DAYS = 60
POLL_SECONDS = 1.0

TIMESTAMP_RE = re.compile(r"^(?P<stamp>\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s+(?P<body>.*)$")
MATCH_TIME_RE = re.compile(r"^(?P<clock>\d{1,3}:\d{2}(?::\d{2})?)\s+(?P<body>.*)$")
QUAKE_COLOR_RE = re.compile(r"\^(?:[0-9A-Za-z])")
CHAT_PATTERNS = (
    (
        "PUBLIC",
        re.compile(r"^(?:say|chat):\s+(?P<sender>.+?):\s+(?P<message>.+)$", re.IGNORECASE),
    ),
    (
        "TEAM",
        re.compile(r"^(?:sayteam|teamsay|team):\s+(?P<sender>.+?):\s+(?P<message>.+)$", re.IGNORECASE),
    ),
    (
        "WHISPER",
        re.compile(
            r"^(?:tell|whisper|privmsg|pm):\s+(?P<sender>.+?)\s+(?:->|to)\s+(?P<target>.+?):\s+(?P<message>.+)$",
            re.IGNORECASE,
        ),
    ),
)


def log(message: str) -> None:
    print(f"{ADDON_LABEL} {message}", flush=True)


def load_runtime_env() -> dict[str, str]:
    state: dict[str, str] = {}

    runtime_mod_dir = os.getenv("TAYSTJK_ACTIVE_MOD_DIR")
    if runtime_mod_dir:
        state["TAYSTJK_ACTIVE_MOD_DIR"] = runtime_mod_dir.strip()

    if not RUNTIME_ENV_PATH.is_file():
        return state

    try:
        for raw_line in RUNTIME_ENV_PATH.read_text(encoding="utf-8").splitlines():
            line = raw_line.strip()
            if not line or "=" not in line:
                continue
            key, raw_value = line.split("=", 1)
            key = key.strip()
            if key != "TAYSTJK_ACTIVE_MOD_DIR":
                continue
            state.setdefault(key, raw_value.strip().strip("\"'"))
    except OSError as exc:
        log(f"Failed to read runtime env {RUNTIME_ENV_PATH}: {exc}")

    return state


def active_mod_dir() -> str:
    runtime_env = load_runtime_env()
    return runtime_env.get("TAYSTJK_ACTIVE_MOD_DIR", "taystjk").strip() or "taystjk"


def active_server_log_path() -> Path:
    return HOME_DIR / active_mod_dir() / "server.log"


def current_plain_log_path(now: datetime) -> Path:
    return CHATLOGS_DIR / f"chat-{now.strftime('%Y-%m-%d')}.log"


def latest_symlink_path() -> Path:
    return CHATLOGS_DIR / "latest.log"


def ensure_directories() -> None:
    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    CHATLOGS_DIR.mkdir(parents=True, exist_ok=True)


def strip_quake_colors(value: str) -> str:
    return QUAKE_COLOR_RE.sub("", value)


def normalize_text(value: str) -> str:
    return strip_quake_colors(value).strip()


def split_log_prefix(raw_line: str) -> tuple[str, str]:
    line = raw_line.rstrip()

    timestamp_match = TIMESTAMP_RE.match(line)
    if timestamp_match:
        return timestamp_match.group("stamp"), timestamp_match.group("body").strip()

    match_time = MATCH_TIME_RE.match(line)
    if match_time:
        stamp = datetime.now().astimezone().strftime("%Y-%m-%d %H:%M:%S")
        return stamp, match_time.group("body").strip()

    stamp = datetime.now().astimezone().strftime("%Y-%m-%d %H:%M:%S")
    return stamp, raw_line.strip()


def parse_chat_line(raw_line: str) -> tuple[str, str, str, str | None, str] | None:
    stamp, body = split_log_prefix(raw_line)

    for channel, pattern in CHAT_PATTERNS:
        match = pattern.match(body)
        if not match:
            continue

        sender = normalize_text(match.group("sender"))
        target = normalize_text(match.groupdict().get("target") or "") or None
        message = normalize_text(match.group("message"))

        if not sender or not message:
            return None

        tz_name = datetime.now().astimezone().tzname() or "LOCAL"
        return (f"{stamp} {tz_name}", channel, sender, target, message)

    return None


def format_chat_entry(entry: tuple[str, str, str, str | None, str]) -> str:
    timestamp, channel, sender, target, message = entry
    if channel == "WHISPER" and target:
        return f"[{timestamp}] [{channel}] {sender} -> {target}: {message}"
    return f"[{timestamp}] [{channel}] {sender}: {message}"


def cleanup_old_logs(now: datetime) -> None:
    plain_cutoff = now.date() - timedelta(days=KEEP_PLAIN_DAYS)
    delete_cutoff = now.date() - timedelta(days=KEEP_TOTAL_DAYS)

    for path in sorted(CHATLOGS_DIR.glob("chat-*.log")):
        stamp = path.stem.removeprefix("chat-")
        try:
            file_date = datetime.strptime(stamp, "%Y-%m-%d").date()
        except ValueError:
            continue

        if file_date < delete_cutoff:
            try:
                path.unlink()
            except OSError:
                pass
            continue

        if file_date >= plain_cutoff:
            continue

        gz_path = path.with_suffix(".log.gz")
        if gz_path.exists():
            try:
                path.unlink()
            except OSError:
                pass
            continue

        try:
            with path.open("rb") as source, gzip.open(gz_path, "wb") as target:
                target.writelines(source)
            path.unlink()
        except OSError:
            continue

    for path in sorted(CHATLOGS_DIR.glob("chat-*.log.gz")):
        stamp = path.name.removeprefix("chat-").removesuffix(".log.gz")
        try:
            file_date = datetime.strptime(stamp, "%Y-%m-%d").date()
        except ValueError:
            continue

        if file_date < delete_cutoff:
            try:
                path.unlink()
            except OSError:
                pass


def update_latest_symlink(target: Path) -> None:
    latest = latest_symlink_path()
    try:
        if latest.exists() or latest.is_symlink():
            latest.unlink()
        latest.symlink_to(target.name)
    except OSError:
        pass


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


def stop_worker() -> None:
    pid = read_existing_pid()
    if pid is None:
        log("Managed chat logger is already stopped")
        return

    if not process_is_running(pid):
        try:
            PID_PATH.unlink()
        except OSError:
            pass
        log("Removed stale managed chat logger PID file")
        return

    try:
        os.kill(pid, signal.SIGTERM)
    except OSError as exc:
        log(f"Failed to stop managed chat logger process {pid}: {exc}")
        return

    deadline = time.time() + 5
    while time.time() < deadline:
        if not process_is_running(pid):
            break
        time.sleep(0.1)

    if process_is_running(pid):
        try:
            os.kill(pid, signal.SIGKILL)
        except OSError:
            pass

    try:
        PID_PATH.unlink()
    except OSError:
        pass

    log("Managed chat logger stopped")


def ensure_worker_running() -> None:
    ensure_directories()

    pid = read_existing_pid()
    if pid is not None and process_is_running(pid):
        log(f"Managed chat logger already running with PID {pid}")
        log(f"Chat logs directory: {CHATLOGS_DIR}")
        return

    if pid is not None:
        try:
            PID_PATH.unlink()
        except OSError:
            pass

    with WORKER_LOG_PATH.open("a", encoding="utf-8") as worker_log:
        process = subprocess.Popen(
            [sys.executable, str(SCRIPT_PATH), "--worker"],
            cwd=str(HOME_DIR),
            env=os.environ.copy(),
            start_new_session=True,
            stdout=worker_log,
            stderr=subprocess.STDOUT,
            close_fds=True,
        )

    PID_PATH.write_text(f"{process.pid}\n", encoding="utf-8")
    log(f"Managed chat logger started in the background with PID {process.pid}")
    log(f"Chat logs directory: {CHATLOGS_DIR}")
    log(f"Worker log file: {WORKER_LOG_PATH}")


def worker_loop() -> int:
    ensure_directories()
    current_date = datetime.now().astimezone().date()
    cleanup_old_logs(datetime.now().astimezone())

    server_log_path = active_server_log_path()
    attached = False
    offset = 0

    while True:
        now = datetime.now().astimezone()
        if now.date() != current_date:
            current_date = now.date()
            cleanup_old_logs(now)

        if not server_log_path.is_file():
            time.sleep(POLL_SECONDS)
            continue

        try:
            with server_log_path.open("r", encoding="utf-8", errors="replace") as handle:
                handle.seek(0, os.SEEK_END)
                if attached:
                    handle.seek(offset, os.SEEK_SET)
                else:
                    offset = handle.tell()
                    attached = True

                while True:
                    line = handle.readline()
                    if line:
                        offset = handle.tell()
                        parsed = parse_chat_line(line)
                        if parsed is None:
                            continue

                        output_path = current_plain_log_path(datetime.now().astimezone())
                        with output_path.open("a", encoding="utf-8") as output:
                            output.write(format_chat_entry(parsed) + "\n")
                        update_latest_symlink(output_path)
                        continue

                    if not server_log_path.exists():
                        attached = False
                        offset = 0
                        break

                    try:
                        current_size = server_log_path.stat().st_size
                    except OSError:
                        time.sleep(POLL_SECONDS)
                        continue

                    if current_size < offset:
                        attached = False
                        offset = 0
                        break

                    time.sleep(POLL_SECONDS)
        except OSError:
            time.sleep(POLL_SECONDS)


def main() -> int:
    if len(sys.argv) > 1 and sys.argv[1] == "--worker":
        return worker_loop()

    if len(sys.argv) > 1 and sys.argv[1] == "--stop":
        stop_worker()
        return 0

    ensure_worker_running()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
