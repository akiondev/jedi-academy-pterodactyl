#!/usr/bin/env python3
"""Tests for the bundled simple-only match-announcer event addon."""

from __future__ import annotations

import importlib.util
import json
import os
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
ADDON_PATH = REPO_ROOT / "bundled-addons" / "defaults" / "match-announcer.py"


def _load_module():
    spec = importlib.util.spec_from_file_location("match_announcer_under_test", ADDON_PATH)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


MA = _load_module()


def _default_config(**overrides):
    cfg = {
        "enabled": True,
        "rcon_host": "127.0.0.1",
        "rcon_timeout_seconds": 1,
        "announce_command": "svsay",
        "match_start_command": "svsay",
        "match_end_command": "svsay",
        "gametype": 6,
        "require_map_restart_vote": True,
        "log_file": "/tmp/match-announcer-test.log",
    }
    cfg.update(overrides)
    return cfg


class _Recorder:
    def __init__(self) -> None:
        self.entries: list[tuple[str, str | None]] = []

    def __call__(self, line: str, command: str | None = None) -> None:
        self.entries.append((line, command))

    @property
    def lines(self) -> list[str]:
        return [line for line, _ in self.entries]


class _Clock:
    def __init__(self, t: float = 0.0) -> None:
        self.t = t

    def __call__(self) -> float:
        return self.t

    def advance(self, seconds: float) -> None:
        self.t += seconds


def _raw(text: str) -> dict:
    return {"type": "raw_line", "raw": text}


def _init_game(gametype: int = 6) -> dict:
    return {
        "type": "init_game",
        "raw": rf"InitGame: \version\TaystJK\g_gametype\{gametype}\sv_hostname\test",
    }


def _team_change(name: str, new_team: str, slot: str = "0", old_team: str = "FREE") -> dict:
    return {"type": "team_change", "slot": slot, "name": name, "old_team": old_team, "new_team": new_team}


def _kill_line(killer: int, victim: int, killer_name: str, victim_name: str, mod: str = "MOD_SABER") -> dict:
    return _raw(f"Kill: {killer} {victim} 3: {killer_name} killed {victim_name} by {mod}")


VOTEPASSED_LINE = 'broadcast: print "@@@VOTEPASSED (map_restart 5), command will be executed in 3 seconds.\\n"'
SCOREBOARD_LINE = (
    'broadcast: print "\\nScore   Kills   Deaths   Net   Dmg Given   Dmg Taken   Net Dmg   '
    'Dmg/Death   Team Dmg   Time   Name   \\n0       0       1        -1    76          130         '
    '-54       76          0          0      Akion\\n\\n"'
)


