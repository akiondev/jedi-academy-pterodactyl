# `scripts/common/` — shared runtime layer (PR-A skeleton)

This directory holds shared library files that are intended to be
sourced by per-runtime entrypoints in a later PR. They are introduced
here as an isolated, parallel skeleton and are **not used by the
runtime yet**.

PR-A only adds the files. Specifically:

- `scripts/entrypoint.sh` does **not** source any file from this
  directory.
- `docker/Dockerfile` does **not** copy this directory into the image.
- No function names, hardcoded strings (e.g. `TaystJK`, `taystjk`),
  paths, env vars, or defaults are renamed in PR-A. They remain
  byte-identical to the originals in `scripts/entrypoint.sh`.

The functions in these files are textually copied from
`scripts/entrypoint.sh`. They will be deduplicated (i.e. removed from
`scripts/entrypoint.sh` and sourced from here) in PR-B once the
runtime shim and `runtime.json` model land.

## Files

| File                              | Contents (copied from `scripts/entrypoint.sh`)                              |
|-----------------------------------|------------------------------------------------------------------------------|
| `jka_runtime_common.sh`           | Color globals, `setup_colors`, header/section/kv/info/ok/warn/log/fail, list/debug/ready/bool/join/print_command_preview, `count_dir_files`. |
| `jka_security.sh`                 | `require_safe_component`, `require_safe_container_path`, `require_no_newlines`, `normalize_optional_boolean`. |
| `jka_server_cfg.sh`               | `escape_server_cfg_value`, `read_server_cvar`, `upsert_server_cvar`, `write_runtime_state`, `configure_server_settings`, `configure_server_logging`, `active_server_log_path`, `resolve_effective_server_settings`, `ensure_managed_taystjk_server_config`. |
| `jka_addon_loader.sh`             | `sync_addon_docs`, `sync_image_managed_addon_tree`, `sync_managed_addon_examples`, `sync_managed_addon_defaults`, `install_managed_status_helper`, `install_managed_chatlogger_helper`, `configure_addons`, `print_addon_summary`, `run_addons`. |
| `jka_antivpn_bootstrap.sh`        | `configure_anti_vpn`, `anti_vpn_provider_row`, `print_anti_vpn_providers`, `anti_vpn_allowlist_status`, `print_anti_vpn_summary`. |
| `jka_install_layout.sh`           | Placeholder for PR-B (extracted from `scripts/install_taystjk.sh`). |
| `jka_runtime_manifest.sh`         | `load_runtime_manifest` — parses `/opt/jka/runtime.json` (schema_version 2) and exports `JKA_PATH_*` variables. Enforces `engine_dist_dir != engine_payload_root`. |
| `jka_runtime_sync.sh`             | `sync_runtime_files` — copies image-managed engine binaries (matched by `JKA_PATH_ENGINE_BINARY_GLOB` under `JKA_PATH_ENGINE_DIST`) and payload subdirectories (under `JKA_PATH_ENGINE_PAYLOAD_ROOT`) into `/home/container/`. Honors `JKA_CONTAINER_ROOT` for tests. |

## Sourcing order (when wired up in PR-B)

`jka_runtime_common.sh` declares the `COLOR_*` globals and
`LAST_FAILURE_MESSAGE`, plus the logging primitives (`info`, `ok`,
`warn`, `fail`, …). All other files in this directory assume that
`jka_runtime_common.sh` has been sourced first.
