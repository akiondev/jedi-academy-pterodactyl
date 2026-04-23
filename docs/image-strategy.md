# Image and tag strategy

## Platform identity

- Repository: `akiondev/jedi-academy-pterodactyl`
- GHCR package: `ghcr.io/akiondev/jedi-academy-pterodactyl`
- Docker Hub mirror: `docker.io/akiondev/jedi-academy-pterodactyl`

Both registries publish the same runtime families under the same tag names.

## Final public runtime tags

The project now uses exactly one mutable public tag per runtime:

1. `taystjk-modern64`
2. `taystjk-legacy32`
3. `openjk-modern64`
4. `openjk-legacy32`
5. `ybeproxy-legacy32`
6. `vanilla-legacy32`

`vanilla-legacy32` is always listed last where ordering matters.

## Removed tag families

The project does **not** publish:

- global `latest`
- `latest-<runtime>`
- immutable commit tags (`<runtime>-<branch>-<sha>`, `master-<sha>`, `main-<sha>`, etc.)
- release image tags (`v<semver>`)
- legacy alias tags (for example bare `taystjk`)

## Auto-update policy for engine binaries

Upstream-check + auto-rebuild of image-managed engine binaries is enabled for:

- `taystjk-modern64`
- `taystjk-legacy32`

It is not enabled for:

- `openjk-modern64`
- `openjk-legacy32`
- `ybeproxy-legacy32`
- `vanilla-legacy32`

## Runtime ownership model

- `taystjk-modern64`: image-managed TaystJK engine + image-managed `taystjk/` payload
- `taystjk-legacy32`: image-managed TaystJK i386 engine + image-managed `taystjk/` payload
- `openjk-modern64`: image-managed OpenJK x86_64 engine + image-managed `base/jampgamex86_64.so`
- `openjk-legacy32`: image-managed OpenJK i386 engine + image-managed `base/jampgamei386.so`
- `ybeproxy-legacy32`: **manual operator-supplied engine**, image-managed `base/jampgamei386.so` (YBEProxy payload)
- `vanilla-legacy32`: **manual operator-supplied engine**, no image-managed engine payload

## Shared runtime baseline (all six tags)

All runtime images ship:

- `/opt/jka` runtime layout
- `runtime.json` manifest
- shared runtime/common layer
- bundled addon docs/examples/defaults
- `jka-antivpn`

