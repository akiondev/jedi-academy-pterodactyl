#!/usr/bin/env python3
"""
Managed default helper: persistent player chat logging.

This helper is refreshed from the image into:
  /home/container/addons/defaults/40-chatlogger.py

During managed startup it launches a detached background worker that:
  - tails the resolved active server log path with ``tail -n 0 -F``
    (so log rotation, truncation, unlink+recreate and engine restarts
    of ``server.log`` are handled transparently)
  - extracts public/team/whisper/admin chat lines, including common
    JKA / TaystJK / JAPro mod-specific verbs
  - writes clean daily chat logs into /home/container/chatlogs

The worker is hardened against single-line parse failures, writes a
periodic heartbeat to ``chatlogger-helper.log``, and validates the
recorded PID against ``/proc/<pid>/cmdline`` so a recycled PID can never
prevent a fresh start.
"""

from __future__ import annotations

import errno
import gzip
import json
import os
import re
import signal
import subprocess
import sys
import time
import traceback
from datetime import datetime, timedelta
from pathlib import Path

ADDON_LABEL = "[helper:chatlogger]"
HOME_DIR = Path("/home/container")
LOGS_DIR = HOME_DIR / "logs"
CHATLOGS_DIR = HOME_DIR / "chatlogs"
SCRIPT_PATH = Path(__file__).resolve()
PID_PATH = LOGS_DIR / "chatlogger.pid"
WORKER_LOG_PATH = LOGS_DIR / "chatlogger-helper.log"
STATE_PATH = LOGS_DIR / "chatlogger-state.json"
RUNTIME_ENV_PATH = HOME_DIR / ".runtime" / "taystjk-effective.env"

WORKER_MARKER = "40-chatlogger.py"
WORKER_FLAG = "--worker"


