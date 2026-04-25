#!/usr/bin/env python3
"""
Regression test: JKA_ADDON_CONFIG_JSON env var parsing in announcer.py.

Verifies that the env var format produced by the shell loader
(``jq -c '.addons.announcer'``) is parsed by ``load_config()`` in
``bundled-addons/defaults/announcer.py`` without falling back to
built-in defaults.

This catches the bash brace-counting trap where::

    JKA_ADDON_CONFIG_JSON="${VAR:-{}}"

appended a stray ``}`` because bash terminates ``${...}`` at the first
``}`` inside the default expression, leaving the second ``}`` as a
literal character -- producing ``<json-value>}`` instead of
``<json-value>``.

Run from the repository root::

    python3 scripts/test/announcer_config_env_test.py
"""

from __future__ import annotations

import importlib.util
import json
import os
import sys
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
ANNOUNCER_PATH = REPO_ROOT / "bundled-addons" / "defaults" / "announcer.py"

# Compact JSON exactly as ``jq -c '.addons.announcer'`` would produce from
# the default jka-addons.json template. This is the "shell loader format".
_SHELL_LOADER_SAMPLE: dict = {
    "enabled": True,
    "order": 20,
    "type": "scheduled",
    "script": "announcer.py",
    "announce_command": "svsay",
    "interval_seconds": 300,
    "messages_file": "announcer.messages.txt",
}
SHELL_LOADER_SAMPLE_JSON = json.dumps(_SHELL_LOADER_SAMPLE, separators=(",", ":"))


def _import_announcer():
    """Import announcer.py as a module without executing its __main__ guard."""
    spec = importlib.util.spec_from_file_location("announcer", ANNOUNCER_PATH)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class AnnouncerConfigEnvTest(unittest.TestCase):
    """Regression: JKA_ADDON_CONFIG_JSON must parse without falling back to defaults."""

    _ENV_KEYS = ("JKA_ADDON_CONFIG_JSON", "JKA_ADDONS_CONFIG_PATH", "JKA_ADDON_NAME")

    def setUp(self) -> None:
        # Save and restore env vars around each test.
        self._saved = {k: os.environ.get(k) for k in self._ENV_KEYS}
        # Point away from any real config file so the file-fallback path
        # is never reached during these tests.
        os.environ["JKA_ADDONS_CONFIG_PATH"] = "/nonexistent/jka-addons.json"

    def tearDown(self) -> None:
        for k, v in self._saved.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v

    def _load_config(self, config_json: str) -> tuple[dict, list[str]]:
        """Set JKA_ADDON_CONFIG_JSON and call load_config(); return (config, log_lines)."""
        os.environ["JKA_ADDON_CONFIG_JSON"] = config_json
        announcer = _import_announcer()
        logged: list[str] = []
        announcer.log = lambda msg: logged.append(msg)
        config = announcer.load_config()
        return config, logged

    # ------------------------------------------------------------------
    # Happy-path: correct shell-loader format parses cleanly
    # ------------------------------------------------------------------

    def test_single_object_json_parses_without_fallback(self) -> None:
        """Compact single-object JSON from jq -c parses without falling back."""
        config, logged = self._load_config(SHELL_LOADER_SAMPLE_JSON)

        fallbacks = [m for m in logged if "using defaults" in m.lower()]
        self.assertEqual(
            fallbacks,
            [],
            f"load_config() fell back to defaults. Logged: {logged}",
        )
        self.assertEqual(config["interval_seconds"], 300)
        self.assertEqual(config["announce_command"], "svsay")

    def test_config_values_come_from_env_not_built_in_defaults(self) -> None:
        """Values in the env var override the built-in DEFAULT_CONFIG."""
        custom = dict(_SHELL_LOADER_SAMPLE)
        custom["interval_seconds"] = 600
        custom["announce_command"] = "say"
        _, logged = self._load_config(json.dumps(custom, separators=(",", ":")))

        fallbacks = [m for m in logged if "using defaults" in m.lower()]
        self.assertEqual(fallbacks, [], f"Unexpected fallback logged: {logged}")

    # ------------------------------------------------------------------
    # Regression: the old buggy ${VAR:-{}} form appended a stray }
    # ------------------------------------------------------------------

    def test_extra_closing_brace_triggers_fallback(self) -> None:
        """A stray trailing } (old bash bug) causes JSON parse failure -> defaults."""
        buggy_json = SHELL_LOADER_SAMPLE_JSON + "}"
        _, logged = self._load_config(buggy_json)

        fallbacks = [m for m in logged if "using defaults" in m.lower()]
        self.assertTrue(
            len(fallbacks) > 0,
            "Expected load_config() to fall back to defaults for input with extra }",
        )

    def test_multiple_concatenated_objects_trigger_fallback(self) -> None:
        """Two back-to-back JSON objects trigger the 'Extra data' parse error."""
        bad_json = SHELL_LOADER_SAMPLE_JSON + SHELL_LOADER_SAMPLE_JSON
        _, logged = self._load_config(bad_json)

        fallbacks = [m for m in logged if "using defaults" in m.lower()]
        self.assertTrue(
            len(fallbacks) > 0,
            "Expected load_config() to fall back to defaults for concatenated objects",
        )

    # ------------------------------------------------------------------
    # Structural sanity: the env var must be a JSON object, not an array
    # ------------------------------------------------------------------

    def test_json_array_triggers_fallback(self) -> None:
        """A JSON array (not object) causes load_config() to fall back."""
        _, logged = self._load_config("[1,2,3]")

        fallbacks = [m for m in logged if "using defaults" in m.lower()]
        self.assertTrue(
            len(fallbacks) > 0,
            "Expected load_config() to fall back when given a JSON array",
        )

    def test_plain_string_triggers_fallback(self) -> None:
        """A plain string is not valid JSON and causes load_config() to fall back."""
        _, logged = self._load_config("not-json")

        fallbacks = [m for m in logged if "using defaults" in m.lower()]
        self.assertTrue(
            len(fallbacks) > 0,
            "Expected load_config() to fall back when given a plain string",
        )


if __name__ == "__main__":
    unittest.main()
