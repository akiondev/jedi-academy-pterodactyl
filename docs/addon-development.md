# Addon Development Guidelines

## Environment model

Addons run inside the same server container as TaystJK.

Important assumptions:

- working directory is `/home/container`
- addons are executed as the `container` user
- Bash addons run as `bash /home/container/addons/<file>.sh`
- Python addons run as `python3 /home/container/addons/<file>.py`
- addons are not sourced into the entrypoint shell

Because addons run as child processes, exporting environment variables inside an addon does not change the parent entrypoint process. If you need to affect startup, do it through files or other runtime state that the server reads later.

## Placement

Put your addon files in:

`/home/container/addons`

Recommended naming style:

```text
00-prepare.sh
10-validate.py
20-patch-config.sh
30-send-webhook.py
```

This keeps execution order explicit and predictable.

## Bash addon guidance

Recommended Bash practices:

- start with `#!/usr/bin/env bash` when you want local editor friendliness
- prefer `set -euo pipefail`
- keep scripts idempotent when possible
- log clearly with `echo` or `printf`
- fail intentionally with a non-zero exit code when something is truly wrong

Example Bash addon:

```bash
#!/usr/bin/env bash
set -euo pipefail

CONFIG="/home/container/taystjk/server.cfg"

if [[ -f "$CONFIG" ]]; then
  grep -q 'seta sv_hostname' "$CONFIG" || echo 'seta sv_hostname "My Server"' >> "$CONFIG"
fi
```

## Python addon guidance

Recommended Python practices:

- use Python 3 only
- keep dependencies minimal unless you intentionally manage a venv or pip install flow
- print explicit status messages for visibility in the console
- exit non-zero when you want strict mode to stop startup

Example Python addon:

```python
#!/usr/bin/env python3
from pathlib import Path
import sys

required = Path("/home/container/base/assets0.pk3")

if not required.exists():
    print("Required file missing:", required)
    sys.exit(1)

print("Addon validation passed")
```

## Recommended patterns

Good addon patterns:

- patching config files
- validating required files
- downloading optional content
- sending webhooks
- syncing extra runtime data with `rsync`
- caching addon state in SQLite
- calling JSON APIs with `curl` + `jq`

## Strict vs best-effort behavior

With `ADDONS_STRICT=false`:

- addon failures are logged
- startup continues

With `ADDONS_STRICT=true`:

- addon failures stop startup
- addon timeouts stop startup

Choose strict mode only for addons that are truly required for a safe or correct server launch.

## Timeout-aware design

Each addon has a fixed timeout.

To avoid unnecessary timeouts:

- keep startup-time network calls short
- retry cautiously inside the addon
- prefer local cached state when possible
- avoid long blocking maintenance tasks during launch

If you need heavier maintenance logic, consider making it incremental or moving some work outside the critical startup path.

## Logging guidance

If `ADDONS_LOG_OUTPUT=true`, addon stdout and stderr are shown directly in the Pterodactyl console.

Good logging style:

- short status messages
- clear failure reasons
- no secrets in output

Avoid printing API keys, tokens, or sensitive config values.

## Safety notes

This addon loader is not a sandbox.

Addons can:

- change files
- download content
- call external services
- consume CPU and memory

Only run addons you personally trust for your own container.
