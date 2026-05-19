#!/usr/bin/env python3
"""
Event-driven default helper: simple-only match announcer.

This addon is a tiny standalone consumer of the supervisor's NDJSON
event stream. It announces match start and match end in-game via one
 short RCON chat line for TFFA (gametype 6).

Activation rules (intentionally narrow):

    * Start in passive mode.
    * Watch ``raw_line`` events for a passed ``map_restart`` callvote
      (``@@@VOTEPASSED (map_restart``). When seen, arm the addon for
      a short internal timeout.
    * The next ``init_game`` event activates match tracking only if
       the addon is armed AND ``g_gametype`` parsed from the InitGame
       line is gametype 6.
    * After consuming the ``init_game``, the armed flag is cleared.
    * Ordinary mapchanges, server startup, timelimit map cycles and
      ShutdownGame never start a match on their own.

Match tracking:

    * State lives in memory only; no database, no persistent files.

End of match:

    * The primary summary trigger is the **team-score line**:
      a ``red:X  blue:Y`` raw_line. As soon as it arrives while a
      match is running and the summary has not already been sent,
      the summary fires immediately. We do **not** wait for any
      per-team ``(RED)/(BLUE) score:`` line, scoreboard broadcast,
      or ``ShutdownGame`` event — those used to introduce a 6 s
      delay in production when the follow-up lines were not
      delivered to the addon as ``raw_line`` events.
     * ``Exit: Timelimit hit.`` and ``Exit: Kill limit hit.`` are
       recognised only as a hint flag for the ``ShutdownGame``
      fallback; they are not required for the normal summary path.
    * As a fallback the final
      ``broadcast: print "...Score   Kills   Deaths..."`` scoreboard
      line also triggers the summary when no ``red:X  blue:Y`` line
      was observed.
    * ``ShutdownGame`` is a last-resort cleanup path; it sends the
      summary only if an Exit hint was received and no summary was
      already sent.
    * ``red:X  blue:Y`` lines provide the authoritative score for
      determining winner / draw.
    * After sending the summary, all in-memory match state is wiped
      and the addon returns to passive mode. A new match requires
      another passed ``map_restart`` callvote.

Output:

    Match start always emits exactly one configured line.
     Match end always emits exactly one configured line.

     TFFA end output resolves RED/BLUE/DRAW from ``red:X  blue:Y``.
     No roster, top-player, kill-count, late-join warning, or multi-line
     summary output exists.

The addon honours the same RCON resolution path as
``live-team-announcer.py``: it reads ``JKA_ADDON_CONFIG_JSON`` from
the environment (falling back to the central
``/home/container/config/jka-addons.json``) and resolves the server
port + RCON password from the runtime env / ``taystjk-effective.env``.
"""

from __future__ import annotations

import datetime as _dt
import json
import os
import re
import select
import shlex
import signal
import socket
import sys
import time
from pathlib import Path
from typing import Any, Callable

ADDON_LABEL = "[helper:match-announcer]"
HOME_DIR = Path(os.environ.get("JKA_MATCH_ANNOUNCER_HOME", "/home/container"))
ADDON_CONFIG_ENV = "JKA_ADDON_CONFIG_JSON"
ADDON_NAME_ENV = "JKA_ADDON_NAME"
ADDONS_CONFIG_PATH = Path(
    os.environ.get(
        "JKA_ADDONS_CONFIG_PATH",
        str(HOME_DIR / "config" / "jka-addons.json"),
    )
)
DEFAULT_ADDON_NAME = "match_announcer"
LOGS_DIR = HOME_DIR / "logs"
DEFAULT_LOG_PATH = LOGS_DIR / "bundled-match-announcer.log"
RUNTIME_ENV_PATH = HOME_DIR / ".runtime" / "taystjk-effective.env"

