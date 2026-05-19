#!/usr/bin/env python3
"""
Event-driven default helper: persistent player chat logging.

This is the Phase 2 (event-bus) replacement for the legacy
``chatlogger.py`` daemon that used to tail
``/home/container/.runtime/live/server-output.log`` and
``/home/container/<mod>/server.log`` with ``tail -F``. The supervisor
now reads the dedicated server's stdout/stderr exactly once and
publishes parsed events through a central dispatcher; this addon
receives those events as newline-delimited JSON on stdin.

Protocol:
    One JSON object per line on stdin. Required field: ``"type"``.
    For chat events the dispatcher emits ``chat_message`` with
    optional ``slot``, ``name``, ``message``, ``raw`` and ``time``
    fields. For mod-specific verbs the dispatcher emits ``raw_line``
    with the full original line so we can apply a richer classifier.

Output:
    Daily plain-text logs in ``/home/container/chatlogs/`` named
    ``chat-YYYY-MM-DD.log`` plus a ``latest.log`` symlink, matching
    the on-disk layout of the legacy daemon so existing operator
    tooling keeps working.

What this version does NOT do (compared to the legacy daemon):
- No ``tail -F``.
- No reading of ``server.log``.
- No reading of the runtime live-output mirror.
- No background daemonisation, PID files, or self-respawn — the
  supervisor's addon runner owns lifecycle.
- No ``--status`` / ``--selftest`` subcommands; status is now
  visible in the supervisor console output.
"""

from __future__ import annotations

import datetime as _dt
import json
import os
import re
import signal
import sys
from pathlib import Path

ADDON_LABEL = "[helper:chatlogger]"
HOME_DIR = Path("/home/container")
CHATLOGS_DIR = HOME_DIR / "chatlogs"

# Sender name normalisation: drop Quake-style colour codes (^1, ^a)
# and trim ANSI sequences that some mods sneak into chat names.
QUAKE_COLOR_RE = re.compile(r"\^(?:[0-9A-Za-z])")
ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
TIMESTAMP_PREFIX_RE = re.compile(
    r"^(?:\s*(?:\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}|\d{1,3}:\d{2}(?::\d{2})?)\s+)?(?P<body>.*)$"
)

PUBLIC_VERBS = ("say", "chat", "globalchat")
TEAM_VERBS = ("sayteam", "teamsay", "team", "teamchat")
WHISPER_VERBS = ("tell", "whisper", "privmsg", "pm")
ADMIN_VERBS = ("amsay", "smsay", "amchat", "smchat", "tsay", "csay", "vsay", "vchat")
ADMIN_TELL_VERBS = ("amtell", "smtell", "ampm")

# Pre-classified channel patterns for the broader raw_line fallback so
# mod-specific verbs (JAPro etc.) still produce typed entries.
RAW_VERB_PATTERNS = [
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
]

SENDER_BLOCKLIST = frozenset({"server", "taystjk", "openjk"})


def _env_int(name: str, default: int, minimum: int = 1) -> int:
    raw = os.environ.get(name)
    if raw is None or raw.strip() == "":
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    return max(minimum, value)


KEEP_PLAIN_DAYS = _env_int("CHATLOGGER_KEEP_PLAIN_DAYS", 7)


def log(message: str) -> None:
    """Print a diagnostic line to the supervisor-prefixed stdout."""
    print(f"{ADDON_LABEL} {message}", flush=True)


def strip_quake_colors(value: str) -> str:
    return QUAKE_COLOR_RE.sub("", value)


def strip_ansi(value: str) -> str:
    return ANSI_RE.sub("", value)


def normalize_text(value: str) -> str:
    return strip_quake_colors(strip_ansi(value)).strip()


def split_log_prefix(raw_line: str) -> str:
    """Return the body of a server log line with any leading engine
    timestamp prefix removed."""
    match = TIMESTAMP_PREFIX_RE.match(raw_line)
    if match is None:
        return raw_line.strip()
    return match.group("body").strip()


def classify_raw_line(raw_line: str) -> tuple[str, str, str, str | None] | None:
    """Best-effort classifier for ``raw_line`` events. Returns
    ``(channel, sender, message, target)`` if the line looks like a
    chat message, otherwise ``None``."""
    body = split_log_prefix(raw_line)
    if not body:
        return None
    for channel, pattern in RAW_VERB_PATTERNS:
        match = pattern.match(body)
        if match is None:
            continue
        sender = normalize_text(match.group("sender"))
        message = normalize_text(match.group("message"))
        if not sender or not message:
            return None
        if sender.lower() in SENDER_BLOCKLIST:
            return None
        target = None
        if "target" in match.groupdict():
            target = normalize_text(match.group("target"))
        return channel, sender, message, target
    return None


def ensure_directories() -> None:
    CHATLOGS_DIR.mkdir(parents=True, exist_ok=True)


