#!/usr/bin/env python3
"""
Smoke test for the event-driven chatlogger addon.

Spawns ``bundled-addons/defaults/chatlogger.py`` with a
sandboxed ``HOME`` (so it writes into a tmp dir instead of
``/home/container/chatlogs``), pipes a small batch of NDJSON events
into stdin and asserts the resulting daily chat log contains the
expected entries with no tail/server.log involvement.

The test deliberately covers:
  * a ``chat_message`` event from the supervisor's narrow parser;
  * a ``raw_line`` event with a mod-specific verb (JAPro ``amsay``);
  * a ``raw_line`` event that is NOT chat (must be ignored);
  * a ``chat_message`` from a blocklisted sender (``server`` —
    anti-VPN broadcasts) which must be ignored.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
ADDON_PATH = REPO_ROOT / "bundled-addons" / "defaults" / "chatlogger.py"


def _ndjson(*events: dict) -> str:
    return "".join(json.dumps(ev) + "\n" for ev in events)


class EventChatloggerTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory(prefix="chatlogger-smoke-")
        self.addCleanup(self.tmp.cleanup)
        # The addon writes to /home/container/chatlogs; remap that path
        # by replacing the constant via PYTHONPATH-injected shim. Since
        # the addon hardcodes the path, the simplest way to redirect is
        # to symlink /home/container -> our tmp dir is unsafe, so we
        # instead patch the module by writing a wrapper script that
        # monkey-patches the module's constants before main().
        wrapper = Path(self.tmp.name) / "run_wrapper.py"
        wrapper.write_text(
            "import importlib.util, pathlib, sys\n"
            f"spec = importlib.util.spec_from_file_location('chatlogger', '{ADDON_PATH}')\n"
            "mod = importlib.util.module_from_spec(spec)\n"
            "spec.loader.exec_module(mod)\n"
            f"mod.CHATLOGS_DIR = pathlib.Path('{self.tmp.name}') / 'chatlogs'\n"
            "sys.exit(mod.main(sys.argv[1:]))\n",
            encoding="utf-8",
        )
        self.wrapper = wrapper

    def _run(self, payload: str) -> subprocess.CompletedProcess:
        result = subprocess.run(
            [sys.executable, str(self.wrapper)],
            input=payload,
            text=True,
            capture_output=True,
            timeout=15,
            check=False,
        )
        return result

    def test_writes_chat_message_event(self) -> None:
        now = datetime(2026, 4, 25, 10, 0, 0, tzinfo=timezone.utc).isoformat()
        payload = _ndjson(
            {
                "type": "chat_message",
                "time": now,
                "source": "stdout",
                "slot": "0",
                "name": "akiondev",
                "message": "hello world",
                "raw": "say: akiondev: hello world",
            },
            {
                "type": "raw_line",
                "time": now,
                "source": "stdout",
                "raw": "amsay: admin: server going down in 5",
            },
            {
                "type": "raw_line",
                "time": now,
                "source": "stdout",
                "raw": "ClientConnect: 0 [1.2.3.4] \"akiondev\"",
            },
            {
                "type": "chat_message",
                "time": now,
                "source": "stdout",
                "name": "server",
                "message": "[Anti-VPN] VPN BLOCKED: nope",
                "raw": "say: server: [Anti-VPN] VPN BLOCKED: nope",
            },
        )

        result = self._run(payload)
        self.assertEqual(
            result.returncode,
            0,
            msg=f"chatlogger exited {result.returncode}; stderr={result.stderr}",
        )
        chatlogs_dir = Path(self.tmp.name) / "chatlogs"
        files = sorted(chatlogs_dir.glob("chat-*.log"))
        self.assertEqual(len(files), 1, f"expected one daily log, got {files}")
        contents = files[0].read_text(encoding="utf-8")
        self.assertIn("[PUBLIC] akiondev: hello world", contents)
        self.assertIn("[ADMIN] admin: server going down in 5", contents)
        # ClientConnect must not appear in chat output.
        self.assertNotIn("ClientConnect", contents)
        # Anti-VPN broadcasts (sender=server) must be filtered out.
        self.assertNotIn("[Anti-VPN]", contents)

    def test_startup_announces_event_bus_mode(self) -> None:
        result = self._run("")
        self.assertEqual(result.returncode, 0)
        self.assertIn("event-driven chatlogger", result.stdout)
        self.assertIn("NDJSON events from stdin", result.stdout)


if __name__ == "__main__":
    unittest.main()