def _env_int(name: str, default: int, minimum: int = 1) -> int:
    raw = os.getenv(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    if value < minimum:
        return minimum
    return value


KEEP_PLAIN_DAYS = _env_int("CHATLOGGER_KEEP_PLAIN_DAYS", 7)
KEEP_TOTAL_DAYS = _env_int("CHATLOGGER_KEEP_TOTAL_DAYS", 60)
HEARTBEAT_SECONDS = _env_int("CHATLOGGER_HEARTBEAT_SECONDS", 300, minimum=10)
TAIL_RESTART_BACKOFF_MAX = _env_int("CHATLOGGER_TAIL_RESTART_BACKOFF_MAX", 30, minimum=1)
TAIL_RESTART_BACKOFF_INITIAL = 1
CLEANUP_INTERVAL_SECONDS = 3600

TIMESTAMP_RE = re.compile(r"^(?P<stamp>\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s+(?P<body>.*)$")
MATCH_TIME_RE = re.compile(r"^(?:\d{1,3}:\d{2}(?::\d{2})?)\s+(?P<body>.*)$")
QUAKE_COLOR_RE = re.compile(r"\^(?:[0-9A-Za-z])")
# Full ANSI CSI escape sequences (ESC [ ... final-byte).  The live-output
# mirror written by the anti-VPN supervisor preserves raw engine stdout,
# which can include terminal colour sequences wrapping chat/broadcast lines.
ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")

# Chat verb groups. Order matters: more specific verbs first so a generic
# fallback does not steal a known channel classification.
PUBLIC_VERBS = ("say", "chat", "globalchat")
TEAM_VERBS = ("sayteam", "teamsay", "team", "teamchat")
WHISPER_VERBS = ("tell", "whisper", "privmsg", "pm")
ADMIN_VERBS = ("amsay", "smsay", "amchat", "smchat", "tsay", "csay", "vsay", "vchat")
ADMIN_TELL_VERBS = ("amtell", "smtell", "ampm")

CHAT_PATTERNS: tuple[tuple[str, "re.Pattern[str]"], ...] = (
    (
        "WHISPER",
        re.compile(
            r"^(?:" + "|".join(WHISPER_VERBS) + r")\s*:\s*(?P<sender>.+?)\s*(?:->|to)\s*(?P<target>.+?)\s*:\s*(?P<message>.+)$",
            re.IGNORECASE,
        ),
    ),
    (
        "ADMIN_WHISPER",
        re.compile(
            r"^(?:" + "|".join(ADMIN_TELL_VERBS) + r")\s*:\s*(?P<sender>.+?)\s*(?:->|to)\s*(?P<target>.+?)\s*:\s*(?P<message>.+)$",
            re.IGNORECASE,
        ),
    ),
    (
        "TEAM",
        re.compile(
            r"^(?:" + "|".join(TEAM_VERBS) + r")\s*:\s*(?P<sender>.+?)\s*:\s*(?P<message>.+)$",
            re.IGNORECASE,
        ),
    ),
    (
        "ADMIN",
        re.compile(
            r"^(?:" + "|".join(ADMIN_VERBS) + r")\s*:\s*(?P<sender>.+?)\s*:\s*(?P<message>.+)$",
            re.IGNORECASE,
        ),
    ),
    (
        "PUBLIC",
        re.compile(
            r"^(?:" + "|".join(PUBLIC_VERBS) + r")\s*:\s*(?P<sender>.+?)\s*:\s*(?P<message>.+)$",
            re.IGNORECASE,
        ),
    ),
)

# Generic fallback: ``<verb>: <name>: <message>`` for unknown but
# chat-shaped mod prefixes. We classify these as PUBLIC since we cannot
# tell the channel reliably without an explicit verb match.
GENERIC_CHAT_RE = re.compile(
    r"^(?P<verb>[A-Za-z][A-Za-z0-9_]{1,15})\s*:\s*(?P<sender>[^:]{1,64}?)\s*:\s*(?P<message>.+)$"
)
# Verbs that look chat-shaped but are definitively NOT chat.
GENERIC_VERB_BLOCKLIST = frozenset(
    {
        "initgame",
        "shutdowngame",
        "clientconnect",
        "clientdisconnect",
        "clientbegin",
        "changeteam",
        "kill",
        "item",
        "exit",
        "score",
        "challenge",
        "weapon",
        "duel",
        "info",
        "warning",
        "error",
        "broadcast",
        "rcon",
        "status",
    }
)

# Sender names that are never real players.
# "server" is the name used when RCON issues a ``say`` command (e.g.
# anti-VPN broadcasts appear as ``say: server: [Anti-VPN] ...``).
# "taystjk" / "openjk" appear when the engine itself echoes internal
# version/status lines through the ``say`` verb.
SENDER_BLOCKLIST = frozenset({"server", "taystjk", "openjk"})

# JKA engines and mods sometimes broadcast slot-score tables via the
# ``say`` verb using the format ``<slot>  <team>: <value>``.  Real player
# names never start with a bare digit followed by two or more spaces.
SENDER_SLOT_RE = re.compile(r"^\d+\s{2,}")


def log(message: str) -> None:
    print(f"{ADDON_LABEL} {message}", flush=True)


def worker_log(message: str) -> None:
    """Append a timestamped line to the worker log file."""
    try:
        WORKER_LOG_PATH.parent.mkdir(parents=True, exist_ok=True)
        stamp = datetime.now().astimezone().strftime("%Y-%m-%d %H:%M:%S %Z")
        with WORKER_LOG_PATH.open("a", encoding="utf-8") as handle:
            handle.write(f"[{stamp}] {message}\n")
    except OSError:
        # Last resort: fall back to stdout, which is captured by the
        # detached worker via Popen(stdout=worker_log).
        print(f"{ADDON_LABEL} {message}", flush=True)


def load_runtime_env() -> dict[str, str]:
    state: dict[str, str] = {}

    tracked_keys = {
        "TAYSTJK_ACTIVE_MOD_DIR",
        "TAYSTJK_ACTIVE_SERVER_LOG_PATH",
        "TAYSTJK_LIVE_OUTPUT_ENABLED",
        "TAYSTJK_LIVE_OUTPUT_PATH",
    }

    for key in tracked_keys:
        value = os.getenv(key)
        if value:
            state[key] = value.strip()

    if not RUNTIME_ENV_PATH.is_file():
        return state

    try:
        for raw_line in RUNTIME_ENV_PATH.read_text(encoding="utf-8").splitlines():
            line = raw_line.strip()
            if not line or "=" not in line:
                continue
            key, raw_value = line.split("=", 1)
            key = key.strip()
            if key not in tracked_keys:
                continue
            state.setdefault(key, raw_value.strip().strip("\"'"))
    except OSError as exc:
        log(f"Failed to read runtime env {RUNTIME_ENV_PATH}: {exc}")

    return state


def active_mod_dir() -> str:
    runtime_env = load_runtime_env()
    return runtime_env.get("TAYSTJK_ACTIVE_MOD_DIR", "taystjk").strip() or "taystjk"


def active_server_log_path() -> Path:
    runtime_env = load_runtime_env()
    runtime_log_path = runtime_env.get("TAYSTJK_ACTIVE_SERVER_LOG_PATH", "").strip()
    if runtime_log_path:
        return Path(runtime_log_path)
    return HOME_DIR / active_mod_dir() / "server.log"


def live_output_path() -> Path | None:
    """Return the supervisor-managed live mirror file when available.

    The chatlogger prefers this file because it is written directly by the
    anti-VPN supervisor which owns the dedicated server's stdout/stderr
    pipes. It contains every line the engine prints, including lines that
    the engine does not flush to its own server.log promptly. When the
    supervisor is disabled (``TAYSTJK_LIVE_OUTPUT_ENABLED`` != ``true``)
    we fall back to tailing ``server.log`` for backwards compatibility.
    """
    runtime_env = load_runtime_env()
    enabled = runtime_env.get("TAYSTJK_LIVE_OUTPUT_ENABLED", "").strip().lower()
    if enabled != "true":
        return None
    raw_path = runtime_env.get("TAYSTJK_LIVE_OUTPUT_PATH", "").strip()
    if not raw_path:
        return None
    return Path(raw_path)


def preferred_tail_source() -> tuple[Path, str]:
    """Return (path, source_label) for the chatlogger to tail.

    Prefers the runtime live-output mirror when available; otherwise falls
    back to the active server.log path.
    """
    live = live_output_path()
    if live is not None:
        return live, "live-output"
    return active_server_log_path(), "server-log"


def current_plain_log_path(now: datetime) -> Path:
    return CHATLOGS_DIR / f"chat-{now.strftime('%Y-%m-%d')}.log"


def latest_symlink_path() -> Path:
    return CHATLOGS_DIR / "latest.log"


def ensure_directories() -> None:
    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    CHATLOGS_DIR.mkdir(parents=True, exist_ok=True)


def strip_quake_colors(value: str) -> str:
    return QUAKE_COLOR_RE.sub("", value)


def strip_ansi(value: str) -> str:
    return ANSI_RE.sub("", value)


def normalize_text(value: str) -> str:
    return strip_quake_colors(strip_ansi(value)).strip()


def current_local_timestamp() -> str:
    return datetime.now().astimezone().strftime("%Y-%m-%d %H:%M:%S")


def split_log_prefix(raw_line: str) -> tuple[str, str]:
    line = raw_line.rstrip()

    timestamp_match = TIMESTAMP_RE.match(line)
    if timestamp_match:
        return timestamp_match.group("stamp"), timestamp_match.group("body").strip()

    match_time = MATCH_TIME_RE.match(line)
    if match_time:
        return current_local_timestamp(), match_time.group("body").strip()

    return current_local_timestamp(), raw_line.strip()


def parse_chat_line(raw_line: str) -> tuple[str, str, str, str | None, str] | None:
    if not raw_line or not raw_line.strip():
        return None

    # Strip ANSI terminal escape sequences before timestamp/body extraction
    # so that ANSI codes wrapping the verb (e.g. ESC[35msay:…ESC[0m) do not
    # prevent the verb patterns from matching at position zero.
    clean_line = strip_ansi(raw_line)

    stamp, body = split_log_prefix(clean_line)
    if not body:
        return None

    for channel, pattern in CHAT_PATTERNS:
        match = pattern.match(body)
        if not match:
            continue

        sender = normalize_text(match.group("sender"))
        target = normalize_text(match.groupdict().get("target") or "") or None
        message = normalize_text(match.group("message"))

        if not sender or not message:
            return None

        # Reject known non-player senders (engine broadcasts, RCON say output).
        if sender.lower() in SENDER_BLOCKLIST:
            return None

        # Reject slot-score lines emitted by JKA engines/mods (e.g. "0  blue: 0").
        if SENDER_SLOT_RE.match(sender):
            return None

        tz_name = datetime.now().astimezone().tzname() or "LOCAL"
        return (f"{stamp} {tz_name}", channel, sender, target, message)

    # Generic fallback for unknown but chat-shaped mod verbs.
    generic = GENERIC_CHAT_RE.match(body)
    if generic:
        verb = generic.group("verb").lower()
        if verb not in GENERIC_VERB_BLOCKLIST:
            sender = normalize_text(generic.group("sender"))
            message = normalize_text(generic.group("message"))
            # Reject obviously non-chat senders that snuck through.
            if sender and message and "@@@" not in sender and "@@@" not in message:
                if sender.lower() not in SENDER_BLOCKLIST and not SENDER_SLOT_RE.match(sender):
                    tz_name = datetime.now().astimezone().tzname() or "LOCAL"
                    return (f"{stamp} {tz_name}", "PUBLIC", sender, None, message)

    return None


def format_chat_entry(entry: tuple[str, str, str, str | None, str]) -> str:
    timestamp, channel, sender, target, message = entry
    if channel in {"WHISPER", "ADMIN_WHISPER"} and target:
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


# ---------------------------------------------------------------------------
# PID / lifecycle handling
# ---------------------------------------------------------------------------


def read_pid_record() -> dict | None:
    """Read the PID file as JSON. Falls back to legacy ``int``-only files."""
    if not PID_PATH.is_file():
        return None
    try:
        raw = PID_PATH.read_text(encoding="utf-8").strip()
    except OSError:
        return None
    if not raw:
        return None
    try:
        record = json.loads(raw)
        if isinstance(record, dict) and "pid" in record:
            return record
    except ValueError:
        pass
    # Legacy format: a bare integer.
    try:
        return {"pid": int(raw), "legacy": True}
    except ValueError:
        return None


def write_pid_record(pid: int) -> None:
    record = {
        "pid": pid,
        "start_time": time.time(),
        "script": str(SCRIPT_PATH),
    }
    PID_PATH.write_text(json.dumps(record) + "\n", encoding="utf-8")


def proc_cmdline(pid: int) -> str:
    """Return the cmdline of ``pid`` as a single space-joined string."""
    try:
        with open(f"/proc/{pid}/cmdline", "rb") as handle:
            raw = handle.read()
    except OSError:
        return ""
    parts = [piece.decode("utf-8", "replace") for piece in raw.split(b"\x00") if piece]
    return " ".join(parts)


def process_is_running(pid: int) -> bool:
    if pid <= 0:
        return False
    try:
        os.kill(pid, 0)
    except OSError as exc:
        if exc.errno == errno.ESRCH:
            return False
        if exc.errno == errno.EPERM:
            return True
        return False
    return True


def process_is_our_worker(pid: int) -> bool:
    """Verify ``pid`` belongs to a chatlogger worker, not a recycled PID."""
    if not process_is_running(pid):
        return False
    cmdline = proc_cmdline(pid)
    if not cmdline:
        # /proc may be unavailable in some sandboxes; trust the live check.
        return True
    return WORKER_MARKER in cmdline and WORKER_FLAG in cmdline


def stop_worker() -> None:
    record = read_pid_record()
    if record is None:
        log("Managed chat logger is already stopped")
        return

    pid = int(record.get("pid", 0))

    if not process_is_running(pid):
        try:
            PID_PATH.unlink()
        except OSError:
            pass
        log("Removed stale managed chat logger PID file")
        return

    if not process_is_our_worker(pid):
        try:
            PID_PATH.unlink()
        except OSError:
            pass
        log(f"PID {pid} no longer belongs to chat logger worker; PID file cleared")
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

    record = read_pid_record()
    if record is not None:
        pid = int(record.get("pid", 0))
        if process_is_our_worker(pid):
            log(f"Managed chat logger already running with PID {pid}")
            log(f"Chat logs directory: {CHATLOGS_DIR}")
            return
        # Stale or recycled PID: clean up so we can start fresh.
        try:
            PID_PATH.unlink()
        except OSError:
            pass
        if pid > 0 and process_is_running(pid):
            log(f"PID {pid} from PID file is no longer a chat logger worker; restarting")
        else:
            log("Removed stale managed chat logger PID file")

    with WORKER_LOG_PATH.open("a", encoding="utf-8") as handle:
        process = subprocess.Popen(
            [sys.executable, str(SCRIPT_PATH), WORKER_FLAG],
            cwd=str(HOME_DIR),
            env=os.environ.copy(),
            start_new_session=True,
            stdout=handle,
            stderr=subprocess.STDOUT,
            close_fds=True,
        )

    write_pid_record(process.pid)
    log(f"Managed chat logger started in the background with PID {process.pid}")
    log(f"Chat logs directory: {CHATLOGS_DIR}")
    log(f"Worker log file: {WORKER_LOG_PATH}")


# ---------------------------------------------------------------------------
# Worker state (heartbeat, last chat) for --status
# ---------------------------------------------------------------------------


def write_state(state: dict) -> None:
    try:
        STATE_PATH.write_text(json.dumps(state, sort_keys=True) + "\n", encoding="utf-8")
    except OSError:
        pass


def read_state() -> dict:
    if not STATE_PATH.is_file():
        return {}
    try:
        return json.loads(STATE_PATH.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}


# ---------------------------------------------------------------------------
# Worker
# ---------------------------------------------------------------------------


class WorkerRuntime:
    """Mutable, output-side state held by the worker loop."""

    def __init__(self) -> None:
        self.lines_seen = 0
        self.chats_logged = 0
        self.last_chat_at: str | None = None
        self.last_chat_summary: str | None = None
        self.last_heartbeat = 0.0
        self.last_cleanup = time.monotonic()
        self.current_date = datetime.now().astimezone().date()
        self.output_handle = None  # type: ignore[assignment]
        self.output_path: Path | None = None

    # -- output file rotation ------------------------------------------------

    def open_today(self, now: datetime) -> None:
        path = current_plain_log_path(now)
        if self.output_handle is not None and self.output_path == path:
            return
        self.close_output()
        path.parent.mkdir(parents=True, exist_ok=True)
        self.output_handle = path.open("a", encoding="utf-8")
        self.output_path = path
        update_latest_symlink(path)

    def close_output(self) -> None:
        if self.output_handle is not None:
            try:
                self.output_handle.flush()
                self.output_handle.close()
            except OSError:
                pass
        self.output_handle = None
        self.output_path = None

    def maybe_roll_date(self, now: datetime) -> None:
        if now.date() != self.current_date:
            self.current_date = now.date()
            self.close_output()
            cleanup_old_logs(now)
            self.last_cleanup = time.monotonic()

    # -- chat write ----------------------------------------------------------

    def write_chat(self, entry: tuple[str, str, str, str | None, str]) -> None:
        now = datetime.now().astimezone()
        self.maybe_roll_date(now)
        self.open_today(now)
        line = format_chat_entry(entry)
        assert self.output_handle is not None
        self.output_handle.write(line + "\n")
        self.output_handle.flush()
        self.chats_logged += 1
        self.last_chat_at = now.strftime("%Y-%m-%d %H:%M:%S %Z")
        self.last_chat_summary = line

    # -- bookkeeping ---------------------------------------------------------

    def heartbeat(self, *, force: bool = False) -> None:
        now = time.monotonic()
        if not force and (now - self.last_heartbeat) < HEARTBEAT_SECONDS:
            return
        self.last_heartbeat = now
        message = (
            f"alive lines={self.lines_seen} chats={self.chats_logged} "
            f"last_chat={self.last_chat_at or 'never'}"
        )
        worker_log(message)
        tail_path, tail_source = preferred_tail_source()
        write_state(
            {
                "pid": os.getpid(),
                "updated_at": datetime.now().astimezone().strftime("%Y-%m-%d %H:%M:%S %Z"),
                "lines_seen": self.lines_seen,
                "chats_logged": self.chats_logged,
                "last_chat_at": self.last_chat_at,
                "last_chat": self.last_chat_summary,
                "server_log_path": str(active_server_log_path()),
                "tail_source": tail_source,
                "tail_path": str(tail_path),
            }
        )

    def maybe_periodic_cleanup(self) -> None:
        if (time.monotonic() - self.last_cleanup) < CLEANUP_INTERVAL_SECONDS:
            return
        cleanup_old_logs(datetime.now().astimezone())
        self.last_cleanup = time.monotonic()


_shutdown_requested = False


def _request_shutdown(signum, _frame) -> None:  # noqa: ANN001
    global _shutdown_requested
    _shutdown_requested = True
    worker_log(f"received signal {signum}, shutting down")


def _start_tail(server_log_path: Path) -> subprocess.Popen[str]:
    return subprocess.Popen(
        ["tail", "-n", "0", "-F", "--", str(server_log_path)],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
        errors="replace",
        bufsize=1,
        close_fds=True,
    )


def _stop_tail(proc: subprocess.Popen[str]) -> None:
    if proc.poll() is not None:
        return
    try:
        proc.terminate()
        try:
            proc.wait(timeout=3)
        except subprocess.TimeoutExpired:
            proc.kill()
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                pass
    except OSError:
        pass
    for stream in (proc.stdout, proc.stderr):
        if stream is not None:
            try:
                stream.close()
            except OSError:
                pass


def worker_loop() -> int:
    ensure_directories()
    signal.signal(signal.SIGTERM, _request_shutdown)
    signal.signal(signal.SIGINT, _request_shutdown)

    runtime = WorkerRuntime()
    cleanup_old_logs(datetime.now().astimezone())
    runtime.heartbeat(force=True)
    worker_log(
        f"worker starting (pid={os.getpid()}, "
        f"keep_plain_days={KEEP_PLAIN_DAYS}, keep_total_days={KEEP_TOTAL_DAYS}, "
        f"heartbeat_seconds={HEARTBEAT_SECONDS})"
    )

    backoff = TAIL_RESTART_BACKOFF_INITIAL

    while not _shutdown_requested:
        tail_path, tail_source = preferred_tail_source()

        # Wait for the tail source to exist before spawning tail; tail -F
        # can also wait on its own, but blocking here keeps the worker log
        # easier to interpret. The supervisor creates the live mirror file
        # as soon as the dedicated server process starts, so this only
        # blocks early during cold startup.
        if not tail_path.exists():
            worker_log(f"tail source {tail_path} ({tail_source}) not present yet, waiting")
            wait_until = time.monotonic() + 5
            while not _shutdown_requested and time.monotonic() < wait_until:
                if tail_path.exists():
                    break
                time.sleep(0.5)
                runtime.heartbeat()
            if _shutdown_requested:
                break
            continue

        worker_log(f"tailing {tail_path} (source={tail_source})")
        try:
            proc = _start_tail(tail_path)
        except (OSError, ValueError) as exc:
            worker_log(f"failed to start tail: {exc}; retrying in {backoff}s")
            _sleep_with_heartbeat(runtime, backoff)
            backoff = min(backoff * 2, TAIL_RESTART_BACKOFF_MAX)
            continue

        # Successful spawn resets backoff once we actually see output or
        # the process stays alive for a moment.
        spawn_time = time.monotonic()

        try:
            assert proc.stdout is not None
            for raw_line in proc.stdout:
                if _shutdown_requested:
                    break
                runtime.lines_seen += 1
                try:
                    parsed = parse_chat_line(raw_line)
                    if parsed is not None:
                        runtime.write_chat(parsed)
                except Exception:
                    worker_log(
                        "exception while processing line:\n"
                        + traceback.format_exc()
                        + f"offending line: {raw_line.rstrip()!r}"
                    )
                runtime.heartbeat()
                runtime.maybe_periodic_cleanup()

                # Reset backoff once tail has clearly been healthy for a while.
                if (time.monotonic() - spawn_time) > 5:
                    backoff = TAIL_RESTART_BACKOFF_INITIAL
        except Exception:
            worker_log("unexpected exception in tail loop:\n" + traceback.format_exc())
        finally:
            _stop_tail(proc)

        if _shutdown_requested:
            break

        # tail exited (engine restart, log replaced before -F could
        # reattach, etc.). Drain any stderr to the helper log for
        # diagnostics, then restart with backoff.
        try:
            stderr_output = proc.stderr.read() if proc.stderr is not None else ""
        except OSError:
            stderr_output = ""
        rc = proc.returncode
        msg = f"tail exited with rc={rc}, restarting in {backoff}s"
        if stderr_output.strip():
            msg += f"; stderr: {stderr_output.strip()[:500]}"
        worker_log(msg)
        _sleep_with_heartbeat(runtime, backoff)
        backoff = min(backoff * 2, TAIL_RESTART_BACKOFF_MAX)

    runtime.close_output()
    runtime.heartbeat(force=True)
    worker_log("worker exiting")
    return 0


def _sleep_with_heartbeat(runtime: WorkerRuntime, seconds: int) -> None:
    deadline = time.monotonic() + max(0, seconds)
    while not _shutdown_requested and time.monotonic() < deadline:
        remaining = deadline - time.monotonic()
        time.sleep(max(0.0, min(0.5, remaining)))
        runtime.heartbeat()


# ---------------------------------------------------------------------------
# Status / self-test
# ---------------------------------------------------------------------------


def show_status() -> int:
    record = read_pid_record()
    state = read_state()

    tail_path, tail_source = preferred_tail_source()

    print(f"{ADDON_LABEL} status")
    print(f"  pid file:        {PID_PATH}")
    print(f"  worker log:      {WORKER_LOG_PATH}")
    print(f"  chatlogs dir:    {CHATLOGS_DIR}")
    print(f"  tail source:     {tail_source}")
    print(f"  tail path:       {tail_path}")
    print(f"  server log path: {active_server_log_path()}")

    if record is None:
        print("  state:           not running (no PID file)")
        return 0

    pid = int(record.get("pid", 0))
    running = process_is_our_worker(pid)
    print(f"  pid:             {pid}")
    print(f"  pid is worker:   {'yes' if running else 'no'}")

    if state:
        print(f"  last heartbeat:  {state.get('updated_at', 'unknown')}")
        print(f"  lines seen:      {state.get('lines_seen', 0)}")
        print(f"  chats logged:    {state.get('chats_logged', 0)}")
        print(f"  last chat at:    {state.get('last_chat_at') or 'never'}")
        if state.get("last_chat"):
            print(f"  last chat:       {state['last_chat']}")
    else:
        print("  state:           no heartbeat recorded yet")

    return 0 if running else 1


SELFTEST_LINES: tuple[str, ...] = (
    "0:19 say: js hart: works like a charm now",
    "0:57 say: js hart: i type",
    '1:28 say: js hart^7: where u can see if fps is 20 or 30?',
    "2:00 sayteam: Akion: rally at duel room",
    "2:30 tell: Akion -> Robin: meet me at duel room",
    "3:00 amsay: Admin: server restart in 5",
    "3:30 amtell: Admin -> Robin: stop telekilling",
    "4:00 vsay: Akion: hi",
    "2026-04-18 19:32:39 say: js hart: hello with absolute timestamp",
    # Generic fallback: an unknown but chat-shaped verb should match.
    "5:00 modsay: Akion: future mod chat verb",
    # Player name starting with a digit but no double-space (real name, must match).
    "0:01 say: 1337h4x0r: hello",
    # ANSI-wrapped chat: after stripping ESC sequences the verb/sender/message
    # must be parsed correctly and the cleaned sender logged without ANSI noise.
    "0:05 say: \x1b[35mUsop\x1b[0m: i boss",  # must match; sender → "Usop"
    "0:06 say: \x1b[31mPadawan\x1b[0m: kk",   # must match; sender → "Padawan"
    "0:07 say: Padawan: tete\x1b[0m",          # must match; message → "tete"
    "0:00 InitGame: \\version\\TaystJK",  # must NOT match
    "0:00 ClientConnect: 0 [1.2.3.4] (X) \"hart\"",  # must NOT match
    "0:00 @@@VOTEPASSED (g_gametype 6)",  # must NOT match
    "0:00 say: : empty sender",  # must NOT match
    "0:00 rcon: admin: status",  # must NOT match (rcon is blocklisted)
    "0:00 status: server: up",  # must NOT match (status is blocklisted)
    "0:00 ClientDisconnect: 0 [1.2.3.4] (X) \"hart\"",  # must NOT match
    "",  # must NOT match
    # Anti-VPN broadcast via RCON say appears as "say: server: …"
    "0:00 say: server: [Anti-VPN] VPN PASS: Akion cleared checks (10/90). Score remained below the configured threshold.",  # must NOT match
    # Engine version echo (TaystJK server name via say)
    "0:00 say: TaystJK: latest-86f04849f",   # must NOT match
    "0:00 say: TaystJK: latest-86f04849f\x1b[0m",  # must NOT match (ANSI suffix)
    # Slot-score broadcast lines ("0  blue: 0", "2  blue: 7")
    "0:00 say: 0  blue: 0",   # must NOT match
    "0:00 say: 2  blue: 7",   # must NOT match
)


def run_selftest() -> int:
    print(f"{ADDON_LABEL} self-test")
    failures = 0
    for raw in SELFTEST_LINES:
        parsed = parse_chat_line(raw)
        rendered = format_chat_entry(parsed) if parsed is not None else "<no match>"
        print(f"  in : {raw!r}")
        print(f"  out: {rendered}")

    # Sanity assertions so this is also useful as a regression test.
    must_match = [
        "0:19 say: js hart: works like a charm now",
        "0:57 say: js hart: i type",
        '1:28 say: js hart^7: where u can see if fps is 20 or 30?',
        "2:00 sayteam: Akion: rally at duel room",
        "2:30 tell: Akion -> Robin: meet me at duel room",
        "3:00 amsay: Admin: server restart in 5",
        "3:30 amtell: Admin -> Robin: stop telekilling",
        "4:00 vsay: Akion: hi",
        "2026-04-18 19:32:39 say: js hart: hello with absolute timestamp",
        "5:00 modsay: Akion: future mod chat verb",
        "0:01 say: 1337h4x0r: hello",
        "0:05 say: \x1b[35mUsop\x1b[0m: i boss",
        "0:06 say: \x1b[31mPadawan\x1b[0m: kk",
        "0:07 say: Padawan: tete\x1b[0m",
    ]
    must_not_match = [
        "0:00 InitGame: \\version\\TaystJK",
        "0:00 ClientConnect: 0 [1.2.3.4] (X) \"hart\"",
        "0:00 ClientDisconnect: 0 [1.2.3.4] (X) \"hart\"",
        "0:00 @@@VOTEPASSED (g_gametype 6)",
        "0:00 say: : empty sender",
        "0:00 rcon: admin: status",
        "0:00 status: server: up",
        "",
        # Server/engine sender blocklist
        "0:00 say: server: [Anti-VPN] VPN PASS: Akion cleared checks (10/90). Score remained below the configured threshold.",
        "0:00 say: TaystJK: latest-86f04849f",
        "0:00 say: TaystJK: latest-86f04849f\x1b[0m",
        # Slot-score lines
        "0:00 say: 0  blue: 0",
        "0:00 say: 2  blue: 7",
    ]

    # Additionally verify that ANSI codes are cleaned from the output.
    ansi_cases = [
        ("0:05 say: \x1b[35mUsop\x1b[0m: i boss", "Usop", "i boss"),
        ("0:06 say: \x1b[31mPadawan\x1b[0m: kk", "Padawan", "kk"),
        ("0:07 say: Padawan: tete\x1b[0m", "Padawan", "tete"),
    ]
    for raw, exp_sender, exp_message in ansi_cases:
        parsed = parse_chat_line(raw)
        if parsed is None:
            print(f"  FAIL: expected match for ANSI line {raw!r}")
            failures += 1
        else:
            _, _, sender, _, message = parsed
            if sender != exp_sender:
                print(f"  FAIL: ANSI sender: expected {exp_sender!r}, got {sender!r}")
                failures += 1
            if message != exp_message:
                print(f"  FAIL: ANSI message: expected {exp_message!r}, got {message!r}")
                failures += 1

    for raw in must_match:
        if parse_chat_line(raw) is None:
            print(f"  FAIL: expected match for {raw!r}")
            failures += 1
    for raw in must_not_match:
        if parse_chat_line(raw) is not None:
            print(f"  FAIL: expected no match for {raw!r}")
            failures += 1

    if failures:
        print(f"{ADDON_LABEL} self-test failed with {failures} error(s)")
        return 1
    print(f"{ADDON_LABEL} self-test ok")
    return 0


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)

    if args and args[0] == WORKER_FLAG:
        return worker_loop()

    if args and args[0] == "--stop":
        stop_worker()
        return 0

    if args and args[0] == "--status":
        return show_status()

    if args and args[0] == "--selftest":
        return run_selftest()

    ensure_worker_running()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
