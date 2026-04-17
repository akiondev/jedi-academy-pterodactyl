# Addon README

This guide is synced into:

```text
/home/container/addons/docs/ADDON_README.md
```

## 1. What you need to know

- Addons are simple startup scripts.
- Only top-level `.sh` and `.py` files in `/home/container/addons` are executed.
- Rename a top-level addon file to end with `.disable` if you want to keep it without running it.
- Files inside `docs/`, `examples/`, and `defaults/` are **not** executed.
- The built-in `checkserverstatus` helper is controlled by `ADDON_CHECKSERVERSTATUS_ENABLED`.
- The built-in `chatlogger` helper is controlled by `ADDON_CHATLOGGER_ENABLED` and writes daily player chat logs into `/home/container/chatlogs`.
- Scripts run in alphabetical order before normal managed startup.
- If `ADDONS_STRICT=false`, failed addons are logged and startup continues.
- If `ADDONS_STRICT=true`, a failed addon stops startup.
- Useful runtime state is available in:
  - `/home/container/.runtime/taystjk-effective.env`
  - `/home/container/.runtime/taystjk-effective.json`

## 2. How to use addons

1. Put your script directly in:

```text
/home/container/addons
```

2. Use one of these file types:

- `something.sh`
- `something.py`

3. Restart the server with the normal managed startup path.

4. Watch the console for addon logs.

To disable a top-level addon without deleting it, rename it like this:

```text
20-webhook.py.disable
```

Recommended naming:

```text
10-setup.sh
20-webhook.py
90-finish.sh
```

If you want to use the bundled announcer example, copy these files from `examples/` into the top-level addon folder:

```text
/home/container/addons/examples/20-python-announcer.py
/home/container/addons/examples/20-python-announcer.config.json
/home/container/addons/examples/20-python-announcer.messages.txt
```

If you later want to keep that copied announcer without running it, rename the copied script to:

```text
/home/container/addons/20-python-announcer.py.disable
```

## 3. Short examples

### Bash example

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "[addon:bash] Checking config"

TARGET="${TAYSTJK_ACTIVE_SERVER_CONFIG_PATH:-}"
if [[ -z "${TARGET}" || ! -f "${TARGET}" ]]; then
  echo "[addon:bash] Missing config"
  exit 1
fi

echo "[addon:bash] Config found: ${TARGET}"
```

### Python example

```python
#!/usr/bin/env python3
import os
import sys

print("[addon:python] Starting")

config_path = os.getenv("TAYSTJK_ACTIVE_SERVER_CONFIG_PATH", "")
if not config_path or not os.path.isfile(config_path):
    print("[addon:python] Missing config")
    sys.exit(1)

print(f"[addon:python] Config found: {config_path}")
```