# Internal-only constants. Intentionally NOT exposed via the public
# JSON config so operators have a small, simple surface area.
MAX_SVSAY_LINE_LENGTH = 140       # safe upper bound; engine truncates ~150
MAX_LOG_TEMPLATE_LENGTH = 80      # avoid dumping long/user-provided templates in logs
LINE_DELAY_SECONDS = 0.03         # tiny gap to avoid command-buffer truncation
MAP_RESTART_ARM_SECONDS = 30      # how long an armed map_restart vote stays valid
MATCH_START_DELAY_SECONDS = 2.0   # delay between InitGame and "Match Started"
                                  # so that live-team-announcer.py can finish
                                  # its team-join lines before this addon sends
                                  # its single start line.

ALLOWED_ANNOUNCE_COMMANDS = ("svsay", "say")

_CONTROL_CHAR_RE = re.compile(r"[\x00-\x1f\x7f]")
_VOTEPASSED_RE = re.compile(r"@@@VOTEPASSED\s*\(\s*map_restart\b", re.IGNORECASE)
_GAMETYPE_RE = re.compile(r"\\g_gametype\\(\d+)")
_EXIT_TIMELIMIT_RE = re.compile(r"Exit:\s+Timelimit hit\.")
_EXIT_KILLLIMIT_RE = re.compile(r"Exit:\s+Kill limit hit\.")
_SCORE_RE = re.compile(r"\bred:\s*(\d+)\s+blue:\s*(\d+)\b", re.IGNORECASE)
# Final scoreboard broadcast: must contain all of these markers on the same raw_line.
# "Dmg Taken" is present in TaystJK's scoreboard output and makes the pattern
# more specific to avoid false matches on other broadcast lines.
# Markers are checked against a whitespace-normalised copy of the raw line so
# variable spacing or escaped/actual newlines do not break detection.
_SCOREBOARD_MARKERS = ("broadcast: print", "Score", "Kills", "Deaths", "Dmg Given", "Dmg Taken", "Name")
_SCOREBOARD_WHITESPACE_RE = re.compile(r"(?:\\n|\\r|\\t|\s)+")

DEFAULT_CONFIG: dict[str, Any] = {
    "enabled": False,
    "rcon_host": "127.0.0.1",
    "rcon_timeout_seconds": 3,
    "announce_command": "svsay",
    "match_start_command": "svsay",
    "match_end_command": "svsay",
    "log_file": str(DEFAULT_LOG_PATH),
    "gametype": 6,
    "require_map_restart_vote": True,
    "messages": {
        "tffa_start": "^3Match Started ^7- ^2Good Luck, Have Fun!",
        "tffa_end_red": "^3Match Ended ^7- ^1RED TEAM wins.",
        "tffa_end_blue": "^3Match Ended ^7- ^4BLUE TEAM wins.",
        "tffa_end_draw": "^3Match Ended ^7- ^7Draw.",
    },
}


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


def parse_configured_gametypes(config: dict[str, Any]) -> list[int]:
    """Parse configured gametypes, allowing only TFFA/gametype 6.

    ``gametypes`` remains accepted for backward compatibility but all
    values except 6 are ignored. Defaults to 6 only when neither
    ``gametype`` nor ``gametypes`` was explicitly configured.
    Returns an empty list when the operator explicitly configured only
    unsupported values, which means no gametype should activate.
    """
    if "gametypes" in config:
        raw: Any = config.get("gametypes")
    elif "gametype" in config:
        raw = config.get("gametype")
    else:
        raw = 6

    candidates: list[Any]
    if isinstance(raw, (list, tuple, set)):
        candidates = list(raw)
    elif isinstance(raw, str):
        candidates = [part.strip() for part in raw.split(",")]
    else:
        candidates = [raw]

    parsed: list[int] = []
    seen: set[int] = set()
    for item in candidates:
        try:
            value = int(item)
        except (TypeError, ValueError):
            log(f"ignoring invalid gametype value: {item!r}")
            continue
        if value != 6:
            log(f"ignoring unsupported gametype value: {item!r}")
            continue
        if value in seen:
            continue
        parsed.append(value)
        seen.add(value)
    return parsed


DEFAULT_MESSAGES: dict[str, str] = {
    str(k): v
    for k, v in DEFAULT_CONFIG["messages"].items()
    if isinstance(v, str)
}