class MatchAnnouncerSimpleOnlyTest(unittest.TestCase):
    def setUp(self) -> None:
        self.rec = _Recorder()
        self.clock = _Clock()
        self.ann = MA.MatchAnnouncer(_default_config(), self.rec, clock=self.clock)

    def _trigger_match_start_sequence(self, gametype: int = 6, config: dict | None = None) -> None:
        if config is not None:
            self.rec = _Recorder()
            self.clock = _Clock()
            self.ann = MA.MatchAnnouncer(config, self.rec, clock=self.clock)
        self.ann.handle_event(_raw(VOTEPASSED_LINE))
        self.ann.handle_event(_init_game(gametype))
        self.clock.advance(MA.MATCH_START_DELAY_SECONDS + 0.01)
        self.ann.tick()

    def _activate(self, gametype: int = 6, config: dict | None = None) -> None:
        self._trigger_match_start_sequence(gametype, config)
        self.assertTrue(self.ann.match_started)

    def _end_lines(self, *events: dict) -> list[str]:
        self.rec.entries.clear()
        for event in events:
            self.ann.handle_event(event)
        return self.rec.lines

    def test_tffa_start_sends_exactly_one_line(self) -> None:
        self._activate(6)
        self.assertEqual(self.rec.lines, ["^3Match Started ^7- ^2Good Luck, Have Fun!"])

    def test_tffa_end_red_winner_sends_exactly_one_line(self) -> None:
        self._activate(6)
        self.assertEqual(self._end_lines(_raw("red:10  blue:7")), ["^3Match Ended ^7- ^1RED TEAM wins."])

    def test_tffa_end_blue_winner_sends_exactly_one_line(self) -> None:
        self._activate(6)
        self.assertEqual(self._end_lines(_raw("red:7  blue:10")), ["^3Match Ended ^7- ^4BLUE TEAM wins."])

    def test_tffa_end_draw_sends_exactly_one_line(self) -> None:
        self._activate(6)
        self.assertEqual(self._end_lines(_raw("red:10  blue:10")), ["^3Match Ended ^7- ^7Draw."])

    def test_duel_start_does_not_trigger_match_start(self) -> None:
        self._trigger_match_start_sequence(3)
        self.assertFalse(self.ann.match_started)
        self.assertEqual(self.rec.lines, [])

    def test_duel_end_does_not_trigger_match_end(self) -> None:
        self._trigger_match_start_sequence(3)
        self.ann.handle_event(_raw("red:4  blue:1"))
        self.assertFalse(self.ann.match_started)
        self.assertEqual(self.rec.lines, [])

    def test_gametypes_array_ignores_duel_and_accepts_tffa(self) -> None:
        config = _default_config(gametypes=[6, 3])
        config.pop("gametype")
        self._activate(6, config)
        self.assertEqual(self.ann.config["gametypes"], [6])
        self.assertEqual(self.ann.config["gametype"], 6)
        self.assertEqual(self.rec.lines, ["^3Match Started ^7- ^2Good Luck, Have Fun!"])
        self.rec.entries.clear()
        self.ann.reset_all()
        self._trigger_match_start_sequence(3, config)
        self.assertFalse(self.ann.match_started)
        self.assertEqual(self.rec.lines, [])

    def test_gametypes_array_with_only_duel_disables_match_announcer_for_duel(self) -> None:
        config = _default_config(gametypes=[3])
        config.pop("gametype")
        self._trigger_match_start_sequence(3, config)
        self.assertEqual(self.ann.config["gametypes"], [])
        self.assertEqual(self.ann.config["gametype"], 6)
        self.assertFalse(self.ann.match_started)
        self.assertEqual(self.rec.lines, [])
        self.ann.handle_event(_raw("red:1  blue:0"))
        self.assertEqual(self.rec.lines, [])

    def test_legacy_gametype_scalar_still_works(self) -> None:
        config = _default_config(gametype=6)
        self._activate(6, config)
        self.assertEqual(self.ann.config["gametypes"], [6])
        self.assertEqual(self.ann.config["gametype"], 6)
        self.assertEqual(self.rec.lines, ["^3Match Started ^7- ^2Good Luck, Have Fun!"])

    def test_simple_mode_false_does_not_restore_multiline_output(self) -> None:
        config = _default_config(simple_mode=False)
        self._activate(6, config)
        self.assertEqual(self.rec.lines, ["^3Match Started ^7- ^2Good Luck, Have Fun!"])
        self.assertEqual(self._end_lines(_raw("red:1  blue:0")), ["^3Match Ended ^7- ^1RED TEAM wins."])

    def test_custom_messages_from_json_are_used(self) -> None:
        config = _default_config(messages={
            "tffa_start": "Custom start",
            "tffa_end_red": "Custom red {red_score}-{blue_score}",
            "duel_start": "Custom duel start",
            "duel_end_winner": "Custom winner {winner}",
        })
        self._activate(6, config)
        self.assertEqual(self.rec.lines, ["Custom start"])
        self.assertEqual(self._end_lines(_raw("red:2  blue:1")), ["Custom red 2-1"])
        output = "\n".join(self.rec.lines)
        self.assertNotIn("Custom duel", output)
        self.assertNotIn("{winner}", output)

    def test_each_match_start_and_end_produces_exactly_one_rcon_message_line(self) -> None:
        self._activate(6)
        self.assertEqual(len(self.rec.lines), 1)
        self.rec.entries.clear()
        self.ann.handle_event(_raw("red:1  blue:0"))
        self.assertEqual(len(self.rec.lines), 1)

    def test_no_messages_between_start_and_end(self) -> None:
        self._activate(6)
        self.rec.entries.clear()
        self.ann.handle_event(_team_change("MidMatchJoiner", "BLUE", slot="9"))
        self.ann.handle_event(_kill_line(9, 0, "MidMatchJoiner", "Akion"))
        self.ann.handle_event({"type": "client_connect", "slot": "9", "name": "MidMatchJoiner", "ip": "127.0.0.1"})
        self.ann.handle_event({
            "type": "client_userinfo_changed",
            "slot": "9",
            "name": "MidMatchJoiner",
            "ip": "127.0.0.1",
        })
        self.assertTrue(self.ann.match_started)
        self.assertEqual(self.rec.lines, [])

    def test_client_connect_and_userinfo_during_active_match_send_no_rcon_message(self) -> None:
        self._activate(6)
        self.rec.entries.clear()
        self.ann.handle_event({"type": "client_connect", "slot": "1", "name": "Akion", "ip": "127.0.0.1"})
        self.ann.handle_event({"type": "client_userinfo_changed", "slot": "1", "name": "Akion", "ip": "127.0.0.1"})
        self.assertEqual(self.rec.lines, [])

    def test_non_configured_gametype_is_ignored(self) -> None:
        self.ann = MA.MatchAnnouncer(_default_config(gametypes=[6]), self.rec, clock=self.clock)
        self.ann.handle_event(_raw(VOTEPASSED_LINE))
        self.ann.handle_event(_init_game(3))
        self.clock.advance(MA.MATCH_START_DELAY_SECONDS + 0.01)
        self.ann.tick()
        self.assertEqual(self.rec.lines, [])

    def test_no_output_contains_duel_winner_template_or_player_winner(self) -> None:
        config = _default_config(messages={
            "tffa_start": "Custom start",
            "tffa_end_blue": "Custom blue",
            "duel_start": "Duel Started",
            "duel_end_winner": "Duel Ended {winner}",
        })
        self._activate(6, config)
        self.ann.handle_event(_raw('(BLUE) score: 9  ping: 12  client: [TESTGUID0000] 0 "Akion"'))
        self.ann.handle_event(_kill_line(0, 1, "Akion", "Bot1"))
        self.ann.handle_event(_raw("red:1  blue:2"))
        output = "\n".join(self.rec.lines)
        self.assertNotIn("Duel Ended", output)
        self.assertNotIn("{winner}", output)
        self.assertNotIn("Akion", output)

    def test_scoreboard_fallback_still_sends_one_end_line(self) -> None:
        self._activate(6)
        self.assertEqual(self._end_lines(_raw(SCOREBOARD_LINE)), ["^3Match Ended ^7- ^7Draw."])


