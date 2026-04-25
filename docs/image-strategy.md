# Image and tag strategy

## Platform identity

- Repository: `akiondev/jedi-academy-pterodactyl`
- GHCR package: `ghcr.io/akiondev/jedi-academy-pterodactyl`
- Docker Hub mirror: `docker.io/akiondev/jedi-academy-pterodactyl`

Both registries publish the same runtime image under the same tag names.

## Final public runtime tags

The project publishes a single runtime family — `taystjk-modern64` — under the following tags:

1. `latest` — only on the default branch (`main`)
2. `taystjk`
3. `taystjk-modern64`
4. `taystjk-modern64-master-<short_sha>` — immutable, derived from upstream TaystJK master HEAD

## Removed runtime families

The previous matrix containing `taystjk-legacy32`, `openjk-modern64`, `openjk-legacy32`, `ybeproxy-legacy32`, and `vanilla-legacy32` has been removed in full: their Dockerfiles, image manifests, eggs, runtime install scripts, docs, workflow matrix entries, publishing tags, and tests no longer exist in this repository. The TaystJK modern64 image is the only supported runtime.

## Auto-update policy for the engine binary

Upstream-check + auto-rebuild of the image-managed `taystjkded.x86_64` binary is enabled for `taystjk-modern64`. The scheduled job in `.github/workflows/upstream-taystjk-master-sync.yml` republishes the canonical mutable tags above whenever upstream master changes.

## Runtime ownership model

- Engine binary: image-managed TaystJK build under `/opt/jka/engine/taystjkded.x86_64`. It is synced into `/home/container/taystjkded.x86_64` only when the operator opts in by setting `TAYSTJK_AUTO_UPDATE_BINARY=true` in the panel; otherwise the binary at `/home/container/<SERVER_BINARY>` is treated as user-owned and left untouched.
- TaystJK mod payload: image-managed under `/opt/jka/engine-payload/taystjk`. It is mirrored into `/home/container/taystjk` only when `server.sync_managed_taystjk_payload` is `true` (default) in `/home/container/config/jka-runtime.json`.
- Everything else (`server.cfg`, mod folders, addons, anti-VPN keys, RCON password, etc.) is operator-owned.

## Shared runtime baseline

The image ships:

- `/opt/jka` runtime layout
- `runtime.json` manifest
- shared `runtime/common` shell layer (including the JSON config loader)
- bundled addon docs/examples/defaults
- `jka-antivpn` Go supervisor