def _default_messages() -> dict[str, str]:
    return dict(DEFAULT_MESSAGES)


def normalize_messages(value: Any) -> dict[str, str]:
    messages = _default_messages()
    if isinstance(value, dict):
        for key, raw in value.items():
            if isinstance(raw, str):
                messages[str(key)] = raw
    return messages


def _load_section_from_addons_file(name: str) -> dict[str, Any] | None:
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
    gametypes = parse_configured_gametypes(config)
    config["gametypes"] = gametypes
    # Preserve a legacy scalar for log output / callers that inspect the loaded config.
    # Keep the legacy scalar as a valid TFFA integer for callers that inspect it.
    # This may intentionally differ from an empty `gametypes` list: an empty list
    # means the operator explicitly configured no supported gametype (for example
    # `gametypes: [3]`), and activation is controlled only by `gametypes`.
    config["gametype"] = gametypes[0] if gametypes else 6
    config["require_map_restart_vote"] = safe_bool(
        config.get("require_map_restart_vote", True), True
    )
    config["messages"] = normalize_messages(config.get("messages"))

    def _safe_command(value: Any, fallback: str) -> str:
        candidate = str(value or "").strip().lower()
        if candidate in ALLOWED_ANNOUNCE_COMMANDS:
            return candidate
        return fallback

    raw_announce = config.get("announce_command", "svsay")
    announce = _safe_command(raw_announce, "svsay")
    if announce != str(raw_announce or "").strip().lower():
        log(f"announce_command={raw_announce!r} not allowed; using svsay")
    config["announce_command"] = announce

    # Per-group commands: missing/invalid → fall back to announce_command.
    raw_start = config.get("match_start_command")
    if raw_start is None or str(raw_start).strip() == "":
        config["match_start_command"] = announce
    else:
        validated = _safe_command(raw_start, announce)
        if validated != str(raw_start).strip().lower():
            log(f"match_start_command={raw_start!r} not allowed; using {announce!r}")
        config["match_start_command"] = validated

    raw_end = config.get("match_end_command")
    if raw_end is None or str(raw_end).strip() == "":
        config["match_end_command"] = announce
    else:
        validated = _safe_command(raw_end, announce)
        if validated != str(raw_end).strip().lower():
            log(f"match_end_command={raw_end!r} not allowed; using {announce!r}")
        config["match_end_command"] = validated

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


def sanitize_text(value: str) -> str:
    """Strip control characters; never alter colour codes (^N)."""
    return _CONTROL_CHAR_RE.sub("", value or "").strip()


def quote_rcon_arg(value: str) -> str:
    """Wrap *value* in double quotes, escaping any backslashes and double quotes."""
    escaped = value.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def parse_gametype(raw_init_game_line: str) -> int | None:
    """Return the integer ``g_gametype`` from an InitGame line, or None."""
    if not raw_init_game_line:
        return None
    match = _GAMETYPE_RE.search(raw_init_game_line)
    if match is None:
        return None
    try:
        return int(match.group(1))
    except ValueError:
        return None


def is_final_scoreboard_broadcast(raw_line: str) -> bool:
    if not raw_line:
        return False
    # Collapse runs of whitespace and literal "\n"/"\r"/"\t" escapes so a marker
    # like "Dmg Given" still matches when the engine writes them with extra
    # spacing or with escaped newlines embedded in the broadcast payload.
    normalised = _SCOREBOARD_WHITESPACE_RE.sub(" ", raw_line)
    return all(marker in normalised for marker in _SCOREBOARD_MARKERS)