class ConfigParsingTest(unittest.TestCase):
    def test_parse_configured_gametypes_allows_only_tffa(self) -> None:
        self.assertEqual(MA.parse_configured_gametypes({}), [6])
        self.assertEqual(MA.parse_configured_gametypes({"gametypes": [6, "3", "bad", -1]}), [6])
        self.assertEqual(MA.parse_configured_gametypes({"gametypes": "6, 3, bad"}), [6])
        self.assertEqual(MA.parse_configured_gametypes({"gametype": 0}), [])
        self.assertEqual(MA.parse_configured_gametypes({"gametype": 6}), [6])
        self.assertEqual(MA.parse_configured_gametypes({"gametype": "3"}), [])
        self.assertEqual(MA.parse_configured_gametypes({"gametypes": [3], "gametype": 6}), [])
        self.assertEqual(MA.parse_configured_gametypes({"gametypes": ["bad"]}), [])
        self.assertEqual(MA.parse_configured_gametypes({"gametypes": []}), [])

    def test_load_config_normalizes_messages_and_commands(self) -> None:
        os.environ["JKA_ADDON_CONFIG_JSON"] = json.dumps({
            "enabled": True,
            "announce_command": "say",
            "match_start_command": "rcon",
            "match_end_command": "exec",
            "gametypes": "6,3",
            "messages": {"tffa_start": "Custom", "duel_end_winner": 123},
            "log_file": "/tmp/match-announcer-fallback-test.log",
        })
        try:
            cfg = MA.load_config()
        finally:
            del os.environ["JKA_ADDON_CONFIG_JSON"]
        self.assertEqual(cfg["announce_command"], "say")
        self.assertEqual(cfg["match_start_command"], "say")
        self.assertEqual(cfg["match_end_command"], "say")
        self.assertEqual(cfg["gametypes"], [6])
        self.assertEqual(cfg["gametype"], 6)
        self.assertEqual(cfg["messages"]["tffa_start"], "Custom")
        self.assertNotIn("duel_end_winner", cfg["messages"])

    def test_missing_and_malformed_message_templates_fall_back_to_defaults(self) -> None:
        rec = _Recorder()
        clock = _Clock()
        ann = MA.MatchAnnouncer(_default_config(messages={"tffa_end_red": "broken {missing}"}), rec, clock=clock)
        ann.handle_event(_raw(VOTEPASSED_LINE))
        ann.handle_event(_init_game(6))
        clock.advance(MA.MATCH_START_DELAY_SECONDS + 0.01)
        ann.tick()
        rec.entries.clear()
        ann.handle_event(_raw("red:1  blue:0"))
        self.assertEqual(rec.lines, ["^3Match Ended ^7- ^1RED TEAM wins."])


