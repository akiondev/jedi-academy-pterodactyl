# YBEProxy legacy32 image

`ybeproxy-legacy32` is a separate runtime family published alongside
the default TaystJK image and the `openjk-modern64`,
`openjk-legacy32` and `vanilla-legacy32` images. It pairs a 32-bit
(i386) Linux runtime environment for an operator-supplied dedicated
server engine with the **YBEProxy** game-module proxy
(`base/jampgamei386.so`) built from the
[Yberion/JKA_YBEProxy](https://github.com/Yberion/JKA_YBEProxy)
`main` branch.

YBEProxy is a server-side game-module proxy. It is **not** a dedicated
server engine and does **not** replace the engine the same way OpenJK
or TaystJK do. It hooks into an existing engine's game-module loading
path by replacing the loaded `jampgamei386.so`.

This image does **not** replace the TaystJK image, the
`openjk-modern64` image, the `openjk-legacy32` image or the
`vanilla-legacy32` image. It is an additive runtime family for
operators who want to run a 32-bit dedicated server with the YBEProxy
game-module proxy installed.

## Scope

- **Image-managed payload only.** The image's only baked-in mode
  artifact is the YBEProxy `base/jampgamei386.so`, built from upstream
  source. The image does not build or ship any server engine.
- **Manual engine.** Operators must upload their own legally obtained
  32-bit (i386) Linux Jedi Academy MP dedicated server binary (such as
  `linuxjampded`, `openjkded.i386` or `taystjkded.i386`) into the
  server volume. The default `SERVER_BINARY` value in the egg is
  `./linuxjampded`.
- **Architecture:** 32-bit (i386). The image enables the i386
  architecture in Debian and installs the i386 dynamic loader plus the
  i386 versions of every shared library the typical 32-bit engine and
  the YBEProxy module link against.
- **Default mode:** `fs_game=base`. Other mod directories are
  user-managed: upload them yourself before startup. Mod directories
  other than `base` bypass the image-managed YBEProxy payload.

The on-disk layout inside the image is:

- `/opt/jka/engine/` — empty; no image-managed engine.
- `/opt/jka/engine-payload/base/jampgamei386.so` — YBEProxy game-module
  proxy built from `Yberion/JKA_YBEProxy` master.

## Base assets and engine binary are operator-supplied

The image ships **no Jedi Academy assets** and **no server engine
binary**. Operators must mount or upload:

- A legally obtained 32-bit Linux dedicated server binary at
  `/home/container/<SERVER_BINARY>` (default `./linuxjampded`).
- Their own legally obtained `base/` PK3s
  (`assets0.pk3`, `assets1.pk3`, `assets2.pk3`, `assets3.pk3`,
  `mp_chars.pk3`, …).

## Other mods stay user-managed

Only the image-managed `base/jampgamei386.so` (YBEProxy) payload is
synced into the live game tree. Any other mod (custom maps, gameplay
mods, alternative `jampgame*.so` builds, etc.) remains
**user-managed**.

## What is the same as the other runtime images

- The **anti-VPN supervisor** (`/usr/local/bin/jka-antivpn`) is built
  and installed identically.
- The **addon loader** under `/opt/jka/bundled-addons/` and the
  operator-provided `addons.d/` drop-in directory work the same way and
  follow the same execution conventions.
- The `/opt/jka/` runtime layout and `runtime.json` manifest model
  (`schema_version: 2`) are shared. The YBEProxy legacy32 image
  declares its own `image/ybeproxy-legacy32/runtime.json`.

## Tags

See [`image-strategy.md`](image-strategy.md) for the full tag policy.
The YBEProxy legacy32 family publishes:

- `ybeproxy-legacy32` — canonical mutable tag for this runtime.

No extra `latest-*`, SHA/immutable, or release image tags are published.