def current_plain_log_path(now: _dt.datetime) -> Path:
    return CHATLOGS_DIR / f"chat-{now.strftime('%Y-%m-%d')}.log"


def latest_symlink_path() -> Path:
    return CHATLOGS_DIR / "latest.log"


def update_latest_symlink(target: Path) -> None:
    link = latest_symlink_path()
    try:
        if link.is_symlink() or link.exists():
            link.unlink()
        link.symlink_to(target.name)
    except OSError as exc:
        log(f"warning: could not refresh latest.log symlink: {exc}")


def cleanup_old_logs(now: _dt.datetime) -> None:
    cutoff = now - _dt.timedelta(days=KEEP_PLAIN_DAYS)
    cutoff_naive = cutoff.replace(tzinfo=None) if cutoff.tzinfo else cutoff
    for path in CHATLOGS_DIR.glob("chat-*.log"):
        try:
            stamp = _dt.datetime.strptime(path.stem[len("chat-") :], "%Y-%m-%d")
        except ValueError:
            continue
        if stamp < cutoff_naive:
            try:
                path.unlink()
            except OSError as exc:
                log(f"warning: could not remove old log {path}: {exc}")


def format_entry(now: _dt.datetime, channel: str, sender: str, message: str, target: str | None) -> str:
    stamp = now.strftime("%Y-%m-%d %H:%M:%S")
    if target:
        return f"[{stamp}] [{channel}] {sender} -> {target}: {message}\n"
    return f"[{stamp}] [{channel}] {sender}: {message}\n"


class ChatWriter:
    """Daily-rotating chat log writer."""

    def __init__(self) -> None:
        self._date: str | None = None
        self._fp = None
        self._last_cleanup: _dt.date | None = None

    def _maybe_roll(self, now: _dt.datetime) -> None:
        date_key = now.strftime("%Y-%m-%d")
        if date_key == self._date and self._fp is not None:
            return
        if self._fp is not None:
            try:
                self._fp.close()
            except OSError:
                pass
        target = current_plain_log_path(now)
        self._fp = target.open("a", encoding="utf-8")
        self._date = date_key
        update_latest_symlink(target)

    def write(self, now: _dt.datetime, channel: str, sender: str, message: str, target: str | None) -> None:
        self._maybe_roll(now)
        if self._fp is None:
            return
        self._fp.write(format_entry(now, channel, sender, message, target))
        self._fp.flush()
        if self._last_cleanup != now.date():
            cleanup_old_logs(now)
            self._last_cleanup = now.date()

    def close(self) -> None:
        if self._fp is not None:
            try:
                self._fp.close()
            finally:
                self._fp = None


def event_to_entry(event: dict) -> tuple[str, str, str, str | None] | None:
    """Map a parsed event dict to ``(channel, sender, message, target)``.
    Returns ``None`` for events we do not record."""
    event_type = event.get("type")
    raw_line = event.get("raw", "") or ""

    if event_type == "chat_message":
        sender = normalize_text(event.get("name", ""))
        message = normalize_text(event.get("message", ""))
        if not sender or not message:
            return None
        if sender.lower() in SENDER_BLOCKLIST:
            return None
        # Stock supervisor parser does not distinguish channel; classify
        # via the raw line if present so sayteam vs say is preserved.
        body = split_log_prefix(raw_line).lower()
        if body.startswith("sayteam") or body.startswith("teamsay"):
            channel = "TEAM"
        elif body.startswith("tell") or body.startswith("whisper"):
            channel = "WHISPER"
        else:
            channel = "PUBLIC"
        return channel, sender, message, None

    if event_type == "raw_line":
        return classify_raw_line(raw_line)

    return None


def parse_event_time(value) -> _dt.datetime:
    if isinstance(value, str) and value:
        try:
            return _dt.datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone()
        except ValueError:
            pass
    return _dt.datetime.now().astimezone()


def main(argv: list[str] | None = None) -> int:
    del argv  # unused
    ensure_directories()
    log(
        "starting event-driven chatlogger; reading NDJSON events from stdin "
        f"(keep_plain_days={KEEP_PLAIN_DAYS}, output_dir={CHATLOGS_DIR})"
    )

    writer = ChatWriter()

    def _shutdown(signum, frame) -> None:  # pylint: disable=unused-argument
        del signum, frame
        writer.close()
        sys.exit(0)

    signal.signal(signal.SIGINT, _shutdown)
    signal.signal(signal.SIGTERM, _shutdown)

    try:
        for line in sys.stdin:
            line = line.strip()
            if not line:
                continue
            try:
                event = json.loads(line)
            except json.JSONDecodeError as exc:
                log(f"warning: skipped malformed event: {exc}")
                continue
            entry = event_to_entry(event)
            if entry is None:
                continue
            channel, sender, message, target = entry
            now = parse_event_time(event.get("time"))
            writer.write(now, channel, sender, message, target)
    finally:
        writer.close()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
