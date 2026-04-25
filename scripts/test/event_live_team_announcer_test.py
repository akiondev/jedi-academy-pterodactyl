#!/usr/bin/env python3
"""
Smoke test for the event-driven live team announcer addon.

Verifies that:
  * the addon refuses to run unless its config has enabled=true
  * the addon parses team_change NDJSON events from stdin
  * the addon never tails server.log or the live-output mirror
"""

from __future__ import annotations

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


REPO_ROOT = Path(__file__).resolve().parent.parent.parent
ADDON_PATH = REPO_ROOT / "bundled-addons" / "defaults" / "events" / "30-live-team-announcer.py"


class _UDPServer:
    """Tiny UDP listener that records the first datagram received."""

    def __init__(self) -> None:
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.sock.bind(("127.0.0.1", 0))
        self.sock.settimeout(5.0)
        self.received: bytes | None = None
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    @property
    def port(self) -> int:
        return self.sock.getsockname()[1]

    def _run(self) -> None:
        try:
            data, _ = self.sock.recvfrom(65535)
            self.received = data
        except OSError:
            pass

    def close(self) -> None:
        self.sock.close()
        self._thread.join(timeout=1.0)


class LiveTeamAnnouncerTest(unittest.TestCase):
    def _run_addon(self, config: dict, env_overrides: dict, stdin_payload: str, timeout: float = 10.0) -> tuple[int, str, str, Path]:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            home_dir = tmp_path / "home" / "container"
            (home_dir / "logs").mkdir(parents=True)
            (home_dir / ".runtime").mkdir(parents=True)

            # Copy the addon and its config into the test sandbox so the
            # addon's relative-path lookup for *.config.json finds the
            # test-supplied config file.
            sandbox_addon = tmp_path / "30-live-team-announcer.py"
            sandbox_addon.write_text(ADDON_PATH.read_text(encoding="utf-8"), encoding="utf-8")
            cfg_path = sandbox_addon.with_suffix(".config.json")
            cfg_path.write_text(json.dumps(config), encoding="utf-8")

            env = os.environ.copy()
            env.update({
                "HOME": str(home_dir),
                "JKA_LIVE_TEAM_ANNOUNCER_HOME": str(home_dir),
            })
            env.update(env_overrides)

            proc = subprocess.run(
                [sys.executable, str(sandbox_addon)],
                input=stdin_payload,
                env=env,
                text=True,
                capture_output=True,
                timeout=timeout,
            )
            return proc.returncode, proc.stdout, proc.stderr, sandbox_addon

    def test_disabled_addon_exits_immediately(self) -> None:
        rc, _stdout, stderr, _ = self._run_addon(
            config={"enabled": False},
            env_overrides={},
            stdin_payload="",
        )
        self.assertEqual(rc, 0)
        self.assertIn("disabled", stderr)

    def test_team_change_event_triggers_rcon_announcement(self) -> None:
        server = _UDPServer()
        try:
            event = {
                "type": "team_change",
                "source": "stdout",
                "slot": "0",
                "ip": "10.0.0.1",
                "name": "akiondev",
                "old_team": "BLUE",
                "new_team": "RED",
                "raw": "ChangeTeam: 0 ...",
            }
            payload = json.dumps(event) + "\n"
            rc, _stdout, stderr, _ = self._run_addon(
                config={
                    "enabled": True,
                    "rcon_host": "127.0.0.1",
                    "announce_command": "svsay",
                    "min_seconds_between_announcements": 0,
                },
                env_overrides={
                    "TAYSTJK_EFFECTIVE_SERVER_PORT": str(server.port),
                    "TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD": "secret",
                },
                stdin_payload=payload,
            )
            # Give the listener thread a moment to capture the datagram
            # if the addon has already sent it.
            for _ in range(20):
                if server.received is not None:
                    break
                time.sleep(0.05)
            self.assertEqual(rc, 0, msg=stderr)
            self.assertIsNotNone(server.received, msg=f"no UDP packet received; stderr={stderr}")
            decoded = server.received.lstrip(b"\xff").decode("utf-8", errors="replace")
            self.assertIn("rcon secret svsay", decoded)
            self.assertIn("akiondev", decoded)
            self.assertIn("RED TEAM", decoded)
        finally:
            server.close()

    def test_addon_ignores_non_team_change_events(self) -> None:
        server = _UDPServer()
        try:
            payload = json.dumps({"type": "chat_message", "name": "x", "message": "hi"}) + "\n"
            rc, _stdout, stderr, _ = self._run_addon(
                config={
                    "enabled": True,
                    "min_seconds_between_announcements": 0,
                },
                env_overrides={
                    "TAYSTJK_EFFECTIVE_SERVER_PORT": str(server.port),
                    "TAYSTJK_EFFECTIVE_SERVER_RCON_PASSWORD": "secret",
                },
                stdin_payload=payload,
            )
            self.assertEqual(rc, 0, msg=stderr)
            self.assertIsNone(server.received, msg="non-team_change event must not produce RCON traffic")
        finally:
            server.close()

    def test_addon_source_does_not_tail_files(self) -> None:
        text = ADDON_PATH.read_text(encoding="utf-8")
        # Strip the module docstring so descriptive mentions in the
        # banner do not count as active code.
        if text.startswith('"""'):
            close = text.find('"""', 3)
            if close != -1:
                text = text[close + 3 :]
        for forbidden in ("server-output.log", "TAYSTJK_LIVE_OUTPUT_PATH", "fallback_to_server_log"):
            self.assertNotIn(forbidden, text, msg=f"addon must not reference {forbidden}")
        # `tail -F` / `tail -f` patterns must be absent.
        self.assertNotIn("tail -F", text)
        self.assertNotIn("tail -f", text)


if __name__ == "__main__":
    unittest.main(verbosity=2)