class ParsingAndSanitizationTest(unittest.TestCase):
    def test_scoreboard_detection_tolerates_whitespace(self) -> None:
        self.assertTrue(MA.is_final_scoreboard_broadcast(SCOREBOARD_LINE))
        self.assertTrue(MA.is_final_scoreboard_broadcast('broadcast: print "Score\tKills\tDeaths\tDmg Given\tDmg Taken\tName"'))
        self.assertFalse(MA.is_final_scoreboard_broadcast('broadcast: print "Hello world"'))


class _UDPServer:
    def __init__(self) -> None:
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.sock.bind(("127.0.0.1", 0))
        self.sock.settimeout(5.0)
        self.received: list[bytes] = []
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    @property
    def port(self) -> int:
        return self.sock.getsockname()[1]

    def _run(self) -> None:
        while not self._stop.is_set():
            try:
                data, _ = self.sock.recvfrom(65535)
            except OSError:
                return
            self.received.append(data)

    def close(self) -> None:
        self._stop.set()
        try:
            self.sock.close()
        except OSError:
            pass
        self._thread.join(timeout=1.0)


class MatchAnnouncerSubprocessTest(unittest.TestCase):
    def _run_addon(self, config: dict, env_overrides: dict, stdin_payload: str, timeout: float = 10.0):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            home_dir = tmp_path / "home" / "container"
            (home_dir / "logs").mkdir(parents=True)
            (home_dir / ".runtime").mkdir(parents=True)
            sandbox_addon = tmp_path / "match-announcer.py"
            sandbox_addon.write_text(ADDON_PATH.read_text(encoding="utf-8"), encoding="utf-8")
            env = os.environ.copy()
            env.update({
                "HOME": str(home_dir),
                "JKA_MATCH_ANNOUNCER_HOME": str(home_dir),
                "JKA_ADDON_NAME": "match_announcer",
                "JKA_ADDON_CONFIG_JSON": json.dumps(config),
            })
            env.update(env_overrides)
            return subprocess.run(
                [sys.executable, str(sandbox_addon)],
                input=stdin_payload,
                env=env,
                text=True,
                capture_output=True,
                timeout=timeout,
            )

    def test_disabled_addon_exits_immediately(self) -> None:
        proc = self._run_addon(config={"enabled": False}, env_overrides={}, stdin_payload="")
        self.assertEqual(proc.returncode, 0)
        self.assertIn("disabled", proc.stderr)

    def test_rcon_payload_is_quoted_safely_and_start_is_one_datagram(self) -> None:
        server = _UDPServer()
        try:
            payload_events = [
                {"type": "raw_line", "raw": VOTEPASSED_LINE},
                {"type": "init_game", "raw": r"InitGame: \version\TaystJK\g_gametype\6\sv_hostname\test"},
                {"type": "team_change", "slot": "0", "name": 'Aki"on', "old_team": "FREE", "new_team": "BLUE"},
            ]
            stdin_payload = "".join(json.dumps(ev) + "\n" for ev in payload_events)
            proc = self._run_addon(
                config=_default_config(messages={"tffa_start": 'Match "Started" only'}),
                env_overrides={
                    "TAYSTJK_EFFECTIVE_SERVER_PORT": str(server.port),
                    "TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD": "secret",
                },
                stdin_payload=stdin_payload,
                timeout=15.0,
            )
            for _ in range(80):
                if len(server.received) >= 1:
                    break
                time.sleep(0.05)
            self.assertEqual(proc.returncode, 0, msg=proc.stderr)
            self.assertEqual(len(server.received), 1, msg=f"expected one datagram; got {len(server.received)}")
            decoded = server.received[0].lstrip(b"\xff").decode("utf-8", errors="replace")
            self.assertTrue(decoded.startswith("rcon secret svsay "), msg=decoded)
            arg = decoded[len("rcon secret svsay "):]
            self.assertTrue(arg.startswith('"') and arg.endswith('"'), msg=arg)
            self.assertIn('Match \\"Started\\" only', decoded)
        finally:
            server.close()


if __name__ == "__main__":
    unittest.main(verbosity=2)