class MatchAnnouncer:
    """In-memory match state machine.

    Decoupled from RCON I/O so unit tests can drive it directly. The
    ``send`` callable receives one short string per svsay line.
    """

    def __init__(
        self,
        config: dict[str, Any],
        send: Callable[..., None],
        clock: Callable[[], float] = time.monotonic,
    ) -> None:
        self.config = dict(config)
        # MatchAnnouncer can be constructed directly by tests or other
        # in-process callers, bypassing load_config(), so normalize the
        # operator-facing fields defensively here too.
        self.config["gametypes"] = parse_configured_gametypes(self.config)
        # Activation uses only this filtered list. The scalar is kept as valid
        # TFFA metadata even when the list is empty due to explicit unsupported
        # operator config such as `gametypes: [3]`.
        self.config["gametype"] = self.config["gametypes"][0] if self.config["gametypes"] else 6
        self.gametypes = self.config["gametypes"]
        self.config["messages"] = normalize_messages(self.config.get("messages"))
        self._send = send
        self._clock = clock
        self.reset_all()

    # ----- state helpers ------------------------------------------------

    def reset_all(self) -> None:
        self.armed = False
        self.armed_at: float | None = None
        self._reset_match()

    def _reset_match(self) -> None:
        self.match_started = False
        self.match_ending = False
        self.summary_sent = False
        self.match_start_sent = False
        self.pending_match_start = False
        self.pending_match_start_at: float | None = None
        self.final_red_score: int | None = None
        self.final_blue_score: int | None = None
        self.current_gametype: int | None = None

    def _is_armed_now(self) -> bool:
        if not self.armed:
            return False
        if self.armed_at is None:
            return True
        return (self._clock() - self.armed_at) <= MAP_RESTART_ARM_SECONDS

    # ----- event ingestion ---------------------------------------------

    def handle_event(self, event: dict) -> None:
        if not isinstance(event, dict):
            return
        etype = event.get("type")
        if etype == "raw_line":
            self._on_raw_line(str(event.get("raw") or ""))
        elif etype == "init_game":
            self._on_init_game(str(event.get("raw") or ""))
        elif etype == "shutdown_game":
            self._on_shutdown_game()
        elif etype == "team_change":
            self._on_team_change(event)
        # After every event, give the deferred match-start a chance to
        # fire if its 2 s window has elapsed.
        self._maybe_send_pending_match_start()

    def tick(self) -> None:
        """Wall-clock pump for the deferred match-start timer.

        The main I/O loop calls this whenever ``select`` times out so
        the pending ``Match Started`` message still fires even if no
        further events arrive within the 2 s window. The end-of-match
        summary is **not** time-driven; it fires synchronously from
        ``red:X blue:Y`` (or the scoreboard / ShutdownGame fallback).
        """
        self._maybe_send_pending_match_start()

    def has_pending_match_start(self) -> bool:
        return self.pending_match_start

    def _on_raw_line(self, raw: str) -> None:
        if not raw:
            return

        # 1. Passed map_restart vote arms the addon (passive -> armed).
        if _VOTEPASSED_RE.search(raw):
            self.armed = True
            self.armed_at = self._clock()
            return

        # The remaining handlers only matter once a match is running.
        if not self.match_started:
            return

        # 2. End-of-match exit lines — hint only; no longer required for summary.
        if _EXIT_TIMELIMIT_RE.search(raw) or _EXIT_KILLLIMIT_RE.search(raw):
            self.match_ending = True
            return

        # 3. Team score line (red:X  blue:Y) — primary immediate trigger.
        #    As soon as we see this while a match is running and the
        #    summary has not been sent, fire the summary synchronously.
        #    We do not wait for any per-team player-score line, scoreboard
        #    broadcast, or ShutdownGame — those used to introduce a 6 s
        #    delay in production when the follow-up lines did not reach
        #    the addon as raw_line events.
        score_match = _SCORE_RE.search(raw)
        if score_match is not None:
            try:
                self.final_red_score = int(score_match.group(1))
                self.final_blue_score = int(score_match.group(2))
            except ValueError:
                pass
            if not self.summary_sent:
                self._send_summary_and_reset()
            return

        # 4. Scoreboard broadcast — fallback trigger when no red:X blue:Y
        #    line was observed (the team-score line is the primary path).
        if not self.summary_sent and is_final_scoreboard_broadcast(raw):
            self._send_summary_and_reset()
            return

    def _on_init_game(self, raw: str) -> None:
        gametype = parse_gametype(raw)
        require_vote = bool(self.config.get("require_map_restart_vote", True))
        # Ignore InitGame for an unrelated gametype regardless of state.
        if gametype is None or gametype not in self.gametypes:
            self.armed = False
            self.armed_at = None
            return

        # If we still require a passed vote, only activate when armed.
        if require_vote and not self._is_armed_now():
            # Stay passive; clear any stale arming flag.
            self.armed = False
            self.armed_at = None
            return

        # Activate match tracking. Always start from a clean slate.
        self._reset_match()
        self.current_gametype = gametype
        self.match_started = True
        # Consume the armed flag.
        self.armed = False
        self.armed_at = None

        # Defer the "Match Started" announcement so that
        # live-team-announcer.py can finish printing its team-join
        # lines before this addon emits its single start line.
        # The actual emit happens in tick() / _maybe_send_pending_match_start()
        # once the window elapses.
        self.pending_match_start = True
        self.pending_match_start_at = self._clock() + MATCH_START_DELAY_SECONDS

    def _on_shutdown_game(self) -> None:
        # ShutdownGame is only a fallback. Do nothing if we already
        # sent the summary or if we are still passive.
        if not self.match_started:
            return
        if self.summary_sent:
            self._reset_match()
            return
        if self.match_ending:
            self._send_summary_and_reset()
        # Otherwise: ignore. ShutdownGame on its own (e.g. the
        # ShutdownGame that accompanies the map_restart itself) must
        # not produce a fake "Match Ended" announcement.

    def _on_team_change(self, _event: dict) -> None:
        pass

    # ----- output ------------------------------------------------------

    def _command_for_group(self, group: str) -> str:
        if group == "start":
            return self.config.get("match_start_command") or self.config.get("announce_command", "svsay")
        # "end" and any other group default to the end command.
        return self.config.get("match_end_command") or self.config.get("announce_command", "svsay")

    def _emit(self, line: str, group: str = "end") -> None:
        line = line.strip()
        if not line:
            return
        if len(line) > MAX_SVSAY_LINE_LENGTH:
            line = line[:MAX_SVSAY_LINE_LENGTH]
        command = self._command_for_group(group)
        try:
            self._send(line, command)
        except TypeError:
            # Backward-compatible: legacy senders (and most unit tests) use a
            # single-argument callable that only captures the rendered line.
            self._send(line)

    def _maybe_send_pending_match_start(self) -> None:
        if not self.pending_match_start:
            return
        if self.pending_match_start_at is not None and self._clock() < self.pending_match_start_at:
            return
        # Clear the pending state *before* emitting so a re-entrant
        # send (e.g. from RCON failure logging) cannot fire twice.
        self.pending_match_start = False
        self.pending_match_start_at = None
        if self.match_start_sent:
            return
        self.match_start_sent = True
        self._send_match_start()

    def _send_match_start(self) -> None:
        self._emit(self._render_message("tffa_start"), group="start")

    # ----- score-block helpers ------------------------------------------

    # The end-of-match summary is fired synchronously from `_on_raw_line`
    # as soon as a `red:X blue:Y` line (or the scoreboard fallback) is
    # observed. There is no time-driven collection window: the supervisor's
    # event delivery did not always pump per-team `(RED)/(BLUE) score:`
    # lines as `raw_line` events, which previously delayed the summary
    # until the `ShutdownGame` fallback ~6 s later.

    def _format_template(self, key: str, template: str, values: dict[str, Any]) -> str | None:
        try:
            return template.format(**values)
        except (KeyError, IndexError, ValueError) as exc:
            safe_template = template[:MAX_LOG_TEMPLATE_LENGTH]
            if len(template) > MAX_LOG_TEMPLATE_LENGTH:
                safe_template += "..."
            value_keys = sorted(str(k) for k in values)
            log(
                f"message template {key!r} format failed: {exc}; "
                f"template={safe_template!r}, value_keys={value_keys!r}; using default"
            )
            return None

    def _render_message(self, key: str, **values: Any) -> str:
        messages = self.config.get("messages")
        defaults = _default_messages()
        template = defaults.get(key, "")
        if isinstance(messages, dict) and isinstance(messages.get(key), str):
            template = messages[key]
        rendered = self._format_template(key, template, values)
        if rendered is None:
            rendered = self._format_template(key, defaults.get(key, ""), values)
        if rendered is None:
            rendered = defaults.get(key, "")
        return sanitize_text(rendered)

    def _send_summary_and_reset(self) -> None:
        if self.summary_sent:
            return
        self.summary_sent = True

        red = self.final_red_score
        blue = self.final_blue_score
        values = {
            "red_score": red if red is not None else "",
            "blue_score": blue if blue is not None else "",
        }
        if red is not None and blue is not None and red > blue:
            line = self._render_message("tffa_end_red", **values)
        elif red is not None and blue is not None and blue > red:
            line = self._render_message("tffa_end_blue", **values)
        else:
            line = self._render_message("tffa_end_draw", **values)

        self._emit(line, group="end")

        # Wipe match state. armed is already False at this point; the
        # next match strictly requires another passed map_restart vote.
        self._reset_match()


