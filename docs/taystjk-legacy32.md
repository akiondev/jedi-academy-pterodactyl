# TaystJK legacy32 image (experimental)

`taystjk-legacy32` is an **experimental** runtime family published
alongside the default TaystJK image and the `openjk-modern64`,
`openjk-legacy32`, `vanilla-legacy32` and `ybeproxy-legacy32` images.
It is built from the same TaystJK source tree as the default TaystJK
image (`taysta/TaystJK` master), but targets a 32-bit (i386) Linux MP
dedicated server (`taystjkded.i386` plus the `taystjk/` payload).

It does **not** replace the default TaystJK image, the
`openjk-modern64` image or the `openjk-legacy32` image. It exists for
compatibility experiments with legacy 32-bit mods and binaries.

## Status: experimental

The 32-bit TaystJK dedicated server target is not part of the upstream
TaystJK CI matrix for the dedicated server, and broad mod-binary
compatibility with the resulting `taystjkded.i386` has not been
verified end-to-end in this repository's automation.

For production deployments, prefer:

- The default TaystJK image (`latest` / `taystjk` tag) for modern
  64-bit TaystJK servers.
- The `openjk-legacy32` image for a stable 32-bit OpenJK dedicated
  server.

### Publish gating

To avoid accidentally pushing experimental images to mutable tags, the
`taystjk-legacy32` runtime is **opt-in for publishing**:

- The image is **always** built (no push) on pull requests and pushes
  that touch its source files, so regressions in the build are caught
  immediately.
- The image is **only published** to GHCR / Docker Hub when one of the
  following is true:
  - The CI workflow is started via `workflow_dispatch`.
  - The repository variable `PUBLISH_TAYSTJK_LEGACY32` is set to
    `true`.

Until compatibility verification lands, this runtime should be
considered internal and is not part of the standard release surface.

## Scope

- **Runtime kind:** experimental, source-built MP dedicated server.
- **MP dedicated server only.** All client/renderer/UI/SP/JK2 targets
  are disabled in the source build.
- **Architecture:** 32-bit (i386). The image enables the i386
  architecture in Debian and installs the i386 dynamic loader plus
  the i386 versions of every shared library `taystjkded.i386` and the
  TaystJK payload typically link against.

The on-disk layout inside the image is:

- `/opt/jka/engine/taystjkded.i386` — the 32-bit TaystJK dedicated
  server binary built from source.
- `/opt/jka/engine-payload/taystjk/` — the image-managed mode-defining
  payload (mirrors the default TaystJK image's `taystjk/` subdirectory
  layout).

## Base assets are operator-supplied

The image ships **no Jedi Academy assets**. Operators must provide
their own legally obtained `base/` PK3s (`assets0.pk3`, `assets1.pk3`,
`assets2.pk3`, `assets3.pk3`, `mp_chars.pk3`, …) in
`/home/container/base`.

## Other mods stay user-managed

Only the image-managed `taystjk/` payload is synced into the live game
tree. Any other mod (custom maps, gameplay mods, alternative
`jampgame*.so` builds, etc.) remains **user-managed**.

## What is the same as the other runtime images

- The **anti-VPN supervisor** (`/usr/local/bin/jka-antivpn`) is built
  and installed identically.
- The **addon loader** under `/opt/jka/bundled-addons/` and the
  operator-provided `addons.d/` drop-in directory work the same way and
  follow the same execution conventions.
- The `/opt/jka/` runtime layout and `runtime.json` manifest model
  (`schema_version: 2`) are shared. The TaystJK legacy32 image
  declares its own `image/taystjk-legacy32/runtime.json`.

## Tags

See [`image-strategy.md`](image-strategy.md) for the full tag policy.
The TaystJK legacy32 family currently publishes (only when the publish
gating above is satisfied):

- `taystjk-legacy32` — latest experimental TaystJK legacy32 master
  build (mutable).
- `taystjk-legacy32-master-<short_sha>` — pinned to an upstream
  TaystJK commit (immutable).

The `latest`, `taystjk*`, `openjk-modern64*`, `openjk-legacy32*`,
`vanilla-legacy32*` and `ybeproxy-legacy32*` tags continue to point at
their respective runtimes and are unaffected by this image.
