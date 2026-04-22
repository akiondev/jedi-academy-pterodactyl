# `scripts/common/` — shared runtime layer

This directory holds shared library files that are sourced by
`scripts/entrypoint.sh` at startup.

## Files

| File                              | Contents                                                                    |
|-----------------------------------|-----------------------------------------------------------------------------|
| `jka_runtime_common.sh`           | Color globals, `setup_colors`, header/section/kv/info/ok/warn/log/fail, list/debug/ready/bool/join/print_command_preview, `count_dir_files`. |
| `jka_security.sh`                 | `require_safe_component`, `require_safe_container_path`, `require_no_newlines`, `normalize_optional_boolean`. |
| `jka_server_cfg.sh`               | `escape_server_cfg_value`, `read_server_cvar`, `upsert_server_cvar`, `write_runtime_state`, `configure_server_settings`, `configure_server_logging`, `active_server_log_path`, `resolve_effective_server_settings`, `ensure_managed_taystjk_server_config`. |
| `jka_addon_loader.sh`             | `sync_addon_docs`, `sync_image_managed_addon_tree`, `sync_managed_addon_examples`, `sync_managed_addon_defaults`, `install_managed_status_helper`, `install_managed_chatlogger_helper`, `configure_addons`, `print_addon_summary`, `run_addons`. |
| `jka_antivpn_bootstrap.sh`        | `configure_anti_vpn`, `anti_vpn_provider_row`, `print_anti_vpn_providers`, `anti_vpn_allowlist_status`, `print_anti_vpn_summary`. |
| `jka_install_layout.sh`           | Placeholder for future extraction from `scripts/install_taystjk.sh`. |
| `jka_runtime_manifest.sh`         | `load_runtime_manifest` — parses `/opt/jka/runtime.json` (schema_version 2) and exports `JKA_PATH_*` variables. Enforces `engine_dist_dir != engine_payload_root`. |
| `jka_runtime_sync.sh`             | `sync_runtime_files` — copies image-managed engine binaries (matched by `JKA_PATH_ENGINE_BINARY_GLOB` under `JKA_PATH_ENGINE_DIST`) and payload subdirectories (under `JKA_PATH_ENGINE_PAYLOAD_ROOT`) into `/home/container/`. Honors `JKA_CONTAINER_ROOT` for tests. |

## Sourcing order

`scripts/entrypoint.sh` sources files in the following order:

1. `jka_runtime_common.sh` — declares `COLOR_*` globals, `LAST_FAILURE_MESSAGE`, and all logging primitives (`info`, `ok`, `warn`, `fail`, …). All other files assume this has been sourced first.
2. `jka_runtime_manifest.sh`
3. `jka_runtime_sync.sh`
4. `jka_security.sh`
5. `jka_server_cfg.sh`
6. `jka_addon_loader.sh`
7. `jka_antivpn_bootstrap.sh`