def _make_rcon_sender(config: dict[str, Any]) -> Callable[..., None]:
    """Build a sender that issues one RCON command (svsay/say) per call.

    The MatchAnnouncer passes the chosen command (per ``match_start_command``
    / ``match_end_command``) as a second positional argument. If omitted we
    fall back to ``announce_command`` so legacy callers still work.
    """
    last_send_at = [0.0]

    def _send(line: str, command: str | None = None) -> None:
        password = effective_rcon_password()
        if not password:
            log("rcon password unavailable; skipping svsay line")
            return
        port = current_server_port()
        chosen_command = (command or config.get("announce_command") or "svsay").strip().lower()
        if chosen_command not in ALLOWED_ANNOUNCE_COMMANDS:
            chosen_command = "svsay"
        rcon_command = f"{chosen_command} {quote_rcon_arg(line)}"
        # Tiny inter-line delay to avoid command-buffer truncation.
        now = time.monotonic()
        delta = now - last_send_at[0]
        if delta < LINE_DELAY_SECONDS:
            time.sleep(LINE_DELAY_SECONDS - delta)
        try:
            send_rcon_command(
                config["rcon_host"],
                port,
                password,
                config["rcon_timeout_seconds"],
                rcon_command,
            )
            log(f"{chosen_command}: {line}")
        except OSError as exc:
            log(f"rcon send failed: {exc}")
        last_send_at[0] = time.monotonic()

    return _send


