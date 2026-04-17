# Addon Support

## Purpose

This project includes a lightweight runtime addon loader for self-hosted Pterodactyl users.

It is intentionally simple:

- no plugin manifests
- no dependency graph
- no engine hook system
- no security sandbox

The loader simply executes user-owned scripts from the current server container before normal managed startup.

## Addon directory

Default addon directory:

`/home/container/addons`

If the directory does not exist, startup continues normally.

Addon files are never synced back to the Git repository. They only affect the current server container and volume.

## Supported file types

Supported addon script formats:

- `*.sh` — executed with `bash`
- `*.py` — executed with `python3`

Ignored entries:

- directories
- hidden files such as `.example.sh`
- unsupported file types

## Execution order

Addons are read in alphabetical order by filename.

Example order:

```text
00-prepare.sh
10-download-extra-files.py
20-patch-config.sh
```

Each addon is executed one by one before the normal dedicated server startup path continues.

If the container is started with a custom command instead of the normal managed TaystJK startup path, addon loading is bypassed intentionally.

## Modes

Addon support is controlled by these variables:

- `ADDONS_ENABLED`
- `ADDONS_DIR`
- `ADDONS_STRICT`
- `ADDONS_TIMEOUT_SECONDS`
- `ADDONS_LOG_OUTPUT`

Behavior:

- `ADDONS_ENABLED=true`: scan and run addons from `ADDONS_DIR`
- `ADDONS_ENABLED=false`: skip addon loading entirely
- `ADDONS_STRICT=false`: log addon failures and continue startup
- `ADDONS_STRICT=true`: addon failures or timeouts stop startup
- `ADDONS_LOG_OUTPUT=true`: addon stdout and stderr are shown in the console
- `ADDONS_LOG_OUTPUT=false`: only loader status messages are shown

Default behavior is best-effort:

- addon loading is enabled
- strict mode is disabled
- failures are logged but do not stop startup

## Timeout behavior

Each addon has its own timeout controlled by:

`ADDONS_TIMEOUT_SECONDS`

If an addon exceeds that limit:

- it is logged as timed out
- startup continues in best-effort mode
- startup stops in strict mode

## Runtime tooling baseline

The runtime image includes a broader addon authoring baseline so users can build their own Bash and Python workflows directly in the server container.

Available tools include:

- `bash`
- `python3`
- `python3-pip`
- `python3-venv`
- `sqlite3`
- `curl`
- `wget`
- `unzip`
- `tar`
- `jq`
- `ca-certificates`
- `git`
- `rsync`
- `procps`

These packages are there to give addon authors flexibility. They are not a framework feature by themselves.

## Example use cases

- Bash script to patch `server.cfg` before startup
- Bash script to download extra files into the active mod directory
- Python script to send a webhook before launch
- Python script to validate that required files exist
- Bash or Python maintenance logic for logs and temporary files
- SQLite-backed helper script that tracks local addon state
- Bash script using `jq` to parse JSON from an external API

## Responsibility model

Addons are intentionally powerful inside the current server container.

They may:

- read and write files
- modify configs
- download files
- call external APIs
- manage local state
- patch the runtime contents of the server volume

Addon behavior is the server owner’s responsibility.

This project does not treat user addons as trusted project code.