def main(argv: list[str] | None = None) -> int:
    del argv
    global _log_handle  # noqa: PLW0603

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

    log(
        "starting event-driven match announcer; reading NDJSON events from stdin "
        f"(gametypes={config['gametypes']}, require_map_restart_vote={config['require_map_restart_vote']})"
    )

    announcer = MatchAnnouncer(config, _make_rcon_sender(config))

    def _shutdown(_signum, _frame) -> None:
        log("shutting down on signal")
        sys.exit(0)

    signal.signal(signal.SIGINT, _shutdown)
    signal.signal(signal.SIGTERM, _shutdown)

    # Non-blocking event loop: poll stdin with a short timeout so the
    # deferred match-start timer fires even if no further events
    # arrive within the 2 s window.
    eof = False
    while not eof:
        try:
            rlist, _, _ = select.select([sys.stdin], [], [], 0.25)
        except (OSError, ValueError):
            break
        if rlist:
            raw = sys.stdin.readline()
            if raw == "":
                eof = True
            else:
                line = raw.strip()
                if line:
                    try:
                        event = json.loads(line)
                    except json.JSONDecodeError as exc:
                        log(f"warning: skipped malformed event: {exc}")
                        event = None
                    if event is not None:
                        announcer.handle_event(event)
        else:
            announcer.tick()

    # Flush any pending match-start that has not yet fired so the EOF
    # path (e.g. tests, supervisor shutdown immediately after a fresh
    # match start) still produces the announcement.
    flush_deadline = time.monotonic() + MATCH_START_DELAY_SECONDS + 0.5
    while announcer.has_pending_match_start() and time.monotonic() < flush_deadline:
        time.sleep(0.05)
        announcer.tick()

    log("stdin closed; exiting")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
